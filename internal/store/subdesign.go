package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/zeptop-dev/bosun/pkg/subdesign"
)

const SettingSubPresetLibrary = "sub_design_presets"
const SettingSubCustomPresets = "sub_design_custom_presets"

// SubDesignPreset keeps the upstream snapshot independently of the active
// design and templates. Source text is never returned by the catalogue API.
type SubDesignPreset struct {
	Key       string            `json:"key"`
	Name      string            `json:"name"`
	Source    string            `json:"source"`
	Revision  string            `json:"revision,omitempty"`
	URL       string            `json:"url,omitempty"`
	Supported bool              `json:"supported"`
	Reason    string            `json:"reason,omitempty"`
	Design    *subdesign.Design `json:"design,omitempty"`
	Content   string            `json:"content,omitempty"`
}

type SubPresetLibrary struct {
	Revision string            `json:"revision"`
	SyncedAt int64             `json:"synced_at"`
	Items    []SubDesignPreset `json:"items"`
}

var ErrSubPresetLimit = errors.New("at most 100 custom subscription presets")
var ErrSubPresetName = errors.New("a custom subscription preset already uses this name")

// SaveSubCustomPreset appends a named snapshot without racing another admin's
// save/delete. No change to this library can change an active subscription.
func (s *Store) SaveSubCustomPreset(ctx context.Context, p SubDesignPreset) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var list []SubDesignPreset
	if err := readSettingTx(ctx, tx, SettingSubCustomPresets, &list); err != nil {
		return err
	}
	if len(list) >= 100 {
		return ErrSubPresetLimit
	}
	for _, old := range list {
		if strings.EqualFold(old.Name, p.Name) {
			return ErrSubPresetName
		}
		if old.Key == p.Key {
			return fmt.Errorf("duplicate subscription preset key")
		}
	}
	list = append(list, p)
	if err := writeSettingTx(ctx, tx, SettingSubCustomPresets, list); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeleteSubCustomPreset(ctx context.Context, key string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var list []SubDesignPreset
	if err := readSettingTx(ctx, tx, SettingSubCustomPresets, &list); err != nil {
		return err
	}
	found := false
	for i, p := range list {
		if p.Key == key {
			list = append(list[:i], list[i+1:]...)
			found = true
			break
		}
	}
	if !found {
		return ErrNotFound
	}
	if list == nil {
		list = []SubDesignPreset{}
	}
	if err := writeSettingTx(ctx, tx, SettingSubCustomPresets, list); err != nil {
		return err
	}
	return tx.Commit()
}

// ApplySubDesign commits the draft and only the explicitly rendered formats
// together. Read/merge/write of the template map stays in one transaction.
func (s *Store) ApplySubDesign(ctx context.Context, d *subdesign.Design, rendered map[string]string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current SubTemplates
	if err := readSettingTx(ctx, tx, SettingSubTemplates, &current); err != nil {
		return err
	}
	if current == nil {
		current = SubTemplates{}
	}
	for format, body := range rendered {
		current[format] = body
	}
	if err := writeSettingTx(ctx, tx, SettingSubDesign, d); err != nil {
		return err
	}
	if body, selected := rendered["clash"]; selected {
		if err := writeClaudeAppliedTx(ctx, tx, d, body); err != nil {
			return err
		}
	}
	if err := writeSettingTx(ctx, tx, SettingSubTemplates, current); err != nil {
		return err
	}
	return tx.Commit()
}

func readSettingTx(ctx context.Context, tx *sql.Tx, key string, v any) error {
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT value_json FROM settings WHERE key = ?`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(raw), v)
}

func writeSettingTx(ctx context.Context, tx *sql.Tx, key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO settings (key, value_json) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value_json = excluded.value_json`, key, string(raw))
	return err
}
