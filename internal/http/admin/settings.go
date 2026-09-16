package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/zeptop-dev/bosun/pkg/selfupdate"
	"github.com/zeptop-dev/captain/internal/http/site"
	"github.com/zeptop-dev/captain/internal/mail"
	"github.com/zeptop-dev/captain/internal/service"

	"github.com/zeptop-dev/captain/internal/store"
)

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

func (h *handlers) getACME(w http.ResponseWriter, r *http.Request) {
	var v store.ACMESettings
	if err := h.Store.GetSetting(r.Context(), store.SettingACME, &v); err != nil {
		serverErr(w, err)
		return
	}
	// The token is write-only; tell the UI whether one is set.
	ok(w, map[string]any{"email": v.Email, "has_cloudflare_token": v.CloudflareToken != ""})
}

// putACME stores the ACME account; an empty token keeps the current one,
// "-" clears it.
func (h *handlers) putACME(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, CloudflareToken string }
	if !readJSON(w, r, &in) {
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
		serverErr(w, err)
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

func (h *handlers) getSubscription(w http.ResponseWriter, r *http.Request) {
	var v service.SubscriptionSettings
	if err := h.Store.GetSetting(r.Context(), service.SettingSubscription, &v); err != nil {
		serverErr(w, err)
		return
	}
	if v.URLs == nil {
		v.URLs = []string{}
	}
	ok(w, v)
}

func (h *handlers) putSubscription(w http.ResponseWriter, r *http.Request) {
	var in service.SubscriptionSettings
	if !readJSON(w, r, &in) {
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
		serverErr(w, err)
		return
	}
	if h.SubLinks != nil {
		h.SubLinks.Invalidate()
	}
	ok(w, in)
}

func (h *handlers) getSite(w http.ResponseWriter, r *http.Request) {
	v := site.Defaults("")
	_ = h.Store.GetSetting(r.Context(), site.SettingSite, &v)
	ok(w, v)
}

func (h *handlers) putSite(w http.ResponseWriter, r *http.Request) {
	putSetting[site.Settings](h, w, r, site.SettingSite, nil)
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
	if !readJSON(w, r, &in) {
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
		serverErr(w, err)
		return
	}
	h.getOIDC(w, r)
}

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
	if !readJSON(w, r, &in) {
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
		serverErr(w, err)
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

func (h *handlers) getInvite(w http.ResponseWriter, r *http.Request) {
	getSetting[store.InviteSettings](h, w, r, store.SettingInvite, nil)
}

func (h *handlers) putInvite(w http.ResponseWriter, r *http.Request) {
	putSetting(h, w, r, store.SettingInvite, func(_ context.Context, v *store.InviteSettings) string {
		if v.Percent < 0 || v.Percent > 100 || v.Level2 < 0 || v.Level2 > 100 || v.Level3 < 0 || v.Level3 > 100 {
			return "percent must be 0-100"
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
		return ""
	})
}

func (h *handlers) getNotice(w http.ResponseWriter, r *http.Request) {
	getSetting[store.NoticeSettings](h, w, r, store.SettingNotice, nil)
}

func (h *handlers) putNotice(w http.ResponseWriter, r *http.Request) {
	putSetting[store.NoticeSettings](h, w, r, store.SettingNotice, nil)
}

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
	if !readJSON(w, r, &v) {
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
		serverErr(w, err)
		return
	}
	h.getRegistration(w, r)
}
