// Package branding handles public brand images without accepting active content.
package branding

import (
	"bytes"
	"errors"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/url"
	"strings"
	"unicode"

	_ "golang.org/x/image/webp"
)

const (
	MaxInputBytes  = 2 << 20
	MaxOutputBytes = 4 << 20
	MaxDimension   = 2048
	AssetPrefix    = "/brand-assets/"
)

var (
	ErrImage      = errors.New("image must be a valid PNG, JPEG or WebP, at most 2048 pixels per side")
	ErrOutputSize = errors.New("normalized image exceeds 4 MiB")
)

// Normalize decodes only bounded raster images and writes a new PNG, removing
// metadata, animation and appended content. SVG/HTML never enter asset storage.
func Normalize(data []byte) ([]byte, error) {
	if len(data) == 0 || len(data) > MaxInputBytes {
		return nil, ErrImage
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg" && format != "webp") ||
		config.Width < 1 || config.Height < 1 || config.Width > MaxDimension || config.Height > MaxDimension {
		return nil, ErrImage
	}
	im, decodedFormat, err := image.Decode(bytes.NewReader(data))
	if err != nil || decodedFormat != format || im.Bounds().Dx() != config.Width || im.Bounds().Dy() != config.Height {
		return nil, ErrImage
	}
	out := boundedBuffer{}
	if err := png.Encode(&out, im); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

type boundedBuffer struct{ bytes.Buffer }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > MaxOutputBytes-b.Len() {
		return 0, ErrOutputSize
	}
	return b.Buffer.Write(p)
}

var _ io.Writer = (*boundedBuffer)(nil)

func ValidDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// ValidAssetURL accepts site-relative images and explicitly supplied HTTP(S)
// images. The server never fetches these external image URLs.
func ValidAssetURL(value string) bool {
	if value == "" {
		return true
	}
	if len(value) > 2048 || strings.TrimSpace(value) != value || strings.Contains(value, "\\") || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return false
	}
	u, err := url.Parse(value)
	if err == nil && strings.HasPrefix(value, "/") {
		return !strings.HasPrefix(value, "//") && !strings.HasPrefix(u.Path, "//") &&
			u.Scheme == "" && u.Host == "" && u.User == nil && u.Opaque == "" &&
			!strings.Contains(u.Path, "\\") && strings.IndexFunc(u.Path, unicode.IsControl) < 0
	}
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != "" && u.User == nil && u.Opaque == ""
}
