// Package http wires Captain's HTTP API.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"gitlab.com/boyang-hu/captain/internal/config"
	"gitlab.com/boyang-hu/captain/internal/domain"
	"gitlab.com/boyang-hu/captain/internal/http/admin"
	"gitlab.com/boyang-hu/captain/internal/http/agent"
	"gitlab.com/boyang-hu/captain/internal/http/sub"
	"gitlab.com/boyang-hu/captain/internal/service"
	"gitlab.com/boyang-hu/captain/internal/store"
)

// Server is the HTTP front.
type Server struct {
	cfg   *config.Config
	store *store.Store
	log   *slog.Logger
	mux   *http.ServeMux
}

// New builds the router.
func New(cfg *config.Config, st *store.Store, log *slog.Logger) *Server {
	s := &Server{cfg: cfg, store: st, log: log, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "time": time.Now().Unix()})
	})
	sessions := &sessionAuth{store: st}
	admin.Register(s.mux, admin.Deps{Store: st, Log: log, Sessions: sessions})
	sub.Register(s.mux, sub.Deps{Store: st, Log: log, Service: &service.Subscription{Store: st}})
	agent.Register(s.mux, agent.Deps{
		Store: st, Log: log,
		State: &service.AgentState{Store: st, PullSeconds: cfg.Agent.PullSeconds, PushSeconds: cfg.Agent.PushSeconds},
	})
	return s
}

// Handler returns the root handler with common middleware.
func (s *Server) Handler() http.Handler {
	return s.recover(s.logRequests(s.mux))
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(rw, r)
		s.log.Debug("http", "method", r.Method, "path", r.URL.Path, "status", rw.status, "ms", time.Since(start).Milliseconds())
	})
}

func (s *Server) recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("panic", "err", rec, "path", r.URL.Path)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) { w.status = code; w.ResponseWriter.WriteHeader(code) }

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// sessionAuth implements admin.SessionStore over the store.
type sessionAuth struct{ store *store.Store }

const sessionTTL = 30 * 24 * time.Hour

func (a *sessionAuth) Create(ctx context.Context, userID int64) (*domain.Session, error) {
	sess := &domain.Session{ID: newSessionID(), UserID: userID, ExpiresAt: time.Now().Add(sessionTTL)}
	return sess, a.store.CreateSession(ctx, sess)
}

func (a *sessionAuth) Resolve(ctx context.Context, id string) (*domain.User, error) {
	u, err := a.store.SessionUser(ctx, id, time.Now())
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	return u, err
}

func (a *sessionAuth) Delete(ctx context.Context, id string) error {
	return a.store.DeleteSession(ctx, id)
}
