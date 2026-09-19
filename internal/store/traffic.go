package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
)

// AddTraffic records one delta for a user on an inbound (daily bucket) and
// charges it to the subscription that fits the inbound's group best.
func (s *Store) AddTraffic(ctx context.Context, userID, inboundID int64, up, down int64, at time.Time) error {
	var group sql.NullInt64
	_ = s.db.QueryRowContext(ctx, `SELECT group_id FROM inbounds WHERE id = ?`, inboundID).Scan(&group)
	var groups []int64
	if group.Valid {
		groups = []int64{group.Int64}
	}
	return s.AddTrafficBatch(ctx, inboundID, groups, []spec.UserTraffic{{UserID: userID, Up: up, Down: down}}, at)
}

// NodeGroups lists the distinct user groups of a node's enabled inbounds.
func (s *Store) NodeGroups(ctx context.Context, nodeID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT group_id FROM inbounds WHERE node_id = ? AND enabled = 1 AND group_id IS NOT NULL`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var g int64
		if err := rows.Scan(&g); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// AddTrafficBatch records a node report's deltas in one transaction:
// each sample lands in the daily bucket of inboundID (the node's first
// inbound, for stats) and on the user's subscription that matches one of
// the node's groups, else the soonest-expiring usable one (chargeableTx).
func (s *Store) AddTrafficBatch(ctx context.Context, inboundID int64, groups []int64, samples []spec.UserTraffic, at time.Time) error {
	out := make([]TrafficSample, 0, len(samples))
	for _, t := range samples {
		out = append(out, TrafficSample{UserID: t.UserID, InboundID: inboundID, Groups: groups, Up: t.Up, Down: t.Down})
	}
	_, err := s.AddTrafficSamples(ctx, out, at)
	return err
}

// TrafficSample is one user's delta on one inbound: InboundID for the
// daily bucket, Groups the subscription groups that may pay for it (the
// inbound's own group, or every group of the node when the agent could
// not say which inbound; empty = any active subscription).
type TrafficSample struct {
	UserID    int64
	InboundID int64
	Groups    []int64
	Up, Down  int64
}

// AddTrafficSamples records a report's deltas in one transaction and
// charges each to the subscription that fits its inbound's group best
// (chargeableTx): the soonest-expiring usable subscription of a plan in
// Groups, else the soonest-expiring usable one of any plan.
func (s *Store) AddTrafficSamples(ctx context.Context, samples []TrafficSample, at time.Time) ([]int64, error) {
	if len(samples) == 0 {
		return nil, nil
	}
	day := at.UTC().Truncate(24 * time.Hour).Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	ts := now()
	var first []int64
	seen := map[int64]bool{}
	for _, t := range samples {
		if t.Up < 0 || t.Down < 0 || (t.Up == 0 && t.Down == 0) {
			continue
		}
		if !seen[t.UserID] {
			seen[t.UserID] = true
			res, err := tx.ExecContext(ctx, `UPDATE users SET first_connected_at = ? WHERE id = ? AND first_connected_at IS NULL`, at.Unix(), t.UserID)
			if err != nil {
				return nil, err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				first = append(first, t.UserID)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO traffic_daily (user_id, inbound_id, day, up_bytes, down_bytes) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(user_id, inbound_id, day) DO UPDATE SET up_bytes = up_bytes + excluded.up_bytes, down_bytes = down_bytes + excluded.down_bytes`,
			t.UserID, t.InboundID, day, t.Up, t.Down); err != nil {
			return nil, err
		}
		if id, found := chargeableTx(ctx, tx, t.UserID, t.Groups, at); found {
			if _, err := tx.ExecContext(ctx, `UPDATE subscriptions SET used_up_bytes = used_up_bytes + ?, used_down_bytes = used_down_bytes + ?, updated_at = ? WHERE id = ?`, t.Up, t.Down, ts, id); err != nil {
				return nil, err
			}
		}
	}
	return first, tx.Commit()
}

// UpsertOnline records client IPs seen for a user on a node.
func (s *Store) UpsertOnline(ctx context.Context, userID, nodeID int64, ips []string, at time.Time) error {
	for _, ip := range ips {
		if _, err := s.db.ExecContext(ctx, `INSERT INTO online_devices (user_id, node_id, ip, last_seen_at) VALUES (?, ?, ?, ?)
			ON CONFLICT(user_id, node_id, ip) DO UPDATE SET last_seen_at = excluded.last_seen_at`, userID, nodeID, ip, at.Unix()); err != nil {
			return err
		}
	}
	return nil
}

