package admin

import (
	"net/http"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/auth"
	"github.com/zeptop-dev/captain/internal/http/ratelimit"
)

func (h *handlers) resetPreview(w http.ResponseWriter, r *http.Request) {
	counts, err := h.Store.ResetPreview(r.Context())
	if err != nil {
		serverErr(w, err)
		return
	}
	ok(w, counts)
}

func (h *handlers) resetSite(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password          string `json:"password"`
		Code              string `json:"code"`
		Confirmation      string `json:"confirmation"`
		NodesAcknowledged bool   `json:"nodes_acknowledged"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Confirmation != "RESET" {
		fail(w, http.StatusBadRequest, "type RESET to confirm")
		return
	}
	actor := userFrom(r)
	ip := ratelimit.ClientIP(r)
	if h.Logins != nil {
		if allowed, _ := h.Logins.Allow(ip); !allowed {
			fail(w, http.StatusTooManyRequests, "too many failed attempts; try again later")
			return
		}
	}
	if !auth.VerifyPasswordOrDummy(actor.PasswordHash, in.Password) {
		if h.Logins != nil {
			h.Logins.Fail(ip)
		}
		fail(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	counts, err := h.Store.ResetPreview(r.Context())
	if err != nil {
		serverErr(w, err)
		return
	}
	if counts["nodes"] > 0 && !in.NodesAcknowledged {
		fail(w, http.StatusBadRequest, "acknowledge that remote nodes retain their running configuration")
		return
	}
	secret, enabled, err := h.Store.TOTP(r.Context(), actor.ID)
	if err != nil {
		serverErr(w, err)
		return
	}
	if enabled {
		if strings.TrimSpace(in.Code) == "" {
			fail(w, http.StatusPreconditionRequired, "authenticator code required")
			return
		}
		if !auth.VerifyTOTPOnce("staff:"+auth.SHA256Hex(secret), secret, in.Code, time.Now()) {
			if h.Logins != nil {
				h.Logins.Fail(ip)
			}
			fail(w, http.StatusUnauthorized, "invalid or already used authenticator code")
			return
		}
	}
	fresh, err := h.Store.ResetSite(r.Context(), actor.ID)
	if err != nil {
		h.Log.Error("site reset failed", "err", err)
		fail(w, http.StatusInternalServerError, "site reset failed; existing data was retained")
		return
	}
	*actor = *fresh // the audit middleware records this action against ID 1
	if h.State != nil {
		h.State.Reset()
	}
	if h.Dyn != nil {
		h.Dyn.Reset()
	}
	if h.Diagnostics != nil {
		h.Diagnostics.Reset()
	}
	if h.Probe != nil {
		h.Probe.Reset()
	}
	if h.Heartbeat != nil {
		h.Heartbeat.Reset()
	}
	if h.Allow != nil {
		h.Allow.Invalidate()
	}
	if h.SubLinks != nil {
		h.SubLinks.Invalidate()
	}
	if h.Bot != nil {
		h.Bot.Invalidate()
	}
	if h.Hooks != nil {
		h.Hooks.Invalidate()
	}
	if h.Mail != nil {
		h.Mail.Invalidate()
	}
	if h.Logins != nil {
		h.Logins.Reset(ip)
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: h.Secure, MaxAge: -1})
	ok(w, map[string]any{"ok": true, "admin_id": fresh.ID})
}
