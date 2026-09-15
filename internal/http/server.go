// Package http wires Captain's HTTP API.
package http

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/zeptop-dev/bosun/pkg/selfupdate"
	"github.com/zeptop-dev/captain/internal/backup"
	"github.com/zeptop-dev/captain/internal/certs"
	"github.com/zeptop-dev/captain/internal/http/mcp"
	"github.com/zeptop-dev/captain/internal/http/oauth"
	"github.com/zeptop-dev/captain/internal/http/probe"
	"github.com/zeptop-dev/captain/internal/http/ratelimit"
	"github.com/zeptop-dev/captain/internal/http/site"
	"github.com/zeptop-dev/captain/internal/mail"
	"github.com/zeptop-dev/captain/internal/notify"
	"github.com/zeptop-dev/captain/internal/telegram"
	"github.com/zeptop-dev/captain/internal/webhook"
	"log/slog"
	"net/http"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/config"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/http/admin"
	"github.com/zeptop-dev/captain/internal/http/agent"
	paymenthttp "github.com/zeptop-dev/captain/internal/http/payment"
	"github.com/zeptop-dev/captain/internal/http/portal"
	"github.com/zeptop-dev/captain/internal/http/sub"
	"github.com/zeptop-dev/captain/internal/metrics"
	"github.com/zeptop-dev/captain/internal/payment"
	"github.com/zeptop-dev/captain/internal/payment/alipay"
	"github.com/zeptop-dev/captain/internal/payment/btcpay"
	"github.com/zeptop-dev/captain/internal/payment/coinbase"
	"github.com/zeptop-dev/captain/internal/payment/coinpayments"
	"github.com/zeptop-dev/captain/internal/payment/epay"
	"github.com/zeptop-dev/captain/internal/payment/mgate"
	"github.com/zeptop-dev/captain/internal/payment/stripe"
	"github.com/zeptop-dev/captain/internal/service"
	"github.com/zeptop-dev/captain/internal/store"
	"github.com/zeptop-dev/captain/web"
)

// Server is the HTTP front.
type Server struct {
	backups  *backup.Manager
	certs    *service.Certs
	external *service.External
	state    *service.AgentState
	probe    *probe.Router
	probeSvc *service.Probe
	hooks    *webhook.Hub
	bot      *telegram.Bot
	subLinks *service.SubLinks
	cfg      *config.Config
	store    *store.Store
	log      *slog.Logger
	mux      *http.ServeMux
}

// Options carries wiring the config alone cannot express (test gateways).
type Options struct {
	Gateways map[string]payment.Gateway // overrides config-built gateways when set
	// CertIssuer replaces the Let's Encrypt issuer (tests).
	CertIssuer certs.Issuer
	// DNSBase overrides the Cloudflare API base (tests).
	DNSBase string
}

