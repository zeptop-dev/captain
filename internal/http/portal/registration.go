package portal

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/captcha"
	"github.com/zeptop-dev/captain/internal/http/ratelimit"
	"github.com/zeptop-dev/captain/internal/mail"
	"github.com/zeptop-dev/captain/internal/store"
)

func authFail(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	if code == "code_wait" && w.Header().Get("Retry-After") == "" {
		w.Header().Set("Retry-After", "60")
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message, "code": code})
}
func codeCooldown(w http.ResponseWriter, err error) {
	var cooldown *store.CodeCooldownError
	if errors.As(err, &cooldown) {
		w.Header().Set("Retry-After", strconv.Itoa(cooldown.RetryAfter))
	}
	authFail(w, 429, "code_wait", store.ErrCodeTooSoon.Error())
}

func registrationError(w http.ResponseWriter, err error) {
	code := "registration_failed"
	switch err.Error() {
	case "this email domain is not accepted":
		code = "email_domain"
	case "registration requires an invite code":
		code = "invite_required"
	case "too many sign-ups from this address; try again later":
		code = "registration_ip_limit"
	default:
		code = "captcha_failed"
	}
	authFail(w, 403, code, err.Error())
}
func (h *handlers) sendEmailVerification(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var in struct{ Captcha string }
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		authFail(w, 400, "invalid_input", "bad json")
		return
	}
	var reg store.RegistrationSettings
	if err := h.Store.GetSetting(r.Context(), store.SettingRegistration, &reg); err != nil {
		h.serverErr(w, "registration settings", err)
		return
	}
	if err := captcha.Verify(r.Context(), captcha.Settings{Provider: reg.Captcha.Provider, SiteKey: reg.Captcha.SiteKey, SecretKey: reg.Captcha.SecretKey}, in.Captcha, ratelimit.ClientIP(r)); err != nil {
		authFail(w, 403, "captcha_failed", "captcha verification failed; try again")
		return
	}
	ms := h.mailSettings(r)
	if !ms.Enabled() {
		authFail(w, 409, "mail_unavailable", "mail is not configured")
		return
	}
	if allowed, _ := h.codes.Allow(ratelimit.ClientIP(r)); !allowed {
		authFail(w, 429, "code_rate_limit", "too many codes requested; try again later")
		return
	}
	code, err := h.Store.NewCode(r.Context(), u.Email, store.EmailVerificationPurpose(u.AgentID), 10*time.Minute)
	if errors.Is(err, store.ErrCodeTooSoon) {
		codeCooldown(w, err)
		return
	}
	if err != nil {
		h.serverErr(w, "new verification code", err)
		return
	}
	h.codes.Fail(ratelimit.ClientIP(r))
	if err = mail.Send(r.Context(), ms, mail.Configured(ms, u.Lang).Code(h.SiteName, u.Email, "email_verify", code)); err != nil {
		authFail(w, 502, "mail_failed", "could not send the email; contact the administrator")
		return
	}
	ok(w, map[string]any{"sent": true, "retry_after": 60})
}
func (h *handlers) verifyEmail(w http.ResponseWriter, r *http.Request) {
	var in struct{ Code string }
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		authFail(w, 400, "invalid_input", "bad json")
		return
	}
	u := userFrom(r)
	if err := h.Store.VerifyUserEmail(r.Context(), u.ID, u.Email, strings.TrimSpace(in.Code)); err != nil {
		if errors.Is(err, store.ErrCodeInvalid) {
			authFail(w, 400, "code_invalid", err.Error())
		} else {
			h.serverErr(w, "verify email", err)
		}
		return
	}
	h.writeMe(w, r, u)
}
