package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/zeptop-dev/bosun/pkg/selfupdate"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/auth"
)

type RouteSchedule struct {
	Enabled       bool   `json:"enabled"`
	IntervalHours int    `json:"interval_hours"`
	City          string `json:"city"`
	Family        string `json:"family"`
}

func (p RouteSchedule) Validate() error {
	if err := (spec.ReturnRouteRequest{City: p.City, Family: p.Family}).Validate(); err != nil {
		return err
	}
	if p.IntervalHours != 6 && p.IntervalHours != 12 && p.IntervalHours != 24 {
		return fmt.Errorf("interval must be 6, 12 or 24 hours")
	}
	return nil
}
func (s *Store) RouteSchedule(ctx context.Context, id int64) (RouteSchedule, error) {
	p := RouteSchedule{IntervalHours: 6, City: "beijing", Family: "ipv4"}
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT config_json FROM node_route_schedules WHERE node_id=?`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	err = json.Unmarshal([]byte(raw), &p)
	return p, err
}

// Caller holds Topology; changing the schedule starts a fresh interval.
// Enabling never silently runs an immediate measurement.
func (s *Store) SetRouteSchedule(ctx context.Context, id int64, p RouteSchedule) error {
	if err := p.Validate(); err != nil {
		return err
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var rawOld string
	e := tx.QueryRowContext(ctx, "SELECT config_json FROM node_route_schedules WHERE node_id=?", id).Scan(&rawOld)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	var old RouteSchedule
	if e == nil && json.Unmarshal([]byte(rawOld), &old) == nil && (old.City != p.City || old.Family != p.Family) {
		if _, err = tx.ExecContext(ctx, "DELETE FROM node_route_states WHERE node_id=?", id); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE node_jobs SET done_at=?,error='route schedule changed' WHERE node_id=? AND route_check=1 AND done_at IS NULL`, now(), id); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE monitor_incidents SET ended_at=?,updated_at=?,resolution='disabled' WHERE node_id=? AND kind LIKE 'route_%' AND ended_at IS NULL`, now(), now(), id); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO node_route_schedules(node_id,config_json,next_at) VALUES(?,?,?) ON CONFLICT(node_id) DO UPDATE SET config_json=excluded.config_json,next_at=excluded.next_at`, id, string(raw), time.Now().Add(time.Duration(p.IntervalHours)*time.Hour).Unix()); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) ReturnRoutes(ctx context.Context, id int64) ([]NodeJob, error) {
	if err := s.expireNetworkDiagnostics(ctx, id); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,node_id,kind,params_json,result_json,error,created_at,done_at FROM node_jobs WHERE node_id=? AND kind=? AND route_check=1 AND created_at>=? ORDER BY created_at DESC,rowid DESC LIMIT 120`, id, spec.NetworkDiagnosticKind, time.Now().Add(-30*24*time.Hour).Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NodeJob{}
	for rows.Next() {
		j, err := scanNodeJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// ScheduleReturnRoutes only queues jobs; no network I/O occurs under the locks.
// Node, schedule and next_at are read and changed in the same transaction.
func (s *Store) ScheduleReturnRoutes(ctx context.Context, at time.Time) error {
	s.Topology.Lock()
	defer s.Topology.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT r.node_id,r.config_json,n.version FROM node_route_schedules r JOIN nodes n ON n.id=r.node_id WHERE r.next_at<=? AND n.token_hash IS NOT NULL AND n.last_seen_at>=? ORDER BY r.next_at LIMIT 16`, at.Unix(), at.Add(-3*time.Minute).Unix())
	if err != nil {
		return err
	}
	type due struct {
		id      int64
		p       RouteSchedule
		version string
	}
	var list []due
	for rows.Next() {
		var d due
		var raw string
		if err := rows.Scan(&d.id, &raw, &d.version); err != nil {
			rows.Close()
			return err
		}
		if json.Unmarshal([]byte(raw), &d.p) == nil && d.p.Validate() == nil {
			list = append(list, d)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, d := range list {
		// Advance even unsupported/disabled rows to avoid starving later nodes.
		if _, err = tx.ExecContext(ctx, `UPDATE node_route_schedules SET next_at=? WHERE node_id=?`, at.Add(time.Duration(d.p.IntervalHours)*time.Hour).Unix(), d.id); err != nil {
			return err
		}
		if !d.p.Enabled || (d.version != spec.ReturnRouteMinimumVersion && !selfupdate.Newer(d.version, spec.ReturnRouteMinimumVersion)) {
			continue
		}
		req := spec.DiagnosticRequest{Type: "return_route", Route: &spec.ReturnRouteRequest{City: d.p.City, Family: d.p.Family}}
		err = queueNetworkDiagnostic(ctx, tx, auth.Token(12), d.id, req, at.Unix())
		if errors.Is(err, ErrDiagnosticPending) {
			_, err = tx.ExecContext(ctx, `UPDATE node_route_schedules SET next_at=? WHERE node_id=?`, at.Add(5*time.Minute).Unix(), d.id)
		}
		if err != nil {
			return err
		}
	}
	// Bound retention even after a schedule is switched off.
	if _, err = tx.ExecContext(ctx, `DELETE FROM node_jobs WHERE route_check=1 AND created_at<?`, at.Add(-30*24*time.Hour).Unix()); err != nil {
		return err
	}
	return tx.Commit()
}
