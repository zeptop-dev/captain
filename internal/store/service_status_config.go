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
	c.Normalize()
	// Read the revision and both reference lists from one snapshot. A deletion
	// must never leave a response with the old revision and the new allowlist.
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return c, err
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(ctx, `SELECT public_enabled, revision FROM service_status_settings WHERE id=1`).Scan(&c.PublicEnabled, &c.Revision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return c, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT entry_id FROM service_status_public_entries ORDER BY position, entry_id`)
	if err != nil {
		return c, err
	}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return c, err
		}
		c.PublicEntryIDs = append(c.PublicEntryIDs, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return c, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT entry_id, starts_at, ends_at, note FROM service_status_maintenance ORDER BY position, entry_id`)
	if err != nil {
		return c, err
	}
	for rows.Next() {
		var m ServiceMaintenance
		if err = rows.Scan(&m.EntryID, &m.StartsAt, &m.EndsAt, &m.Note); err != nil {
			rows.Close()
			return c, err
		}
		c.Maintenance = append(c.Maintenance, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return c, err
	}
	return c, tx.Commit()
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
	var revision int64
	err = tx.QueryRowContext(ctx, `SELECT revision FROM service_status_settings WHERE id=1`).Scan(&revision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return c, err
	}
	if revision != c.Revision {
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
		var raw string
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
	_, err = tx.ExecContext(ctx, `INSERT INTO service_status_settings(id,public_enabled,revision) VALUES(1,?,?) ON CONFLICT(id) DO UPDATE SET public_enabled=excluded.public_enabled, revision=excluded.revision`, c.PublicEnabled, c.Revision)
	if err != nil {
		return c, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM service_status_public_entries`); err != nil {
		return c, err
	}
	for i, id := range c.PublicEntryIDs {
		if _, err = tx.ExecContext(ctx, `INSERT INTO service_status_public_entries(entry_id,position) VALUES(?,?)`, id, i); err != nil {
			return c, err
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM service_status_maintenance`); err != nil {
		return c, err
	}
	for i, m := range c.Maintenance {
		if _, err = tx.ExecContext(ctx, `INSERT INTO service_status_maintenance(entry_id,position,starts_at,ends_at,note) VALUES(?,?,?,?,?)`, m.EntryID, i, m.StartsAt, m.EndsAt, m.Note); err != nil {
			return c, err
		}
	}
	return c, tx.Commit()
}
