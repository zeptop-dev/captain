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
	return s.GrantSubscriptionMode(ctx, userID, plan, at, s.DefaultGrantMode(ctx))
}

// GrantSubscriptionMode is GrantSubscription with an explicit stacking choice.
func (s *Store) GrantSubscriptionMode(ctx context.Context, userID int64, plan *domain.Plan, at time.Time, mode GrantMode) (*domain.Subscription, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := grantTx(ctx, tx, userID, plan, 0, at, mode, 0); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.ActiveSubscription(ctx, userID)
}

// grantTx gives userID the plan for periodDays (0 = the plan's base
// period). Renewing the same plan before it expires extends the time and
// refills the quota. Otherwise the mode decides: stack next to the current
// subscriptions, queue behind them, or replace them. orderID (0 = none)
// is the paid order behind the grant; a queued row remembers it so that
// cancelling the row refunds exactly that order.
func grantTx(ctx context.Context, tx *sql.Tx, userID int64, plan *domain.Plan, periodDays int, at time.Time, mode GrantMode, orderID int64) error {
	if periodDays <= 0 {
		periodDays = plan.PeriodDays
	}
	var reset sql.NullInt64
	if next := NextReset(plan, at); next != nil && plan.QuotaBytes > 0 {
		reset = sql.NullInt64{Int64: next.Unix(), Valid: true}
	}
	// Renewal: same plan, still active.
	var subID, override int64
	var resetDay int
	var curExpires sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT id, expires_at, quota_override, reset_day FROM subscriptions WHERE user_id = ? AND plan_id = ? AND status = 'active' ORDER BY id DESC LIMIT 1`, userID, plan.ID).Scan(&subID, &curExpires, &override, &resetDay)
	if err == nil && (!curExpires.Valid || curExpires.Int64 > at.Unix()) {
		var expires sql.NullInt64
		if periodDays > 0 {
			base := at
			if curExpires.Valid {
				base = time.Unix(curExpires.Int64, 0)
			}
			expires = sql.NullInt64{Int64: base.AddDate(0, 0, periodDays).Unix(), Valid: true}
		}
		quota := plan.QuotaBytes
		switch {
		case override < 0:
			quota = 0
		case override > 0:
			quota = override
		}
		if resetDay > 0 {
			if next := nextResetFor(plan, resetDay, at); next != nil && quota > 0 {
				reset = sql.NullInt64{Int64: next.Unix(), Valid: true}
			}
		}
		// A renewal buys time, not a fresh counter: what was used stays
		// used and the reset cycle (if the plan has one) carries on. A
		// plan without a reset cycle gets the new period's allowance
		// added instead, or the renewal would buy nothing once the quota
		// is spent.
		var curReset sql.NullInt64
		var curQuota int64
		_ = tx.QueryRowContext(ctx, `SELECT reset_at, quota_bytes FROM subscriptions WHERE id = ?`, subID).Scan(&curReset, &curQuota)
		if !reset.Valid && !curReset.Valid && quota > 0 {
			quota += curQuota
		}
		if curReset.Valid && curReset.Int64 > at.Unix() {
			reset = curReset
		}
		_, err := tx.ExecContext(ctx, `UPDATE subscriptions SET expires_at = ?, quota_bytes = ?, reset_at = ?, updated_at = ? WHERE id = ?`,
			expires, quota, reset, now(), subID)
		return err
	}
	if mode == GrantQueue {
		// Nothing usable to wait for: start now instead.
		var n int
		_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscriptions sub WHERE sub.user_id = ? AND sub.status = 'active' AND `+usableSQL, userID, at.Unix()).Scan(&n)
		if n == 0 {
			mode = GrantStack
		}
	}
	switch mode {
	case GrantQueue:
		_, err := tx.ExecContext(ctx, `INSERT INTO subscriptions (user_id, plan_id, starts_at, expires_at, quota_bytes, reset_at, status, period_days, order_id, created_at, updated_at)
			VALUES (?, ?, ?, NULL, ?, NULL, 'queued', ?, NULLIF(?, 0), ?, ?)`, userID, plan.ID, at.Unix(), plan.QuotaBytes, periodDays, orderID, now(), now())
		return err
	case GrantReplace:
		if _, err := tx.ExecContext(ctx, `UPDATE subscriptions SET status = 'expired', updated_at = ? WHERE user_id = ? AND status = 'active'`, now(), userID); err != nil {
			return err
		}
	default:
		// Stacking: a group the user only had through an earlier grant is
		// now derived from the live subscriptions (AccessGroups), so drop it.
		// Only a group the user still gets from a live subscription (or
		// from the plan being granted) is dropped; a hand-set group that
		// merely matches an old, expired plan stays.
		if _, err := tx.ExecContext(ctx, `UPDATE users SET group_id = NULL, updated_at = ? WHERE id = ? AND (group_id = ? OR group_id IN (
			SELECT p.group_id FROM subscriptions sub JOIN plans p ON p.id = sub.plan_id WHERE sub.user_id = ? AND sub.status = 'active' AND p.group_id IS NOT NULL AND `+usableSQL+`))`,
			now(), userID, nullInt64(plan.GroupID), userID, at.Unix()); err != nil {
			return err
		}
	}
	var expires sql.NullInt64
	if periodDays > 0 {
		expires = sql.NullInt64{Int64: at.AddDate(0, 0, periodDays).Unix(), Valid: true}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO subscriptions (user_id, plan_id, starts_at, expires_at, quota_bytes, reset_at, status, period_days, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, 'active', ?, ?, ?)`, userID, plan.ID, at.Unix(), expires, plan.QuotaBytes, reset, periodDays, now(), now()); err != nil {
		return err
	}
	if mode != GrantReplace {
		return nil
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

// ActiveSubscription returns the user's primary subscription: the usable
// one that lasts longest, else the most recent active row. Callers that
// care about every plan use Subscriptions.
func (s *Store) ActiveSubscription(ctx context.Context, userID int64) (*domain.Subscription, error) {
	return scanSubscription(s.db.QueryRowContext(ctx, `SELECT `+subscriptionCols+` FROM subscriptions sub WHERE user_id = ? AND status = 'active'
		ORDER BY (`+usableSQL+`) DESC, expires_at IS NULL DESC, expires_at DESC, id DESC LIMIT 1`, userID, time.Now().Unix()))
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

// ApplyTrial grants the configured trial plan to a brand-new account. It is
// a no-op when no trial is set or the user already has a subscription.
func (s *Store) ApplyTrial(ctx context.Context, userID int64, at time.Time) error {
	var tr TrialSettings
	if err := s.GetSetting(ctx, SettingTrial, &tr); err != nil || tr.PlanID == 0 {
		return err
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscriptions WHERE user_id = ?`, userID).Scan(&n); err != nil || n > 0 {
		return err
	}
	plan, err := s.PlanByID(ctx, tr.PlanID)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := grantTx(ctx, tx, userID, plan, tr.PeriodDays, at, GrantStack, 0); err != nil {
		return err
	}
	return tx.Commit()
}