// New builds the router.
func New(cfg *config.Config, st *store.Store, log *slog.Logger, opts ...Options) *Server {
	s := &Server{cfg: cfg, store: st, log: log, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "time": time.Now().Unix()})
	})
	sessions := &sessionAuth{store: st}
	gateways := buildGateways(cfg, log)
	var certIssuer certs.Issuer
	var dnsBase string
	if cfg.DataDir != "" {
		certIssuer = &certs.ACMEIssuer{Dir: filepath.Join(cfg.DataDir, "certs-issued"), Staging: cfg.TLS.Staging}
	}
	for _, o := range opts {
		if o.Gateways != nil {
			gateways = o.Gateways
		}
		if o.CertIssuer != nil {
			certIssuer = o.CertIssuer
		}
		if o.DNSBase != "" {
			dnsBase = o.DNSBase
		}
	}
	names := []string{"balance"}
	for name := range gateways {
		names = append(names, name)
	}
	orders := &service.Orders{Store: st, Gateways: gateways, Log: log}
	subSvc := &service.Subscription{Store: st}
	base := strings.TrimRight(cfg.BaseURL, "/")

	logins := ratelimit.New()
	secure := strings.HasPrefix(strings.ToLower(cfg.BaseURL), "https://")
	s.subLinks = &service.SubLinks{Store: st, BaseURL: base}
	mailer := &mail.Loader{Store: st}
	s.bot = &telegram.Bot{Store: st, Log: log, SiteName: cfg.SiteName, PortalURL: base + "/portal/", SubURL: s.subLinks.URL}
	s.hooks = &webhook.Hub{Store: st, Log: log}
	notifier := &notify.Notifier{Store: st, Mail: mailer, Bot: s.bot, Hooks: s.hooks, SiteName: cfg.SiteName, Log: log}
	orders.OnPaid = func(ctx context.Context, o *domain.Order) {
		s.state.Invalidate()
		notifier.Event(ctx, webhook.OrderPaid, map[string]any{"order_no": o.No, "user_id": o.UserID, "plan_id": o.PlanID, "amount_cents": o.AmountCents, "gateway": o.Gateway, "period_days": o.PeriodDays})
		var ts store.TelegramSettings
		_ = st.GetSetting(ctx, store.SettingTelegram, &ts)
		if !ts.NotifyOrders {
			return
		}
		email := ""
		if u, err := st.UserByID(ctx, o.UserID); err == nil {
			email = u.Email
		}
		notifier.Admin(ctx, fmt.Sprintf("💰 Order %s paid: %.2f via %s\n%s", o.No, float64(o.AmountCents)/100, o.Gateway, email))
	}
	s.probeSvc = &service.Probe{Store: st, Notify: notifier, Log: log}
	s.state = &service.AgentState{Store: st, PullSeconds: cfg.Agent.PullSeconds, PushSeconds: cfg.Agent.PushSeconds, EnforceDevices: cfg.EnforceDevices(), Probe: s.probeSvc}
	resolve := func(r *http.Request) *domain.User {
		c, err := r.Cookie("captain_session")
		if err != nil {
			return nil
		}
		u, _, _ := sessions.Resolve(r.Context(), c.Value)
		return u
	}
	s.external = &service.External{Store: st}
	mcp.Register(s.mux, mcp.Deps{Store: st, Probe: s.probeSvc, Log: log, Version: cfg.Version,
		Resolve: func(ctx context.Context, token string) *domain.User {
			u, err := st.UserByAPIToken(ctx, token)
			if err != nil || u == nil || !u.IsStaff() || u.Status != "active" {
				return nil
			}
			return u
		},
		NewUser: func(email, password string) (*domain.User, error) { return admin.NewUser(email, password, "user") },
		OnTicketReply: func(ctx context.Context, t *domain.Ticket, body string) {
			if u, err := st.UserByID(ctx, t.UserID); err == nil {
				notifier.User(ctx, u.ID, u.Email, "Ticket #"+strconv.FormatInt(t.ID, 10)+": "+t.Subject, body)
			}
		},
	})
	if cfg.DataDir != "" {
		s.backups = &backup.Manager{Store: st, Dir: filepath.Join(cfg.DataDir, "backups"), Log: log}
	}
	s.certs = &service.Certs{Store: st, Issuer: certIssuer, Log: log, Notify: notifier}
	s.probe = probe.Register(s.mux, probe.Deps{Store: st, Probe: s.probeSvc, SiteName: cfg.SiteName, Resolve: resolve, Page: web.Probe()})
	admin.Register(s.mux, admin.Deps{Store: st, Log: log, Sessions: sessions, State: s.state, Metrics: s.metricsHandler(st), Backups: s.backups, Certs: s.certs, DNS: &service.DNS{Store: st, Log: log, Base: dnsBase}, BaseURL: base, Version: cfg.Version, Logins: logins, Secure: secure, SubLinks: s.subLinks, Mail: mailer, SiteName: cfg.SiteName, Notify: notifier, Bot: s.bot, Hooks: s.hooks, Probe: s.probeSvc, External: s.external,
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
	// Landing page at the root (custom design under <data_dir>/site wins);
	// unknown paths outside the SPAs fall through to it as well.
	web.Inject = func(r *http.Request) (string, string) {
		var ss site.Settings
		_ = st.GetSetting(r.Context(), site.SettingSite, &ss)
		return ss.InjectHead, ss.InjectBody
	}
	siteHandler := web.Site(filepath.Join(cfg.DataDir, "site"))
	s.mux.Handle("GET /{$}", siteHandler)
	s.mux.Handle("GET /assets/", siteHandler)
	site.Register(s.mux, site.Deps{Store: st, SiteName: cfg.SiteName, Registration: cfg.Portal.Registration, ProbeURL: func(r *http.Request) string {
		ps := s.probeSvc.Settings(r.Context())
		if !ps.Enabled || ps.Visibility == "admins" {
			return ""
		}
		if len(ps.Hosts) > 0 {
			return "https://" + ps.Hosts[0] + "/"
		}
		if ps.Path != "" {
			return ps.Path + "/"
		}
		return ""
	}})
	oauth.Register(s.mux, oauth.Deps{Store: st, Sessions: sessions, Log: log, Hooks: s.hooks, BaseURL: base, Secure: secure, Registration: cfg.Portal.Registration,
		Resolve: func(r *http.Request) *domain.User {
			c, err := r.Cookie("captain_session")
			if err != nil {
				return nil
			}
			u, _, err := sessions.Resolve(r.Context(), c.Value)
			if err != nil {
				return nil
			}
			return u
		}})
	portal.Register(s.mux, portal.Deps{Store: st, Log: log, Sessions: sessions, Orders: orders, Subscription: subSvc, BaseURL: base, Gateways: names, Registration: cfg.Portal.Registration, Logins: logins, Secure: secure, SubLinks: s.subLinks, Mail: mailer, SiteName: cfg.SiteName, Notify: notifier, Bot: s.bot})
	paymenthttp.Register(s.mux, paymenthttp.Deps{Log: log, Orders: orders, ReturnTo: base + "/portal/orders", Gateways: gateways, Store: st})
	sub.Register(s.mux, sub.Deps{Store: st, Log: log, Service: subSvc, Name: cfg.SiteName})
	agent.Register(s.mux, agent.Deps{Pairs: ratelimit.New(),
		Store: st, Log: log, BaseURL: base,
		State: s.state, Probe: s.probeSvc,
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
	paidURL, cancelURL := base+"/portal/orders?paid=1", base+"/portal/orders?cancelled=1"
	add := func(name string, gw payment.Gateway, err error) {
		if err != nil {
			log.Error(name+" disabled", "err", err)
			return
		}
		out[name] = gw
	}
	if a := cfg.Payments.Alipay; a != nil {
		gw, err := alipay.New(alipay.Config{AppID: a.AppID, PrivateKey: a.PrivateKey, PublicKey: a.PublicKey, Subject: a.Subject,
			NotifyURL: base + "/api/payment/alipay/notify", PageURL: base + "/api/payment/alipay/page", ReturnURL: paidURL})
		add("alipay", gw, err)
	}
	if c := cfg.Payments.Coinbase; c != nil {
		gw, err := coinbase.New(coinbase.Config{APIKey: c.APIKey, WebhookSecret: c.WebhookSecret, Currency: c.Currency, RedirectURL: paidURL, CancelURL: cancelURL})
		add("coinbase", gw, err)
	}
	if c := cfg.Payments.CoinPayments; c != nil {
		gw, err := coinpayments.New(coinpayments.Config{MerchantID: c.MerchantID, PublicKey: c.PublicKey, PrivateKey: c.PrivateKey, IPNSecret: c.IPNSecret, Currency: c.Currency,
			NotifyURL: base + "/api/payment/coinpayments/notify", ReturnURL: paidURL, CancelURL: cancelURL})
		add("coinpayments", gw, err)
	}
	if b := cfg.Payments.BTCPay; b != nil {
		gw, err := btcpay.New(btcpay.Config{URL: b.URL, StoreID: b.StoreID, APIKey: b.APIKey, WebhookSecret: b.WebhookSecret, Currency: b.Currency, RedirectURL: paidURL})
		add("btcpay", gw, err)
	}
	if m := cfg.Payments.MGate; m != nil {
		gw, err := mgate.New(mgate.Config{URL: m.URL, AppID: m.AppID, AppSecret: m.AppSecret, SourceCurrency: m.SourceCurrency,
			NotifyURL: base + "/api/payment/mgate/notify", ReturnURL: paidURL})
		add("mgate", gw, err)
	}
	return out
}

// Handler returns the root handler with common middleware.
// Handler returns the root handler. Requests on a subscription-only host
// reach nothing but /sub/.
func (s *Server) Handler() http.Handler {
	h := s.recover(s.logRequests(s.probe.Wrap(s.subLinks.SubscriptionOnly(s.mux))))
	// Any write under /api/admin (whichever package registered it) may
	// change what nodes should run: drop the cached node state after it.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/admin/") && s.state != nil {
			defer s.state.Invalidate()
		}
		h.ServeHTTP(w, r)
	})
}

