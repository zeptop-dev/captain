package store

import (
	"context"
	"time"
)

// AddTraffic records a delta for a user on an inbound (daily bucket) and
// charges it to the user's active subscription.
func (s *Store) AddTraffic(ctx context.Context, userID, inboundID int64, up, down int64, at time.Time) error {
	day := at.UTC().Truncate(24 * time.Hour).Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO traffic_daily (user_id, inbound_id, day, up_bytes, down_bytes) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id, inbound_id, day) DO UPDATE SET up_bytes = up_bytes + excluded.up_bytes, down_bytes = down_bytes + excluded.down_bytes`,
		userID, inboundID, day, up, down); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE subscriptions SET used_up_bytes = used_up_bytes + ?, used_down_bytes = used_down_bytes + ?, updated_at = ?
		WHERE user_id = ? AND status = 'active'`, up, down, now(), userID); err != nil {
		return err
	}
	return tx.Commit()
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
func (s *Store) UpsertForwardStatus(ctx context.Context, nodeID int64, tag string, up bool, rttMs int64, lastErr string, active, total, in, out int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO forward_status (node_id, tag, up, rtt_ms, last_error, active_conn, total_conn, bytes_in, bytes_out, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id, tag) DO UPDATE SET up = excluded.up, rtt_ms = excluded.rtt_ms, last_error = excluded.last_error,
		active_conn = excluded.active_conn, total_conn = excluded.total_conn, bytes_in = excluded.bytes_in, bytes_out = excluded.bytes_out, updated_at = excluded.updated_at`,
		nodeID, tag, boolInt(up), rttMs, lastErr, active, total, in, out, now())
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
