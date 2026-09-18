// Package portal serves the user-facing API under /api/portal.
package portal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/captcha"
	"github.com/zeptop-dev/captain/internal/http/origin"
	"github.com/zeptop-dev/captain/internal/http/ratelimit"
	"github.com/zeptop-dev/captain/internal/mail"
	"github.com/zeptop-dev/captain/internal/notify"
	"github.com/zeptop-dev/captain/internal/telegram"
	"github.com/zeptop-dev/captain/internal/webhook"

	"github.com/zeptop-dev/bosun/pkg/subscription"
	"github.com/zeptop-dev/captain/internal/auth"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/http/admin"
	"github.com/zeptop-dev/captain/internal/service"
	"github.com/zeptop-dev/captain/internal/store"
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
	Secure       bool             // HTTPS-only session cookies
	Notify       *notify.Notifier // nil = no notifications
	Bot          *telegram.Bot    // nil = no Telegram
}

const cookieName = "captain_session"

type handlers struct{ Deps }

// Register mounts the portal routes.
func Register(mux *http.ServeMux, d Deps) {
	h := &handlers{d}
	mux.HandleFunc("POST /api/portal/register", h.sameOrigin(h.register))
	mux.HandleFunc("GET /api/portal/register/policy", h.registerPolicy)
	mux.HandleFunc("POST /api/portal/verify/send", h.sameOrigin(h.sendCode))
	mux.HandleFunc("POST /api/portal/password/reset", h.sameOrigin(h.resetPassword))
	mux.HandleFunc("POST /api/portal/login", h.sameOrigin(h.login))
	mux.HandleFunc("POST /api/portal/logout", h.logout)
	mux.HandleFunc("GET /api/portal/me", h.requireUser(h.me))
	mux.HandleFunc("DELETE /api/portal/me/hwid-devices/{hwid}", h.requireUser(h.deleteHwidDevice))
	mux.HandleFunc("GET /api/portal/plans", h.plans)
	mux.HandleFunc("GET /api/portal/servers", h.requireUser(h.servers))
	mux.HandleFunc("GET /api/portal/orders", h.requireUser(h.orders))
	mux.HandleFunc("POST /api/portal/orders", h.requireUser(h.createOrder))
	mux.HandleFunc("POST /api/portal/orders/quote", h.requireUser(h.quote))
	mux.HandleFunc("DELETE /api/portal/subscriptions/{id}", h.requireUser(h.cancelQueued))
	mux.HandleFunc("GET /api/portal/notice", h.notice)
	mux.HandleFunc("POST /api/portal/ref", h.ref)
	// The portal tells us which language the user reads, so their mail can
	// follow it instead of the panel-wide setting.
	mux.HandleFunc("PUT /api/portal/me/lang", h.requireUser(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Lang string `json:"lang"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			fail(w, http.StatusBadRequest, "bad json")
			return
		}
		lang := strings.TrimSpace(in.Lang)
		if lang != "" && !mail.Supported(lang) {
			fail(w, http.StatusBadRequest, "unsupported language")
			return
		}
		if err := h.Store.SetUserLang(r.Context(), userFrom(r).ID, lang); err != nil {
			fail(w, http.StatusInternalServerError, "internal error")
			return
		}
		ok(w, map[string]string{"lang": lang})
	}))
	mux.HandleFunc("GET /api/portal/invite", h.requireUser(h.invite))
	mux.HandleFunc("POST /api/portal/invite/bind", h.requireUser(h.bindInvite))
	mux.HandleFunc("POST /api/portal/invite/transfer", h.requireUser(h.transferCommission))
	mux.HandleFunc("POST /api/portal/invite/withdraw", h.requireUser(h.withdraw))
	mux.HandleFunc("GET /api/portal/invite/withdrawals", h.requireUser(h.withdrawals))
	h.registerOps(mux)
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
		if !origin.Allowed(r, h.BaseURL) {
			fail(w, http.StatusForbidden, "cross-site request refused")
			return
		}
		u, _, err := h.Sessions.Resolve(r.Context(), c.Value)
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
	var in struct{ Email, Password, Code, Invite, Captcha string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || !strings.Contains(in.Email, "@") || len(in.Password) < 8 {
		fail(w, http.StatusBadRequest, "valid email and a password of 8+ chars are required")
		return
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	ip := ratelimit.ClientIP(r)
	inviter := h.inviterFrom(r, in.Invite)
	if err := h.registrationAllowed(r, email, ip, inviter, in.Captcha); err != nil {
		fail(w, http.StatusForbidden, err.Error())
		return
	}
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
	u.InvitedBy = inviter
	u.RegisterIP = ip
	if err := h.Store.CreateUser(r.Context(), u); err != nil {
		fail(w, http.StatusConflict, "email already registered")
		return
	}
	if err := h.Store.ApplyTrial(r.Context(), u.ID, time.Now()); err != nil {
		h.Log.Warn("trial grant", "user", u.ID, "err", err)
	}
	h.Notify.Event(r.Context(), webhook.UserRegistered, map[string]any{"user_id": u.ID, "email": u.Email, "invited_by": u.InvitedBy, "method": "password"})
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
	if errors.Is(err, store.ErrNotFound) {
		auth.VerifyPasswordOrDummy("", in.Password)
	}
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
	sess, err := h.Sessions.Create(r.Context(), u.ID, false)
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
	now := time.Now()
	devices, _ := h.Store.OnlineDevices(r.Context(), u.ID, now.Add(-5*time.Minute))
	var subView any
	if sub != nil {
		subView = map[string]any{
			"plan_id": sub.PlanID, "starts_at": sub.StartsAt, "expires_at": sub.ExpiresAt, "reset_at": sub.ResetAt,
			"quota_bytes": sub.QuotaBytes, "used_bytes": sub.UsedUpBytes + sub.UsedDownBytes, "usable": sub.Usable(now),
			"online_devices": len(devices),
		}
	}
	// Every plan the user holds: active ones (soonest expiry first) then
	// queued ones, each with its plan name for the cards.
	all, _ := h.Store.Subscriptions(r.Context(), u.ID)
	names := map[int64]string{}
	subs := make([]map[string]any, 0, len(all))
	for _, s := range all {
		name, seen := names[s.PlanID]
		if !seen {
			if p, err := h.Store.PlanByID(r.Context(), s.PlanID); err == nil {
				name = p.Name
			}
			names[s.PlanID] = name
		}
		subs = append(subs, map[string]any{
			"id": s.ID, "plan_id": s.PlanID, "plan_name": name, "status": s.Status, "starts_at": s.StartsAt, "expires_at": s.ExpiresAt, "reset_at": s.ResetAt,
			"quota_bytes": s.QuotaBytes, "used_bytes": s.UsedUpBytes + s.UsedDownBytes, "usable": s.Status == "active" && s.Usable(now), "period_days": s.PeriodDays,
		})
	}
	var ss service.SubscriptionSettings
	_ = h.Store.GetSetting(r.Context(), service.SettingSubscription, &ss)
	var hwids []store.HwidDevice
	hwidLimit := 0
	if ss.HWID.Enabled {
		hwids, _ = h.Store.HwidDevices(r.Context(), u.ID)
		hwidLimit = h.Subscription.HWIDLimit(r.Context(), u, ss.HWID.FallbackLimit)
	}
	if hwids == nil {
		hwids = []store.HwidDevice{}
	}
	ok(w, map[string]any{
		"id": u.ID, "email": u.Email, "balance_cents": u.BalanceCents,
		"hwid_enabled":     ss.HWID.Enabled,
		"hwid_devices":     hwids,
		"hwid_limit":       hwidLimit,
		"subscription_url": h.subURL(r.Context(), u.SubToken),
		"subscription":     subView,
		"subscriptions":    subs,
		"online_devices":   len(devices),
		"single_plan":      ss.SinglePlan,
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
	if err != nil && !errors.Is(err, service.ErrNoAccess) && !errors.Is(err, service.ErrDisabled) {
		fail(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(lines))
	for _, l := range lines {
		tags := l.Tags
		if tags == nil {
			tags = []string{}
		}
		out = append(out, map[string]any{"name": l.Name, "host": l.Host, "port": l.Port, "protocol": l.Inbound.Protocol, "uri": subscription.ShareURI(l), "tags": tags})
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
		PlanID     int64  `json:"plan_id"`
		PeriodDays int    `json:"period_days"`
		Coupon     string `json:"coupon"`
		Gateway    string `json:"gateway"`
		Activation string `json:"activation"` // "" now, "queue" after the current plans lapse
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.PlanID == 0 || in.Gateway == "" {
		fail(w, http.StatusBadRequest, "plan_id and gateway are required")
		return
	}
	order, co, err := h.Orders.CreateWith(r.Context(), userFrom(r), in.PlanID, in.PeriodDays, in.Coupon, in.Gateway, clientIP(r), in.Activation)
	switch {
	case errors.Is(err, store.ErrInsufficientBalance):
		fail(w, http.StatusPaymentRequired, "insufficient balance")
		return
	case errors.Is(err, service.ErrGateway):
		fail(w, http.StatusBadRequest, "gateway not available")
		return
	case err != nil:
		fail(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), "orders: "))
		return
	}
	resp := map[string]any{"order_no": order.No, "status": order.Status, "amount_cents": order.AmountCents}
	if co != nil {
		resp["pay_url"] = co.URL
	}
	ok(w, resp)
}

// cancelQueued drops a plan the user bought "after the current one" and no
// longer wants. Only queued rows can go; nothing is refunded here.
func (h *handlers) cancelQueued(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err := h.Store.CancelQueued(r.Context(), userFrom(r).ID, id); err != nil {
		fail(w, http.StatusNotFound, "no such queued plan")
		return
	}
	ok(w, map[string]bool{"ok": true})
}

func clientIP(r *http.Request) string { return ratelimit.ClientIP(r) }

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
	var reg store.RegistrationSettings
	_ = h.Store.GetSetting(r.Context(), store.SettingRegistration, &reg)
	out := map[string]any{"open": h.Registration, "verify": ms.Enabled() && ms.VerifyRegistration, "reset": ms.Enabled(),
		"invite_only": reg.InviteOnly, "email_suffixes": reg.EmailSuffixes}
	if cs := (captcha.Settings{Provider: reg.Captcha.Provider, SiteKey: reg.Captcha.SiteKey, SecretKey: reg.Captcha.SecretKey}); cs.Enabled() {
		out["captcha"] = map[string]string{"provider": cs.Provider, "site_key": cs.SiteKey}
	}
	ok(w, out)
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
		var reg store.RegistrationSettings
		_ = h.Store.GetSetting(r.Context(), store.SettingRegistration, &reg)
		if !reg.EmailAllowed(email) {
			fail(w, http.StatusForbidden, "this email domain is not accepted")
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
	if err := mail.Send(r.Context(), ms, mail.For(ms.Language).Code(h.SiteName, email, in.Purpose, code)); err != nil {
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
	if u.IsStaff() {
		// A mailbox must not be enough to take over a staff account.
		fail(w, http.StatusForbidden, "staff passwords are reset by an administrator")
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
	_ = h.Store.DeleteUserSessions(r.Context(), u.ID)
	ok(w, map[string]bool{"ok": true})
}

// RefCookie carries an invite code from a ?ref= link to account creation.
const RefCookie = "captain_ref"

// inviterFrom resolves an explicit invite code or the ref cookie to a user ID.
func (h *handlers) inviterFrom(r *http.Request, code string) *int64 {
	code = strings.TrimSpace(code)
	if code == "" {
		if c, err := r.Cookie(RefCookie); err == nil {
			code = c.Value
		}
	}
	if code == "" {
		return nil
	}
	u, err := h.Store.UserByInviteCode(r.Context(), code)
	if err != nil {
		return nil
	}
	return &u.ID
}

// ref stores an invite code in a cookie (30 days) so the landing page and
// the portal can record it when the visitor signs up later, by any method.
func (h *handlers) ref(w http.ResponseWriter, r *http.Request) {
	var in struct{ Code string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Code) == "" {
		fail(w, http.StatusBadRequest, "code required")
		return
	}
	code := strings.ToUpper(strings.TrimSpace(in.Code))
	if _, err := h.Store.UserByInviteCode(r.Context(), code); err != nil {
		fail(w, http.StatusNotFound, "unknown invite code")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: RefCookie, Value: code, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: h.Secure, MaxAge: 30 * 24 * 3600})
	ok(w, map[string]bool{"ok": true})
}

func (h *handlers) quote(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PlanID     int64  `json:"plan_id"`
		PeriodDays int    `json:"period_days"`
		Coupon     string `json:"coupon"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.PlanID == 0 {
		fail(w, http.StatusBadRequest, "plan_id required")
		return
	}
	q, _, err := h.Orders.Price(r.Context(), userFrom(r), in.PlanID, in.PeriodDays, in.Coupon)
	if err != nil {
		fail(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), "orders: "))
		return
	}
	ok(w, q)
}