// UpsertForwardStatus stores a relay rule's latest report.
func (s *Store) UpsertForwardStatus(ctx context.Context, nodeID int64, f agentproto.ForwardStatus) error {
	targets := ""
	if len(f.Targets) > 0 {
		b, _ := json.Marshal(f.Targets)
		targets = string(b)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO forward_status (node_id, tag, up, rtt_ms, last_error, active_conn, total_conn, bytes_in, bytes_out, updated_at, targets_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id, tag) DO UPDATE SET up = excluded.up, rtt_ms = excluded.rtt_ms, last_error = excluded.last_error,
		active_conn = excluded.active_conn, total_conn = excluded.total_conn, bytes_in = excluded.bytes_in, bytes_out = excluded.bytes_out,
		updated_at = excluded.updated_at, targets_json = excluded.targets_json`,
		nodeID, f.Tag, boolInt(f.Up), f.RTTMillis, f.LastError, f.ActiveConn, f.TotalConn, f.BytesIn, f.BytesOut, now(), targets)
	return err
}

// AddInboundTraffic folds a node's per-inbound counters into the day row.
func (s *Store) AddInboundTraffic(ctx context.Context, inboundID, up, down int64, at time.Time) error {
	day := at.UTC().Truncate(24 * time.Hour).Unix()
	_, err := s.db.ExecContext(ctx, `INSERT INTO inbound_traffic_daily (inbound_id, day, up_bytes, down_bytes) VALUES (?, ?, ?, ?)
		ON CONFLICT(inbound_id, day) DO UPDATE SET up_bytes = up_bytes + excluded.up_bytes, down_bytes = down_bytes + excluded.down_bytes`, inboundID, day, up, down)
	return err
}

// InboundUsage is today's and the lifetime traffic of one inbound.
type InboundUsage struct {
	Today int64 `json:"today"`
	Total int64 `json:"total"`
}

// InboundTrafficByNode returns usage per inbound id for a node.
func (s *Store) InboundTrafficByNode(ctx context.Context, nodeID int64, at time.Time) (map[int64]InboundUsage, error) {
	day := at.UTC().Truncate(24 * time.Hour).Unix()
	rows, err := s.db.QueryContext(ctx, `SELECT t.inbound_id, SUM(CASE WHEN t.day = ? THEN t.up_bytes + t.down_bytes ELSE 0 END), SUM(t.up_bytes + t.down_bytes)
		FROM inbound_traffic_daily t JOIN inbounds i ON i.id = t.inbound_id WHERE i.node_id = ? GROUP BY t.inbound_id`, day, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]InboundUsage{}
	for rows.Next() {
		var id int64
		var u InboundUsage
		if err := rows.Scan(&id, &u.Today, &u.Total); err != nil {
			return nil, err
		}
		out[id] = u
	}
	return out, rows.Err()
}

// AddOutboundTraffic folds a node's per-outbound counters into the day row.
func (s *Store) AddOutboundTraffic(ctx context.Context, nodeID int64, tag string, up, down int64, at time.Time) error {
	day := at.UTC().Truncate(24 * time.Hour).Unix()
	_, err := s.db.ExecContext(ctx, `INSERT INTO outbound_traffic_daily (node_id, tag, day, up_bytes, down_bytes) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(node_id, tag, day) DO UPDATE SET up_bytes = up_bytes + excluded.up_bytes, down_bytes = down_bytes + excluded.down_bytes`, nodeID, tag, day, up, down)
	return err
}

// OutboundTrafficByNode returns today's and lifetime usage per outbound tag.
func (s *Store) OutboundTrafficByNode(ctx context.Context, nodeID int64, at time.Time) (map[string]InboundUsage, error) {
	day := at.UTC().Truncate(24 * time.Hour).Unix()
	rows, err := s.db.QueryContext(ctx, `SELECT tag, SUM(CASE WHEN day = ? THEN up_bytes + down_bytes ELSE 0 END), SUM(up_bytes + down_bytes) FROM outbound_traffic_daily WHERE node_id = ? GROUP BY tag`, day, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]InboundUsage{}
	for rows.Next() {
		var tag string
		var u InboundUsage
		if err := rows.Scan(&tag, &u.Today, &u.Total); err != nil {
			return nil, err
		}
		out[tag] = u
	}
	return out, rows.Err()
}
