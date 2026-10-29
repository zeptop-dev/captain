package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type MonitorIncident struct {
	ID             int64   `json:"id"`
	NodeID         int64   `json:"node_id"`
	Node           string  `json:"node"`
	Kind           string  `json:"kind"`
	StartedAt      int64   `json:"started_at"`
	UpdatedAt      int64   `json:"updated_at"`
	EndedAt        *int64  `json:"ended_at"`
	Resolution     string  `json:"resolution"`
	Value          float64 `json:"value"`
	Threshold      float64 `json:"threshold"`
	NotifiedAt     int64   `json:"notified_at"`
	AcknowledgedAt int64   `json:"acknowledged_at"`
	AcknowledgedBy string  `json:"acknowledged_by"` // immutable audit label, independent of editable staff IDs
}

type IncidentCheck struct {
	Kind             string
	Active           bool
	Value, Threshold float64
	Resolution       string // empty means recovered; disabled/reset never emits a recovery
}

type IncidentNotice struct {
	ID        int64
	Kind      string
	Recovered bool
}

// CheckIncident persists state before claiming a best-effort notification. An
// unnotified incident can notify on a later evaluation after silence expires.
func (s *Store) CheckIncident(ctx context.Context, nodeID int64, c IncidentCheck, at time.Time) (*IncidentNotice, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var id, updated, notified int64
	var ended sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT id,updated_at,notified_at,ended_at FROM monitor_incidents WHERE node_id=? AND kind=? ORDER BY id DESC LIMIT 1`, nodeID, c.Kind).Scan(&id, &updated, &notified, &ended)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if updated > at.Unix() {
		return nil, nil
	}
	active := id != 0 && !ended.Valid
	if !active && !c.Active {
		return nil, nil
	}
	if !active {
		r, e := tx.ExecContext(ctx, `INSERT INTO monitor_incidents(node_id,kind,started_at,updated_at,value,threshold) VALUES(?,?,?,?,?,?)`, nodeID, c.Kind, at.Unix(), at.Unix(), c.Value, c.Threshold)
		if e != nil {
			return nil, e
		}
		id, _ = r.LastInsertId()
		notified = 0
	} else {
		// Disabling a rule supplies no new measurement. Preserve the last valid
		// value/threshold in history instead of replacing them with artificial zeros.
		if !c.Active && c.Resolution == "disabled" {
			_, err = tx.ExecContext(ctx, `UPDATE monitor_incidents SET updated_at=? WHERE id=?`, at.Unix(), id)
		} else {
			_, err = tx.ExecContext(ctx, `UPDATE monitor_incidents SET updated_at=?,value=?,threshold=? WHERE id=?`, at.Unix(), c.Value, c.Threshold, id)
		}
		if err != nil {
			return nil, err
		}
	}
	muted, err := monitorMuted(ctx, tx, nodeID, at.Unix())
	if err != nil {
		return nil, err
	}
	var notice *IncidentNotice
	if c.Active {
		if notified == 0 && !muted {
			if _, err = tx.ExecContext(ctx, `UPDATE monitor_incidents SET notified_at=? WHERE id=?`, at.Unix(), id); err != nil {
				return nil, err
			}
			notice = &IncidentNotice{ID: id, Kind: c.Kind}
		}
	} else {
		resolution := c.Resolution
		if resolution == "" {
			resolution = "recovered"
		}
		if _, err = tx.ExecContext(ctx, `UPDATE monitor_incidents SET ended_at=?,resolution=? WHERE id=?`, at.Unix(), resolution, id); err != nil {
			return nil, err
		}
		if resolution == "recovered" && notified != 0 && !muted {
			notice = &IncidentNotice{ID: id, Kind: c.Kind, Recovered: true}
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return notice, nil
}

// DisableMonitoring ends events without claiming a recovery, and prevents
// availability from spanning a period when collection was switched off.
func (s *Store) DisableMonitoring(ctx context.Context, at time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE monitor_incidents SET ended_at=?,updated_at=?,resolution='disabled' WHERE ended_at IS NULL`, at.Unix(), at.Unix()); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM monitor_observations`); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) AcknowledgeIncident(ctx context.Context, id int64, actor string, at time.Time) error {
	r, err := s.db.ExecContext(ctx, `UPDATE monitor_incidents SET acknowledged_at=?,acknowledged_by=? WHERE id=? AND ended_at IS NULL AND acknowledged_at=0`, at.Unix(), actor, id)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) MonitorIncidents(ctx context.Context, nodeID, before int64, state string) ([]MonitorIncident, error) {
	query := `SELECT i.id,i.node_id,n.name,i.kind,i.started_at,i.updated_at,i.ended_at,i.resolution,i.value,i.threshold,i.notified_at,i.acknowledged_at,i.acknowledged_by FROM monitor_incidents i JOIN nodes n ON n.id=i.node_id WHERE 1=1`
	args := []any{}
	if nodeID > 0 {
		query += ` AND i.node_id=?`
		args = append(args, nodeID)
	}
	if before > 0 {
		query += ` AND i.id<?`
		args = append(args, before)
	}
	switch state {
	case "open":
		query += ` AND i.ended_at IS NULL`
	case "acknowledged":
		query += ` AND i.ended_at IS NULL AND i.acknowledged_at>0`
	case "resolved":
		query += ` AND i.ended_at IS NOT NULL`
	}
	rows, err := s.db.QueryContext(ctx, query+` ORDER BY i.id DESC LIMIT 51`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MonitorIncident{}
	for rows.Next() {
		var v MonitorIncident
		if err = rows.Scan(&v.ID, &v.NodeID, &v.Node, &v.Kind, &v.StartedAt, &v.UpdatedAt, &v.EndedAt, &v.Resolution, &v.Value, &v.Threshold, &v.NotifiedAt, &v.AcknowledgedAt, &v.AcknowledgedBy); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
