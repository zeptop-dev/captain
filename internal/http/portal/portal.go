// Package portal serves the user-facing API under /api/portal.
package portal

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/zeptop-dev/captain/internal/http/ratelimit"
	"github.com/zeptop-dev/captain/internal/mail"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/auth"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/http/admin"
	"github.com/zeptop-dev/captain/internal/service"
	"github.com/zeptop-dev/captain/internal/store"
	"github.com/zeptop-dev/captain/internal/subscription"
)

// Deps are the handlers' dependencies.
type Deps struct {
	Store        *store.Store
	Log          *slog.Logger
	Sessions     admin.SessionStore
	Orders       *service.Orders
	Subscription *service.Subscription
	BaseURL      string
	Gateways     []string // names offered to users, e.g. epay, stripe, balance
	Registration bool
	Logins       *ratelimit.Limiter // throttles failed sign-ins; nil disables
	SubLinks     *service.SubLinks  // subscription URL builder
	Mail         *mail.Loader       // nil = mail off
	SiteName     string
	Secure       bool // HTTPS-only session cookies
}

const cookieName = "captain_session"

type handlers struct{ Deps }

// Register mounts the portal routes.
func Register(mux *http.ServeMux, d Deps) {
	h := &handlers{d}
	mux.HandleFunc("POST /api/portal/register", h.register)
	mux.HandleFunc("GET /api/portal/register/policy", h.registerPolicy)
	mux.HandleFunc("POST /api/portal/verify/send", h.sendCode)
	mux.HandleFunc("POST /api/portal/password/reset", h.resetPassword)
	mux.HandleFunc("POST /api/portal/login", h.login)
	mux.HandleFunc("POST /api/portal/logout", h.logout)
	mux.HandleFunc("GET /api/portal/me", h.requireUser(h.me))
	mux.HandleFunc("GET /api/portal/plans", h.plans)
	mux.HandleFunc("GET /api/portal/servers", h.requireUser(h.servers))
	mux.HandleFunc("GET /api/portal/orders", h.requireUser(h.orders))
	mux.HandleFunc("POST /api/portal/orders", h.requireUser(h.createOrder))
}

type ctxKey struct{}

func userFrom(r *http.Request) *domain.User {
	u, _ := r.Context().Value(ctxKey{}).(*domain.User)
	return u
}

func (h *handlers) requireUser(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		if err != nil {
			fail(w, http.StatusUnauthorized, "not logged in")
			return
		}
		u, err := h.Sessions.Resolve(r.Context(), c.Value)
		if err != nil {
			fail(w, http.StatusInternalServerError, "internal error")
			return
		}
		if u == nil || u.Status != "active" {
			fail(w, http.StatusUnauthorized, "not logged in")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
	}
}

