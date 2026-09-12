package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// GetSetting reads a JSON setting into v; missing keys leave v untouched.
func (s *Store) GetSetting(ctx context.Context, key string, v any) error {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value_json FROM settings WHERE key = ?`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(raw), v)
}

// SetSetting stores v as JSON under key.
func (s *Store) SetSetting(ctx context.Context, key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO settings (key, value_json) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value_json = excluded.value_json`, key, string(raw))
	return err
}

// ACMESettings is the panel-wide certificate automation account pushed to
// every node.
type ACMESettings struct {
	Email           string `json:"email"`
	CloudflareToken string `json:"cloudflare_token"`
}

// SettingACME is the settings key for ACMESettings.
const SettingACME = "acme"

// NoticeSettings is the announcement shown on the portal home page.
type NoticeSettings struct {
	Enabled bool   `json:"enabled"`
	Title   string `json:"title"`
	Body    string `json:"body"`
}

// SettingNotice is the settings key.
const SettingNotice = "notice"

// RegistrationSettings limits who can sign up.
type RegistrationSettings struct {
	// EmailSuffixes, when non-empty, allows only these domains ("gmail.com").
	EmailSuffixes []string `json:"email_suffixes"`
	// InviteOnly requires a valid invite code (link or field) to register.
	InviteOnly bool `json:"invite_only"`
	// IPLimit caps sign-ups per address within IPWindowHours; 0 = off.
	IPLimit       int `json:"ip_limit"`
	IPWindowHours int `json:"ip_window_hours"`
	// Captcha protects the password sign-up form.
	Captcha captchaSettings `json:"captcha"`
}

type captchaSettings = struct {
	Provider  string `json:"provider"`
	SiteKey   string `json:"site_key"`
	SecretKey string `json:"secret_key"`
}

// SettingRegistration is the settings key.
const SettingRegistration = "registration"

// EmailAllowed applies the suffix whitelist.
func (r RegistrationSettings) EmailAllowed(email string) bool {
	if len(r.EmailSuffixes) == 0 {
		return true
	}
	at := strings.LastIndexByte(email, '@')
	if at < 0 {
		return false
	}
	domain := strings.ToLower(email[at+1:])
	for _, s := range r.EmailSuffixes {
		s = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(s), "@"))
		if s != "" && (domain == s || strings.HasSuffix(domain, "."+s)) {
			return true
		}
	}
	return false
}

// RegistrationsFromIP counts accounts created from ip since the cutoff.
func (s *Store) RegistrationsFromIP(ctx context.Context, ip string, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE register_ip = ? AND created_at >= ?`, ip, since.Unix()).Scan(&n)
	return n, err
}
