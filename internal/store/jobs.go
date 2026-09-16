package store

import (
	"context"
	"database/sql"
	"github.com/zeptop-dev/captain/internal/domain"
	"os"
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
	rows, err := s.db.QueryContext(ctx, `SELECT sub.id, sub.reset_at, sub.reset_day, p.reset_days, p.reset_mode FROM subscriptions sub JOIN plans p ON p.id = sub.plan_id
		WHERE sub.status = 'active' AND sub.reset_at IS NOT NULL AND sub.reset_at <= ?`, at.Unix())
	if err != nil {
		return 0, err
	}
	type due struct {
		id       int64
		resetAt  int64
		resetDay int
		plan     domain.Plan
	}
	var list []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.id, &d.resetAt, &d.resetDay, &d.plan.ResetDays, &d.plan.ResetMode); err != nil {
			rows.Close()
			return 0, err
		}
		list = append(list, d)
	}
	rows.Close()
	var n int64
	for _, d := range list {
		var nextVal sql.NullInt64
		if d.resetDay > 0 {
			if next := nextResetFor(&d.plan, d.resetDay, at); next != nil {
				nextVal = sql.NullInt64{Int64: next.Unix(), Valid: true}
			}
		} else if d.plan.EffectiveResetMode() == "days" && d.plan.ResetDays > 0 {
			// Keep the cadence anchored to the original schedule.
			next := time.Unix(d.resetAt, 0)
			for !next.After(at) {
				next = next.AddDate(0, 0, d.plan.ResetDays)
			}
			nextVal = sql.NullInt64{Int64: next.Unix(), Valid: true}
		} else if next := NextReset(&d.plan, at); next != nil {
			nextVal = sql.NullInt64{Int64: next.Unix(), Valid: true}
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE subscriptions SET used_up_bytes = 0, used_down_bytes = 0, reset_at = ?, updated_at = ? WHERE id = ?`, nextVal, now(), d.id); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// Gauges are the panel-wide numbers the metrics endpoint exports.
type Gauges struct {
	Nodes, NodesOnline, Users, UsableSubscriptions, PendingOrders, ExpiringWeek int64
	// NodeLastSeen is seconds since each node's last report (by node name).
	NodeLastSeen map[string]float64
}

// Gauges reads the current numbers; online = reported within window.
func (s *Store) Gauges(ctx context.Context, at time.Time, window time.Duration) (*Gauges, error) {
	g := &Gauges{NodeLastSeen: map[string]float64{}}
	q := func(dst *int64, query string, args ...any) error {
		return s.db.QueryRowContext(ctx, query, args...).Scan(dst)
	}
	if err := q(&g.Nodes, `SELECT COUNT(*) FROM nodes WHERE token_hash IS NOT NULL AND token_hash != ''`); err != nil {
		return nil, err
	}
	if err := q(&g.NodesOnline, `SELECT COUNT(*) FROM nodes WHERE last_seen_at >= ?`, at.Add(-window).Unix()); err != nil {
		return nil, err
	}
	if err := q(&g.Users, `SELECT COUNT(*) FROM users WHERE role = 'user' AND status = 'active'`); err != nil {
		return nil, err
	}
	if err := q(&g.UsableSubscriptions, `SELECT COUNT(*) FROM subscriptions sub WHERE sub.status = 'active' AND `+usableSQL, at.Unix()); err != nil {
		return nil, err
	}
	if err := q(&g.PendingOrders, `SELECT COUNT(*) FROM orders WHERE status = 'pending'`); err != nil {
		return nil, err
	}
	if err := q(&g.ExpiringWeek, `SELECT COUNT(*) FROM subscriptions WHERE status = 'active' AND expires_at IS NOT NULL AND expires_at BETWEEN ? AND ?`, at.Unix(), at.AddDate(0, 0, 7).Unix()); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT name, last_seen_at FROM nodes WHERE last_seen_at IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var seen int64
		if err := rows.Scan(&name, &seen); err != nil {
			return nil, err
		}
		g.NodeLastSeen[name] = at.Sub(time.Unix(seen, 0)).Seconds()
	}
	return g, rows.Err()
}

// PruneHistory drops daily traffic buckets older than keepDays and
// notification receipts older than 180 days; the tables otherwise grow
// by users × inbounds rows a day forever.
func (s *Store) PruneHistory(ctx context.Context, at time.Time, keepDays int) (int64, error) {
	if keepDays <= 0 {
		keepDays = 400
	}
	day := at.UTC().AddDate(0, 0, -keepDays).Truncate(24 * time.Hour).Unix()
	var total int64
	for _, q := range []string{
		`DELETE FROM traffic_daily WHERE day < ?`,
		`DELETE FROM inbound_traffic_daily WHERE day < ?`,
		`DELETE FROM outbound_traffic_daily WHERE day < ?`,
	} {
		res, err := s.db.ExecContext(ctx, q, day)
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM notifications WHERE sent_at < ?`, at.AddDate(0, 0, -180).Unix())
	if err != nil {
		return total, err
	}
	n, _ := res.RowsAffected()
	return total + n, nil
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
	// ViaRelay marks an address that belongs to one of the panel's own
	// nodes: the connection came through a forward, the client's real
	// address is hidden behind the relay (so it is not a second device).
	ViaRelay bool `json:"via_relay,omitempty"`
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
	limits, err := s.DeviceLimits(ctx)
	if err != nil {
		return nil, err
	}
	// Addresses of our own nodes are relays: a forward without PROXY
	// protocol hides the clients behind it, so however many arrive that
	// way they count as one device, not one per relay.
	relay := map[string]bool{}
	if nodes, err := s.ListNodes(ctx); err == nil {
		for _, n := range nodes {
			for _, a := range []string{n.PublicAddr, n.InternalAddr, n.V6Addr} {
				if a != "" {
					relay[a] = true
				}
			}
		}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT user_id, ip FROM online_devices WHERE last_seen_at >= ?`, cutoff.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	direct := map[int64]map[string]bool{}
	viaRelay := map[int64]bool{}
	for rows.Next() {
		var id int64
		var ip string
		if err := rows.Scan(&id, &ip); err != nil {
			return nil, err
		}
		if relay[ip] {
			viaRelay[id] = true
			continue
		}
		if direct[id] == nil {
			direct[id] = map[string]bool{}
		}
		direct[id][ip] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := map[int64]bool{}
	for id, limit := range limits {
		if limit <= 0 {
			continue
		}
		n := len(direct[id])
		if viaRelay[id] {
			n++
		}
		if n > limit {
			out[id] = true
		}
	}
	return out, nil
}

// Backup writes a consistent snapshot of the database to path using
// SQLite's online VACUUM INTO, which works while the panel is serving.
func (s *Store) Backup(ctx context.Context, path string) error {
	_ = os.Remove(path)
	_, err := s.db.ExecContext(ctx, "VACUUM INTO ?", path)
	return err
}

// SpeedLimits returns each user's plan speed limit in Mbps (only users
// whose active plan has one).
func (s *Store) SpeedLimits(ctx context.Context) (map[int64]int, error) {
	return s.userLimits(ctx, "speed_limit_mbps", time.Now())
}

// DeviceLimits returns each user's plan device limit (only users whose
// active plan has one).
func (s *Store) DeviceLimits(ctx context.Context) (map[int64]int, error) {
	return s.userLimits(ctx, "device_limit", time.Now())
}
