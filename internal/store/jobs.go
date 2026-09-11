package store

import (
	"context"
	"time"
)

// ExpireSubscriptions marks past-due active subscriptions expired.
func (s *Store) ExpireSubscriptions(ctx context.Context, at time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE subscriptions SET status = 'expired', updated_at = ? WHERE status = 'active' AND expires_at IS NOT NULL AND expires_at <= ?`, now(), at.Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ResetQuotas zeroes usage on subscriptions whose reset time has passed and
// schedules the next reset from the plan's reset_days. Returns rows reset.
func (s *Store) ResetQuotas(ctx context.Context, at time.Time) (int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT sub.id, sub.reset_at, p.reset_days FROM subscriptions sub JOIN plans p ON p.id = sub.plan_id
		WHERE sub.status = 'active' AND sub.reset_at IS NOT NULL AND sub.reset_at <= ? AND p.reset_days > 0`, at.Unix())
	if err != nil {
		return 0, err
	}
	type due struct {
		id      int64
		resetAt int64
		days    int
	}
	var list []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.id, &d.resetAt, &d.days); err != nil {
			rows.Close()
			return 0, err
		}
		list = append(list, d)
	}
	rows.Close()
	var n int64
	for _, d := range list {
		next := time.Unix(d.resetAt, 0)
		for !next.After(at) {
			next = next.AddDate(0, 0, d.days)
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE subscriptions SET used_up_bytes = 0, used_down_bytes = 0, reset_at = ?, updated_at = ? WHERE id = ?`, next.Unix(), now(), d.id); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// PurgeSessions removes expired sessions.
func (s *Store) PurgeSessions(ctx context.Context, at time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, at.Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// PurgeOnline removes online-device rows not seen since cutoff.
func (s *Store) PurgeOnline(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM online_devices WHERE last_seen_at < ?`, cutoff.Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// OnlineDevice is one client IP seen for a user.
type OnlineDevice struct {
	NodeID     int64     `json:"node_id"`
	IP         string    `json:"ip"`
	LastSeenAt time.Time `json:"last_seen_at"`
}

// OnlineDevices lists a user's IPs seen since cutoff.
func (s *Store) OnlineDevices(ctx context.Context, userID int64, cutoff time.Time) ([]OnlineDevice, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT node_id, ip, last_seen_at FROM online_devices WHERE user_id = ? AND last_seen_at >= ? ORDER BY last_seen_at DESC`, userID, cutoff.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OnlineDevice
	for rows.Next() {
		var d OnlineDevice
		var at int64
		if err := rows.Scan(&d.NodeID, &d.IP, &at); err != nil {
			return nil, err
		}
		d.LastSeenAt = unix(at)
		out = append(out, d)
	}
	return out, rows.Err()
}

// OverDeviceLimit returns user IDs whose distinct online IPs since cutoff
// exceed their plan's device limit (limit 0 = unlimited).
func (s *Store) OverDeviceLimit(ctx context.Context, cutoff time.Time) (map[int64]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT o.user_id, COUNT(DISTINCT o.ip), p.device_limit
		FROM online_devices o
		JOIN subscriptions sub ON sub.user_id = o.user_id AND sub.status = 'active'
		JOIN plans p ON p.id = sub.plan_id
		WHERE o.last_seen_at >= ? AND p.device_limit > 0
		GROUP BY o.user_id, p.device_limit HAVING COUNT(DISTINCT o.ip) > p.device_limit`, cutoff.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		var n, limit int
		if err := rows.Scan(&id, &n, &limit); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}
