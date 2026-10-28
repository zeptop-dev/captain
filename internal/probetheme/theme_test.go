package probetheme

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

func archive(t *testing.T, version string, extra map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	files := map[string]string{"komari-theme.json": `{"name":{"en":"Test"},"short":"Test","version":"` + version + `","configuration":{"data":[{"key":"enabled","type":"switch","default":true},{"key":"nodes","type":"nodes","default":"[]"}]}}`, "dist/index.html": `<title>Komari Monitor</title><meta content="A simple server monitor tool."><script src="/assets/app.js"></script>`, "dist/assets/app.js": "window.theme=true;", "dist/details.html": "static detail", "LICENSE": "test license"}
	for name, contents := range extra {
		files[name] = contents
	}
	for name, v := range files {
		f, e := w.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		f.Write([]byte(v))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func manager(t *testing.T) *Manager {
	t.Helper()
	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "theme.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err = db.Migrate(context.Background(), conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	return New(store.New(conn))
}

func TestRejectUnsafeArchives(t *testing.T) {
	for _, name := range []string{"../escape", "/absolute", "dist/../../escape", `dist\escape`, "dist//double", "C:/file"} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(archive(t, "1", map[string]string{name: "bad"})); err == nil {
				t.Fatal("accepted unsafe path")
			}
		})
	}
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	h := &zip.FileHeader{Name: "link"}
	h.SetMode(fs.ModeSymlink | 0777)
	f, _ := w.CreateHeader(h)
	f.Write([]byte("/etc/passwd"))
	w.Close()
	if _, err := Parse(b.Bytes()); err == nil {
		t.Fatal("accepted symlink")
	}
	b.Reset()
	w = zip.NewWriter(&b)
	for i := 0; i < 2; i++ {
		f, _ := w.Create("dist/index.html")
		f.Write([]byte("a"))
	}
	w.Close()
	if _, err := Parse(b.Bytes()); err == nil {
		t.Fatal("accepted duplicate entry")
	}
	if _, err := Parse(archive(t, "1", map[string]string{"komari-theme.json": `{"name":"x","short":"../x"}`})); err == nil {
		t.Fatal("accepted bad short")
	}
	if _, err := Parse(archive(t, "1", map[string]string{"dist/bomb": strings.Repeat("0", maxFileBytes+1)})); err == nil {
		t.Fatal("accepted expanded file bomb")
	}
}
func TestPackageServingIsolationAndRoutes(t *testing.T) {
	p, err := Parse(archive(t, "1", nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		url  string
		code int
		body string
	}{{"/", 200, "&lt;Brand&gt;"}, {"/assets/app.js", 200, "window.theme"}, {"/themes/Test/dist/assets/app.js", 200, "window.theme"}, {"/details", 200, "static detail"}, {"/instance/server", 200, "&lt;Brand&gt;"}, {"/assets/missing.js", 404, ""}, {"/_next/missing.js", 404, ""}, {"/api/nodes", 404, ""}, {"/admin", 404, ""}, {"/themes/Old/dist/assets/app.js", 404, ""}, {"/themes/Test/komari-theme.json", 404, ""}, {"/../LICENSE", 404, ""}} {
		t.Run(tc.url, func(t *testing.T) {
			r := httptest.NewRequest("GET", tc.url, nil)
			w := httptest.NewRecorder()
			p.ServePage(w, r, "<Brand>", "hello\" world")
			if w.Code != tc.code || !strings.Contains(w.Body.String(), tc.body) {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if w.Code == 200 && w.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("missing nosniff")
			}
		})
	}
}
func TestManagerVersionsCASAndReset(t *testing.T) {
	m := manager(t)
	ctx := context.Background()
	a, err := m.Install(ctx, archive(t, "1", nil))
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.Install(ctx, archive(t, "2", nil))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Install(ctx, archive(t, "1", map[string]string{"dist/new.js": "changed"})); err == nil {
		t.Fatal("overwrote immutable version")
	}
	if err = m.Activate(ctx, a.Digest, 0); err != nil {
		t.Fatal(err)
	}
	if err = m.Activate(ctx, b.Digest, 0); !IsConflict(err) {
		t.Fatal("accepted stale activation", err)
	}
	if err = m.SaveSettings(ctx, a.Digest, 1, map[string]any{"enabled": false, "nodes": "[\"node-public-id\"]"}); err != nil {
		t.Fatal(err)
	}
	if err = m.SaveSettings(ctx, a.Digest, 1, map[string]any{}); !IsConflict(err) {
		t.Fatal("accepted stale settings", err)
	}
	short, settings, err := m.PublicSettings(ctx)
	if err != nil || short != "Test" || settings["enabled"] != false {
		t.Fatal(short, settings, err)
	}
	if _, ok := settings["nodes"].([]any); !ok {
		t.Fatal("selector was not decoded")
	}
	if err = m.Remove(ctx, a.Digest); err == nil {
		t.Fatal("removed active version")
	}
	if err = m.Activate(ctx, b.Digest, 1); err != nil {
		t.Fatal(err)
	}
	if err = m.Activate(ctx, a.Digest, 2); err != nil {
		t.Fatal(err)
	}
	_, settings, _ = m.PublicSettings(ctx)
	if settings["enabled"] != false {
		t.Fatal("rollback lost settings")
	}
	// Reset discovers and removes both package/state tables, and a later switch
	// recreates the singleton rather than leaving theme management broken.
	admin := &domain.User{Email: "admin@example.com", PasswordHash: "test", Role: domain.RoleAdmin, Status: "active"}
	if err = m.Store.CreateStaff(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Store.ResetSite(ctx, admin.ID); err != nil {
		t.Fatal(err)
	}
	catalog, err := m.List(ctx)
	if err != nil || len(catalog.Packages) != 0 || catalog.State.Active != "" {
		t.Fatal(catalog, err)
	}
	if err = m.Activate(ctx, "", 0); err != nil {
		t.Fatal(err)
	}
}
func TestSettingsValidation(t *testing.T) {
	p, err := Parse(archive(t, "1", nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{`{"enabled":"false"}`, `{"nodes":[1]}`, `{"nodes":"oops"}`, `{"__proto__":{}}`} {
		var v map[string]any
		json.Unmarshal([]byte(input), &v)
		if _, err = p.Manifest.Settings(v); err == nil {
			t.Fatal("accepted", input)
		}
	}
}

func TestPublicConfigurationUsesActiveVersionSchema(t *testing.T) {
	m := manager(t)
	ctx := context.Background()
	// The same short identifier is deliberately reused while a text field turns
	// into a node selector. Filtering v2 settings with v1's schema would miss it.
	a, err := m.Install(ctx, archive(t, "1", map[string]string{"komari-theme.json": `{"name":"Test","short":"Test","version":"1","configuration":{"type":"managed","data":[{"key":"selection","type":"string"}]}}`}))
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.Install(ctx, archive(t, "2", map[string]string{"komari-theme.json": `{"name":"Test","short":"Test","version":"2","configuration":{"type":"managed","data":[{"key":"selection","type":"nodes"}]}}`}))
	if err != nil {
		t.Fatal(err)
	}
	if err = m.SaveSettings(ctx, a.Digest, 1, map[string]any{"selection": "text setting"}); err != nil {
		t.Fatal(err)
	}
	if err = m.SaveSettings(ctx, b.Digest, 1, map[string]any{"selection": []any{"node-public-id"}}); err != nil {
		t.Fatal(err)
	}
	check := func(c PublicConfiguration) {
		t.Helper()
		fields, err := c.Manifest.Fields()
		if err != nil || len(fields) != 1 || c.Manifest.Short != "Test" || c.SettingsRevision != 2 {
			t.Fatalf("invalid configuration snapshot: %+v, %v", c, err)
		}
		switch c.Digest {
		case a.Digest:
			if c.Manifest.Version != "1" || fields[0].Type != "string" || c.Settings["selection"] != "text setting" || c.Revision%2 != 1 {
				t.Fatalf("mixed v1 configuration: %+v", c)
			}
		case b.Digest:
			values, ok := c.Settings["selection"].([]any)
			if c.Manifest.Version != "2" || fields[0].Type != "nodes" || !ok || len(values) != 1 || values[0] != "node-public-id" || c.Revision%2 != 0 {
				t.Fatalf("mixed v2 configuration: %+v", c)
			}
		default:
			t.Fatalf("unexpected package %s", c.Digest)
		}
	}
	if err = m.Activate(ctx, a.Digest, 0); err != nil {
		t.Fatal(err)
	}
	old, err := m.PublicConfiguration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Activate(ctx, b.Digest, 1); err != nil {
		t.Fatal(err)
	}
	current, err := m.PublicConfiguration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	check(old)
	check(current)
	// Filtering a returned selector must not mutate the saved configuration.
	current.Settings["selection"] = []any{}
	current.Manifest.Configuration.Data[0] = ' '
	current, err = m.PublicConfiguration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	check(current)

	// Activation and configuration retrieval may race. The state revision and
	// package data must always come from the same transaction, not two reads.
	done := make(chan error, 1)
	go func() {
		for i := int64(0); i < 100; i++ {
			digest := a.Digest
			if i%2 == 1 {
				digest = b.Digest
			}
			if err := m.Store.ActivateProbeTheme(ctx, digest, i+2); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	for i := 0; i < 100; i++ {
		c, err := m.PublicConfiguration(ctx)
		if err != nil {
			t.Fatal(err)
		}
		check(c)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if err = m.Activate(ctx, "", 102); err != nil {
		t.Fatal(err)
	}
	builtin, err := m.PublicConfiguration(ctx)
	if err != nil || builtin.Digest != "" || builtin.Manifest.Short != "default" || len(builtin.Settings) != 0 || builtin.Revision != 103 {
		t.Fatalf("invalid built-in configuration: %+v, %v", builtin, err)
	}
}

// Optional original release package smoke. ZIPs stay outside the repository.
func TestOriginalReleasePackages(t *testing.T) {
	dir := os.Getenv("CAPTAIN_THEME_RELEASE_DIR")
	if dir == "" {
		t.Skip("set CAPTAIN_THEME_RELEASE_DIR to verify original release ZIPs")
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.zip"))
	if err != nil || len(files) == 0 {
		t.Fatal("no release ZIPs", err)
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			p, err := Parse(data)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = p.Manifest.Settings(nil); err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			w := httptest.NewRecorder()
			p.ServePage(w, r, "Captain", "Monitor")
			if w.Code != 200 {
				t.Fatal(w.Code)
			}
			t.Log(p.Manifest.Short, p.Manifest.Version, len(p.files), p.Digest)
		})
	}
}
