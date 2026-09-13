// Package admin serves the admin console API under /api/admin.
package admin

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/zeptop-dev/bosun/pkg/selfupdate"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/backup"
	"github.com/zeptop-dev/captain/internal/http/ratelimit"
	"github.com/zeptop-dev/captain/internal/http/site"
	"github.com/zeptop-dev/captain/internal/mail"
	"github.com/zeptop-dev/captain/internal/notify"
	"github.com/zeptop-dev/captain/internal/service"
	"github.com/zeptop-dev/captain/internal/telegram"
	"github.com/zeptop-dev/captain/internal/webhook"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/auth"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

// SessionStore is the cookie session backend.
type SessionStore interface {
	Create(ctx context.Context, userID int64) (*domain.Session, error)
	Resolve(ctx context.Context, id string) (*domain.User, error)
	Delete(ctx context.Context, id string) error
}

// Deps are the handlers' dependencies.
type Deps struct {
	Store    *store.Store
	Log      *slog.Logger
	Sessions SessionStore
	Version  string
	// Updater checks/applies Captain's own releases; BosunReleases only
	// looks up the latest bosun tag for the node list. Either may be nil.
	Updater       *selfupdate.Client
	BosunReleases *selfupdate.Client
	// Mail reads the mail settings; nil disables mail features.
	Mail     *mail.Loader
	SiteName string
	// BaseURL is the panel's public origin (webhook URLs shown to admins).
	BaseURL string
	// SubLinks builds user subscription URLs; nil falls back to nothing.
	SubLinks *service.SubLinks
	// Logins throttles failed sign-ins per client address; nil disables.
	Logins *ratelimit.Limiter
	// Secure marks session cookies HTTPS-only (base_url is https).
	Secure bool
	// Notify reaches users (Telegram/mail); nil disables.
	Notify *notify.Notifier
	// Backups runs database snapshots; nil hides the backup card.
	Backups *backup.Manager
	// Certs issues panel-managed certificates; nil = uploads/webhooks only.
	Certs *service.Certs
	// DNS keeps Cloudflare records in step with node and entry names.
	DNS *service.DNS
	// Bot exposes the Telegram settings cache; nil disables.
	Bot *telegram.Bot
	// Hooks is the webhook hub (settings cache invalidation, test delivery).
	Hooks *webhook.Hub
	// Probe is the monitoring service (settings cache, live data).
	Probe *service.Probe
	// External syncs airport subscriptions into external nodes.
	External *service.External
}

const cookieName = "captain_session"

type handlers struct{ Deps }

