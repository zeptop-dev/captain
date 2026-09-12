package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
)

func (s *Store) CreatePlan(ctx context.Context, p *domain.Plan) error {
	ts := now()
	res, err := s.db.ExecContext(ctx, `INSERT INTO plans (name, price_cents, period_days, quota_bytes, device_limit, speed_limit_mbps, reset_days, reset_mode, prices_json, group_id, sort, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Name, p.PriceCents, p.PeriodDays, p.QuotaBytes, p.DeviceLimit, p.SpeedLimitMbps, p.ResetDays, p.ResetMode, encodePrices(p.Prices), nullInt64(p.GroupID), p.Sort, boolInt(p.Enabled), ts, ts)
	if err != nil {
		return err
	}
	p.ID, _ = res.LastInsertId()
	return nil
}

func (s *Store) PlanByID(ctx context.Context, id int64) (*domain.Plan, error) {
	var p domain.Plan
	var group sql.NullInt64
	var enabled int
	var prices string
	err := s.db.QueryRowContext(ctx, `SELECT id, name, price_cents, period_days, quota_bytes, device_limit, speed_limit_mbps, reset_days, reset_mode, prices_json, group_id, sort, enabled FROM plans WHERE id = ?`, id).
		Scan(&p.ID, &p.Name, &p.PriceCents, &p.PeriodDays, &p.QuotaBytes, &p.DeviceLimit, &p.SpeedLimitMbps, &p.ResetDays, &p.ResetMode, &prices, &group, &p.Sort, &enabled)
	if err != nil {
		return nil, wrapNotFound(err)
	}
	p.GroupID = int64Ptr(group)
	p.Enabled = enabled == 1
	p.Prices = decodePrices(prices)
	return &p, nil
}

// GrantSubscription expires the user's current subscription and starts a new
// one from plan; the user is moved to the plan's group.
func (s *Store) GrantSubscription(ctx context.Context, userID int64, plan *domain.Plan, at time.Time) (*domain.Subscription, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := grantTx(ctx, tx, userID, plan, 0, at); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.ActiveSubscription(ctx, userID)
}

// grantTx gives userID the plan for periodDays (0 = the plan's base
// period). Renewing the same plan before it expires extends the time and
// refills the quota; anything else replaces the current subscription.
func grantTx(ctx context.Context, tx *sql.Tx, userID int64, plan *domain.Plan, periodDays int, at time.Time) error {
	if periodDays <= 0 {
		periodDays = plan.PeriodDays
	}
	var reset sql.NullInt64
	if next := NextReset(plan, at); next != nil && plan.QuotaBytes > 0 {
		reset = sql.NullInt64{Int64: next.Unix(), Valid: true}
	}
	// Renewal: same plan, still active.
	var subID int64
	var curExpires sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT id, expires_at FROM subscriptions WHERE user_id = ? AND plan_id = ? AND status = 'active' ORDER BY id DESC LIMIT 1`, userID, plan.ID).Scan(&subID, &curExpires)
	if err == nil && (!curExpires.Valid || curExpires.Int64 > at.Unix()) {
		var expires sql.NullInt64
		if periodDays > 0 {
			base := at
			if curExpires.Valid {
				base = time.Unix(curExpires.Int64, 0)
			}
			expires = sql.NullInt64{Int64: base.AddDate(0, 0, periodDays).Unix(), Valid: true}
		}
		_, err := tx.ExecContext(ctx, `UPDATE subscriptions SET expires_at = ?, quota_bytes = ?, used_up_bytes = 0, used_down_bytes = 0, reset_at = ?, updated_at = ? WHERE id = ?`,
			expires, plan.QuotaBytes, reset, now(), subID)
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE subscriptions SET status = 'expired', updated_at = ? WHERE user_id = ? AND status = 'active'`, now(), userID); err != nil {
		return err
	}
	var expires sql.NullInt64
	if periodDays > 0 {
		expires = sql.NullInt64{Int64: at.AddDate(0, 0, periodDays).Unix(), Valid: true}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO subscriptions (user_id, plan_id, starts_at, expires_at, quota_bytes, reset_at, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, 'active', ?, ?)`, userID, plan.ID, at.Unix(), expires, plan.QuotaBytes, reset, now(), now()); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE users SET group_id = ?, updated_at = ? WHERE id = ?`, nullInt64(plan.GroupID), now(), userID)
	return err
}

// NextReset returns when the quota next resets after at for the plan's
// reset mode, or nil when it never does.
func NextReset(plan *domain.Plan, at time.Time) *time.Time {
	var next time.Time
	switch plan.EffectiveResetMode() {
	case "days":
		if plan.ResetDays <= 0 {
			return nil
		}
		next = at.AddDate(0, 0, plan.ResetDays)
	case "monthly":
		y, m, _ := at.Date()
		next = time.Date(y, m+1, 1, 0, 0, 0, 0, at.Location())
	case "yearly":
		next = time.Date(at.Year()+1, 1, 1, 0, 0, 0, 0, at.Location())
	default:
		return nil
	}
	return &next
}

func encodePrices(p []domain.PlanPrice) string {
	if len(p) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(p)
	return string(b)
}

func decodePrices(raw string) []domain.PlanPrice {
	var out []domain.PlanPrice
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &out)
	}
	return out
}

func planByIDTx(ctx context.Context, tx *sql.Tx, id int64) (*domain.Plan, error) {
	var p domain.Plan
	var group sql.NullInt64
	var enabled int
	var prices string
	err := tx.QueryRowContext(ctx, `SELECT id, name, price_cents, period_days, quota_bytes, device_limit, speed_limit_mbps, reset_days, reset_mode, prices_json, group_id, sort, enabled FROM plans WHERE id = ?`, id).
		Scan(&p.ID, &p.Name, &p.PriceCents, &p.PeriodDays, &p.QuotaBytes, &p.DeviceLimit, &p.SpeedLimitMbps, &p.ResetDays, &p.ResetMode, &prices, &group, &p.Sort, &enabled)
	if err != nil {
		return nil, wrapNotFound(err)
	}
	p.GroupID = int64Ptr(group)
	p.Enabled = enabled == 1
	p.Prices = decodePrices(prices)
	return &p, nil
}

// ActiveSubscription returns the user's current subscription, if any.
func (s *Store) ActiveSubscription(ctx context.Context, userID int64) (*domain.Subscription, error) {
	var sub domain.Subscription
	var starts int64
	var expires, reset sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT id, user_id, plan_id, starts_at, expires_at, quota_bytes, used_up_bytes, used_down_bytes, reset_at, status
		FROM subscriptions WHERE user_id = ? AND status = 'active' ORDER BY id DESC LIMIT 1`, userID).
		Scan(&sub.ID, &sub.UserID, &sub.PlanID, &starts, &expires, &sub.QuotaBytes, &sub.UsedUpBytes, &sub.UsedDownBytes, &reset, &sub.Status)
	if err != nil {
		return nil, wrapNotFound(err)
	}
	sub.StartsAt = unix(starts)
	sub.ExpiresAt, sub.ResetAt = unixPtr(expires), unixPtr(reset)
	return &sub, nil
}

// CreateGroup inserts a user group.
func (s *Store) CreateGroup(ctx context.Context, name string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO user_groups (name) VALUES (?)`, name)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdatePlan(ctx context.Context, p *domain.Plan) error {
	_, err := s.db.ExecContext(ctx, `UPDATE plans SET name = ?, price_cents = ?, period_days = ?, quota_bytes = ?, device_limit = ?, speed_limit_mbps = ?, reset_days = ?, reset_mode = ?, prices_json = ?, group_id = ?, sort = ?, enabled = ?, updated_at = ? WHERE id = ?`,
		p.Name, p.PriceCents, p.PeriodDays, p.QuotaBytes, p.DeviceLimit, p.SpeedLimitMbps, p.ResetDays, p.ResetMode, encodePrices(p.Prices), nullInt64(p.GroupID), p.Sort, boolInt(p.Enabled), now(), p.ID)
	return err
}

func (s *Store) DeletePlan(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM plans WHERE id = ?`, id)
	return err
}
