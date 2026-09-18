// Package admin serves the admin console API under /api/admin.
package admin

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/http/origin"

	"github.com/zeptop-dev/bosun/pkg/selfupdate"
	"github.com/zeptop-dev/captain/internal/backup"
	"github.com/zeptop-dev/captain/internal/http/ratelimit"
	"github.com/zeptop-dev/captain/internal/mail"
	"github.com/zeptop-dev/captain/internal/notify"
	"github.com/zeptop-dev/captain/internal/service"
	"github.com/zeptop-dev/captain/internal/telegram"
	"github.com/zeptop-dev/captain/internal/webhook"

	"github.com/zeptop-dev/captain/internal/auth"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

// SessionStore is the cookie session backend.
type SessionStore interface {
	// Create mints a session; admin marks one issued by the admin login
	// (password + authenticator), the only kind requireAdmin accepts.
	Create(ctx context.Context, userID int64, admin bool) (*domain.Session, error)
	Resolve(ctx context.Context, id string) (*domain.User, bool, error)
	Delete(ctx context.Context, id string) error
}

// Deps are the handlers' dependencies.
type Deps struct {
	Store *store.Store
	// State is the node desired-state builder; every non-GET admin request
	// drops its cache so nodes see edits at once.
	State *service.AgentState
	// Metrics serves the Prometheus exposition at GET /api/admin/metrics.
	Metrics  http.Handler
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
	// Allow is the console allow-list, shared with the MCP endpoint.
	Allow *service.AdminAllow
	// Dyn is the dynamic speed limiter (settings cache invalidation).
	Dyn *service.DynLimit
	// External syncs airport subscriptions into external nodes.
	External *service.External
}

const cookieName = "captain_session"

type handlers struct {
	Deps
}

// Register mounts the admin routes.
func Register(mux *http.ServeMux, d Deps) {
	h := &handlers{Deps: d}
	mux.HandleFunc("POST /api/admin/login", h.sameOrigin(h.login))
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
	mux.HandleFunc("POST /api/admin/nodes/{id}/jobs", h.requireAdmin(h.createNodeJob))
	mux.HandleFunc("GET /api/admin/nodes/{id}/jobs/{job}", h.requireAdmin(h.getNodeJob))
	mux.HandleFunc("POST /api/admin/nodes/upgrade-all", h.requireAdmin(h.upgradeAllNodes))
	h.registerOps(mux)
	h.registerExternal(mux)
	h.registerSpeedtest(mux)
	h.registerBackup(mux)
	h.registerCertificates(mux)
	h.registerDomains(mux)
	h.registerIngresses(mux)
	h.registerSubTemplates(mux)
	h.registerSubDesign(mux)
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
	if d.Metrics != nil {
		mux.HandleFunc("GET /api/admin/metrics", h.requireAdmin(func(w http.ResponseWriter, r *http.Request) { d.Metrics.ServeHTTP(w, r) }))
	}
	mux.HandleFunc("GET /api/admin/users/{id}", h.requireAdmin(h.getUser))
	mux.HandleFunc("PATCH /api/admin/users/{id}", h.requireAdmin(h.updateUser))
	mux.HandleFunc("DELETE /api/admin/users/{id}", h.requireAdmin(h.deleteUser))
	mux.HandleFunc("POST /api/admin/users/{id}/grant", h.requireAdmin(h.grantPlan))
	mux.HandleFunc("POST /api/admin/users/{id}/balance", h.requireAdmin(h.adjustBalance))
	mux.HandleFunc("POST /api/admin/users/{id}/rotate-token", h.requireAdmin(h.rotateToken))
	mux.HandleFunc("PUT /api/admin/users/{id}/hwid-limit", h.requireAdmin(h.putHwidLimit))
	mux.HandleFunc("DELETE /api/admin/users/{id}/hwid-devices/{hwid}", h.requireAdmin(h.deleteHwidDevice))

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

// readJSON decodes the body into v and answers 400 itself when it is not
// JSON, so handlers only spell out the checks that are theirs.
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if !decode(r, v) {
		fail(w, http.StatusBadRequest, "bad json")
		return false
	}
	return true
}

// getSetting answers the stored settings document of type T (zero value
// when unset), after fill has had a chance to add defaults or masks.
func getSetting[T any](h *handlers, w http.ResponseWriter, r *http.Request, key string, fill func(*T)) {
	var v T
	_ = h.Store.GetSetting(r.Context(), key, &v)
	if fill != nil {
		fill(&v)
	}
	ok(w, v)
}

// putSetting decodes, lets check normalise or refuse (its message is the
// 400), stores under key and echoes the stored value.
//
// The body is decoded onto the stored document, so a caller that sends
// only some keys (a script, the MCP tools) edits those and leaves the
// rest alone instead of clearing them. Only for documents that are an
// object: decoding a JSON array onto an existing slice reuses its
// elements, which would let one rule inherit fields from whatever used
// to be in its place.
func putSetting[T any](h *handlers, w http.ResponseWriter, r *http.Request, key string, check func(context.Context, *T) string) {
	var v T
	if k := reflect.ValueOf(&v).Elem().Kind(); k == reflect.Struct || k == reflect.Map {
		_ = h.Store.GetSetting(r.Context(), key, &v)
	}
	if !readJSON(w, r, &v) {
		return
	}
	if check != nil {
		if msg := check(r.Context(), &v); msg != "" {
			fail(w, http.StatusBadRequest, msg)
			return
		}
	}
	if err := h.Store.SetSetting(r.Context(), key, v); err != nil {
		serverErr(w, err)
		return
	}
	ok(w, v)
}

