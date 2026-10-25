package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const SettingServiceStatus = "service_status"
const ServiceStatusPath = "/service-status"

var (
	ErrServiceStatusConflict     = errors.New("service status settings changed; reload before saving")
	ErrServiceStatusInvalid      = errors.New("invalid service status settings")
	ErrServiceStatusPathConflict = errors.New("service status path conflicts with server monitoring")
)

// ServiceStatusConfig controls presentation only. It does not publish a
// subscription, change routing, silence monitoring, or disable an inbound.
type ServiceStatusConfig struct {
	Revision       int64                `json:"revision"`
	PublicEnabled  bool                 `json:"public_enabled"`
	PublicEntryIDs []int64              `json:"public_entry_ids"`
	Maintenance    []ServiceMaintenance `json:"maintenance"`
}

type ServiceMaintenance struct {
	EntryID  int64  `json:"entry_id"`
	StartsAt int64  `json:"starts_at"`
	EndsAt   int64  `json:"ends_at"`
	Note     string `json:"note"`
}

func (c *ServiceStatusConfig) Normalize() {
	if c.PublicEntryIDs == nil {
		c.PublicEntryIDs = []int64{}
	}
	if c.Maintenance == nil {
		c.Maintenance = []ServiceMaintenance{}
	}
}

func (s *Store) ServiceStatusConfig(ctx context.Context) (ServiceStatusConfig, error) {
	var c ServiceStatusConfig
	err := s.GetSetting(ctx, SettingServiceStatus, &c)
	c.Normalize()
	return c, err
}

// SaveServiceStatusConfig compares the revision and validates every referenced
// entry in the same transaction. Callers serialize topology/settings edits.
func (s *Store) SaveServiceStatusConfig(ctx context.Context, c ServiceStatusConfig, at time.Time) (ServiceStatusConfig, error) {
	c.Normalize()
	if c.Revision < 0 || len(c.PublicEntryIDs) > 5000 || len(c.Maintenance) > 1000 {
		return c, ErrServiceStatusInvalid
	}
	ids := make(map[int64]bool)
	for _, id := range c.PublicEntryIDs {
		if id <= 0 || ids[id] {
			return c, ErrServiceStatusInvalid
		}
		ids[id] = true
	}
	windows := make(map[int64]bool)
	for i := range c.Maintenance {
		m := &c.Maintenance[i]
		m.Note = strings.TrimSpace(m.Note)
		if m.EntryID <= 0 || windows[m.EntryID] || m.StartsAt <= 0 || m.StartsAt > at.Add(365*24*time.Hour).Unix() || m.EndsAt <= m.StartsAt || m.EndsAt-m.StartsAt > int64(30*24*time.Hour/time.Second) || !utf8.ValidString(m.Note) || utf8.RuneCountInString(m.Note) > 512 {
			return c, ErrServiceStatusInvalid
		}
		for _, r := range m.Note {
			if unicode.IsControl(r) && r != '\n' && r != '\t' {
				return c, ErrServiceStatusInvalid
			}
		}
		windows[m.EntryID] = true
		ids[m.EntryID] = true
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return c, err
	}
	defer tx.Rollback()
	var raw string
	var old ServiceStatusConfig
	err = tx.QueryRowContext(ctx, `SELECT value_json FROM settings WHERE key=?`, SettingServiceStatus).Scan(&raw)
	if err == nil {
		if err = json.Unmarshal([]byte(raw), &old); err != nil {
			return c, err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return c, err
	}
	if old.Revision != c.Revision {
		return c, ErrServiceStatusConflict
	}
	for id := range ids {
		var found int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries WHERE id=?`, id).Scan(&found); err != nil {
			return c, err
		}
		if found != 1 {
			return c, ErrServiceStatusInvalid
		}
	}
	if c.PublicEnabled {
		var p ProbeSettings
		err = tx.QueryRowContext(ctx, `SELECT value_json FROM settings WHERE key=?`, SettingProbe).Scan(&raw)
		if err == nil {
			if err = json.Unmarshal([]byte(raw), &p); err != nil {
				return c, err
			}
			p.Normalize()
			if p.PageOn() && "/"+strings.Trim(p.Path, "/") == ServiceStatusPath {
				return c, ErrServiceStatusPathConflict
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return c, err
		}
	}
	c.Revision++
	b, err := json.Marshal(c)
	if err != nil {
		return c, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO settings(key,value_json) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json`, SettingServiceStatus, string(b))
	if err != nil {
		return c, err
	}
	return c, tx.Commit()
}
