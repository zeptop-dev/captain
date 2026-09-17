package store

import (
	"context"
	"time"
)

// DynLimitSettings (settings key "dynlimit") throttles a user whose
// average rate across all nodes stays above TriggerMbps for TriggerSeconds:
// they get LimitMbps for LimitSeconds, then their plan limit again.
type DynLimitSettings struct {
	Enabled        bool `json:"enabled"`
	TriggerMbps    int  `json:"trigger_mbps"`
	TriggerSeconds int  `json:"trigger_seconds"`
	LimitMbps      int  `json:"limit_mbps"`
	LimitSeconds   int  `json:"limit_seconds"`
	// Windows are local-time ranges like "20:00-02:00" when the limiter
	// is active; empty = always.
	Windows []string `json:"windows"`
	// Whitelist users are never throttled.
	Whitelist []int64 `json:"whitelist"`
	// ThrottleUnlimited also throttles users who have no speed limit at
	// all. That adds a marking outbound to the cores serving them, so
	// those cores restart and every user on the node reconnects; off by
	// default.
	ThrottleUnlimited bool `json:"throttle_unlimited"`
}

const SettingDynLimit = "dynlimit"

// Defaults fills the zero values with the documented defaults.
func (s DynLimitSettings) Defaults() DynLimitSettings {
	if s.TriggerMbps <= 0 {
		s.TriggerMbps = 100
	}
	if s.TriggerSeconds <= 0 {
		s.TriggerSeconds = 60
	}
	if s.LimitMbps <= 0 {
		s.LimitMbps = 30
	}
	if s.LimitSeconds <= 0 {
		s.LimitSeconds = 600
	}
	if s.Windows == nil {
		s.Windows = []string{}
	}
	if s.Whitelist == nil {
		s.Whitelist = []int64{}
	}
	return s
}

// DynLimit is one active throttle.
type DynLimit struct {
	UserID int64     `json:"user_id"`
	Mbps   int       `json:"mbps"`
	Since  time.Time `json:"since"`
	Until  time.Time `json:"until"`
	Rate   int       `json:"rate_mbps"`
}

// SetDynLimit puts (or renews) a throttle on a user.
func (s *Store) SetDynLimit(ctx context.Context, userID int64, mbps, rate int, since, until time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO dyn_limits (user_id, mbps, since, until, rate) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET mbps = excluded.mbps, until = excluded.until, rate = excluded.rate`, userID, mbps, since.Unix(), until.Unix(), rate)
	return err
}

// DynLimitFor returns the user's active throttle, or nil.
func (s *Store) DynLimitFor(ctx context.Context, userID int64, at time.Time) (*DynLimit, error) {
	var d DynLimit
	var since, until int64
	err := s.db.QueryRowContext(ctx, `SELECT user_id, mbps, since, until, rate FROM dyn_limits WHERE user_id = ? AND until > ?`, userID, at.Unix()).Scan(&d.UserID, &d.Mbps, &since, &until, &d.Rate)
	if err != nil {
		return nil, wrapNotFound(err)
	}
	d.Since, d.Until = time.Unix(since, 0), time.Unix(until, 0)
	return &d, nil
}

// DynLimits returns every active throttle as user -> Mbps.
func (s *Store) DynLimits(ctx context.Context, at time.Time) (map[int64]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT user_id, mbps FROM dyn_limits WHERE until > ?`, at.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var id int64
		var mbps int
		if err := rows.Scan(&id, &mbps); err != nil {
			return nil, err
		}
		out[id] = mbps
	}
	return out, rows.Err()
}

// DeleteDynLimit lifts a throttle.
func (s *Store) DeleteDynLimit(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM dyn_limits WHERE user_id = ?`, userID)
	return err
}

// ClearExpiredDynLimits removes throttles whose time is up.
func (s *Store) ClearExpiredDynLimits(ctx context.Context, at time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM dyn_limits WHERE until <= ?`, at.Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
