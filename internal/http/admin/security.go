package admin

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/auth"
	"github.com/zeptop-dev/captain/internal/http/ratelimit"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/service"
	"github.com/zeptop-dev/captain/internal/store"
)

func (h *handlers) getSecurity(w http.ResponseWriter, r *http.Request) {
	var v store.SecuritySettings
	_ = h.Store.GetSetting(r.Context(), store.SettingSecurity, &v)
	if v.AdminAllowCIDRs == nil {
		v.AdminAllowCIDRs = []string{}
	}
	ok(w, v)
}

// putSecurity saves the allow-list, refusing one that would lock the
// caller out.
func (h *handlers) putSecurity(w http.ResponseWriter, r *http.Request) {
	var v store.SecuritySettings
	if !readJSON(w, r, &v) {
		return
	}
	var clean []string
	for _, c := range v.AdminAllowCIDRs {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if strings.Contains(c, "/") {
			if _, _, err := net.ParseCIDR(c); err != nil {
				fail(w, http.StatusBadRequest, "bad CIDR "+c)
				return
			}
		} else if net.ParseIP(c) == nil {
			fail(w, http.StatusBadRequest, "bad address "+c)
			return
		}
		clean = append(clean, c)
	}
	v.AdminAllowCIDRs = clean
	if len(clean) > 0 && !cidrsAllow(clean, ratelimit.ClientIP(r)) {
		fail(w, http.StatusBadRequest, "the allow-list would lock you out: your address "+ratelimit.ClientIP(r)+" is not in it")
		return
	}
	if err := h.Store.SetSetting(r.Context(), store.SettingSecurity, v); err != nil {
		serverErr(w, err)
		return
	}
	h.Allow.Invalidate()
	ok(w, v)
}

// cidrsAllow reports whether ip is inside any entry (bare IPs allowed).
func cidrsAllow(list []string, ipStr string) bool { return service.CIDRsAllow(list, ipStr) }

// adminAllowed applies the console allow-list (cached for 30 s).
func (h *handlers) adminAllowed(ctx context.Context, ip string) bool {
	if h.Allow == nil {
		return true
	}
	return h.Allow.Allowed(ctx, ip)
}