// Register mounts the admin routes.
func Register(mux *http.ServeMux, d Deps) {
	h := &handlers{d}
	mux.HandleFunc("POST /api/admin/login", h.login)
	mux.HandleFunc("POST /api/admin/logout", h.logout)
	mux.HandleFunc("GET /api/admin/me", h.requireAdmin(h.me))
	mux.HandleFunc("GET /api/admin/dashboard", h.requireAdmin(h.dashboard))

	mux.HandleFunc("GET /api/admin/nodes", h.requireAdmin(h.listNodes))
	mux.HandleFunc("POST /api/admin/nodes", h.requireAdmin(h.createNode))
	mux.HandleFunc("GET /api/admin/nodes/{id}", h.requireAdmin(h.getNode))
	mux.HandleFunc("PATCH /api/admin/nodes/{id}", h.requireAdmin(h.updateNode))
	mux.HandleFunc("DELETE /api/admin/nodes/{id}", h.requireAdmin(h.deleteNode))
	mux.HandleFunc("POST /api/admin/nodes/{id}/repair", h.requireAdmin(h.repairNode))
	mux.HandleFunc("POST /api/admin/nodes/{id}/upgrade", h.requireAdmin(h.upgradeNode))
	mux.HandleFunc("POST /api/admin/nodes/upgrade-all", h.requireAdmin(h.upgradeAllNodes))
	h.registerOps(mux)
	h.registerExternal(mux)
	h.registerSpeedtest(mux)
	h.registerBackup(mux)
	h.registerCertificates(mux)
	h.registerDomains(mux)
	h.registerIngresses(mux)
	h.registerSubTemplates(mux)
	mux.HandleFunc("GET /api/admin/coupons", h.requireAdmin(h.listCoupons))
	mux.HandleFunc("POST /api/admin/coupons", h.requireAdmin(h.createCoupon))
	mux.HandleFunc("PATCH /api/admin/coupons/{id}", h.requireAdmin(h.updateCoupon))
	mux.HandleFunc("DELETE /api/admin/coupons/{id}", h.requireAdmin(h.deleteCoupon))
	mux.HandleFunc("GET /api/admin/settings/invite", h.requireAdmin(h.getInvite))
	mux.HandleFunc("PUT /api/admin/settings/invite", h.requireAdmin(h.putInvite))
	mux.HandleFunc("GET /api/admin/settings/surplus", h.requireAdmin(h.getSurplus))
	mux.HandleFunc("PUT /api/admin/settings/surplus", h.requireAdmin(h.putSurplus))
	mux.HandleFunc("GET /api/admin/withdrawals", h.requireAdmin(h.listWithdrawals))
	mux.HandleFunc("POST /api/admin/withdrawals/{id}/status", h.requireAdmin(h.withdrawalStatus))
	mux.HandleFunc("GET /api/admin/settings/notice", h.requireAdmin(h.getNotice))
	mux.HandleFunc("PUT /api/admin/settings/notice", h.requireAdmin(h.putNotice))
	mux.HandleFunc("GET /api/admin/settings/registration", h.requireAdmin(h.getRegistration))
	mux.HandleFunc("PUT /api/admin/settings/registration", h.requireAdmin(h.putRegistration))
	mux.HandleFunc("GET /api/admin/settings/mail", h.requireAdmin(h.getMail))
	mux.HandleFunc("PUT /api/admin/settings/mail", h.requireAdmin(h.putMail))
	mux.HandleFunc("POST /api/admin/settings/mail/test", h.requireAdmin(h.testMail))
	mux.HandleFunc("GET /api/admin/settings/site", h.requireAdmin(h.getSite))
	mux.HandleFunc("PUT /api/admin/settings/site", h.requireAdmin(h.putSite))
	mux.HandleFunc("GET /api/admin/settings/oidc", h.requireAdmin(h.getOIDC))
	mux.HandleFunc("PUT /api/admin/settings/oidc", h.requireAdmin(h.putOIDC))
	mux.HandleFunc("GET /api/admin/settings/subscription", h.requireAdmin(h.getSubscription))
	mux.HandleFunc("PUT /api/admin/settings/subscription", h.requireAdmin(h.putSubscription))
	mux.HandleFunc("GET /api/admin/settings/acme", h.requireAdmin(h.getACME))
	mux.HandleFunc("PUT /api/admin/settings/acme", h.requireAdmin(h.putACME))
	mux.HandleFunc("GET /api/admin/system/update", h.requireAdmin(h.systemUpdate))
	mux.HandleFunc("POST /api/admin/system/update/apply", h.requireAdmin(h.systemUpdateApply))
	mux.HandleFunc("POST /api/admin/system/update/rollback", h.requireAdmin(h.systemUpdateRollback))
	mux.HandleFunc("POST /api/admin/system/restart", h.requireAdmin(h.systemRestart))
	mux.HandleFunc("POST /api/admin/nodes/{id}/inbounds", h.requireAdmin(h.createInbound))
	mux.HandleFunc("PATCH /api/admin/inbounds/{id}", h.requireAdmin(h.updateInbound))
	mux.HandleFunc("DELETE /api/admin/inbounds/{id}", h.requireAdmin(h.deleteInbound))

	mux.HandleFunc("GET /api/admin/users", h.requireAdmin(h.listUsers))
	mux.HandleFunc("POST /api/admin/users", h.requireAdmin(h.createUser))
	mux.HandleFunc("GET /api/admin/users/{id}", h.requireAdmin(h.getUser))
	mux.HandleFunc("PATCH /api/admin/users/{id}", h.requireAdmin(h.updateUser))
	mux.HandleFunc("DELETE /api/admin/users/{id}", h.requireAdmin(h.deleteUser))
	mux.HandleFunc("POST /api/admin/users/{id}/grant", h.requireAdmin(h.grantPlan))
	mux.HandleFunc("POST /api/admin/users/{id}/balance", h.requireAdmin(h.adjustBalance))
	mux.HandleFunc("POST /api/admin/users/{id}/rotate-token", h.requireAdmin(h.rotateToken))

	mux.HandleFunc("GET /api/admin/plans", h.requireAdmin(h.listPlans))
	mux.HandleFunc("POST /api/admin/plans", h.requireAdmin(h.createPlan))
	mux.HandleFunc("PATCH /api/admin/plans/{id}", h.requireAdmin(h.updatePlan))
	mux.HandleFunc("DELETE /api/admin/plans/{id}", h.requireAdmin(h.deletePlan))

	mux.HandleFunc("GET /api/admin/groups", h.requireAdmin(h.listGroups))
	mux.HandleFunc("POST /api/admin/groups", h.requireAdmin(h.createGroup))

	mux.HandleFunc("GET /api/admin/entries", h.requireAdmin(h.listEntries))
	mux.HandleFunc("POST /api/admin/entries", h.requireAdmin(h.createEntry))
	mux.HandleFunc("PATCH /api/admin/entries/{id}", h.requireAdmin(h.updateEntry))
	mux.HandleFunc("DELETE /api/admin/entries/{id}", h.requireAdmin(h.deleteEntry))
	mux.HandleFunc("PUT /api/admin/entries/order", h.requireAdmin(h.reorderEntries))
	mux.HandleFunc("GET /api/admin/entries/tags", h.requireAdmin(h.entryTags))

	mux.HandleFunc("GET /api/admin/orders", h.requireAdmin(h.listOrders))
}

type ctxKey struct{}

func userFrom(r *http.Request) *domain.User {
	u, _ := r.Context().Value(ctxKey{}).(*domain.User)
	return u
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

func decode(r *http.Request, v any) bool { return json.NewDecoder(r.Body).Decode(v) == nil }

func (h *handlers) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var u *domain.User
		if tok := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer")); tok != "" && strings.HasPrefix(tok, "cap_") {
			// Personal API token (scripts, MCP): same role as its owner.
			u, _ = h.Store.UserByAPIToken(r.Context(), tok)
		} else {
			c, err := r.Cookie(cookieName)
			if err != nil {
				fail(w, http.StatusUnauthorized, "not logged in")
				return
			}
			u, err = h.Sessions.Resolve(r.Context(), c.Value)
			if err != nil {
				h.Log.Error("session", "err", err)
				fail(w, http.StatusInternalServerError, "internal error")
				return
			}
		}
		if u == nil || !u.IsStaff() || u.Status != "active" {
			fail(w, http.StatusForbidden, "admin only")
			return
		}
		if !allowed(u.Role, r.Method, r.URL.Path) {
			fail(w, http.StatusForbidden, "your role cannot do that")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
	}
}

// --- auth ---

func (h *handlers) login(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Password, Code string }
	if !decode(r, &in) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	ip := ratelimit.ClientIP(r)
	if h.Logins != nil {
		if allowed, wait := h.Logins.Allow(ip); !allowed {
			fail(w, http.StatusTooManyRequests, "too many failed attempts; try again in "+wait.String())
			return
		}
	}
	u, err := h.Store.UserByEmail(r.Context(), in.Email)
	if errors.Is(err, store.ErrNotFound) || (err == nil && !auth.VerifyPassword(u.PasswordHash, in.Password)) {
		if h.Logins != nil {
			h.Logins.Fail(ip)
		}
		fail(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if h.Logins != nil {
		h.Logins.Reset(ip)
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !u.IsStaff() {
		fail(w, http.StatusForbidden, "admin only")
		return
	}
	// Second factor: the password alone is not enough once TOTP is on.
	if secret, enabled, _ := h.Store.TOTP(r.Context(), u.ID); enabled {
		if strings.TrimSpace(in.Code) == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusPreconditionRequired)
			_ = json.NewEncoder(w).Encode(map[string]any{"totp": true, "error": "authenticator code required"})
			return
		}
		if !auth.VerifyTOTP(secret, in.Code, time.Now()) {
			if h.Logins != nil {
				h.Logins.Fail(ip)
			}
			fail(w, http.StatusUnauthorized, "invalid authenticator code")
			return
		}
	}
	sess, err := h.Sessions.Create(r.Context(), u.ID)
	if err != nil {
		fail(w, http.StatusInternalServerError, "internal error")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: sess.ID, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: h.Secure, Expires: sess.ExpiresAt})
	ok(w, map[string]any{"id": u.ID, "email": u.Email, "role": u.Role})
}

