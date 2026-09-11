package store

import (
	"context"
	"database/sql"
	"time"

	"gitlab.com/boyang-hu/captain/internal/domain"
)

func (s *Store) CreatePlan(ctx context.Context, p *domain.Plan) error {
	ts := now()
	res, err := s.db.ExecContext(ctx, `INSERT INTO plans (name, price_cents, period_days, quota_bytes, device_limit, speed_limit_mbps, reset_days, group_id, sort, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Name, p.PriceCents, p.PeriodDays, p.QuotaBytes, p.DeviceLimit, p.SpeedLimitMbps, p.ResetDays, nullInt64(p.GroupID), p.Sort, boolInt(p.Enabled), ts, ts)
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
	err := s.db.QueryRowContext(ctx, `SELECT id, name, price_cents, period_days, quota_bytes, device_limit, speed_limit_mbps, reset_days, group_id, sort, enabled FROM plans WHERE id = ?`, id).
		Scan(&p.ID, &p.Name, &p.PriceCents, &p.PeriodDays, &p.QuotaBytes, &p.DeviceLimit, &p.SpeedLimitMbps, &p.ResetDays, &group, &p.Sort, &enabled)
	if err != nil {
		return nil, wrapNotFound(err)
	}
	p.GroupID = int64Ptr(group)
	p.Enabled = enabled == 1
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
	if err := grantTx(ctx, tx, userID, plan, at); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.ActiveSubscription(ctx, userID)
}

func grantTx(ctx context.Context, tx *sql.Tx, userID int64, plan *domain.Plan, at time.Time) error {
	if _, err := tx.ExecContext(ctx, `UPDATE subscriptions SET status = 'expired', updated_at = ? WHERE user_id = ? AND status = 'active'`, now(), userID); err != nil {
		return err
	}
	var expires, reset sql.NullInt64
	if plan.PeriodDays > 0 {
		expires = sql.NullInt64{Int64: at.AddDate(0, 0, plan.PeriodDays).Unix(), Valid: true}
	}
	if plan.ResetDays > 0 && plan.QuotaBytes > 0 {
		reset = sql.NullInt64{Int64: at.AddDate(0, 0, plan.ResetDays).Unix(), Valid: true}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO subscriptions (user_id, plan_id, starts_at, expires_at, quota_bytes, reset_at, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, 'active', ?, ?)`, userID, plan.ID, at.Unix(), expires, plan.QuotaBytes, reset, now(), now()); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE users SET group_id = ?, updated_at = ? WHERE id = ?`, nullInt64(plan.GroupID), now(), userID)
	return err
}

func planByIDTx(ctx context.Context, tx *sql.Tx, id int64) (*domain.Plan, error) {
	var p domain.Plan
	var group sql.NullInt64
	var enabled int
	err := tx.QueryRowContext(ctx, `SELECT id, name, price_cents, period_days, quota_bytes, device_limit, speed_limit_mbps, reset_days, group_id, sort, enabled FROM plans WHERE id = ?`, id).
		Scan(&p.ID, &p.Name, &p.PriceCents, &p.PeriodDays, &p.QuotaBytes, &p.DeviceLimit, &p.SpeedLimitMbps, &p.ResetDays, &group, &p.Sort, &enabled)
	if err != nil {
		return nil, wrapNotFound(err)
	}
	p.GroupID = int64Ptr(group)
	p.Enabled = enabled == 1
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
	_, err := s.db.ExecContext(ctx, `UPDATE plans SET name = ?, price_cents = ?, period_days = ?, quota_bytes = ?, device_limit = ?, speed_limit_mbps = ?, reset_days = ?, group_id = ?, sort = ?, enabled = ?, updated_at = ? WHERE id = ?`,
		p.Name, p.PriceCents, p.PeriodDays, p.QuotaBytes, p.DeviceLimit, p.SpeedLimitMbps, p.ResetDays, nullInt64(p.GroupID), p.Sort, boolInt(p.Enabled), now(), p.ID)
	return err
}

func (s *Store) DeletePlan(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM plans WHERE id = ?`, id)
	return err
}
