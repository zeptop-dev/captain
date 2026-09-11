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