func (h *handlers) register(w http.ResponseWriter, r *http.Request) {
	if !h.Registration {
		fail(w, http.StatusForbidden, "registration is closed")
		return
	}
	var in struct{ Email, Password, Code string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || !strings.Contains(in.Email, "@") || len(in.Password) < 8 {
		fail(w, http.StatusBadRequest, "valid email and a password of 8+ chars are required")
		return
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if ms := h.mailSettings(r); ms.Enabled() && ms.VerifyRegistration {
		if err := h.Store.CheckCode(r.Context(), email, "register", strings.TrimSpace(in.Code)); err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	u, err := admin.NewUser(email, in.Password, "user")
	if err != nil {
		fail(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := h.Store.CreateUser(r.Context(), u); err != nil {
		fail(w, http.StatusConflict, "email already registered")
		return
	}
	h.startSession(w, r, u)
}

func (h *handlers) login(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Password string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
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
	u, err := h.Store.UserByEmail(r.Context(), strings.ToLower(strings.TrimSpace(in.Email)))
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
	if u.Status != "active" {
		fail(w, http.StatusForbidden, "account disabled")
		return
	}
	h.startSession(w, r, u)
}

func (h *handlers) startSession(w http.ResponseWriter, r *http.Request, u *domain.User) {
	sess, err := h.Sessions.Create(r.Context(), u.ID)
	if err != nil {
		fail(w, http.StatusInternalServerError, "internal error")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: sess.ID, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: h.Secure, Expires: sess.ExpiresAt})
	h.writeMe(w, r, u)
}

func (h *handlers) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		_ = h.Sessions.Delete(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", HttpOnly: true, MaxAge: -1})
	ok(w, map[string]bool{"ok": true})
}

func (h *handlers) me(w http.ResponseWriter, r *http.Request) { h.writeMe(w, r, userFrom(r)) }

func (h *handlers) writeMe(w http.ResponseWriter, r *http.Request, u *domain.User) {
	sub, err := h.Store.ActiveSubscription(r.Context(), u.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusInternalServerError, "internal error")
		return
	}
	var subView any
	if sub != nil {
		devices, _ := h.Store.OnlineDevices(r.Context(), u.ID, time.Now().Add(-5*time.Minute))
		subView = map[string]any{
			"plan_id": sub.PlanID, "starts_at": sub.StartsAt, "expires_at": sub.ExpiresAt, "reset_at": sub.ResetAt,
			"quota_bytes": sub.QuotaBytes, "used_bytes": sub.UsedUpBytes + sub.UsedDownBytes, "usable": sub.Usable(time.Now()),
			"online_devices": len(devices),
		}
	}
	ok(w, map[string]any{
		"id": u.ID, "email": u.Email, "balance_cents": u.BalanceCents,
		"subscription_url": h.subURL(r.Context(), u.SubToken),
		"subscription":     subView,
		"gateways":         h.Gateways,
	})
}

func (h *handlers) plans(w http.ResponseWriter, r *http.Request) {
	plans, err := h.Store.ListPlans(r.Context(), true)
	if err != nil {
		fail(w, http.StatusInternalServerError, "internal error")
		return
	}
	if plans == nil {
		plans = []*domain.Plan{}
	}
	ok(w, plans)
}

// servers lists the user's entries with per-server share links.
func (h *handlers) servers(w http.ResponseWriter, r *http.Request) {
	lines, _, err := h.Subscription.Lines(r.Context(), userFrom(r), time.Now())
	if err != nil && !errors.Is(err, service.ErrNoAccess) {
		fail(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(lines))
	for _, l := range lines {
		out = append(out, map[string]any{"name": l.Name, "host": l.Host, "port": l.Port, "protocol": l.Inbound.Protocol, "uri": subscription.ShareURI(l)})
	}
	ok(w, out)
}

func (h *handlers) orders(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.OrdersByUser(r.Context(), userFrom(r).ID, 50)
	if err != nil {
		fail(w, http.StatusInternalServerError, "internal error")
		return
	}
	if list == nil {
		list = []*domain.Order{}
	}
	ok(w, list)
}

func (h *handlers) createOrder(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PlanID  int64  `json:"plan_id"`
		Gateway string `json:"gateway"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.PlanID == 0 || in.Gateway == "" {
		fail(w, http.StatusBadRequest, "plan_id and gateway are required")
		return
	}
	order, co, err := h.Orders.Create(r.Context(), userFrom(r), in.PlanID, in.Gateway, clientIP(r))
	switch {
	case errors.Is(err, store.ErrInsufficientBalance):
		fail(w, http.StatusPaymentRequired, "insufficient balance")
		return
	case errors.Is(err, service.ErrGateway):
		fail(w, http.StatusBadRequest, "gateway not available")
		return
	case err != nil:
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	resp := map[string]any{"order_no": order.No, "status": order.Status, "amount_cents": order.AmountCents}
	if co != nil {
		resp["pay_url"] = co.URL
	}
	ok(w, resp)
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
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

func (h *handlers) subURL(ctx context.Context, token string) string {
	if h.SubLinks != nil {
		return h.SubLinks.URL(ctx, token)
	}
	return strings.TrimRight(h.BaseURL, "/") + "/sub/" + token
}

func (h *handlers) mailSettings(r *http.Request) mail.Settings {
	if h.Mail == nil {
		return mail.Settings{}
	}
	return h.Mail.Settings(r.Context())
}

// registerPolicy tells the sign-up page whether a code is required.
func (h *handlers) registerPolicy(w http.ResponseWriter, r *http.Request) {
	ms := h.mailSettings(r)
	ok(w, map[string]any{"open": h.Registration, "verify": ms.Enabled() && ms.VerifyRegistration, "reset": ms.Enabled()})
}

// sendCode mails a verification code for registration or password reset.
// Replies are deliberately the same whether or not the address exists.
func (h *handlers) sendCode(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Purpose string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || !strings.Contains(in.Email, "@") {
		fail(w, http.StatusBadRequest, "valid email required")
		return
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	ms := h.mailSettings(r)
	if !ms.Enabled() {
		fail(w, http.StatusConflict, "mail is not configured")
		return
	}
	ip := ratelimit.ClientIP(r)
	if h.Logins != nil {
		if allowed, wait := h.Logins.Allow(ip); !allowed {
			fail(w, http.StatusTooManyRequests, "too many attempts; try again in "+wait.String())
			return
		}
	}
	switch in.Purpose {
	case "register":
		if !h.Registration {
			fail(w, http.StatusForbidden, "registration is closed")
			return
		}
		if _, err := h.Store.UserByEmail(r.Context(), email); err == nil {
			fail(w, http.StatusConflict, "email already registered")
			return
		}
	case "reset":
		if _, err := h.Store.UserByEmail(r.Context(), email); err != nil {
			ok(w, map[string]bool{"sent": true}) // do not reveal whether the account exists
			return
		}
	default:
		fail(w, http.StatusBadRequest, "purpose must be register or reset")
		return
	}
	code, err := h.Store.NewCode(r.Context(), email, in.Purpose, 10*time.Minute)
	if err != nil {
		fail(w, http.StatusTooManyRequests, err.Error())
		return
	}
	if err := mail.Send(r.Context(), ms, mail.CodeMessage(h.SiteName, email, in.Purpose, code)); err != nil {
		h.Log.Error("send code", "to", email, "err", err)
		fail(w, http.StatusBadGateway, "could not send the email; contact the administrator")
		return
	}
	ok(w, map[string]bool{"sent": true})
}

// resetPassword sets a new password after a mailed code.
func (h *handlers) resetPassword(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Code, Password string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || len(in.Password) < 8 {
		fail(w, http.StatusBadRequest, "email, code and a password of 8+ chars are required")
		return
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	u, err := h.Store.UserByEmail(r.Context(), email)
	if err != nil {
		fail(w, http.StatusBadRequest, "wrong code")
		return
	}
	if err := h.Store.CheckCode(r.Context(), email, "reset", strings.TrimSpace(in.Code)); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		fail(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := h.Store.UpdateUser(r.Context(), u.ID, u.Status, u.GroupID, hash); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]bool{"ok": true})
}
