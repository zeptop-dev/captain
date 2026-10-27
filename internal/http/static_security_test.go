package http

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeptop-dev/captain/internal/config"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/http/site"
	"github.com/zeptop-dev/captain/internal/store"
)

// Exercise the real router as well as the FileServer path cleaner: a custom
// landing page is public, but its sibling configuration, database and backups
// must never become static assets through a prefix or encoded traversal.
func TestStaticRoutesDoNotExposeDataDirectory(t *testing.T) {
	cfg := config.Default()
	cfg.BaseURL = "http://example.com"
	cfg.DataDir = t.TempDir()
	conn, err := db.Open("sqlite", filepath.Join(cfg.DataDir, "captain.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := db.Migrate(context.Background(), conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	const private = "PRIVATE-CONFIG-AND-BACKUP-SENTINEL"
	for _, name := range []string{"config.yaml", "backups/captain-audit.db"} {
		p := filepath.Join(cfg.DataDir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(private), 0600); err != nil {
			t.Fatal(err)
		}
	}
	st := store.New(conn)
	srv := httptest.NewServer(New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
	t.Cleanup(srv.Close)
	get := func(path string) (int, string) {
		t.Helper()
		resp, err := srv.Client().Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, string(b)
	}
	paths := []string{
		"/config.yaml", "/captain.db", "/backups/captain-audit.db",
		"/dashboard../config.yaml", "/admin../config.yaml", "/portal../captain.db",
		"/admin/../config.yaml", "/admin/%2e%2e/config.yaml", "/admin/%2e%2e%2fconfig.yaml",
		"/portal/%2e%2e%2fcaptain.db", "/admin/assets/../../config.yaml",
		"/assets/../config.yaml", "/assets/../../config.yaml", "/assets/../../../captain.db",
		"/assets/%2e%2e/config.yaml", "/assets/%2e%2e%2fconfig.yaml",
		"/assets/%2e%2e%2f%2e%2e%2fconfig.yaml", "/assets/%252e%252e/config.yaml",
		"/assets/..%5cconfig.yaml", "/assets//..//config.yaml",
		"/assets/../backups/captain-audit.db", "/assets/%2e%2e%2fbackups/captain-audit.db",
		"/api/admin/settings/backup/files/captain-audit.db",
		"/api/admin/settings/backup/files/..%2f..%2fconfig.yaml",
	}
	for _, custom := range []bool{false, true} {
		name := "embedded"
		if custom {
			name = "custom"
			for path, body := range map[string]string{
				"index.html":       "<html>PUBLIC-LANDING-PAGE</html>",
				"assets/audit.txt": "PUBLIC-ASSET",
			} {
				p := filepath.Join(cfg.DataDir, "site", path)
				if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
		for _, enabled := range []bool{true, false} {
			if err := st.SetSetting(context.Background(), site.SettingSite, site.Settings{HomepageEnabled: &enabled}); err != nil {
				t.Fatal(err)
			}
			variant := name + "/closed"
			if enabled {
				variant = name + "/open"
			}
			t.Run(variant, func(t *testing.T) {
				if code, body := get("/"); code != http.StatusOK || !strings.Contains(strings.ToLower(body), "<html") {
					t.Fatalf("landing page: status %d", code)
				}
				if custom {
					if code, body := get("/assets/audit.txt"); code != http.StatusOK || body != "PUBLIC-ASSET" {
						t.Fatalf("custom asset: status %d body %q", code, body)
					}
				}
				for _, path := range paths {
					t.Run(path, func(t *testing.T) {
						code, body := get(path)
						if strings.Contains(body, private) || strings.Contains(body, "SQLite format 3") {
							t.Fatalf("private file exposed at %s (status %d)", path, code)
						}
						if code >= http.StatusInternalServerError {
							t.Fatalf("unexpected server failure at %s: %d", path, code)
						}
					})
				}
			})
		}
	}
}
