package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/zeptop-dev/captain/internal/domain"
)

var ErrMapSelection = errors.New("invalid map node selection")
var ErrMapRevision = errors.New("map node selection changed; reload before saving")

// EntryLocationSettings is independent of routing. Link recompilation and old
// Entry editors cannot erase a manually corrected display location.
func (s *Store) EntryLocationSettings(ctx context.Context) (map[int64]*domain.LocationSettings, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT entry_id,settings_json FROM entry_locations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]*domain.LocationSettings{}
	for rows.Next() {
		var id int64
		var raw string
		if err = rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		var settings domain.LocationSettings
		if err = json.Unmarshal([]byte(raw), &settings); err != nil {
			return nil, err
		}
		if err = settings.Validate(); err != nil {
			return nil, err
		}
		out[id] = &settings
	}
	return out, rows.Err()
}

func (s *Store) SetEntryLocation(ctx context.Context, id int64, settings domain.LocationSettings) error {
	settings.Normalize()
	if err := settings.Validate(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var found int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM entries WHERE id=?`, id).Scan(&found); err != nil {
		return wrapNotFound(err)
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO entry_locations(entry_id,settings_json) VALUES(?,?) ON CONFLICT(entry_id) DO UPDATE SET settings_json=excluded.settings_json`, id, string(raw)); err != nil {
		return err
	}
	return tx.Commit()
}

// InboundExitNodes also covers extra Entries bound to a managed reverse user
// inbound. Looking only at a connection's original Entry would locate those
// aliases at the authentication front rather than the actual exit.
func (s *Store) InboundExitNodes(ctx context.Context) (map[int64]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT user_inbound_id,exit_id FROM node_reverse_links UNION ALL SELECT user_inbound_id,exit_id FROM node_wireguard_links`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int64{}
	for rows.Next() {
		var inbound, exit int64
		if err = rows.Scan(&inbound, &exit); err != nil {
			return nil, err
		}
		out[inbound] = exit
	}
	return out, rows.Err()
}

func (s *Store) SiteMapSelection(ctx context.Context) ([]int64, int64, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	var revision int64
	err = tx.QueryRowContext(ctx, `SELECT revision FROM site_map_state WHERE id=1`).Scan(&revision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, 0, err
	}
	ids := []int64{}
	rows, err := tx.QueryContext(ctx, `SELECT entry_id FROM site_map_entries ORDER BY position,entry_id`)
	if err != nil {
		return nil, 0, err
	}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	return ids, revision, tx.Commit()
}

// SaveSiteWithMap saves site copy and its explicit map publication references
// atomically. nil ids is a legacy/omitted field and leaves references intact.
// Only the caller's safe settings DTO (without IDs) belongs in settings JSON.
func (s *Store) SaveSiteWithMap(ctx context.Context, value any, ids []int64, expected *int64) (int64, error) {
	if len(ids) > 5000 {
		return 0, ErrMapSelection
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return 0, err
	}
	var settings map[string]json.RawMessage
	if err = json.Unmarshal(raw, &settings); err != nil || settings == nil {
		return 0, ErrMapSelection
	}
	delete(settings, "map_entry_ids")
	delete(settings, "map_revision")
	raw, err = json.Marshal(settings)
	if err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var revision int64
	err = tx.QueryRowContext(ctx, `SELECT revision FROM site_map_state WHERE id=1`).Scan(&revision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if ids != nil {
		if expected == nil || *expected != revision {
			return 0, ErrMapRevision
		}
		seen := map[int64]bool{}
		for _, id := range ids {
			if id <= 0 || seen[id] {
				return 0, ErrMapSelection
			}
			seen[id] = true
			var found int
			if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries WHERE id=?`, id).Scan(&found); err != nil {
				return 0, err
			}
			if found != 1 {
				return 0, ErrMapSelection
			}
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM site_map_entries`); err != nil {
			return 0, err
		}
		for i, id := range ids {
			if _, err = tx.ExecContext(ctx, `INSERT INTO site_map_entries(entry_id,position) VALUES(?,?)`, id, i); err != nil {
				return 0, err
			}
		}
		revision++
		if _, err = tx.ExecContext(ctx, `INSERT INTO site_map_state(id,revision) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET revision=excluded.revision`, revision); err != nil {
			return 0, err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO settings(key,value_json) VALUES('site',?) ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json`, string(raw)); err != nil {
		return 0, err
	}
	return revision, tx.Commit()
}