type staffView struct {
	ID        int64     `json:"id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

func (h *handlers) listStaff(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.ListStaff(r.Context())
	if err != nil {
		serverErr(w, err)
		return
	}
	out := make([]staffView, 0, len(list))
	for _, u := range list {
		out = append(out, staffView{ID: u.ID, Email: u.Email, Role: u.Role, Status: u.Status, CreatedAt: u.CreatedAt})
	}
	ok(w, out)
}

func (h *handlers) createStaff(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Password, Role string }
	if !decode(r, &in) || !strings.Contains(in.Email, "@") || len(in.Password) < 8 || !domain.ValidStaffRole(in.Role) {
		fail(w, http.StatusBadRequest, "email, a password of 8+ chars and a role (admin, operator, support) are required")
		return
	}
	u, err := NewUser(strings.ToLower(strings.TrimSpace(in.Email)), in.Password, in.Role)
	if err != nil {
		serverErr(w, err)
		return
	}
	if err := h.Store.CreateUser(r.Context(), u); err != nil {
		fail(w, http.StatusConflict, "email already registered")
		return
	}
	ok(w, staffView{ID: u.ID, Email: u.Email, Role: u.Role, Status: u.Status, CreatedAt: u.CreatedAt})
}

func (h *handlers) updateStaff(w http.ResponseWriter, r *http.Request) {
	var in struct{ Role, Status, Password string }
	if !readJSON(w, r, &in) {
		return
	}
	target, err := h.Store.UserByID(r.Context(), idOf(r))
	if err != nil || !target.IsStaff() {
		fail(w, http.StatusNotFound, "no such staff account")
		return
	}
	if in.Role != "" && !domain.ValidStaffRole(in.Role) {
		fail(w, http.StatusBadRequest, "role must be admin, operator or support")
		return
	}
	// The last active admin keeps its role and stays active.
	demoting := (in.Role != "" && in.Role != domain.RoleAdmin) || (in.Status != "" && in.Status != "active")
	if target.IsAdmin() && demoting {
		if n, _ := h.Store.CountAdmins(r.Context()); n <= 1 {
			fail(w, http.StatusConflict, "cannot demote or disable the last admin")
			return
		}
	}
	if in.Role != "" && in.Role != target.Role {
		if err := h.Store.SetRole(r.Context(), target.ID, in.Role); err != nil {
			serverErr(w, err)
			return
		}
	}
	status := target.Status
	if in.Status == "active" || in.Status == "banned" {
		status = in.Status
	}
	hash := ""
	if in.Password != "" {
		if len(in.Password) < 8 {
			fail(w, http.StatusBadRequest, "password too short")
			return
		}
		if hash, err = auth.HashPassword(in.Password); err != nil {
			serverErr(w, err)
			return
		}
	}
	if err := h.Store.UpdateStaff(r.Context(), target.ID, status, hash); err != nil {
		serverErr(w, err)
		return
	}
	if hash != "" {
		_ = h.Store.DeleteUserSessions(r.Context(), target.ID)
	}
	h.listStaff(w, r)
}

func (h *handlers) deleteStaff(w http.ResponseWriter, r *http.Request) {
	target, err := h.Store.UserByID(r.Context(), idOf(r))
	if err != nil || !target.IsStaff() {
		fail(w, http.StatusNotFound, "no such staff account")
		return
	}
	if target.ID == userFrom(r).ID {
		fail(w, http.StatusConflict, "cannot delete yourself")
		return
	}
	if target.IsAdmin() {
		if n, _ := h.Store.CountAdmins(r.Context()); n <= 1 {
			fail(w, http.StatusConflict, "cannot delete the last admin")
			return
		}
	}
	if err := h.Store.DeleteStaff(r.Context(), target.ID); err != nil {
		serverErr(w, err)
		return
	}
	ok(w, map[string]bool{"ok": true})
}

func (h *handlers) listTokens(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.ListAPITokens(r.Context(), userFrom(r).ID)
	if err != nil {
		serverErr(w, err)
		return
	}
	ok(w, list)
}

func (h *handlers) createToken(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name  string
		Scope string
		Days  int // 0 = no expiry
	}
	if !decode(r, &in) || strings.TrimSpace(in.Name) == "" {
		fail(w, http.StatusBadRequest, "name is required")
		return
	}
	var expires *time.Time
	if in.Days > 0 {
		t := time.Now().AddDate(0, 0, in.Days)
		expires = &t
	}
	plain, tok, err := h.Store.CreateAPIToken(r.Context(), userFrom(r).ID, strings.TrimSpace(in.Name), in.Scope, expires)
	if err != nil {
		serverErr(w, err)
		return
	}
	ok(w, map[string]any{"token": plain, "id": tok.ID, "name": tok.Name, "scope": tok.Scope, "expires_at": tok.ExpiresAt})
}

func (h *handlers) deleteToken(w http.ResponseWriter, r *http.Request) {
	if err := h.Store.DeleteAPIToken(r.Context(), userFrom(r).ID, idOf(r)); err != nil {
		serverErr(w, err)
		return
	}
	ok(w, map[string]bool{"ok": true})
}

// totpSetup stores a new (not yet enforced) secret and returns the otpauth URI.
func (h *handlers) totpSetup(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	// Re-enrolling while an authenticator is active would silently switch
	// 2FA off for a session holder: prove the current one first.
	if cur, enabled, _ := h.Store.TOTP(r.Context(), u.ID); enabled {
		var in struct{ Code string }
		_ = decode(r, &in)
		if !auth.VerifyTOTP(cur, in.Code, time.Now()) {
			fail(w, http.StatusForbidden, "current authenticator code required to re-enrol")
			return
		}
	}
	secret := auth.NewTOTPSecret()
	if err := h.Store.SetTOTP(r.Context(), u.ID, secret, false); err != nil {
		serverErr(w, err)
		return
	}
	ok(w, map[string]any{"secret": secret, "uri": auth.TOTPURI(firstNonEmpty(h.SiteName, "Captain"), u.Email, secret)})
}

func (h *handlers) totpEnable(w http.ResponseWriter, r *http.Request) {
	var in struct{ Code string }
	if !readJSON(w, r, &in) {
		return
	}
	u := userFrom(r)
	secret, _, err := h.Store.TOTP(r.Context(), u.ID)
	if err != nil || secret == "" {
		fail(w, http.StatusBadRequest, "run setup first")
		return
	}
	if !auth.VerifyTOTP(secret, in.Code, time.Now()) {
		fail(w, http.StatusBadRequest, "code does not match; check the app's clock")
		return
	}
	if err := h.Store.SetTOTP(r.Context(), u.ID, secret, true); err != nil {
		serverErr(w, err)
		return
	}
	ok(w, map[string]bool{"totp": true})
}

func (h *handlers) totpDisable(w http.ResponseWriter, r *http.Request) {
	var in struct{ Code string }
	if !readJSON(w, r, &in) {
		return
	}
	u := userFrom(r)
	secret, enabled, _ := h.Store.TOTP(r.Context(), u.ID)
	if enabled && !auth.VerifyTOTP(secret, in.Code, time.Now()) {
		fail(w, http.StatusBadRequest, "current authenticator code required")
		return
	}
	if err := h.Store.SetTOTP(r.Context(), u.ID, "", false); err != nil {
		serverErr(w, err)
		return
	}
	ok(w, map[string]bool{"totp": false})
}
