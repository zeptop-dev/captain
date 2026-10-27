package http

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/zeptop-dev/captain/internal/config"
	"github.com/zeptop-dev/captain/internal/http/admin"
	"github.com/zeptop-dev/captain/internal/http/site"
	"github.com/zeptop-dev/captain/internal/service"
)

func homepageRequest(t *testing.T, srv *httptest.Server, method, path, host string) (int, []byte, http.Header) {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if host != "" {
		req.Host = host
	}
	noRedirect := *srv.Client()
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, body, resp.Header
}

func TestHomepageRuntimeSwitchAndLegacyUpdates(t *testing.T) {
	p := newPortalRig(t, true)
	visitor := &client{t: t, srv: p.srv}
	read := func(c *client, path string, enabled bool) map[string]any {
		t.Helper()
		code, raw, _ := c.do("GET", path, nil, nil)
		if code != 200 {
			t.Fatalf("read %s: %d %s", path, code, raw)
		}
		value := mustJSON[map[string]any](t, raw)
		if value["homepage_enabled"] != enabled {
			t.Fatalf("effective homepage flag: %v, want %v", value["homepage_enabled"], enabled)
		}
		return value
	}
	read(p.admin, "/api/admin/settings/site", true)
	read(visitor, "/api/site", true)
	var rows int
	if err := p.db.QueryRow(`SELECT COUNT(*) FROM settings WHERE key='site'`).Scan(&rows); err != nil || rows != 0 {
		t.Fatal("read persisted an implicit homepage default", rows, err)
	}
	if code, _, _ := homepageRequest(t, p.srv, "GET", "/", ""); code != 200 {
		t.Fatal("legacy homepage unavailable", code)
	}
	// Start with a legacy document containing real content shared by other pages.
	seed := site.Defaults("Example brand")
	seed.Theme.SiteScheme = "dark"
	seed.Branding = &site.Branding{LogoLight: "https://example.com/logo.png", Title: "Example portal"}
	if err := p.st.SetSetting(context.Background(), site.SettingSite, seed); err != nil {
		t.Fatal(err)
	}
	put := func(value any, want int) {
		t.Helper()
		if code, raw, _ := p.admin.do("PUT", "/api/admin/settings/site", value, nil); code != want {
			t.Fatalf("save: %d %s, want %d", code, raw, want)
		}
	}
	put(map[string]any{"homepage_enabled": false}, 200)
	closed := read(p.admin, "/api/admin/settings/site", false)
	public := read(visitor, "/api/site", false)
	if public["name"] != seed.Name || !reflect.DeepEqual(public["branding"], closed["branding"]) || !reflect.DeepEqual(public["theme"], closed["theme"]) {
		t.Fatal("closing homepage changed shared presentation", public)
	}
	for path, want := range map[string]string{
		"/": "/portal/",
		"/?ref=EXAMPLE&next=https://evil.example.com&redirect=/admin/": "/portal/?ref=EXAMPLE",
		"/?ref=first&ref=second": "/portal/?ref=first",
		"/?ref=":                 "/portal/",
		"/?ref=A%2BB%26next%3Dhttps%3A%2F%2Fevil.example.com": "/portal/?ref=A%2BB%26next%3Dhttps%3A%2F%2Fevil.example.com",
		"/?ref=A%0D%0AX-Test%3Ayes":                           "/portal/?ref=A%0D%0AX-Test%3Ayes",
	} {
		for _, method := range []string{"GET", "HEAD"} {
			code, body, headers := homepageRequest(t, p.srv, method, path, "")
			if code != 302 || headers.Get("Location") != want || headers.Get("Cache-Control") != "no-store" || headers.Get("X-Test") != "" || (method == "HEAD" && len(body) != 0) {
				t.Fatalf("%s %s: %d %v", method, path, code, headers)
			}
		}
	}
	for _, path := range []string{"/portal/", "/portal/login", "/admin/", "/downloads", "/downloads/", "/service-status", "/service-status/"} {
		if code, body, headers := homepageRequest(t, p.srv, "GET", path, ""); code != 200 || headers.Get("Location") != "" || !bytes.Contains(bytes.ToLower(body), []byte("<html")) {
			t.Fatalf("independent page intercepted: %s %d", path, code)
		}
	}
	for _, path := range []string{"/assets/not-a-real-file.js", "/assets/unknown", "/assets/"} {
		if code, _, headers := homepageRequest(t, p.srv, "GET", path, ""); code != 404 || headers.Get("Location") != "" {
			t.Fatalf("missing asset returned a page: %s %d", path, code)
		}
	}
	// Old editors, explicit null, and failed writes cannot turn the homepage on.
	put(map[string]any{"name": "Example brand"}, 200)
	put(map[string]any{"homepage_enabled": nil}, 200)
	put(map[string]any{"homepage_enabled": "false"}, 400)
	put(map[string]any{"homepage_enabled": true, "map_entry_ids": []int64{}, "map_revision": -1}, 409)
	put(map[string]any{"homepage_enabled": true, "map_entry_ids": []int64{999999}, "map_revision": closed["map_revision"]}, 400)
	if current := read(p.admin, "/api/admin/settings/site", false); !reflect.DeepEqual(current, closed) {
		t.Fatal("legacy or failed write changed content", current)
	}
	if code, _, _ := p.admin.do("PUT", "/api/admin/settings/site", map[string]any{"homepage_enabled": true}, map[string]string{"Origin": "https://evil.example.com"}); code != 403 {
		t.Fatal("cross-origin write accepted", code)
	}
	put(map[string]any{"homepage_enabled": true}, 200)
	read(visitor, "/api/site", true)
	if code, _, headers := homepageRequest(t, p.srv, "GET", "/", ""); code != 200 || headers.Get("Cache-Control") != "no-store" {
		t.Fatal("reopening requires restart or cached redirect", code, headers)
	}
	// Unreadable settings are not an implicit enabled default.
	if _, err := p.db.Exec(`UPDATE settings SET value_json='broken' WHERE key='site'`); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/api/site", "/assets/missing.js"} {
		if code, _, headers := homepageRequest(t, p.srv, "GET", path, ""); code != 500 || headers.Get("Cache-Control") != "no-store" {
			t.Fatalf("settings failure masked: %s %d", path, code)
		}
	}
	put(map[string]any{"homepage_enabled": true}, 500)
}

