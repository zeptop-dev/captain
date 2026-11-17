package http

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zeptop-dev/captain/internal/captcha"
	"github.com/zeptop-dev/captain/internal/mail"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestRegistrationLegalAndVerificationAPI(t *testing.T) {
	ctx := context.Background()
	orig := mail.SendFunc
	var messages []mail.Message
	mail.SendFunc = func(_ context.Context, _ mail.Settings, m mail.Message) error {
		messages = append(messages, m)
		return nil
	}
	defer func() { mail.SendFunc = orig }()
	p := newPortalRig(t, true, func(s *store.Store) {
		m := mail.Settings{Provider: "smtp", FromAddress: "noreply@example.com", VerifyRegistration: true}
		m.SMTP.Host = "smtp.example.com"
		s.SetSetting(ctx, mail.SettingKey, m)
	})
	legal := &store.RegistrationLegal{TermsURL: "/terms", DocumentVersion: "v1"}
	p.st.SetSetting(ctx, store.SettingRegistration, store.RegistrationSettings{Legal: legal})
	anon := &client{t: t, srv: p.srv}
	payload := map[string]any{"Email": "new@example.com", "Password": "password123"}
	status, b, _ := anon.do("POST", "/api/portal/register", payload, nil)
	if status != 409 || !strings.Contains(string(b), `"code":"legal_changed"`) {
		t.Fatal(status, string(b))
	}
	status, b, _ = anon.do("POST", "/api/portal/verify/send", map[string]string{"Email": "new@example.com", "Purpose": "register"}, nil)
	if status != 200 {
		t.Fatal(status, string(b))
	}
	code := extractCode(messages[len(messages)-1].Subject)
	status, _, hdr := anon.do("POST", "/api/portal/verify/send", map[string]string{"Email": "new@example.com", "Purpose": "register"}, nil)
	if status != 429 || hdr.Get("Retry-After") != "60" {
		t.Fatal(status, hdr)
	}
	payload["Code"], payload["LegalAccepted"], payload["LegalVersion"] = code, true, legal.Version()
	status, b, _ = anon.do("POST", "/api/portal/register", payload, nil)
	if status != 200 || !strings.Contains(string(b), `"verified":true`) {
		t.Fatal(status, string(b))
	}
	// Anonymous and cross-origin callers cannot mark someone else's mailbox verified.
	status, _, _ = (&client{t: t, srv: p.srv}).do("POST", "/api/portal/me/email/verification", map[string]string{"Code": code}, nil)
	if status != 401 {
		t.Fatal(status)
	}
	_, oldSession := p.customer("old@example.com")
	status, b, _ = oldSession.do("GET", "/api/portal/me", nil, nil)
	if status != 200 || !strings.Contains(string(b), `"verified":false`) {
		t.Fatal(status, string(b))
	}
	status, b, _ = oldSession.do("POST", "/api/portal/me/email/verification/send", map[string]string{}, nil)
	if status != 200 {
		t.Fatal(status, string(b))
	}
	code = extractCode(messages[len(messages)-1].Subject)
	status, _, _ = oldSession.do("POST", "/api/portal/me/email/verification", map[string]string{"Code": code}, map[string]string{"Origin": "https://evil.example.com"})
	if status != 403 {
		t.Fatal(status)
	}
	status, b, _ = oldSession.do("POST", "/api/portal/me/email/verification", map[string]string{"Code": code}, nil)
	if status != 200 || !strings.Contains(string(b), `"verified":true`) {
		t.Fatal(status, string(b))
	}
	status, _, _ = oldSession.do("POST", "/api/portal/me/email/verification", map[string]string{"Code": code}, nil)
	if status != 400 {
		t.Fatal("replay", status)
	}
	// Old settings writers must not remove consent requirements.
	status, b, _ = p.admin.do("PUT", "/api/admin/settings/registration", map[string]any{}, nil)
	if status != 200 || !strings.Contains(string(b), `"document_version":"v1"`) {
		t.Fatal(status, string(b))
	}
}
func TestRegistrationCaptchaAndSpecificErrors(t *testing.T) {
	ctx := context.Background()
	orig := captcha.VerifyFunc
	captcha.VerifyFunc = func(_ context.Context, _ captcha.Settings, token, _ string) error {
		if token != "fresh" {
			return errors.New("captcha failed")
		}
		return nil
	}
	defer func() { captcha.VerifyFunc = orig }()
	origMail := mail.SendFunc
	sent := 0
	mail.SendFunc = func(context.Context, mail.Settings, mail.Message) error { sent++; return nil }
	defer func() { mail.SendFunc = origMail }()
	reg := store.RegistrationSettings{}
	reg.Captcha.Provider = "turnstile"
	reg.Captcha.SiteKey = "site"
	reg.Captcha.SecretKey = "secret"
	p := newPortalRig(t, true, func(s *store.Store) {
		s.SetSetting(ctx, store.SettingRegistration, reg)
		m := mail.Settings{Provider: "smtp", FromAddress: "noreply@example.com"}
		m.SMTP.Host = "smtp.example.com"
		s.SetSetting(ctx, mail.SettingKey, m)
	})
	c := &client{t: t, srv: p.srv}
	status, b, _ := c.do("POST", "/api/portal/verify/send", map[string]string{"Email": "new@example.com", "Purpose": "register"}, nil)
	if status != 403 || sent != 0 || !strings.Contains(string(b), `"code":"captcha_failed"`) {
		t.Fatal(status, string(b), sent)
	}
	status, b, _ = c.do("POST", "/api/portal/verify/send", map[string]string{"Email": "new@example.com", "Purpose": "register", "Captcha": "fresh"}, nil)
	if status != 200 || sent != 1 {
		t.Fatal(status, string(b), sent)
	}
	reg.EmailSuffixes = []string{"allowed.example.com"}
	p.st.SetSetting(ctx, store.SettingRegistration, reg)
	status, b, _ = c.do("POST", "/api/portal/register", map[string]string{"Email": "new@example.com", "Password": "password123"}, nil)
	if status != 403 || !strings.Contains(string(b), `"code":"email_domain"`) {
		t.Fatal(status, string(b))
	}
}
