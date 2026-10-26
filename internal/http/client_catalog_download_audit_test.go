package http

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/clientcatalog"
	"github.com/zeptop-dev/captain/internal/config"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

const catalogFixtureBody = "synthetic installer bytes for authorization tests"

type catalogAuditSource struct {
	started chan struct{}
	resume  chan struct{}
	once    sync.Once
}

func (s *catalogAuditSource) Check(context.Context, string) (*clientcatalog.Release, bool, error) {
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(catalogFixtureBody)))
	return &clientcatalog.Release{Version: "v1.0.0", URL: "https://github.com/clash-verge-rev/clash-verge-rev/releases/tag/v1.0.0", PublishedAt: time.Now(), Assets: []clientcatalog.Asset{{
		ID: 41, Name: "Clash.Verge_1.0.0_x64-setup.exe", Platform: "windows", Arch: "x64", PackageType: "exe", Size: int64(len(catalogFixtureBody)), Version: "v1.0.0", Digest: "sha256:" + hash,
		URL: "https://github.com/clash-verge-rev/clash-verge-rev/releases/download/v1.0.0/Clash.Verge_1.0.0_x64-setup.exe",
	}}}, false, nil
}

func (s *catalogAuditSource) Download(ctx context.Context, _ clientcatalog.Asset, w io.Writer) (string, error) {
	if s.started != nil {
		s.once.Do(func() { close(s.started) })
		select {
		case <-s.resume:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	_, err := io.WriteString(w, catalogFixtureBody)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(catalogFixtureBody))), err
}

func newCatalogAuditRig(t *testing.T, source clientcatalog.Source) (*portalRig, int64) {
	t.Helper()
	cfg := config.Default()
	cfg.BaseURL, cfg.DataDir = "http://example.com", t.TempDir()
	conn, err := db.Open("sqlite", filepath.Join(cfg.DataDir, "captain.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := db.Migrate(context.Background(), conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	st := store.New(conn)
	u := &domain.User{Email: "catalog-admin@example.com", Role: domain.RoleAdmin, Status: "active"}
	if err := st.CreateStaff(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	token, info, err := st.CreateAPIToken(context.Background(), u.ID, "catalog tests", "full", nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), Options{ClientCatalogSource: source}).Handler())
	t.Cleanup(srv.Close)
	return &portalRig{t: t, st: st, db: conn, srv: srv, admin: &client{t: t, srv: srv, token: token}}, info.ID
}

func catalogAuditCall(t *testing.T, c *client, method, path string, input any, want int) []byte {
	t.Helper()
	code, body, _ := c.do(method, path, input, nil)
	if code != want {
		t.Fatalf("%s %s: want %d got %d %s", method, path, want, code, body)
	}
	return body
}

func catalogAuditState(t *testing.T, c *client) clientcatalog.CatalogView {
	t.Helper()
	return mustJSON[clientcatalog.CatalogView](t, catalogAuditCall(t, c, "GET", "/api/admin/settings/client-catalog", nil, 200))
}

func awaitCatalogJob(t *testing.T, c *client, state string) clientcatalog.CatalogView {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		view := catalogAuditState(t, c)
		if view.Job != nil && view.Job.State != "downloading" {
			if view.Job.State != state {
				t.Fatalf("unexpected final download status: %+v", view.Job)
			}
			return view
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("download did not settle")
	return clientcatalog.CatalogView{}
}

func TestClientCatalogDownloadVisibilityAndRangesAudit(t *testing.T) {
	p, _ := newCatalogAuditRig(t, &catalogAuditSource{})
	visitor := &client{t: t, srv: p.srv}
	_, customer := p.customer("catalog-download@example.com")
	const base = "/api/admin/settings/client-catalog/clash-verge-rev"
	catalogAuditCall(t, p.admin, "POST", base+"/check", nil, 200)
	catalogAuditCall(t, p.admin, "POST", base+"/download", map[string]int{"asset_id": 41}, 202)
	view := awaitCatalogJob(t, p.admin, "complete")
	var cv clientcatalog.Client
	for _, c := range view.Clients {
		if c.ID == "clash-verge-rev" {
			cv = c
		}
	}
	if len(cv.CachedAssets) != 1 || view.Storage.UsedBytes != int64(len(catalogFixtureBody)) {
		t.Fatalf("cache accounting: %+v", view)
	}
	for i := range cv.Platforms {
		if cv.Platforms[i].Platform == "windows" {
			cv.Platforms[i].Source, cv.Platforms[i].ShowPortal = "cache", true
			cv.Platforms[i].AssetIDs = []int64{41}
		}
	}
	cv = mustJSON[clientcatalog.Client](t, catalogAuditCall(t, p.admin, "PUT", base, map[string]any{"revision": cv.Revision, "platforms": cv.Platforms}, 200))
	rows := mustJSON[[]clientcatalog.PublishedClient](t, catalogAuditCall(t, customer, "GET", "/api/portal/client-catalog", nil, 200))
	if len(rows) != 1 || len(rows[0].Downloads) != 1 || !strings.HasPrefix(rows[0].Downloads[0].URL, "/downloads/clients/") {
		t.Fatal("missing local published download", rows)
	}
	path := rows[0].Downloads[0].URL
	for _, method := range []string{"GET", "HEAD"} {
		code, _, _ := visitor.do(method, path, nil, map[string]string{"Range": "bytes=0-3"})
		if code != 401 && code != 404 {
			t.Fatalf("anonymous access to portal-only package: %d", code)
		}
	}
	code, data, headers := customer.do("GET", path, nil, map[string]string{"Range": "bytes=0-3"})
	if code != 206 || string(data) != catalogFixtureBody[:4] || !strings.Contains(headers.Get("Content-Disposition"), "attachment") {
		t.Fatalf("authorized range: %d %s %v", code, data, headers)
	}
	catalogAuditCall(t, p.admin, "DELETE", base+"/cache/41", nil, 409)
	for i := range cv.Platforms {
		if cv.Platforms[i].Platform == "windows" {
			cv.Platforms[i].ShowHome = true
		}
	}
	cv = mustJSON[clientcatalog.Client](t, catalogAuditCall(t, p.admin, "PUT", base, map[string]any{"revision": cv.Revision, "platforms": cv.Platforms}, 200))
	if data := catalogAuditCall(t, visitor, "GET", path, nil, 200); string(data) != catalogFixtureBody {
		t.Fatal("download changed original bytes")
	}
	for i := range cv.Platforms {
		cv.Platforms[i].ShowHome, cv.Platforms[i].ShowPortal = false, false
		cv.Platforms[i].Source = "official"
		cv.Platforms[i].AssetIDs = nil
	}
	catalogAuditCall(t, p.admin, "PUT", base, map[string]any{"revision": cv.Revision, "platforms": cv.Platforms}, 200)
	for _, c := range []*client{visitor, customer} {
		catalogAuditCall(t, c, "GET", path, nil, 404)
	}
	catalogAuditCall(t, p.admin, "DELETE", base+"/cache/41", nil, 200)
	if state := catalogAuditState(t, p.admin); state.Storage.UsedBytes != 0 {
		t.Fatal("deleted cache still counts toward quota", state.Storage)
	}
}