func TestHomepageSwitchKeepsDomainIsolationAndPermissions(t *testing.T) {
	p := newPortalRig(t, true)
	put := func(path string, value any) {
		t.Helper()
		if code, raw, _ := p.admin.do("PUT", path, value, nil); code != 200 {
			t.Fatalf("save %s: %d %s", path, code, raw)
		}
	}
	put("/api/admin/settings/site", map[string]any{"homepage_enabled": false})
	put("/api/admin/settings/probe", map[string]any{"enabled": true, "visibility": "public", "path": "/status", "hosts": []string{"probe.example.com"}})
	put("/api/admin/settings/subscription", service.SubscriptionSettings{URLs: []string{"https://sub.example.com"}})
	for _, test := range []struct {
		host, path string
		want       int
	}{
		{"PrObE.Example.COM:443", "/", 200},
		{"probe.example.com", "/api/site", 404},
		{"probe.example.com", "/portal/", 404},
		{"SuB.Example.COM:443", "/", 404},
		{"sub.example.com", "/api/site", 404},
		{"sub.example.com", "/portal/", 404},
		{"", "/status/", 200},
	} {
		if code, _, headers := homepageRequest(t, p.srv, "GET", test.path, test.host); code != test.want || headers.Get("Location") != "" {
			t.Fatalf("domain isolation: %s%s %d %v", test.host, test.path, code, headers)
		}
	}
	for _, role := range []string{"operator", "support"} {
		staff, err := admin.NewUser(role+"@example.com", "password123", role)
		if err != nil {
			t.Fatal(err)
		}
		if err := p.st.CreateStaff(context.Background(), staff); err != nil {
			t.Fatal(err)
		}
		c := &client{t: t, srv: p.srv}
		if code, _, _ := c.do("POST", "/api/admin/login", map[string]string{"Email": staff.Email, "Password": "password123"}, nil); code != 200 {
			t.Fatal("staff login", code)
		}
		if code, _, _ := c.do("PUT", "/api/admin/settings/site", map[string]any{"homepage_enabled": true}, nil); code != 403 {
			t.Fatal("unauthorized homepage change", role, code)
		}
	}
}

func TestHomepageSwitchCustomPageAndSharedAssets(t *testing.T) {
	p := newPortalRig(t, true)
	cfg := config.Default()
	cfg.BaseURL, cfg.DataDir = "http://example.com", t.TempDir()
	for name, content := range map[string]string{"index.html": "<html>CUSTOM-HOMEPAGE</html>", "assets/logo.txt": "SHARED-ASSET"} {
		path := filepath.Join(cfg.DataDir, "site", name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	srv := httptest.NewServer(New(cfg, p.st, slog.Default()).Handler())
	defer srv.Close()
	for _, enabled := range []bool{true, false, true} {
		if err := p.st.SetSetting(context.Background(), site.SettingSite, site.Settings{HomepageEnabled: &enabled}); err != nil {
			t.Fatal(err)
		}
		code, body, headers := homepageRequest(t, srv, "GET", "/", "")
		if enabled {
			if code != 200 || !bytes.Contains(body, []byte("CUSTOM-HOMEPAGE")) {
				t.Fatal("custom page lost", code)
			}
		} else if code != 302 || bytes.Contains(body, []byte("CUSTOM-HOMEPAGE")) || headers.Get("Cache-Control") != "no-store" {
			t.Fatal("custom page bypasses switch", code, headers)
		}
		if code, body, _ := homepageRequest(t, srv, "GET", "/assets/logo.txt", ""); code != 200 || string(body) != "SHARED-ASSET" {
			t.Fatal("shared custom asset lost", code)
		}
		_, page, _ := homepageRequest(t, srv, "GET", "/downloads", "")
		asset := regexp.MustCompile(`(?:src|href)="(/assets/[^" ]+\.js)"`).FindSubmatch(page)
		if len(asset) != 2 {
			t.Fatal("build site before testing")
		}
		for _, method := range []string{"GET", "HEAD"} {
			code, body, headers := homepageRequest(t, srv, method, string(asset[1]), "")
			if code != 200 || strings.Contains(strings.ToLower(string(body)), "<html") || !strings.Contains(headers.Get("Content-Type"), "javascript") || !strings.Contains(headers.Get("Cache-Control"), "immutable") {
				t.Fatal("independent embedded asset lost", method, code, headers)
			}
			if method == "HEAD" && len(body) != 0 {
				t.Fatal("HEAD asset has a body")
			}
		}
		if !enabled {
			if code, _, _ := homepageRequest(t, srv, "GET", "/assets/missing.js", ""); code != 404 {
				t.Fatal("custom assets fall back to homepage", code)
			}
		}
	}
}
