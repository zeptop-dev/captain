package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
)

// SubAdjust is an operator edit of a user's active subscription. Nil
// pointers leave a field alone.
type SubAdjust struct {
	AddDays       int    // extend expiry (negative shortens); ignored for never-expiring plans
	QuotaOverride *int64 // bytes; 0 = back to the plan's quota; -1 = unlimited
	ResetDay      *int   // 1..28 monthly reset day; 0 = plan mode
	ResetUsage    bool   // zero the counters now
	SubID         int64  // which subscription; 0 = the primary one
}

// ErrNoActiveSubscription is returned when the user has nothing to adjust.
var ErrNoActiveSubscription = errors.New("user has no active subscription")

// AdjustSubscription applies the edit and returns the updated subscription.
func (s *Store) AdjustSubscription(ctx context.Context, userID int64, adj SubAdjust, at time.Time) (*domain.Subscription, error) {
	var sub *domain.Subscription
	var err error
	if adj.SubID > 0 {
		sub, err = s.SubscriptionByID(ctx, userID, adj.SubID)
	} else {
		sub, err = s.ActiveSubscription(ctx, userID)
	}
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNoActiveSubscription
		}
		return nil, err
	}
	plan, err := s.PlanByID(ctx, sub.PlanID)
	if err != nil {
		return nil, err
	}
	if adj.AddDays != 0 && sub.ExpiresAt != nil {
		base := *sub.ExpiresAt
		if base.Before(at) { // expired but still marked active: extend from now
			base = at
		}
		exp := base.AddDate(0, 0, adj.AddDays)
		if _, err := s.db.ExecContext(ctx, `UPDATE subscriptions SET expires_at = ?, updated_at = ? WHERE id = ?`, exp.Unix(), now(), sub.ID); err != nil {
			return nil, err
		}
	}
	if adj.QuotaOverride != nil {
		quota := plan.QuotaBytes
		override := *adj.QuotaOverride
		switch {
		case override < 0:
			quota, override = 0, -1
		case override > 0:
			quota = override
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE subscriptions SET quota_bytes = ?, quota_override = ?, updated_at = ? WHERE id = ?`, quota, override, now(), sub.ID); err != nil {
			return nil, err
		}
	}
	if adj.ResetDay != nil {
		day := *adj.ResetDay
		if day < 0 || day > 28 {
			return nil, errors.New("reset day must be 1-28 (0 = plan default)")
		}
		var reset sql.NullInt64
		if next := nextResetFor(plan, day, at); next != nil && (sub.QuotaBytes > 0 || plan.QuotaBytes > 0) {
			reset = sql.NullInt64{Int64: next.Unix(), Valid: true}
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE subscriptions SET reset_day = ?, reset_at = ?, updated_at = ? WHERE id = ?`, day, reset, now(), sub.ID); err != nil {
			return nil, err
		}
	}
	if adj.ResetUsage {
		if _, err := s.db.ExecContext(ctx, `UPDATE subscriptions SET used_up_bytes = 0, used_down_bytes = 0, updated_at = ? WHERE id = ?`, now(), sub.ID); err != nil {
			return nil, err
		}
	}
	return s.SubscriptionByID(ctx, userID, sub.ID)
}

// nextResetFor is NextReset with an optional per-user monthly day: when
// day > 0 the quota resets on that day each month regardless of the plan's
// mode (unless the plan never resets and has no quota).
func nextResetFor(plan *domain.Plan, day int, at time.Time) *time.Time {
	if day <= 0 {
		return NextReset(plan, at)
	}
	y, m, d := at.Date()
	next := time.Date(y, m, day, 0, 0, 0, 0, at.Location())
	if d >= day {
		next = next.AddDate(0, 1, 0)
	}
	return &next
}

// ExpiringUser is a row of the renewal view.
type ExpiringUser struct {
	UserID     int64      `json:"user_id"`
	Email      string     `json:"email"`
	PlanID     int64      `json:"plan_id"`
	PlanName   string     `json:"plan_name"`
	ExpiresAt  *time.Time `json:"expires_at"`
	QuotaBytes int64      `json:"quota_bytes"`
	UsedBytes  int64      `json:"used_bytes"`
	Balance    int64      `json:"balance_cents"`
}

// ExpiringUsers lists active subscriptions ordered by expiry (soonest and
// already-lapsed first), for batch renewals.
func (s *Store) ExpiringUsers(ctx context.Context, limit int) ([]ExpiringUser, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT u.id, u.email, p.id, p.name, sub.expires_at, sub.quota_bytes, sub.used_up_bytes + sub.used_down_bytes, u.balance_cents
		FROM subscriptions sub JOIN users u ON u.id = sub.user_id JOIN plans p ON p.id = sub.plan_id
		WHERE sub.status = 'active' AND u.role = 'user' ORDER BY sub.expires_at IS NULL, sub.expires_at LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExpiringUser{}
	for rows.Next() {
		var e ExpiringUser
		var exp sql.NullInt64
		if err := rows.Scan(&e.UserID, &e.Email, &e.PlanID, &e.PlanName, &exp, &e.QuotaBytes, &e.UsedBytes, &e.Balance); err != nil {
			return nil, err
		}
		e.ExpiresAt = unixPtr(exp)
		out = append(out, e)
	}
	return out, rows.Err()
}
