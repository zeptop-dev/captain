package http

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/clauderules"
	"github.com/zeptop-dev/captain/internal/config"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/http/admin"
	"github.com/zeptop-dev/captain/internal/store"
)

// pausedClaudeSessions pauses after the final authorization has read the
// requesting administrator, but before requireAdmin can act on that snapshot.
// Other sessions continue to resolve live account state normally.
type pausedClaudeSessions struct {
	store      *store.Store
	syncID     int64
	operatorID int64
	calls      atomic.Int32
	resolved   chan struct{}
	resume     chan struct{}
}

func (s *pausedClaudeSessions) Create(context.Context, int64, bool) (*domain.Session, error) {
	return nil, fmt.Errorf("session creation is not used by this test")
}

func (s *pausedClaudeSessions) Delete(context.Context, string) error { return nil }

func (s *pausedClaudeSessions) Resolve(ctx context.Context, session string) (*domain.User, bool, error) {
	id := s.operatorID
	if session == "sync-admin" {
		id = s.syncID
	}
	u, err := s.store.StaffByID(ctx, id)
	if err != nil {
		return nil, false, err
	}
	if session == "sync-admin" && s.calls.Add(1) == 2 {
		close(s.resolved)
		select {
		case <-s.resume:
		case <-ctx.Done():
			return nil, false, ctx.Err()
		}
	}
	return u, true, nil
}

func TestClaudeSyncFinalAuthorizationSerializesWithHTTPRevocation(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "captain.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := db.Migrate(ctx, conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	st := store.New(conn)
	requester := &domain.User{Email: "requester@example.com", Role: domain.RoleAdmin, Status: "active"}
	operator := &domain.User{Email: "operator@example.com", Role: domain.RoleAdmin, Status: "active"}
	for _, u := range []*domain.User{requester, operator} {
		if err := st.CreateStaff(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	sessions := &pausedClaudeSessions{store: st, syncID: requester.ID, operatorID: operator.ID, resolved: make(chan struct{}), resume: make(chan struct{})}
	var resumeOnce sync.Once
	resume := func() { resumeOnce.Do(func() { close(sessions.resume) }) }
	defer resume()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Default()
	cfg.BaseURL = "http://example.com"
	s := New(cfg, st, log)
	// Use the production root middleware, routing and staff mutation handlers;
	// only session resolution is replaced to expose the otherwise tiny window.
	s.mux = http.NewServeMux()
	admin.Register(s.mux, admin.Deps{Store: st, Log: log, Sessions: sessions, State: s.state, BaseURL: cfg.BaseURL,
		ClaudeRules: claudeFetchFunc(func(context.Context) (clauderules.Snapshot, error) { return testClaudeSnapshot("a"), nil }),
	})
	root := s.Handler()
	revokeEntered := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			close(revokeEntered)
		}
		root.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	syncClient := &client{t: t, srv: srv, cookie: &http.Cookie{Name: "captain_session", Value: "sync-admin"}}
	revokeClient := &client{t: t, srv: srv, cookie: &http.Cookie{Name: "captain_session", Value: "other-admin"}}
	syncDone, revokeDone := make(chan int, 1), make(chan int, 1)
	go func() {
		status, _, _ := syncClient.do(http.MethodPost, claudeRulesPath+"/sync", nil, nil)
		syncDone <- status
	}()
	select {
	case <-sessions.resolved:
	case <-time.After(5 * time.Second):
		t.Fatal("final authorization did not reach the account snapshot")
	}
	go func() {
		status, _, _ := revokeClient.do(http.MethodPatch, fmt.Sprintf("/api/admin/admins/%d", requester.ID), map[string]string{"status": "banned"}, nil)
		revokeDone <- status
	}()
	select {
	case <-revokeEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("revocation request did not arrive")
	}
	var revokedBeforeCommit bool
	var revokeStatus int
	select {
	case revokeStatus = <-revokeDone:
		revokedBeforeCommit = true
	case <-time.After(250 * time.Millisecond):
		// Exclusive finalization must hold the real HTTP revocation until the
		// authorization snapshot and publication finish together.
	}
	resume()
	var syncStatus int
	select {
	case syncStatus = <-syncDone:
	case <-time.After(5 * time.Second):
		t.Fatal("sync did not finish")
	}
	if !revokedBeforeCommit {
		select {
		case revokeStatus = <-revokeDone:
		case <-time.After(5 * time.Second):
			t.Fatal("revocation did not finish")
		}
	}
	if syncStatus != http.StatusOK || revokeStatus != http.StatusOK {
		t.Fatalf("unexpected statuses: sync=%d revoke=%d", syncStatus, revokeStatus)
	}
	user, err := st.StaffByID(ctx, requester.ID)
	if err != nil || user.Status != "banned" {
		t.Fatal("real staff mutation did not revoke the requester", err)
	}
	var cached store.ClaudeRuleLibrary
	if err := st.GetSetting(ctx, store.SettingClaudeRuleLibrary, &cached); err != nil || cached.Snapshot == nil {
		t.Fatal("sync did not publish its snapshot", err)
	}
	if revokedBeforeCommit {
		t.Fatal("HTTP revocation completed after final authorization read but before rule publication; stale administrator credentials still wrote the cache")
	}
}