func (h *handlers) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		_ = h.Sessions.Delete(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", HttpOnly: true, MaxAge: -1})
	ok(w, map[string]bool{"ok": true})
}

func (h *handlers) me(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	_, totp, _ := h.Store.TOTP(r.Context(), u.ID)
	ok(w, map[string]any{"id": u.ID, "email": u.Email, "role": u.Role, "version": h.Version, "totp": totp})
}

func (h *handlers) dashboard(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	stats, err := h.Store.Dashboard(r.Context(), now)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	series, err := h.Store.TrafficSeries(r.Context(), now, 14)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	open, _ := h.Store.OpenTickets(r.Context())
	ok(w, map[string]any{"stats": stats, "traffic": series, "open_tickets": open})
}

// --- nodes ---

type nodeView struct {
	ID           int64      `json:"id"`
	Name         string     `json:"name"`
	PublicAddr   string     `json:"public_addr"`
	InternalAddr string     `json:"internal_addr"`
	V6Addr       string     `json:"v6_addr"`
	Domain       string     `json:"domain"`
	MonitorURL   string     `json:"monitor_url"`
	Version      string     `json:"version"`
	Platform     string     `json:"platform"`
	Hostname     string     `json:"hostname"`
	LastSeenAt   *time.Time `json:"last_seen_at"`
	Online       bool       `json:"online"`
	Paired       bool       `json:"paired"`
	PairCode     string     `json:"pair_code,omitempty"`
	TrafficToday int64      `json:"traffic_today_bytes"`
	Inbounds     int        `json:"inbounds"`
	UpgradeTo    string     `json:"upgrade_to,omitempty"` // pending upgrade request
	Outdated     bool       `json:"outdated"`             // reported version older than the latest bosun release
	CertProblem  bool       `json:"cert_problem"`         // an automatic certificate failed or expires soon
}

func toNodeView(n *domain.Node, at time.Time) nodeView {
	return nodeView{
		ID: n.ID, Name: n.Name, PublicAddr: n.PublicAddr, InternalAddr: n.InternalAddr, V6Addr: n.V6Addr, Domain: n.Domain, MonitorURL: n.MonitorURL,
		Version: n.Version, Platform: n.Platform, Hostname: n.Hostname, LastSeenAt: n.LastSeenAt,
		Online: n.LastSeenAt != nil && at.Sub(*n.LastSeenAt) < 3*time.Minute, Paired: n.Paired, PairCode: n.PairCode,
		UpgradeTo: n.UpgradeTo,
	}
}

// bosunLatest returns the newest bosun release tag, "" when unknown.
func (h *handlers) bosunLatest(ctx context.Context) string {
	if h.BosunReleases == nil {
		return ""
	}
	info := h.BosunReleases.Check(ctx, false)
	if info.Warning != "" && info.Latest == h.BosunReleases.Version {
		return ""
	}
	return info.Latest
}

func (h *handlers) listNodes(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	nodes, err := h.Store.ListNodes(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	traffic, _ := h.Store.NodeTrafficToday(r.Context(), now)
	latest := h.bosunLatest(r.Context())
	certProblems, _ := h.Store.CertProblems(r.Context(), now)
	out := make([]nodeView, 0, len(nodes))
	for _, n := range nodes {
		v := toNodeView(n, now)
		v.PairCode = "" // only shown on create/repair
		v.TrafficToday = traffic[n.ID]
		v.Outdated = latest != "" && n.Version != "" && selfupdate.Newer(latest, n.Version)
		v.CertProblem = certProblems[n.ID]
		if ibs, err := h.Store.AllInboundsByNode(r.Context(), n.ID); err == nil {
			v.Inbounds = len(ibs)
		}
		out = append(out, v)
	}
	ok(w, out)
}

type nodeInput struct {
	Name, PublicAddr, InternalAddr, V6Addr, Domain, MonitorURL string
}

func (h *handlers) createNode(w http.ResponseWriter, r *http.Request) {
	var in nodeInput
	if !decode(r, &in) || in.Name == "" {
		fail(w, http.StatusBadRequest, "name is required")
		return
	}
	n := &domain.Node{Name: in.Name, PublicAddr: in.PublicAddr, InternalAddr: in.InternalAddr, V6Addr: in.V6Addr, Domain: strings.ToLower(strings.TrimSpace(in.Domain)), MonitorURL: in.MonitorURL}
	if err := h.Store.CreateNode(r.Context(), n, auth.PairCode(), 24*time.Hour); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	v := toNodeView(n, time.Now()) // includes the pairing code once
	ok(w, struct {
		nodeView
		DNS []service.Result `json:"dns,omitempty"`
	}{v, h.DNS.EnsureMany(r.Context(), [2]string{n.Domain, n.PublicAddr}, [2]string{n.Domain, n.V6Addr})})
}

func (h *handlers) getNode(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	if !okID {
		fail(w, http.StatusBadRequest, "bad id")
		return
	}
	n, err := h.Store.NodeByID(r.Context(), id)
	if err != nil {
		fail(w, http.StatusNotFound, "node not found")
		return
	}
	v := toNodeView(n, time.Now())
	if n.Paired {
		v.PairCode = ""
	}
	inbounds, _ := h.Store.AllInboundsByNode(r.Context(), id)
	if inbounds == nil {
		inbounds = []*domain.Inbound{}
	}
	ingresses, _ := h.Store.IngressesByNode(r.Context(), id)
	status, _ := h.Store.NodeStatus(r.Context(), id)
	ok(w, map[string]any{"node": v, "inbounds": inbounds, "ingresses": ingresses, "status": status})
}

func (h *handlers) updateNode(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	var in nodeInput
	if !okID || !decode(r, &in) || in.Name == "" {
		fail(w, http.StatusBadRequest, "name is required")
		return
	}
	n := &domain.Node{ID: id, Name: in.Name, PublicAddr: in.PublicAddr, InternalAddr: in.InternalAddr, V6Addr: in.V6Addr, Domain: strings.ToLower(strings.TrimSpace(in.Domain)), MonitorURL: in.MonitorURL}
	if err := h.Store.UpdateNode(r.Context(), n); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]any{"ok": true, "dns": h.DNS.EnsureMany(r.Context(), [2]string{n.Domain, n.PublicAddr}, [2]string{n.Domain, n.V6Addr})})
}

