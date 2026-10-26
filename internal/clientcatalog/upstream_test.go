package clientcatalog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestOfficialCatalogClassifiesInstallers(t *testing.T) {
	cases := []struct{ id, name, platform, arch string }{
		{"clash-verge-rev", "Clash.Verge_1.2.3_arm64_fixed_webview2-setup.exe", "windows", "arm64"},
		{"clash-verge-rev", "Clash.Verge-1.2.3-1.armhfp.rpm", "linux", "armv7"},
		{"clash-verge-rev", "Clash.Verge_1.2.3_aarch64.dmg", "macos", "arm64"},
		{"v2rayn", "v2rayN-windows-64-desktop.zip", "windows", "x64"},
		{"v2rayn", "v2rayN-windows-86.zip", "windows", "x86"},
		{"v2rayn", "v2rayN-linux-rhel-riscv64.rpm", "linux", "riscv64"},
		{"flclash", "FlClash-1.2.3-android-arm64-v8a.apk", "android", "arm64"},
		{"v2rayng", "v2rayNG_1.2.3-fdroid_universal.apk", "android", "universal"},
		{"v2rayng", "v2rayNG_1.2.3_armeabi-v7a.apk", "android", "armv7"},
	}
	for _, tt := range cases {
		p, _ := Find(tt.id)
		a, b := ClassifyAsset(p, tt.name)
		if a != tt.platform || b != tt.arch {
			t.Fatalf("%s: %s %s", tt.name, a, b)
		}
	}
	p, _ := Find("clash-verge-rev")
	for _, name := range []string{"latest.json", "setup.exe.sig", "Clash.aarch64.app.tar.gz", "source.tar.gz", "unknown.exe", "../../package-x64.exe", "evil\n-x64.exe"} {
		if platform, _ := ClassifyAsset(p, name); platform != "" {
			t.Fatal("unsafe or unknown asset offered", name)
		}
	}
}
func TestOfficialTransportRejectsPrivateAndUnapprovedDestinations(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "10.10.0.2", "100.64.1.1", "169.254.169.254", "198.18.0.1", "192.0.2.10", "::1", "fc00::1", "fe80::1", "::ffff:127.0.0.1", "64:ff9b::a0a:2", "2001:db8::1"} {
		if publicAddress(netip.MustParseAddr(raw)) {
			t.Fatal("private/special accepted", raw)
		}
	}
	for _, raw := range []string{"http://github.com/x", "https://github.com:8443/x", "https://example.com/x", "https://github.com.example.com/x", "https://user@github.com/x", "https://127.0.0.1/x"} {
		u, _ := url.Parse(raw)
		if allowedSourceURL(u) {
			t.Fatal("unapproved endpoint", raw)
		}
	}
	c := NewSource()
	for _, raw := range []string{"https://example.com/x", "http://github.com/x"} {
		u, _ := url.Parse(raw)
		if c.client.CheckRedirect(&http.Request{URL: u}, nil) == nil {
			t.Fatal("unsafe redirect", raw)
		}
	}
	if c.client.Transport.(*http.Transport).Proxy != nil {
		t.Fatal("environment proxy enabled")
	}
}
func TestOfficialReleasePinsAssetsAndRejectsWrongRepository(t *testing.T) {
	p, _ := Find("clash-verge-rev")
	tag := "v1.2.3"
	assetURL := "https://github.com/" + p.Repository + "/releases/download/" + tag + "/Client_x64-setup.exe"
	rel := map[string]any{"tag_name": tag, "html_url": "https://github.com/" + p.Repository + "/releases/tag/" + tag, "published_at": time.Now().UTC(), "assets": []map[string]any{{"id": 12, "name": "Client_x64-setup.exe", "size": 12, "browser_download_url": assetURL, "digest": "sha256:" + strings.Repeat("a", 64)}, {"id": 13, "name": "latest.json", "size": 1}}}
	repo := p.Repository
	c := &GitHubSource{client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var data any = map[string]any{"full_name": repo, "archived": true}
		if strings.HasSuffix(r.URL.Path, "/latest") {
			data = rel
		}
		b, _ := json.Marshal(data)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(b)), Header: make(http.Header)}, nil
	})}}
	got, archived, e := c.Check(context.Background(), p.ID)
	if e != nil || !archived || len(got.Assets) != 1 || got.Assets[0].URL != assetURL {
		t.Fatalf("%+v %v %v", got, archived, e)
	}
	rel["prerelease"] = true
	if _, _, e = c.Check(context.Background(), p.ID); e == nil {
		t.Fatal("prerelease accepted")
	}
	delete(rel, "prerelease")
	repo = "other/repository"
	if _, _, e = c.Check(context.Background(), p.ID); e == nil {
		t.Fatal("repository redirect accepted")
	}
}
func TestDownloadValidatesSizeAndUpstreamDigest(t *testing.T) {
	body := []byte("installer-fixture")
	sum := fmt.Sprintf("%x", sha256.Sum256(body))
	c := &GitHubSource{client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Header: make(http.Header)}, nil
	})}}
	a := Asset{Name: "Client_x64.exe", URL: "https://github.com/clash-verge-rev/clash-verge-rev/releases/download/v1/Client_x64.exe", Size: int64(len(body)), Digest: "sha256:" + sum}
	var out bytes.Buffer
	got, e := c.Download(context.Background(), a, &out)
	if e != nil || got != sum || !bytes.Equal(out.Bytes(), body) {
		t.Fatal(got, e)
	}
	a.Digest = "sha256:" + strings.Repeat("a", 64)
	if _, e = c.Download(context.Background(), a, io.Discard); e == nil {
		t.Fatal("digest mismatch accepted")
	}
	a.Digest = ""
	a.Size--
	if _, e = c.Download(context.Background(), a, io.Discard); e == nil {
		t.Fatal("wrong byte length accepted")
	}
}
