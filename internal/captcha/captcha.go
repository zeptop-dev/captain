// Package captcha verifies challenge tokens from Cloudflare Turnstile,
// Google reCAPTCHA (v2/v3) and hCaptcha. All three share the same
// siteverify shape: POST secret+response, read {"success": bool}.
package captcha

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Settings selects a provider; empty Provider disables the check.
type Settings struct {
	Provider  string `json:"provider"` // "", "turnstile", "recaptcha", "hcaptcha"
	SiteKey   string `json:"site_key"`
	SecretKey string `json:"secret_key"`
}

// Enabled reports whether a provider is fully configured.
func (s Settings) Enabled() bool {
	return s.Provider != "" && s.SiteKey != "" && s.SecretKey != ""
}

var endpoints = map[string]string{
	"turnstile": "https://challenges.cloudflare.com/turnstile/v0/siteverify",
	"recaptcha": "https://www.google.com/recaptcha/api/siteverify",
	"hcaptcha":  "https://hcaptcha.com/siteverify",
}

// VerifyFunc performs the check; tests replace it.
var VerifyFunc = verify

// Verify checks a token from the browser widget.
func Verify(ctx context.Context, s Settings, token, remoteIP string) error {
	if !s.Enabled() {
		return nil
	}
	if strings.TrimSpace(token) == "" {
		return errors.New("captcha required")
	}
	return VerifyFunc(ctx, s, token, remoteIP)
}

func verify(ctx context.Context, s Settings, token, remoteIP string) error {
	ep, ok := endpoints[s.Provider]
	if !ok {
		return errors.New("unknown captcha provider")
	}
	form := url.Values{"secret": {s.SecretKey}, "response": {token}}
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.New("captcha verification unavailable")
	}
	defer resp.Body.Close()
	var out struct {
		Success bool     `json:"success"`
		Errors  []string `json:"error-codes"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&out); err != nil || !out.Success {
		return errors.New("captcha failed")
	}
	return nil
}