func (h *handlers) deleteNode(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	if !okID {
		fail(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := h.Store.DeleteNode(r.Context(), id); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]bool{"ok": true})
}

// repairNode issues a new pairing code and revokes the old token.
func (h *handlers) repairNode(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	if !okID {
		fail(w, http.StatusBadRequest, "bad id")
		return
	}
	code := auth.PairCode()
	if err := h.Store.ResetPairCode(r.Context(), id, code, 24*time.Hour); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]string{"pair_code": code})
}

func (h *handlers) createInbound(w http.ResponseWriter, r *http.Request) {
	nodeID, okID := pathID(r)
	var ib domain.Inbound
	if !okID || !decode(r, &ib) || ib.Tag == "" || ib.Protocol == "" || ib.Port == 0 {
		fail(w, http.StatusBadRequest, "tag, protocol and port are required")
		return
	}
	ib.NodeID = nodeID
	ib.Enabled = true
	if msg := h.checkIngress(r, &ib); msg != "" {
		fail(w, http.StatusBadRequest, msg)
		return
	}
	if err := h.Store.CreateInbound(r.Context(), &ib); err != nil {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	ok(w, ib)
}

func (h *handlers) updateInbound(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	cur, err := h.Store.InboundByID(r.Context(), id)
	if !okID || err != nil {
		fail(w, http.StatusNotFound, "inbound not found")
		return
	}
	ib := *cur
	if !decode(r, &ib) || ib.Tag == "" || ib.Protocol == "" || ib.Port == 0 {
		fail(w, http.StatusBadRequest, "tag, protocol and port are required")
		return
	}
	ib.ID, ib.NodeID = cur.ID, cur.NodeID
	if msg := h.checkIngress(r, &ib); msg != "" {
		fail(w, http.StatusBadRequest, msg)
		return
	}
	if err := h.Store.UpdateInbound(r.Context(), &ib); err != nil {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	ok(w, ib)
}

func (h *handlers) deleteInbound(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	if !okID {
		fail(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := h.Store.DeleteInbound(r.Context(), id); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]bool{"ok": true})
}

// --- users ---

type userView struct {
	ID           int64      `json:"id"`
	Email        string     `json:"email"`
	UUID         string     `json:"uuid"`
	SubToken     string     `json:"sub_token"`
	SubURL       string     `json:"sub_url"`
	GroupID      *int64     `json:"group_id"`
	BalanceCents int64      `json:"balance_cents"`
	Status       string     `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
	PlanName     string     `json:"plan_name"`
	ExpiresAt    *time.Time `json:"expires_at"`
	QuotaBytes   int64      `json:"quota_bytes"`
	UsedBytes    int64      `json:"used_bytes"`
	SubUsable    bool       `json:"sub_usable"`
}

func (h *handlers) listUsers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	const per = 50
	rows, total, err := h.Store.ListUsers(r.Context(), q.Get("q"), per, (page-1)*per, time.Now())
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]userView, 0, len(rows))
	for _, row := range rows {
		u := row.User
		out = append(out, userView{ID: u.ID, Email: u.Email, UUID: u.UUID, SubToken: u.SubToken, SubURL: h.subURL(r.Context(), u.SubToken), GroupID: u.GroupID, BalanceCents: u.BalanceCents, Status: u.Status, CreatedAt: u.CreatedAt,
			PlanName: row.PlanName, ExpiresAt: row.ExpiresAt, QuotaBytes: row.QuotaBytes, UsedBytes: row.UsedBytes, SubUsable: row.SubUsable})
	}
	ok(w, map[string]any{"items": out, "total": total, "page": page, "per_page": per})
}

func (h *handlers) createUser(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Password string }
	if !decode(r, &in) || in.Email == "" || len(in.Password) < 8 {
		fail(w, http.StatusBadRequest, "email and a password of 8+ chars are required")
		return
	}
	u, err := NewUser(in.Email, in.Password, "user")
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.Store.CreateUser(r.Context(), u); err != nil {
		fail(w, http.StatusConflict, "email already exists")
		return
	}
	ok(w, map[string]any{"id": u.ID, "email": u.Email, "uuid": u.UUID, "sub_token": u.SubToken})
}

func (h *handlers) getUser(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	u, err := h.Store.UserByID(r.Context(), id)
	if !okID || err != nil {
		fail(w, http.StatusNotFound, "user not found")
		return
	}
	sub, err := h.Store.ActiveSubscription(r.Context(), id)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	orders, _ := h.Store.OrdersByUser(r.Context(), id, 20)
	if orders == nil {
		orders = []*domain.Order{}
	}
	devices, _ := h.Store.OnlineDevices(r.Context(), id, time.Now().Add(-5*time.Minute))
	if devices == nil {
		devices = []store.OnlineDevice{}
	}
	ok(w, map[string]any{"id": u.ID, "email": u.Email, "uuid": u.UUID, "sub_token": u.SubToken, "sub_url": h.subURL(r.Context(), u.SubToken), "group_id": u.GroupID, "status": u.Status,
		"invite_code": u.InviteCode, "invited_by": u.InvitedBy,
		"balance_cents": u.BalanceCents, "created_at": u.CreatedAt, "subscription": sub, "orders": orders, "devices": devices})
}

func (h *handlers) updateUser(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	var in struct {
		Status   string
		GroupID  *int64
		Password string
	}
	if !okID || !decode(r, &in) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	if in.Status != "active" && in.Status != "banned" {
		fail(w, http.StatusBadRequest, "status must be active or banned")
		return
	}
	hash := ""
	if in.Password != "" {
		if len(in.Password) < 8 {
			fail(w, http.StatusBadRequest, "password too short")
			return
		}
		var err error
		if hash, err = auth.HashPassword(in.Password); err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := h.Store.UpdateUser(r.Context(), id, in.Status, in.GroupID, hash); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]bool{"ok": true})
}

func (h *handlers) deleteUser(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	if !okID {
		fail(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := h.Store.DeleteUser(r.Context(), id); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]bool{"ok": true})
}

func (h *handlers) rotateToken(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	if !okID {
		fail(w, http.StatusBadRequest, "bad id")
		return
	}
	tok := auth.Token(24)
	if err := h.Store.RotateSubToken(r.Context(), id, tok); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = h.Store.RotateShortCode(r.Context(), id)
	ok(w, map[string]string{"sub_token": tok, "sub_url": h.subURL(r.Context(), tok)})
}

func (h *handlers) adjustBalance(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	var in struct{ DeltaCents int64 }
	if !okID || !decode(r, &in) || in.DeltaCents == 0 {
		fail(w, http.StatusBadRequest, "DeltaCents is required")
		return
	}
	if err := h.Store.AdjustBalance(r.Context(), id, in.DeltaCents); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	u, err := h.Store.UserByID(r.Context(), id)
	if err != nil {
		fail(w, http.StatusNotFound, "user not found")
		return
	}
	ok(w, map[string]any{"id": u.ID, "balance_cents": u.BalanceCents})
}

// grantPlan gives a user a plan directly (manual order).
func (h *handlers) grantPlan(w http.ResponseWriter, r *http.Request) {
	userID, okID := pathID(r)
	var in struct{ PlanID int64 }
	if !okID || !decode(r, &in) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	plan, err := h.Store.PlanByID(r.Context(), in.PlanID)
	if err != nil {
		fail(w, http.StatusNotFound, "plan not found")
		return
	}
	sub, err := h.Store.GrantSubscription(r.Context(), userID, plan, time.Now())
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, sub)
}

// --- plans / groups ---

func (h *handlers) listPlans(w http.ResponseWriter, r *http.Request) {
	plans, err := h.Store.ListPlans(r.Context(), false)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if plans == nil {
		plans = []*domain.Plan{}
	}
	ok(w, plans)
}

func (h *handlers) createPlan(w http.ResponseWriter, r *http.Request) {
	var p domain.Plan
	if !decode(r, &p) || p.Name == "" {
		fail(w, http.StatusBadRequest, "name is required")
		return
	}
	p.Enabled = true
	if err := h.Store.CreatePlan(r.Context(), &p); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, p)
}

func (h *handlers) updatePlan(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	cur, err := h.Store.PlanByID(r.Context(), id)
	if !okID || err != nil {
		fail(w, http.StatusNotFound, "plan not found")
		return
	}
	p := *cur
	if !decode(r, &p) || p.Name == "" {
		fail(w, http.StatusBadRequest, "name is required")
		return
	}
	p.ID = cur.ID
	if err := h.Store.UpdatePlan(r.Context(), &p); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, p)
}

func (h *handlers) deletePlan(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	if !okID {
		fail(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := h.Store.DeletePlan(r.Context(), id); err != nil {
		fail(w, http.StatusConflict, "plan is referenced by orders or subscriptions; disable it instead")
		return
	}
	ok(w, map[string]bool{"ok": true})
}

func (h *handlers) listGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := h.Store.ListGroups(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if groups == nil {
		groups = []domain.Group{}
	}
	ok(w, groups)
}

func (h *handlers) createGroup(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name string }
	if !decode(r, &in) || in.Name == "" {
		fail(w, http.StatusBadRequest, "name is required")
		return
	}
	id, err := h.Store.CreateGroup(r.Context(), in.Name)
	if err != nil {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	ok(w, map[string]any{"id": id, "name": in.Name})
}

// --- entries ---

func (h *handlers) listEntries(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.ListEntries(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []*domain.Entry{}
	}
	ok(w, list)
}

func (h *handlers) reorderEntries(w http.ResponseWriter, r *http.Request) {
	var in struct{ IDs []int64 }
	if !decode(r, &in) || len(in.IDs) == 0 {
		fail(w, http.StatusBadRequest, "ids required")
		return
	}
	if err := h.Store.ReorderEntries(r.Context(), in.IDs); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]bool{"ok": true})
}

func (h *handlers) entryTags(w http.ResponseWriter, r *http.Request) {
	tags, err := h.Store.EntryTags(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, tags)
}

// fillEntryDefaults resolves a blank display address: the inbound's TLS
// domain (it has to point at the node anyway), else the node's public
// address; a blank port is the inbound's port.
func (h *handlers) fillEntryDefaults(ctx context.Context, e *domain.Entry) error {
	if e.DisplayHost != "" && e.DisplayPort != 0 {
		return nil
	}
	ib, err := h.Store.InboundByID(ctx, e.InboundID)
	if err != nil {
		return err
	}
	if ib.IngressID != nil {
		// A line ingress: clients dial the provider's public entry (or a
		// relay in front of the line) on the mapped port.
		g, err := h.Store.IngressByID(ctx, *ib.IngressID)
		if err == nil {
			if e.DisplayPort == 0 {
				e.DisplayPort = g.EntryPort(ib.Port)
			}
			if e.DisplayHost == "" {
				if g.ClientHost() == "" {
					return errors.New("this ingress has no public entry: add a port forward on a relay node and use the relay's address here")
				}
				e.DisplayHost = g.ClientHost()
			}
			return nil
		}
	}
	if e.DisplayPort == 0 {
		e.DisplayPort = ib.Port
	}
	if e.DisplayHost == "" {
		if sp := ib.Spec(); sp.TLS != nil && sp.TLS.Mode == spec.TLSStandard && sp.TLS.ServerName != "" {
			e.DisplayHost = sp.TLS.ServerName
		} else if n, err := h.Store.NodeByID(ctx, ib.NodeID); err == nil {
			if n.Domain != "" {
				e.DisplayHost = n.Domain
			} else {
				e.DisplayHost = n.PublicAddr
			}
		}
	}
	if e.DisplayHost == "" {
		return errors.New("display_host is required: the inbound has no TLS domain and the node no public address")
	}
	return nil
}

func (h *handlers) createEntry(w http.ResponseWriter, r *http.Request) {
	var e domain.Entry
	if !decode(r, &e) || e.Name == "" || e.InboundID == 0 {
		fail(w, http.StatusBadRequest, "name and inbound_id are required")
		return
	}
	if err := h.fillEntryDefaults(r.Context(), &e); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	e.Enabled = true
	if err := h.Store.CreateEntry(r.Context(), &e); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, e)
}

func (h *handlers) updateEntry(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	var e domain.Entry
	if !okID || !decode(r, &e) || e.Name == "" || e.InboundID == 0 {
		fail(w, http.StatusBadRequest, "name and inbound_id are required")
		return
	}
	if err := h.fillEntryDefaults(r.Context(), &e); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	e.ID = id
	if err := h.Store.UpdateEntry(r.Context(), &e); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, e)
}

func (h *handlers) deleteEntry(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	if !okID {
		fail(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := h.Store.DeleteEntry(r.Context(), id); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]bool{"ok": true})
}

// --- orders ---

func (h *handlers) listOrders(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	const per = 50
	rows, total, err := h.Store.ListOrders(r.Context(), q.Get("status"), per, (page-1)*per)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	type row struct {
		domain.Order
		Email    string `json:"email"`
		PlanName string `json:"plan_name"`
	}
	out := make([]row, 0, len(rows))
	for _, r := range rows {
		out = append(out, row{Order: r.Order, Email: r.Email, PlanName: r.PlanName})
	}
	ok(w, map[string]any{"items": out, "total": total, "page": page, "per_page": per})
}

// NewUser builds a user with fresh identity secrets.
func NewUser(email, password, role string) (*domain.User, error) {
	hash, err := auth.HashPassword(password)
	if err != nil {
		return nil, err
	}
	return &domain.User{Email: email, PasswordHash: hash, Role: role, UUID: auth.UUID(), SubToken: auth.Token(24), Status: "active"}, nil
}

func ok(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// ---- upgrades ---------------------------------------------------------------

type upgradeInput struct {
	Version string // release tag; empty = latest bosun release
}

func (h *handlers) resolveBosunVersion(ctx context.Context, in upgradeInput) (string, error) {
	v := strings.TrimSpace(in.Version)
	if v == "" {
		v = h.bosunLatest(ctx)
	}
	if v == "" {
		return "", errors.New("could not determine the latest bosun release; pass a version")
	}
	if !strings.HasPrefix(v, "v") {
		return "", errors.New("version must be a release tag like v0.6.0")
	}
	return v, nil
}

// upgradeNode asks one node to move to a bosun release; the request rides
// on the next report response and clears once the node reports that version.
func (h *handlers) upgradeNode(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fail(w, http.StatusBadRequest, "bad id")
		return
	}
	var in upgradeInput
	_ = json.NewDecoder(r.Body).Decode(&in)
	v, err := h.resolveBosunVersion(r.Context(), in)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.Store.SetNodeUpgrade(r.Context(), id, v); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]any{"upgrade_to": v})
}

func (h *handlers) upgradeAllNodes(w http.ResponseWriter, r *http.Request) {
	var in upgradeInput
	_ = json.NewDecoder(r.Body).Decode(&in)
	v, err := h.resolveBosunVersion(r.Context(), in)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	n, err := h.Store.SetAllNodesUpgrade(r.Context(), v)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]any{"upgrade_to": v, "nodes": n})
}

// systemUpdate reports Captain's own update state plus the latest bosun tag.
func (h *handlers) systemUpdate(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"bosun_latest": h.bosunLatest(r.Context())}
	if h.Updater != nil {
		out["captain"] = h.Updater.Check(r.Context(), r.URL.Query().Get("force") == "1")
	}
	ok(w, out)
}

func (h *handlers) systemUpdateApply(w http.ResponseWriter, r *http.Request) {
	if h.Updater == nil {
		fail(w, http.StatusNotFound, "self-update disabled")
		return
	}
	var in struct{ Version string }
	_ = json.NewDecoder(r.Body).Decode(&in)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	ver, err := h.Updater.Apply(ctx, in.Version)
	if err != nil {
		code := http.StatusBadGateway
		if errors.Is(err, selfupdate.ErrInContainer) || errors.Is(err, selfupdate.ErrUpToDate) {
			code = http.StatusConflict
		}
		fail(w, code, err.Error())
		return
	}
	h.Log.Warn("captain updated; restarting", "version", ver)
	ok(w, map[string]any{"installed": ver, "restarting": true})
	selfupdate.Restart(500 * time.Millisecond)
}

func (h *handlers) systemUpdateRollback(w http.ResponseWriter, r *http.Request) {
	if h.Updater == nil {
		fail(w, http.StatusNotFound, "self-update disabled")
		return
	}
	ver, err := h.Updater.Rollback()
	if err != nil {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	h.Log.Warn("captain rolled back; restarting", "version", ver)
	ok(w, map[string]any{"installed": ver, "restarting": true})
	selfupdate.Restart(500 * time.Millisecond)
}

func (h *handlers) systemRestart(w http.ResponseWriter, r *http.Request) {
	h.Log.Warn("restart requested from the admin console")
	ok(w, map[string]bool{"restarting": true})
	selfupdate.Restart(500 * time.Millisecond)
}

// ---- certificate automation settings ------------------------------------------

func (h *handlers) getACME(w http.ResponseWriter, r *http.Request) {
	var v store.ACMESettings
	if err := h.Store.GetSetting(r.Context(), store.SettingACME, &v); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	// The token is write-only; tell the UI whether one is set.
	ok(w, map[string]any{"email": v.Email, "has_cloudflare_token": v.CloudflareToken != ""})
}

// putACME stores the ACME account; an empty token keeps the current one,
// "-" clears it.
func (h *handlers) putACME(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, CloudflareToken string }
	if !decode(r, &in) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	var cur store.ACMESettings
	_ = h.Store.GetSetting(r.Context(), store.SettingACME, &cur)
	cur.Email = strings.TrimSpace(in.Email)
	switch strings.TrimSpace(in.CloudflareToken) {
	case "":
	case "-":
		cur.CloudflareToken = ""
	default:
		cur.CloudflareToken = strings.TrimSpace(in.CloudflareToken)
	}
	if err := h.Store.SetSetting(r.Context(), store.SettingACME, cur); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	// The state revision hashes node.ACME, so every node pulls the new
	// account settings on its next report.
	ok(w, map[string]any{"email": cur.Email, "has_cloudflare_token": cur.CloudflareToken != ""})
}

func (h *handlers) subURL(ctx context.Context, token string) string {
	if h.SubLinks == nil {
		return ""
	}
	return h.SubLinks.URL(ctx, token)
}

// ---- subscription URL settings ------------------------------------------------

func (h *handlers) getSubscription(w http.ResponseWriter, r *http.Request) {
	var v service.SubscriptionSettings
	if err := h.Store.GetSetting(r.Context(), service.SettingSubscription, &v); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if v.URLs == nil {
		v.URLs = []string{}
	}
	ok(w, v)
}

func (h *handlers) putSubscription(w http.ResponseWriter, r *http.Request) {
	var in service.SubscriptionSettings
	if !decode(r, &in) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	clean := make([]string, 0, len(in.URLs))
	for _, u := range in.URLs {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			fail(w, http.StatusBadRequest, "each subscription URL must start with http:// or https://")
			return
		}
		clean = append(clean, strings.TrimRight(u, "/"))
	}
	in.URLs = clean
	if err := h.Store.SetSetting(r.Context(), service.SettingSubscription, in); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if h.SubLinks != nil {
		h.SubLinks.Invalidate()
	}
	ok(w, in)
}

// ---- landing page and external logins -------------------------------------------

func (h *handlers) getSite(w http.ResponseWriter, r *http.Request) {
	v := site.Defaults("")
	_ = h.Store.GetSetting(r.Context(), site.SettingSite, &v)
	ok(w, v)
}

func (h *handlers) putSite(w http.ResponseWriter, r *http.Request) {
	var v site.Settings
	if !decode(r, &v) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	if err := h.Store.SetSetting(r.Context(), site.SettingSite, v); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, v)
}

// getOIDC returns the providers with secrets replaced by a flag.
func (h *handlers) getOIDC(w http.ResponseWriter, r *http.Request) {
	var v store.OIDCSettings
	_ = h.Store.GetSetting(r.Context(), store.SettingOIDC, &v)
	type view struct {
		store.OIDCProvider
		HasSecret bool `json:"has_secret"`
	}
	out := []view{}
	for _, p := range v.Providers {
		has := p.ClientSecret != ""
		p.ClientSecret = ""
		out = append(out, view{OIDCProvider: p, HasSecret: has})
	}
	pw := v.PasswordLogin == nil || *v.PasswordLogin
	ok(w, map[string]any{"providers": out, "password_login": pw})
}

// putOIDC replaces the provider list; a blank client_secret keeps the stored one.
func (h *handlers) putOIDC(w http.ResponseWriter, r *http.Request) {
	var in store.OIDCSettings
	if !decode(r, &in) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	var cur store.OIDCSettings
	_ = h.Store.GetSetting(r.Context(), store.SettingOIDC, &cur)
	old := map[string]string{}
	for _, p := range cur.Providers {
		old[p.ID] = p.ClientSecret
	}
	seen := map[string]bool{}
	for i := range in.Providers {
		p := &in.Providers[i]
		p.ID = strings.ToLower(strings.TrimSpace(p.ID))
		p.Issuer = strings.TrimSpace(p.Issuer)
		p.ClientID = strings.TrimSpace(p.ClientID)
		if p.ID == "" || strings.ContainsAny(p.ID, " /") || seen[p.ID] {
			fail(w, http.StatusBadRequest, "each provider needs a unique id made of letters, digits or dashes")
			return
		}
		seen[p.ID] = true
		if !strings.HasPrefix(p.Issuer, "https://") && !strings.HasPrefix(p.Issuer, "http://") {
			fail(w, http.StatusBadRequest, "issuer must be a URL, e.g. https://casdoor.example.com")
			return
		}
		if p.Name == "" {
			p.Name = p.ID
		}
		if strings.TrimSpace(p.ClientSecret) == "" {
			p.ClientSecret = old[p.ID]
		}
	}
	if err := h.Store.SetSetting(r.Context(), store.SettingOIDC, in); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.getOIDC(w, r)
}

// ---- mail ----------------------------------------------------------------------

func (h *handlers) getMail(w http.ResponseWriter, r *http.Request) {
	var v mail.Settings
	_ = h.Store.GetSetting(r.Context(), mail.SettingKey, &v)
	hasPass, hasKey := v.SMTP.Password != "", v.Resend.APIKey != ""
	v.SMTP.Password, v.Resend.APIKey = "", ""
	ok(w, map[string]any{"settings": v, "has_smtp_password": hasPass, "has_resend_key": hasKey})
}

// putMail stores the settings; blank secrets keep the stored ones.
func (h *handlers) putMail(w http.ResponseWriter, r *http.Request) {
	var in mail.Settings
	if !decode(r, &in) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	var cur mail.Settings
	_ = h.Store.GetSetting(r.Context(), mail.SettingKey, &cur)
	if strings.TrimSpace(in.SMTP.Password) == "" {
		in.SMTP.Password = cur.SMTP.Password
	}
	if strings.TrimSpace(in.Resend.APIKey) == "" {
		in.Resend.APIKey = cur.Resend.APIKey
	}
	if err := h.Store.SetSetting(r.Context(), mail.SettingKey, in); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if h.Mail != nil {
		h.Mail.Invalidate()
	}
	h.getMail(w, r)
}

func (h *handlers) testMail(w http.ResponseWriter, r *http.Request) {
	var in struct{ To string }
	if !decode(r, &in) || !strings.Contains(in.To, "@") {
		fail(w, http.StatusBadRequest, "recipient required")
		return
	}
	if h.Mail == nil {
		fail(w, http.StatusConflict, "mail disabled")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	if err := mail.Send(ctx, h.Mail.Settings(ctx), mail.TestMessage(h.SiteName, in.To)); err != nil {
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	ok(w, map[string]bool{"sent": true})
}

// ---- coupons, invites, notices ---------------------------------------------------

func (h *handlers) listCoupons(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.ListCoupons(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, list)
}

func (h *handlers) createCoupon(w http.ResponseWriter, r *http.Request) {
	var c domain.Coupon
	if !decode(r, &c) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	if strings.TrimSpace(c.Code) == "" {
		c.Code = store.GenerateCouponCode()
	}
	if err := h.Store.CreateCoupon(r.Context(), &c); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	ok(w, c)
}

func (h *handlers) updateCoupon(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	var c domain.Coupon
	if !okID || !decode(r, &c) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	c.ID = id
	if err := h.Store.UpdateCoupon(r.Context(), &c); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	ok(w, c)
}

func (h *handlers) deleteCoupon(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	if !okID {
		fail(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := h.Store.DeleteCoupon(r.Context(), id); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]bool{"ok": true})
}

func (h *handlers) getInvite(w http.ResponseWriter, r *http.Request) {
	var v store.InviteSettings
	_ = h.Store.GetSetting(r.Context(), store.SettingInvite, &v)
	ok(w, v)
}

func (h *handlers) putInvite(w http.ResponseWriter, r *http.Request) {
	var v store.InviteSettings
	if !decode(r, &v) || v.Percent < 0 || v.Percent > 100 || v.Level2 < 0 || v.Level2 > 100 || v.Level3 < 0 || v.Level3 > 100 {
		fail(w, http.StatusBadRequest, "percent must be 0-100")
		return
	}
	if v.Payout != store.PayoutCommission {
		v.Payout = store.PayoutBalance
	}
	methods := []string{}
	for _, m := range v.WithdrawMethods {
		if m = strings.TrimSpace(m); m != "" {
			methods = append(methods, m)
		}
	}
	v.WithdrawMethods = methods
	if err := h.Store.SetSetting(r.Context(), store.SettingInvite, v); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, v)
}

func (h *handlers) getNotice(w http.ResponseWriter, r *http.Request) {
	var v store.NoticeSettings
	_ = h.Store.GetSetting(r.Context(), store.SettingNotice, &v)
	ok(w, v)
}

func (h *handlers) putNotice(w http.ResponseWriter, r *http.Request) {
	var v store.NoticeSettings
	if !decode(r, &v) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	if err := h.Store.SetSetting(r.Context(), store.SettingNotice, v); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, v)
}

// ---- registration limits ---------------------------------------------------------

func (h *handlers) getRegistration(w http.ResponseWriter, r *http.Request) {
	var v store.RegistrationSettings
	_ = h.Store.GetSetting(r.Context(), store.SettingRegistration, &v)
	if v.EmailSuffixes == nil {
		v.EmailSuffixes = []string{}
	}
	has := v.Captcha.SecretKey != ""
	v.Captcha.SecretKey = ""
	ok(w, map[string]any{"settings": v, "has_captcha_secret": has})
}

func (h *handlers) putRegistration(w http.ResponseWriter, r *http.Request) {
	var v store.RegistrationSettings
	if !decode(r, &v) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	var cur store.RegistrationSettings
	_ = h.Store.GetSetting(r.Context(), store.SettingRegistration, &cur)
	if strings.TrimSpace(v.Captcha.SecretKey) == "" {
		v.Captcha.SecretKey = cur.Captcha.SecretKey
	}
	clean := []string{}
	for _, sfx := range v.EmailSuffixes {
		if sfx = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(sfx), "@")); sfx != "" {
			clean = append(clean, sfx)
		}
	}
	v.EmailSuffixes = clean
	if err := h.Store.SetSetting(r.Context(), store.SettingRegistration, v); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.getRegistration(w, r)
}

// allowed applies the role matrix to an admin API call. Admin: everything.
// Operator: everything except settings, system, staff management and the
// landing page. Support: tickets, plus read-only users, orders, dashboard.
func allowed(role, method, path string) bool {
	switch role {
	case domain.RoleAdmin:
		return true
	case domain.RoleOperator:
		for _, p := range []string{"/api/admin/settings/", "/api/admin/system/", "/api/admin/admins", "/api/admin/site"} {
			if strings.HasPrefix(path, p) {
				return false
			}
		}
		return true
	case domain.RoleSupport:
		switch {
		case path == "/api/admin/me", path == "/api/admin/logout", path == "/api/admin/dashboard", strings.HasPrefix(path, "/api/admin/tokens"), strings.HasPrefix(path, "/api/admin/2fa"):
			return true
		case strings.HasPrefix(path, "/api/admin/tickets"):
			return true
		case method == http.MethodGet && (strings.HasPrefix(path, "/api/admin/users") || strings.HasPrefix(path, "/api/admin/orders") || strings.HasPrefix(path, "/api/admin/plans")):
			return true
		}
		return false
	}
	return false
}
