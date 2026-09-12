// Package http wires Captain's HTTP API.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/zeptop-dev/bosun/pkg/selfupdate"
	"github.com/zeptop-dev/captain/internal/http/ratelimit"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/config"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/http/admin"
	"github.com/zeptop-dev/captain/internal/http/agent"
	paymenthttp "github.com/zeptop-dev/captain/internal/http/payment"
	"github.com/zeptop-dev/captain/internal/http/portal"
	"github.com/zeptop-dev/captain/internal/http/sub"
	"github.com/zeptop-dev/captain/internal/payment"
	"github.com/zeptop-dev/captain/internal/payment/epay"
	"github.com/zeptop-dev/captain/internal/payment/stripe"
	"github.com/zeptop-dev/captain/internal/service"
	"github.com/zeptop-dev/captain/internal/store"
	"github.com/zeptop-dev/captain/web"
)

// Server is the HTTP front.
type Server struct {
	cfg   *config.Config
	store *store.Store
	log   *slog.Logger
	mux   *http.ServeMux
}

// Options carries wiring the config alone cannot express (test gateways).
type Options struct {
	Gateways map[string]payment.Gateway // overrides config-built gateways when set
}

// New builds the router.
func New(cfg *config.Config, st *store.Store, log *slog.Logger, opts ...Options) *Server {
	s := &Server{cfg: cfg, store: st, log: log, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "time": time.Now().Unix()})
	})
	sessions := &sessionAuth{store: st}
	gateways := buildGateways(cfg, log)
	for _, o := range opts {
		if o.Gateways != nil {
			gateways = o.Gateways
		}
	}
	names := []string{"balance"}
	for name := range gateways {
		names = append(names, name)
	}
	orders := &service.Orders{Store: st, Gateways: gateways}
	subSvc := &service.Subscription{Store: st}
	base := strings.TrimRight(cfg.BaseURL, "/")

	logins := ratelimit.New()
	secure := strings.HasPrefix(strings.ToLower(cfg.BaseURL), "https://")
	admin.Register(s.mux, admin.Deps{Store: st, Log: log, Sessions: sessions, Version: cfg.Version, Logins: logins, Secure: secure,
		Updater:       &selfupdate.Client{Repo: "zeptop-dev/captain", Binary: "captain", Version: cfg.Version},
		BosunReleases: &selfupdate.Client{Repo: "zeptop-dev/bosun", Binary: "bosun", Version: "v0.0.0"},
	})
	s.mux.Handle("/admin/", web.Admin("/admin/"))
	s.mux.HandleFunc("GET /admin", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/", http.StatusMovedPermanently)
	})
	s.mux.Handle("/portal/", web.Portal("/portal/"))
	s.mux.HandleFunc("GET /portal", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/portal/", http.StatusMovedPermanently)
	})
	s.mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/portal/", http.StatusFound)
	})
	portal.Register(s.mux, portal.Deps{Store: st, Log: log, Sessions: sessions, Orders: orders, Subscription: subSvc, BaseURL: base, Gateways: names, Registration: cfg.Portal.Registration, Logins: logins, Secure: secure})
	paymenthttp.Register(s.mux, paymenthttp.Deps{Log: log, Orders: orders, ReturnTo: base + "/portal/orders", Gateways: gateways, Store: st})
	sub.Register(s.mux, sub.Deps{Store: st, Log: log, Service: subSvc, Name: cfg.SiteName})
	agent.Register(s.mux, agent.Deps{
		Store: st, Log: log,
		State: &service.AgentState{Store: st, PullSeconds: cfg.Agent.PullSeconds, PushSeconds: cfg.Agent.PushSeconds, EnforceDevices: cfg.EnforceDevices()},
	})
	return s
}

// buildGateways instantiates the gateways enabled in the config.
func buildGateways(cfg *config.Config, log *slog.Logger) map[string]payment.Gateway {
	base := strings.TrimRight(cfg.BaseURL, "/")
	out := map[string]payment.Gateway{}
	if e := cfg.Payments.EPay; e != nil {
		gw, err := epay.New(epay.Config{Version: e.Version, URL: e.URL, PID: e.PID, Key: e.Key, Type: e.Type,
			MerchantPrivateKey: e.MerchantPrivateKey, PlatformPublicKey: e.PlatformPublicKey,
			NotifyURL: base + "/api/payment/epay/notify", ReturnURL: base + "/api/payment/epay/return"})
		if err != nil {
			log.Error("epay disabled", "err", err)
		} else {
			out["epay"] = gw
		}
	}
	if st := cfg.Payments.Stripe; st != nil {
		gw, err := stripe.New(stripe.Config{SecretKey: st.SecretKey, WebhookSecret: st.WebhookSecret, Currency: st.Currency,
			SuccessURL: base + "/portal/orders?paid=1", CancelURL: base + "/portal/orders?cancelled=1"})
		if err != nil {
			log.Error("stripe disabled", "err", err)
		} else {
			out["stripe"] = gw
		}
	}
	return out
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
