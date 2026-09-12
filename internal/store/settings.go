package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