// Probe exposes the probe service (jobs).
func (s *Server) Probe() *service.Probe { return s.probeSvc }

// SubLinks exposes the subscription link service (autocert host policy).
func (s *Server) SubLinks() *service.SubLinks { return s.subLinks }

// logRequests logs every request at debug and the ones worth a look
// (errors, slow) at info, with a short request id echoed in X-Request-ID.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := newRequestID()
		w.Header().Set("X-Request-ID", id)
		rw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(rw, r)
		ms := time.Since(start).Milliseconds()
		if rw.status >= 400 || ms > 1000 {
			s.log.Info("http", "id", id, "method", r.Method, "path", r.URL.Path, "status", rw.status, "ms", ms, "ip", ratelimit.ClientIP(r))
			return
		}
		s.log.Debug("http", "id", id, "method", r.Method, "path", r.URL.Path, "status", rw.status, "ms", ms)
	})
}

func newRequestID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func (s *Server) recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				s.log.Error("panic", "err", rec, "path", r.URL.Path, "stack", string(debug.Stack()))
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

func (a *sessionAuth) Create(ctx context.Context, userID int64, admin bool) (*domain.Session, error) {
	sess := &domain.Session{ID: newSessionID(), UserID: userID, ExpiresAt: time.Now().Add(sessionTTL), Admin: admin}
	return sess, a.store.CreateSession(ctx, sess)
}

