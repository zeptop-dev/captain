package http

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/zeptop-dev/captain/internal/branding"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/http/admin"
	"github.com/zeptop-dev/captain/internal/http/site"
	"github.com/zeptop-dev/captain/internal/store"
)

const brandUploadPath = "/api/admin/settings/branding/assets"

func brandRequest(t *testing.T, c *client, method, path, host string, body []byte, headers map[string]string) (int, []byte, http.Header) {
	t.Helper()
	req, err := http.NewRequest(method, c.srv.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if host != "" {
		req.Host = host
	}
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, b, resp.Header
}

func brandPNG(t *testing.T) []byte {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	im.SetNRGBA(1, 1, color.NRGBA{R: 25, G: 80, B: 240, A: 160})
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestBrandAssetUploadAuthorizationAndValidation(t *testing.T) {
	p := newPortalRig(t, true)
	anon := &client{t: t, srv: p.srv}
	_, customer := p.customer("brand-user@example.com")
	clients := []*client{anon, customer}
	for _, role := range []string{"operator", "support"} {
		u, err := admin.NewUser(role+"@example.com", "password123", role)
		if err != nil {
			t.Fatal(err)
		}
		if err := p.st.CreateStaff(context.Background(), u); err != nil {
			t.Fatal(err)
		}
		c := &client{t: t, srv: p.srv}
		if code, b, _ := c.do("POST", "/api/admin/login", map[string]string{"Email": u.Email, "Password": "password123"}, nil); code != 200 {
			t.Fatalf("staff login: %d %s", code, b)
		}
		clients = append(clients, c)
	}
	token, _, err := p.st.CreateAPIToken(context.Background(), 1, "read token", "read", nil)
	if err != nil {
		t.Fatal(err)
	}
	clients = append(clients, &client{t: t, srv: p.srv, token: token})
	for _, c := range clients {
		if code, _, _ := brandRequest(t, c, "POST", brandUploadPath, "", brandPNG(t), nil); code != 401 && code != 403 {
			t.Fatalf("unauthorized upload: %d", code)
		}
	}
	if code, _, _ := brandRequest(t, p.admin, "POST", brandUploadPath, "", brandPNG(t), map[string]string{"Origin": "https://evil.example.com"}); code != 403 {
		t.Fatalf("cross-origin upload: %d", code)
	}
	for _, input := range [][]byte{nil, []byte("<svg xmlns='http://www.w3.org/2000/svg'><script/></svg>"), []byte("<html>image</html>"), brandPNG(t)[:20]} {
		if code, _, _ := brandRequest(t, p.admin, "POST", brandUploadPath, "", input, map[string]string{"Content-Type": "image/png"}); code != 400 {
			t.Fatalf("invalid image: %d", code)
		}
	}
	if code, _, _ := brandRequest(t, p.admin, "POST", brandUploadPath, "", make([]byte, branding.MaxInputBytes+1), nil); code != 413 {
		t.Fatalf("oversize upload: %d", code)
	}
	var count int
	if err := p.db.QueryRow(`SELECT COUNT(*) FROM brand_assets`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed uploads wrote assets: %d %v", count, err)
	}

	// More than the ordinary API's 1 MiB is accepted only by this endpoint;
	// appended bytes are discarded rather than published as part of an image.
	input := append(brandPNG(t), bytes.Repeat([]byte("private-appended-metadata"), 50000)...)
	code, raw, _ := brandRequest(t, p.admin, "POST", brandUploadPath, "", input, nil)
	if code != 200 {
		t.Fatalf("valid upload: %d %s", code, raw)
	}
	url := mustJSON[map[string]string](t, raw)["url"]
	code, imageBytes, h := brandRequest(t, anon, "GET", url, "", nil, nil)
	if code != 200 || h.Get("Content-Type") != "image/png" || h.Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(h.Get("Cache-Control"), "immutable") {
		t.Fatalf("public asset: %d %v", code, h)
	}
	if _, err := png.Decode(bytes.NewReader(imageBytes)); err != nil || bytes.Contains(imageBytes, []byte("private-appended-metadata")) {
		t.Fatal("image was not normalized", err)
	}
	sum := sha256.Sum256(imageBytes)
	if url != "/brand-assets/"+hex.EncodeToString(sum[:])+".png" {
		t.Fatal("URL is not a content hash", url)
	}
	if code, b, _ := brandRequest(t, anon, "HEAD", url, "", nil, nil); code != 200 || len(b) != 0 {
		t.Fatalf("HEAD: %d %s", code, b)
	}
	if code, _, _ := brandRequest(t, anon, "GET", url, "", nil, map[string]string{"If-None-Match": h.Get("ETag")}); code != 304 {
		t.Fatalf("conditional GET: %d", code)
	}
	for _, path := range []string{"/brand-assets/no.png", "/brand-assets/" + strings.Repeat("0", 64) + ".png", url + "/extra"} {
		if code, _, _ := brandRequest(t, anon, "GET", path, "", nil, nil); code != 404 {
			t.Fatalf("invalid public path: %s %d", path, code)
		}
	}
	if code, raw, _ := brandRequest(t, p.admin, "POST", brandUploadPath, "", brandPNG(t), nil); code != 200 || mustJSON[map[string]string](t, raw)["url"] != url {
		t.Fatalf("deduplicated upload: %d %s", code, raw)
	}
	if err := p.db.QueryRow(`SELECT COUNT(*) FROM brand_assets`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate asset rows: %d %v", count, err)
	}
}

func TestBrandSettingsProjectionAndLegacyUpdates(t *testing.T) {
	p := newPortalRig(t, true)
	anon := &client{t: t, srv: p.srv}
	b := &site.Branding{LogoLight: "https://example.com/light.png", LogoDark: "https://example.com/dark.png", Favicon: "https://example.com/icon.png", Title: "Example browser title"}
	s := site.Settings{Name: "Example brand", Description: "Public description", Branding: b, InjectHead: "private-head-marker", InjectBody: "private-body-marker", Theme: site.Theme{SiteScheme: "auto", PortalTitle: "Legacy portal"}}
	if code, raw, _ := p.admin.do("PUT", "/api/admin/settings/site", s, nil); code != 200 {
		t.Fatalf("save: %d %s", code, raw)
	}
	for _, update := range []map[string]any{{"tagline": "new"}, {"tagline": "newer", "branding": nil}} {
		code, raw, _ := p.admin.do("PUT", "/api/admin/settings/site", update, nil)
		if code != 200 {
			t.Fatalf("legacy update: %d %s", code, raw)
		}
		got := mustJSON[site.Settings](t, raw)
		if !reflect.DeepEqual(got.Branding, b) || got.Name != s.Name || got.Theme != s.Theme {
			t.Fatalf("legacy update erased branding: %+v", got)
		}
	}
	code, raw, _ := anon.do("GET", "/api/site", nil, nil)
	if code != 200 {
		t.Fatalf("public site: %d %s", code, raw)
	}
	public := mustJSON[map[string]any](t, raw)
	if !reflect.DeepEqual(mustJSON[site.Settings](t, raw).Branding, b) || public["description"] != s.Description {
		t.Fatalf("public brand: %s", raw)
	}
	for _, forbidden := range []string{"inject_head", "inject_body", "private-head-marker", "private-body-marker"} {
		if bytes.Contains(raw, []byte(forbidden)) {
			t.Fatalf("private setting leaked %s", forbidden)
		}
	}
	for _, field := range []string{"logo_light", "logo_dark", "favicon"} {
		if code, _, _ := p.admin.do("PUT", "/api/admin/settings/site", map[string]any{"branding": map[string]string{field: "javascript:alert(1)"}}, nil); code != 400 {
			t.Fatalf("invalid %s: %d", field, code)
		}
	}
	if code, raw, _ := p.admin.do("PUT", "/api/admin/settings/site", map[string]any{"branding": map[string]string{"logo_dark": ""}}, nil); code != 200 || mustJSON[site.Settings](t, raw).Branding.LogoDark != "" || mustJSON[site.Settings](t, raw).Branding.LogoLight != b.LogoLight {
		t.Fatalf("explicit clear: %d %s", code, raw)
	}
}

func TestProbeBrandingInheritanceDedicatedHostAndPreservation(t *testing.T) {
	p := newPortalRig(t, true)
	anon := &client{t: t, srv: p.srv}
	code, raw, _ := brandRequest(t, p.admin, "POST", brandUploadPath, "", brandPNG(t), nil)
	if code != 200 {
		t.Fatalf("upload: %d %s", code, raw)
	}
	asset := mustJSON[map[string]string](t, raw)["url"]
	siteValue := site.Settings{Name: "Main brand", Description: "Main description", Branding: &site.Branding{LogoLight: asset, LogoDark: "https://example.com/main-dark.png", Favicon: asset, Title: "Main page title"}, InjectHead: "private-head-marker"}
	if code, raw, _ := p.admin.do("PUT", "/api/admin/settings/site", siteValue, nil); code != 200 {
		t.Fatalf("site: %d %s", code, raw)
	}
	settings := map[string]any{"enabled": true, "visibility": "public", "path": "/status", "hosts": []string{"probe.example.com"}, "title": "Independent operations", "logo": "https://example.com/own.png", "logo_dark": "https://example.com/own-dark.png", "favicon": "https://example.com/own-icon.png"}
	put := func(value any) store.ProbeSettings {
		t.Helper()
		code, b, _ := p.admin.do("PUT", "/api/admin/settings/probe", value, nil)
		if code != 200 {
			t.Fatalf("probe save: %d %s", code, b)
		}
		return mustJSON[store.ProbeSettings](t, b)
	}
	put(settings)
	check := func(want map[string]string) {
		t.Helper()
		code, b, _ := brandRequest(t, anon, "GET", "/api/probe", "probe.example.com", nil, nil)
		if code != 200 {
			t.Fatalf("probe: %d %s", code, b)
		}
		got := mustJSON[map[string]any](t, b)
		for k, v := range want {
			if got[k] != v {
				t.Fatalf("probe %s=%v want %q", k, got[k], v)
			}
		}
		for _, forbidden := range []string{"inject_head", "private-head-marker", "follow_site_branding"} {
			if bytes.Contains(b, []byte(forbidden)) {
				t.Fatalf("private field leaked: %s", forbidden)
			}
		}
	}
	independent := map[string]string{"title": "Independent operations", "page_title": "Independent operations", "logo": "https://example.com/own.png", "logo_dark": "https://example.com/own-dark.png", "favicon": "https://example.com/own-icon.png", "description": ""}
	check(independent)
	settings["follow_site_branding"] = true
	put(settings)
	check(map[string]string{"title": "Main brand", "page_title": "Main page title", "logo": asset, "logo_dark": siteValue.Branding.LogoDark, "favicon": asset, "description": "Main description"})
	for _, nulls := range []bool{false, true} {
		legacy := map[string]any{"enabled": true, "visibility": "public", "hosts": []string{"probe.example.com"}}
		if nulls {
			legacy["follow_site_branding"], legacy["logo_dark"], legacy["favicon"] = nil, nil, nil
			legacy["title"], legacy["logo"] = nil, nil
		}
		got := put(legacy)
		if got.FollowSiteBranding == nil || !*got.FollowSiteBranding || got.Title != independent["title"] || got.Logo != independent["logo"] || got.LogoDark != independent["logo_dark"] || got.Favicon != independent["favicon"] {
			t.Fatalf("legacy update erased fields: %+v", got)
		}
	}
	if code, _, _ := brandRequest(t, anon, "GET", "/api/site", "probe.example.com", nil, nil); code != 404 {
		t.Fatalf("probe host exposed main API: %d", code)
	}
	if code, b, h := brandRequest(t, anon, "GET", asset, "probe.example.com", nil, nil); code != 200 || h.Get("Content-Type") != "image/png" || !bytes.Equal(b, brandPNG(t)) {
		t.Fatalf("dedicated host asset: %d %v", code, h)
	}
	if code, _, _ := brandRequest(t, anon, "GET", "/brand-assets/missing.png", "probe.example.com", nil, nil); code != 404 {
		t.Fatalf("invalid asset fell through to probe HTML: %d", code)
	}
	// A main brand update invalidates the anonymous probe snapshot immediately.
	siteValue.Name, siteValue.Branding.Title = "Updated main brand", ""
	if code, raw, _ := p.admin.do("PUT", "/api/admin/settings/site", siteValue, nil); code != 200 {
		t.Fatalf("site update: %d %s", code, raw)
	}
	check(map[string]string{"title": "Updated main brand", "page_title": "Updated main brand"})
	settings["follow_site_branding"] = false
	got := put(settings)
	if got.Title != independent["title"] || got.Logo != independent["logo"] || got.LogoDark != independent["logo_dark"] {
		t.Fatalf("toggle erased independent brand: %+v", got)
	}
	check(independent)
	settings["logo_dark"], settings["favicon"] = "", ""
	got = put(settings)
	if got.LogoDark != "" || got.Favicon != "" {
		t.Fatal("explicit clears not honored")
	}
	settings["title"], settings["logo"] = "", ""
	put(settings)
	check(map[string]string{"title": "Captain", "page_title": "Captain", "logo": "", "logo_dark": "", "favicon": "", "description": ""})
	// Following an uncustomized main brand must not accidentally fall back to
	// the private independent images saved earlier.
	settings["follow_site_branding"] = true
	put(settings)
	siteValue.Branding = &site.Branding{}
	if code, raw, _ := p.admin.do("PUT", "/api/admin/settings/site", siteValue, nil); code != 200 {
		t.Fatalf("clear main assets: %d %s", code, raw)
	}
	check(map[string]string{"title": "Updated main brand", "logo": "", "logo_dark": "", "favicon": ""})
}

func TestBrandAssetsFollowDatabaseBackupAndReset(t *testing.T) {
	p := newPortalRig(t, true)
	code, raw, _ := brandRequest(t, p.admin, "POST", brandUploadPath, "", brandPNG(t), nil)
	if code != 200 {
		t.Fatalf("upload: %d %s", code, raw)
	}
	url := mustJSON[map[string]string](t, raw)["url"]
	digest := strings.TrimSuffix(strings.TrimPrefix(url, branding.AssetPrefix), ".png")
	backupPath := filepath.Join(t.TempDir(), "backup.db")
	if err := p.st.Backup(context.Background(), backupPath); err != nil {
		t.Fatal(err)
	}
	conn, err := db.Open("sqlite", backupPath)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	backup := store.New(conn)
	b, err := backup.BrandAsset(context.Background(), digest)
	if err != nil || !bytes.Equal(b, brandPNG(t)) {
		t.Fatal("backup lost image", err)
	}
	if code, raw, _ := p.admin.do("POST", resetPath, resetInput(), nil); code != 200 {
		t.Fatalf("reset: %d %s", code, raw)
	}
	anon := &client{t: t, srv: p.srv}
	if code, _, _ := anon.do("GET", url, nil, nil); code != 404 {
		t.Fatalf("image survived reset: %d", code)
	}
	if _, err := backup.BrandAsset(context.Background(), digest); err != nil {
		t.Fatal("reset affected backup", err)
	}
}