// serverErr answers a store/service error: a missing row is the client's
// 404, everything else the 500 it is.
func serverErr(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "not found")
		return
	}
	fail(w, http.StatusInternalServerError, "internal error")
}

func (h *handlers) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.adminAllowed(r.Context(), ratelimit.ClientIP(r)) {
			fail(w, http.StatusForbidden, "your address is not on the admin allow-list")
			return
		}
		var u *domain.User
		if tok := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer")); tok != "" && strings.HasPrefix(tok, "cap_") {
			// Personal API token (scripts, MCP): the owner's role, narrowed
			// by the token's scope. Staff management stays with the
			// interactive login, so a leaked token cannot mint an admin.
			var scope string
			u, scope, _ = h.Store.UserByAPIToken(r.Context(), tok)
			if u != nil && scope == store.ScopeRead && r.Method != http.MethodGet {
				fail(w, http.StatusForbidden, "this token is read-only")
				return
			}
			if u != nil && strings.HasPrefix(r.URL.Path, "/api/admin/admins") {
				fail(w, http.StatusForbidden, "staff accounts can only be managed from the console")
				return
			}
		} else {
			c, err := r.Cookie(cookieName)
			if err != nil {
				fail(w, http.StatusUnauthorized, "not logged in")
				return
			}
			if !origin.Allowed(r, h.BaseURL) {
				fail(w, http.StatusForbidden, "cross-site request refused")
				return
			}
			var admin bool
			u, admin, err = h.Sessions.Resolve(r.Context(), c.Value)
			if err != nil {
				h.Log.Error("session", "err", err)
				fail(w, http.StatusInternalServerError, "internal error")
				return
			}
			if u != nil && !admin {
				// A portal / OIDC / reset session, even for a staff account:
				// the admin console needs the admin login (with TOTP).
				fail(w, http.StatusUnauthorized, "admin login required")
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
		if h.State != nil && r.Method != http.MethodGet {
			defer h.State.Invalidate()
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
	}
}

// --- auth ---

func (h *handlers) login(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Password, Code string }
	if !readJSON(w, r, &in) {
		return
	}
	ip := ratelimit.ClientIP(r)
	if !h.adminAllowed(r.Context(), ip) {
		fail(w, http.StatusForbidden, "your address is not on the admin allow-list")
		return
	}
	if h.Logins != nil {
		if allowed, wait := h.Logins.Allow(ip); !allowed {
			fail(w, http.StatusTooManyRequests, "too many failed attempts; try again in "+wait.String())
			return
		}
	}
	u, err := h.Store.UserByEmail(r.Context(), in.Email)
	hash := ""
	if err == nil {
		hash = u.PasswordHash
	}
	if errors.Is(err, store.ErrNotFound) || (err == nil && !auth.VerifyPasswordOrDummy(hash, in.Password)) {
		if hash == "" {
			auth.VerifyPasswordOrDummy("", in.Password)
		}
		if h.Logins != nil {
			h.Logins.Fail(ip)
		}
		fail(w, http.StatusUnauthorized, "invalid credentials")
		return
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
		if !auth.VerifyTOTPOnce(strconv.FormatInt(u.ID, 10), secret, in.Code, time.Now()) {
			if h.Logins != nil {
				h.Logins.Fail(ip)
			}
			fail(w, http.StatusUnauthorized, "invalid or already used authenticator code")
			return
		}
	}
	// Only a complete login clears the per-address failure count: resetting
	// it after the password alone let an attacker who holds the password
	// guess authenticator codes for ever.
	if h.Logins != nil {
		h.Logins.Reset(ip)
	}
	sess, err := h.Sessions.Create(r.Context(), u.ID, true)
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
		serverErr(w, err)
		return
	}
	series, err := h.Store.TrafficSeries(r.Context(), now, 14)
	if err != nil {
		serverErr(w, err)
		return
	}
	open, _ := h.Store.OpenTickets(r.Context())
	ok(w, map[string]any{"stats": stats, "traffic": series, "open_tickets": open})
}

// --- nodes ---

// --- users ---

// --- plans / groups ---

// --- entries ---

// --- orders ---

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

// ---- certificate automation settings ------------------------------------------

// ---- subscription URL settings ------------------------------------------------

// ---- landing page and external logins -------------------------------------------

// ---- mail ----------------------------------------------------------------------

// ---- coupons, invites, notices ---------------------------------------------------

// ---- registration limits ---------------------------------------------------------

// allowed applies the role matrix to an admin API call. Admin: everything.
// Operator: everything except settings, system, staff management and the
// landing page. Support: tickets, plus read-only users, orders, dashboard.
func allowed(role, method, path string) bool {
	switch role {
	case domain.RoleAdmin:
		return true
	case domain.RoleOperator:
		// Audit rules reach every node's core config and the audit log is
		// a record of what users visited: both stay with the admin, like
		// the settings switches that turn them on.
		for _, p := range []string{"/api/admin/settings/", "/api/admin/system/", "/api/admin/admins", "/api/admin/site", "/api/admin/audit"} {
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
		case strings.HasSuffix(path, "/connections"), strings.HasSuffix(path, "/links"):
			// A help-desk account has no business reading where a
			// customer went, nor their subscription links (the user
			// payloads drop the token for this role too).
			return false
		case method == http.MethodGet && (strings.HasPrefix(path, "/api/admin/users") || strings.HasPrefix(path, "/api/admin/orders") || strings.HasPrefix(path, "/api/admin/plans")):
			return true
		}
		return false
	}
	return false
}

// sameOrigin refuses cross-site browser POSTs to a cookie-minting route.
func (h *handlers) sameOrigin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !origin.Allowed(r, h.BaseURL) {
			fail(w, http.StatusForbidden, "cross-site request refused")
			return
		}
		next(w, r)
	}
}
