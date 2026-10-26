package branding

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

func TestNormalizeBoundedRaster(t *testing.T) {
	im := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	im.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 170})
	for _, format := range []string{"png", "jpeg", "webp"} {
		t.Run(format, func(t *testing.T) {
			var input bytes.Buffer
			switch format {
			case "png":
				if err := png.Encode(&input, im); err != nil {
					t.Fatal(err)
				}
			case "jpeg":
				if err := jpeg.Encode(&input, im, nil); err != nil {
					t.Fatal(err)
				}
			case "webp":
				// A one-pixel lossless raster, with no metadata or animation.
				b, err := base64.StdEncoding.DecodeString("UklGRiAAAABXRUJQVlA4TBQAAAAvAAAAAAdQgVQIIAAKmv7HiIj+Bw==")
				if err != nil {
					t.Fatal(err)
				}
				input.Write(b)
			}
			input.WriteString("<script>discard appended content</script>")
			out, err := Normalize(input.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := png.Decode(bytes.NewReader(out))
			if err != nil {
				t.Fatal(err)
			}
			if decoded.Bounds().Dx() < 1 || bytes.Contains(out, []byte("<script>")) {
				t.Fatal("unsafe output")
			}
		})
	}
	var tooWide bytes.Buffer
	if err := png.Encode(&tooWide, image.NewGray(image.Rect(0, 0, MaxDimension+1, 1))); err != nil {
		t.Fatal(err)
	}
	for _, b := range [][]byte{nil, []byte("<svg></svg>"), []byte("GIF89a"), []byte("\x89PNG\r\n\x1a\n"), tooWide.Bytes(), make([]byte, MaxInputBytes+1)} {
		if _, err := Normalize(b); err == nil {
			t.Fatal("invalid raster accepted")
		}
	}
}

func TestBrandAssetURLs(t *testing.T) {
	for _, v := range []string{"", "https://example.com/logo.png", "http://example.com/a?b=1", "/logo.png", "/a%20b.png?v=1", AssetPrefix + strings.Repeat("a", 64) + ".png"} {
		if !ValidAssetURL(v) {
			t.Fatalf("valid URL rejected: %q", v)
		}
	}
	for _, v := range []string{"javascript:alert(1)", "data:image/png;base64,AA", "//example.com/a", "/%2fexample.com/a", "/%5cexample.com/a", "/logo%00.png", "/%XX.png", "https://user:password@example.com/a", "https://example.com/\na", " https://example.com/a", "https:\\example.com/a"} {
		if ValidAssetURL(v) {
			t.Fatalf("unsafe URL accepted: %q", v)
		}
	}
}

func TestNormalizeOutputBudget(t *testing.T) {
	// JPEG can stay below the input limit while its normalized lossless PNG
	// expands past the output budget. Stop encoding instead of storing it.
	im := image.NewNRGBA(image.Rect(0, 0, 1500, 1500))
	var state uint32 = 17
	for i := 0; i < len(im.Pix); i += 4 {
		for channel := 0; channel < 3; channel++ {
			state ^= state << 13
			state ^= state >> 17
			state ^= state << 5
			im.Pix[i+channel] = byte(state)
		}
		im.Pix[i+3] = 255
	}
	var input bytes.Buffer
	if err := jpeg.Encode(&input, im, &jpeg.Options{Quality: 55}); err != nil {
		t.Fatal(err)
	}
	if input.Len() > MaxInputBytes {
		t.Fatal("fixture exceeds input limit")
	}
	if _, err := Normalize(input.Bytes()); !errors.Is(err, ErrOutputSize) {
		t.Fatalf("output limit: %v", err)
	}
}
