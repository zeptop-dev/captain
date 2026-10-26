package http

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	cc "github.com/zeptop-dev/captain/internal/clientcatalog"
	"github.com/zeptop-dev/captain/internal/config"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

type catalogBlockedSource struct {
	entered, release chan struct{}
	check            bool
}

func (s *catalogBlockedSource) Check(ctx context.Context, _ string) (*cc.Release, bool, error) {
	if s.check {
		close(s.entered)
		select {
		case <-s.release:
		case <-ctx.Done():
			return nil, false, ctx.Err()
		}
	}
	return &cc.Release{Version: "v1.2.3", URL: "https://github.com/clash-verge-rev/clash-verge-rev/releases/tag/v1.2.3", PublishedAt: time.Now().UTC(), Assets: []cc.Asset{{ID: 12, Name: "Example_x64-setup.exe", Platform: "windows", Arch: "x64", PackageType: "exe", Size: 7, URL: "https://github.com/clash-verge-rev/clash-verge-rev/releases/download/v1.2.3/Example_x64-setup.exe", Version: "v1.2.3"}}}, false, nil
}
func (s *catalogBlockedSource) Download(ctx context.Context, _ cc.Asset, w io.Writer) (string, error) {
	close(s.entered)
	select {
	case <-s.release:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	_, e := w.Write([]byte("fixture"))
	return fmt.Sprintf("%x", sha256.Sum256([]byte("fixture"))), e
}
func catalogSecurityRig(t *testing.T, source cc.Source) (*store.Store, *client, int64, int64) {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.BaseURL = "http://example.com"
	conn, e := db.Open("sqlite", filepath.Join(cfg.DataDir, "captain.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { conn.Close() })
	if e = db.Migrate(context.Background(), conn, "sqlite"); e != nil {
		t.Fatal(e)
	}
	st := store.New(conn)
	u := &domain.User{Email: "admin@example.com", Role: domain.RoleAdmin, Status: "active"}
	if e = st.CreateStaff(context.Background(), u); e != nil {
		t.Fatal(e)
	}
	token, info, e := st.CreateAPIToken(context.Background(), u.ID, "test", "full", nil)
	if e != nil {
		t.Fatal(e)
	}
	srv := httptest.NewServer(New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), Options{ClientCatalogSource: source}).Handler())
	t.Cleanup(srv.Close)
	return st, &client{t: t, srv: srv, token: token}, u.ID, info.ID
}
func TestClientCatalogDownloadReauthorizesAfterRemoteIO(t *testing.T) {
	for _, scenario := range []string{"token_revoked", "staff_banned", "session_revoked", "reset", "catalog_retired"} {
		t.Run(scenario, func(t *testing.T) {
			src := &catalogBlockedSource{entered: make(chan struct{}), release: make(chan struct{})}
			st, c, uid, tid := catalogSecurityRig(t, src)
			ctx := context.Background()
			if status, b, _ := c.do("POST", "/api/admin/settings/client-catalog/clash-verge-rev/check", nil, nil); status != 200 {
				t.Fatalf("check %d %s", status, b)
			}
			if scenario == "session_revoked" {
				session, e := (&sessionAuth{store: st}).Create(ctx, uid, true)
				if e != nil {
					t.Fatal(e)
				}
				c.token = ""
				c.cookie = &http.Cookie{Name: "captain_session", Value: session.ID}
			}
			if status, b, _ := c.do("POST", "/api/admin/settings/client-catalog/clash-verge-rev/download", map[string]any{"asset_id": 12}, nil); status != 202 {
				t.Fatalf("download %d %s", status, b)
			}
			select {
			case <-src.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("download did not start")
			}
			if status, _, _ := c.do("POST", "/api/admin/settings/client-catalog/clash-verge-rev/download", map[string]any{"asset_id": 12}, nil); status != 409 {
				t.Fatal("overlap accepted", status)
			}
			if !st.Accounts.TryLock() {
				t.Fatal("download holds Accounts during network")
			}
			var e error
			switch scenario {
			case "token_revoked":
				e = st.DeleteAPIToken(ctx, uid, tid)
			case "staff_banned":
				e = st.UpdateStaff(ctx, uid, "banned", "")
			case "session_revoked":
				e = st.DeleteStaffSessions(ctx, uid)
			case "reset":
				_, e = st.ResetSite(ctx, uid)
			case "catalog_retired":
				_, e = st.DB().ExecContext(ctx, `DELETE FROM client_catalog`)
			}
			st.Accounts.Unlock()
			if e != nil {
				t.Fatal(e)
			}
			close(src.release)
			// Observe via a separately authorized administrator; the revoked caller is
			// not permitted to inspect the final task state either.
			observer := &domain.User{Email: "observer@example.com", Role: domain.RoleAdmin, Status: "active"}
			if e = st.CreateStaff(ctx, observer); e != nil {
				t.Fatal(e)
			}
			token, _, e := st.CreateAPIToken(ctx, observer.ID, "observe", "full", nil)
			if e != nil {
				t.Fatal(e)
			}
			view := &client{t: t, srv: c.srv, token: token}
			deadline := time.Now().Add(5 * time.Second)
			var result cc.CatalogView
			for time.Now().Before(deadline) {
				status, b, _ := view.do("GET", "/api/admin/settings/client-catalog", nil, nil)
				if status != 200 {
					t.Fatalf("view %d %s", status, b)
				}
				result = mustJSON[cc.CatalogView](t, b)
				if result.Job != nil && result.Job.State != "downloading" {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if result.Job == nil || result.Job.State != "failed" {
				t.Fatalf("revoked task did not fail: %+v", result.Job)
			}
			for _, v := range result.Clients {
				if len(v.CachedAssets) > 0 || len(v.Published) > 0 {
					t.Fatalf("revoked task published/cache: %+v", v)
				}
			}
		})
	}
}
func TestClientCatalogCheckReauthorizesAndPreservesOldSnapshot(t *testing.T) {
	src := &catalogBlockedSource{entered: make(chan struct{}), release: make(chan struct{}), check: true}
	st, c, uid, tid := catalogSecurityRig(t, src)
	done := make(chan int, 1)
	go func() {
		status, _, _ := c.do("POST", "/api/admin/settings/client-catalog/clash-verge-rev/check", nil, nil)
		done <- status
	}()
	select {
	case <-src.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("check not started")
	}
	if status, _, _ := c.do("POST", "/api/admin/settings/client-catalog/clash-verge-rev/check", nil, nil); status != 409 {
		t.Fatal("overlap accepted", status)
	}
	if !st.Accounts.TryLock() {
		t.Fatal("check held Accounts")
	}
	e := st.DeleteAPIToken(context.Background(), uid, tid)
	st.Accounts.Unlock()
	if e != nil {
		t.Fatal(e)
	}
	close(src.release)
	if status := <-done; status != 403 {
		t.Fatal("revoked check committed", status)
	}
	saved, e := st.ClientCatalog(context.Background(), "clash-verge-rev")
	if e != nil || saved.Release != nil {
		t.Fatal("revoked result cached", saved, e)
	}
}