func (a *sessionAuth) Resolve(ctx context.Context, id string) (*domain.User, bool, error) {
	u, admin, err := a.store.SessionUser(ctx, id, time.Now())
	if errors.Is(err, store.ErrNotFound) {
		return nil, false, nil
	}
	return u, admin, err
}

func (a *sessionAuth) Delete(ctx context.Context, id string) error {
	return a.store.DeleteSession(ctx, id)
}

// Bot is the Telegram poller to run alongside the server.
func (s *Server) Bot() *telegram.Bot { return s.bot }

// Hooks exposes the webhook hub.
func (s *Server) Hooks() *webhook.Hub { return s.hooks }

// External exposes the subscription importer (jobs).
func (s *Server) External() *service.External { return s.external }

// State exposes the node desired-state builder (jobs invalidate its cache).
func (s *Server) State() *service.AgentState { return s.state }

// Backups exposes the snapshot manager (jobs); nil without a data dir.
func (s *Server) Backups() *backup.Manager { return s.backups }

// Certs exposes the certificate service (jobs renew through it).
func (s *Server) Certs() *service.Certs { return s.certs }

// metricsHandler serves the Prometheus text exposition for the panel:
// process counters plus gauges read from the database on each scrape.
// Mounted at GET /api/admin/metrics behind the admin auth (an API token
// works for scrapers).
func (s *Server) metricsHandler(st *store.Store) http.Handler {
	reg := metrics.NewRegistry()
	reg.Describe("captain_nodes", "gauge", "paired nodes")
	reg.Describe("captain_nodes_online", "gauge", "nodes that reported within three push intervals")
	reg.Describe("captain_node_last_seen_seconds", "gauge", "seconds since the node last reported")
	reg.Describe("captain_users_active", "gauge", "active user accounts")
	reg.Describe("captain_subscriptions_usable", "gauge", "subscriptions that currently grant access")
	reg.Describe("captain_subscriptions_expiring_7d", "gauge", "active subscriptions expiring within 7 days")
	reg.Describe("captain_orders_pending", "gauge", "orders awaiting payment")
	reg.Describe("captain_backup_last_success_timestamp_seconds", "gauge", "unix time of the last successful database snapshot (0 = never)")
	reg.Describe("captain_backup_last_error", "gauge", "1 when the last backup attempt failed")
	reg.Describe("captain_payment_callback_errors_total", "counter", "gateway notifications rejected (bad signature, amount mismatch)")
	reg.Describe("captain_payment_settle_errors_total", "counter", "verified notifications that failed to settle")
	reg.Describe("captain_job_errors_total", "counter", "housekeeping job failures by job")
	reg.Describe("captain_node_reports_total", "counter", "node reports received")
	reg.Describe("captain_node_state_builds_total", "counter", "full node state builds (cache misses)")
	for _, c := range []*metrics.Counter{metrics.PaymentCallbackErrors, metrics.PaymentSettleErrors, metrics.JobErrors, metrics.NodeReports, metrics.NodeStateBuilds} {
		reg.Add(c.Samples)
	}
	push := time.Duration(s.state.PushSeconds) * time.Second
	if push <= 0 {
		push = time.Minute
	}
	reg.Add(func() []metrics.Sample {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		g, err := st.Gauges(ctx, time.Now(), 3*push)
		if err != nil {
			return nil
		}
		out := []metrics.Sample{
			{Name: "captain_nodes", Value: float64(g.Nodes)},
			{Name: "captain_nodes_online", Value: float64(g.NodesOnline)},
			{Name: "captain_users_active", Value: float64(g.Users)},
			{Name: "captain_subscriptions_usable", Value: float64(g.UsableSubscriptions)},
			{Name: "captain_subscriptions_expiring_7d", Value: float64(g.ExpiringWeek)},
			{Name: "captain_orders_pending", Value: float64(g.PendingOrders)},
		}
		for name, secs := range g.NodeLastSeen {
			out = append(out, metrics.Sample{Name: "captain_node_last_seen_seconds", Labels: map[string]string{"node": name}, Value: secs})
		}
		var bs backup.Status
		_ = st.GetSetting(ctx, backup.StatusKey, &bs)
		last := 0.0
		if !bs.LastAt.IsZero() && bs.LastError == "" {
			last = float64(bs.LastAt.Unix())
		}
		failed := 0.0
		if bs.LastError != "" {
			failed = 1
		}
		out = append(out, metrics.Sample{Name: "captain_backup_last_success_timestamp_seconds", Value: last}, metrics.Sample{Name: "captain_backup_last_error", Value: failed})
		return out
	})
	return reg.Handler()
}