func (h *handlers) notice(w http.ResponseWriter, r *http.Request) {
	var n store.NoticeSettings
	_ = h.Store.GetSetting(r.Context(), store.SettingNotice, &n)
	if !n.Enabled {
		ok(w, map[string]any{"enabled": false})
		return
	}
	ok(w, n)
}

// invite returns the user's referral link and stats.
func (h *handlers) invite(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if err := h.Store.EnsureInviteCode(r.Context(), u); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	var inv store.InviteSettings
	_ = h.Store.GetSetting(r.Context(), store.SettingInvite, &inv)
	stats, _ := h.Store.InviteStatsFor(r.Context(), u.ID)
	commission, _ := h.Store.CommissionCents(r.Context(), u.ID)
	methods := inv.WithdrawMethods
	if methods == nil {
		methods = []string{}
	}
	ok(w, map[string]any{"code": u.InviteCode, "url": strings.TrimRight(h.BaseURL, "/") + "/?ref=" + u.InviteCode,
		"enabled": inv.Enabled, "percent": inv.Percent, "first_order_only": inv.FirstOrderOnly, "levels": inv.LevelPercents(),
		"payout": inv.Payout, "commission_cents": commission, "min_withdraw_cents": inv.MinWithdrawCents, "withdraw_methods": methods,
		"invited": stats.Invited, "earned_cents": stats.EarnedCents, "invited_by": u.InvitedBy != nil})
}

// bindInvite lets a user who signed up without a code record one, once.
func (h *handlers) bindInvite(w http.ResponseWriter, r *http.Request) {
	var in struct{ Code string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Code) == "" {
		fail(w, http.StatusBadRequest, "code required")
		return
	}
	inviter, err := h.Store.UserByInviteCode(r.Context(), in.Code)
	if err != nil {
		fail(w, http.StatusNotFound, "unknown invite code")
		return
	}
	if err := h.Store.SetInvitedBy(r.Context(), userFrom(r).ID, inviter.ID); err != nil {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	ok(w, map[string]bool{"ok": true})
}

// registrationAllowed applies the sign-up limits: email whitelist, invite
// requirement, per-IP cap and captcha.
func (h *handlers) registrationAllowed(r *http.Request, email, ip string, inviter *int64, captchaToken string) error {
	var reg store.RegistrationSettings
	_ = h.Store.GetSetting(r.Context(), store.SettingRegistration, &reg)
	if !reg.EmailAllowed(email) {
		return errors.New("this email domain is not accepted")
	}
	if reg.InviteOnly && inviter == nil {
		return errors.New("registration requires an invite code")
	}
	if reg.IPLimit > 0 {
		hours := reg.IPWindowHours
		if hours <= 0 {
			hours = 24
		}
		if n, _ := h.Store.RegistrationsFromIP(r.Context(), ip, time.Now().Add(-time.Duration(hours)*time.Hour)); n >= reg.IPLimit {
			return errors.New("too many sign-ups from this address; try again later")
		}
	}
	cs := captcha.Settings{Provider: reg.Captcha.Provider, SiteKey: reg.Captcha.SiteKey, SecretKey: reg.Captcha.SecretKey}
	if err := captcha.Verify(r.Context(), cs, captchaToken, ip); err != nil {
		return err
	}
	return nil
}

// transferCommission moves referral earnings into the spendable balance.
func (h *handlers) transferCommission(w http.ResponseWriter, r *http.Request) {
	var in struct{ AmountCents int64 }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.AmountCents <= 0 {
		fail(w, http.StatusBadRequest, "amount required")
		return
	}
	if err := h.Store.TransferCommission(r.Context(), userFrom(r).ID, in.AmountCents); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	h.invite(w, r)
}

// withdraw files a payout request against the commission balance.
func (h *handlers) withdraw(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AmountCents     int64
		Method, Account string
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.AmountCents <= 0 || strings.TrimSpace(in.Account) == "" {
		fail(w, http.StatusBadRequest, "amount and account are required")
		return
	}
	var inv store.InviteSettings
	_ = h.Store.GetSetting(r.Context(), store.SettingInvite, &inv)
	if inv.Payout != store.PayoutCommission {
		fail(w, http.StatusBadRequest, "withdrawals are not enabled")
		return
	}
	if in.AmountCents < inv.MinWithdrawCents {
		fail(w, http.StatusBadRequest, "below the minimum withdrawal amount")
		return
	}
	if len(inv.WithdrawMethods) > 0 && !slices.Contains(inv.WithdrawMethods, in.Method) {
		fail(w, http.StatusBadRequest, "unknown withdrawal method")
		return
	}
	u := userFrom(r)
	wd, err := h.Store.CreateWithdrawal(r.Context(), u.ID, in.AmountCents, in.Method, strings.TrimSpace(in.Account))
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	h.Notify.Admin(r.Context(), fmt.Sprintf("💸 Withdrawal #%d: %.2f via %s (%s)\n%s", wd.ID, float64(wd.AmountCents)/100, wd.Method, wd.Account, u.Email))
	h.Notify.Event(r.Context(), webhook.WithdrawalRequested, map[string]any{"withdrawal_id": wd.ID, "user_id": u.ID, "email": u.Email, "amount_cents": wd.AmountCents, "method": wd.Method})
	ok(w, wd)
}

func (h *handlers) withdrawals(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.ListWithdrawals(r.Context(), userFrom(r).ID, "", 50)
	if err != nil {
		fail(w, http.StatusInternalServerError, "internal error")
		return
	}
	if list == nil {
		list = []store.Withdrawal{}
	}
	ok(w, list)
}

// deleteHwidDevice lets the user free a device slot (a phone they no
// longer use) without asking support.
func (h *handlers) deleteHwidDevice(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if err := h.Store.DeleteHwidDevice(r.Context(), u.ID, r.PathValue("hwid")); err != nil {
		fail(w, http.StatusInternalServerError, "internal error")
		return
	}
	ok(w, map[string]bool{"ok": true})
}

// sameOrigin refuses cross-site browser POSTs to the account routes that
// need no session yet (login, register, codes, reset).
func (h *handlers) sameOrigin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !origin.Allowed(r, h.BaseURL) {
			fail(w, http.StatusForbidden, "cross-site request refused")
			return
		}
		next(w, r)
	}
}
