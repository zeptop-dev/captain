package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
)

// GrantMode says how a new plan meets the user's existing subscriptions.
type GrantMode string

const (
	// GrantStack adds the plan next to whatever is active (the default);
	// the same plan still renews in place.
	GrantStack GrantMode = ""
	// GrantReplace expires every active subscription first (the single-plan
	// setting, and the behaviour before multi-plan).
	GrantReplace GrantMode = "replace"
	// GrantQueue parks the plan until no active subscription is usable.
	GrantQueue GrantMode = "queue"
)

// subscriptionCols is the shared select list for domain.Subscription.
const subscriptionCols = `id, user_id, plan_id, starts_at, expires_at, quota_bytes, used_up_bytes, used_down_bytes, reset_at, status, period_days`

func scanSubscription(row interface{ Scan(...any) error }) (*domain.Subscription, error) {
	var sub domain.Subscription
	var starts int64
	var expires, reset sql.NullInt64
	if err := row.Scan(&sub.ID, &sub.UserID, &sub.PlanID, &starts, &expires, &sub.QuotaBytes, &sub.UsedUpBytes, &sub.UsedDownBytes, &reset, &sub.Status, &sub.PeriodDays); err != nil {
		return nil, wrapNotFound(err)
	}
	sub.StartsAt = unix(starts)
	sub.ExpiresAt, sub.ResetAt = unixPtr(expires), unixPtr(reset)
	return &sub, nil
}

// usableSQL is the SQL form of domain.Subscription.Usable for an active row
// aliased sub; the argument is the unix time.
const usableSQL = `(sub.expires_at IS NULL OR sub.expires_at > ?) AND (sub.quota_bytes = 0 OR sub.used_up_bytes + sub.used_down_bytes < sub.quota_bytes)`

// Subscriptions lists the user's active and queued subscriptions: active
// ones first by expiry (soonest first, never-expiring last), then queued
// ones in purchase order.
func (s *Store) Subscriptions(ctx context.Context, userID int64) ([]*domain.Subscription, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+subscriptionCols+` FROM subscriptions WHERE user_id = ? AND status IN ('active', 'queued')
		ORDER BY status = 'queued', expires_at IS NULL, expires_at, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*domain.Subscription{}
	for rows.Next() {
		sub, err := scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

// ActiveSubscriptions is Subscriptions without the queued ones.
func (s *Store) ActiveSubscriptions(ctx context.Context, userID int64) ([]*domain.Subscription, error) {
	all, err := s.Subscriptions(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, sub := range all {
		if sub.Status == "active" {
			out = append(out, sub)
		}
	}
	return out, nil
}

// SubscriptionByID loads one subscription of the user (any status).
func (s *Store) SubscriptionByID(ctx context.Context, userID, id int64) (*domain.Subscription, error) {
	return scanSubscription(s.db.QueryRowContext(ctx, `SELECT `+subscriptionCols+` FROM subscriptions WHERE id = ? AND user_id = ?`, id, userID))
}

// AccessGroups is the set of user groups the user belongs to: the group of
// every usable subscription's plan plus the group set on the user itself.
func (s *Store) AccessGroups(ctx context.Context, u *domain.User, at time.Time) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT p.group_id FROM subscriptions sub JOIN plans p ON p.id = sub.plan_id
		WHERE sub.user_id = ? AND sub.status = 'active' AND p.group_id IS NOT NULL AND `+usableSQL, u.ID, at.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[int64]bool{}
	var out []int64
	add := func(id int64) {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	if u.GroupID != nil {
		add(*u.GroupID)
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		add(id)
	}
	return out, rows.Err()
}

// groupClause renders "(col IS NULL OR col IN (?, ?))" with its args.
func groupClause(col string, groups []int64) (string, []any) {
	q := `(` + col + ` IS NULL`
	args := []any{}
	if len(groups) > 0 {
		q += ` OR ` + col + ` IN (`
		for i, g := range groups {
			if i > 0 {
				q += `, `
			}
			q += `?`
			args = append(args, g)
		}
		q += `)`
	}
	return q + `)`, args
}

// PromoteQueued starts the oldest queued subscription of every user who no
// longer has a usable active one. Returns how many were started.
func (s *Store) PromoteQueued(ctx context.Context, at time.Time) (int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT q.id, q.user_id, q.plan_id, q.period_days FROM subscriptions q
		WHERE q.status = 'queued' AND NOT EXISTS (SELECT 1 FROM subscriptions sub WHERE sub.user_id = q.user_id AND sub.status = 'active' AND `+usableSQL+`)
		AND q.id = (SELECT MIN(id) FROM subscriptions WHERE user_id = q.user_id AND status = 'queued')`, at.Unix())
	if err != nil {
		return 0, err
	}
	type due struct {
		id, userID, planID int64
		days               int
	}
	var list []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.id, &d.userID, &d.planID, &d.days); err != nil {
			rows.Close()
			return 0, err
		}
		list = append(list, d)
	}
	rows.Close()
	var n int64
	for _, d := range list {
		plan, err := s.PlanByID(ctx, d.planID)
		if err != nil {
			continue
		}
		if err := s.startSubscription(ctx, d.id, plan, d.days, at); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// startSubscription flips a queued row to active from at.
func (s *Store) startSubscription(ctx context.Context, id int64, plan *domain.Plan, periodDays int, at time.Time) error {
	if periodDays <= 0 {
		periodDays = plan.PeriodDays
	}
	var expires, reset sql.NullInt64
	if periodDays > 0 {
		expires = sql.NullInt64{Int64: at.AddDate(0, 0, periodDays).Unix(), Valid: true}
	}
	if next := NextReset(plan, at); next != nil && plan.QuotaBytes > 0 {
		reset = sql.NullInt64{Int64: next.Unix(), Valid: true}
	}
	_, err := s.db.ExecContext(ctx, `UPDATE subscriptions SET status = 'active', starts_at = ?, expires_at = ?, quota_bytes = ?, used_up_bytes = 0, used_down_bytes = 0, reset_at = ?, updated_at = ? WHERE id = ? AND status = 'queued'`,
		at.Unix(), expires, plan.QuotaBytes, reset, now(), id)
	return err
}

// CancelQueued drops a queued subscription (portal or admin). When the
// row came from a paid "start after the current plan" order, that order
// is marked refunded and its amount goes back to the user's balance.
func (s *Store) CancelQueued(ctx context.Context, userID, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var planID int64
	if err := tx.QueryRowContext(ctx, `SELECT plan_id FROM subscriptions WHERE id = ? AND user_id = ? AND status = 'queued'`, id, userID).Scan(&planID); err != nil {
		return wrapNotFound(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE subscriptions SET status = 'cancelled', updated_at = ? WHERE id = ?`, now(), id); err != nil {
		return err
	}
	var orderID, amount int64
	err = tx.QueryRowContext(ctx, `SELECT id, amount_cents FROM orders WHERE user_id = ? AND plan_id = ? AND activation = 'queue' AND status = 'paid' ORDER BY paid_at DESC, id DESC LIMIT 1`, userID, planID).Scan(&orderID, &amount)
	if err == nil {
		if _, err := tx.ExecContext(ctx, `UPDATE orders SET status = 'refunded' WHERE id = ?`, orderID); err != nil {
			return err
		}
		if amount > 0 {
			if _, err := tx.ExecContext(ctx, `UPDATE users SET balance_cents = balance_cents + ?, updated_at = ? WHERE id = ?`, amount, now(), userID); err != nil {
				return err
			}
		}
	} else if err != sql.ErrNoRows {
		return err
	}
	return tx.Commit()
}

type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// singlePlan reads the admin switch that turns stacking off (stored under
// the "subscription" settings key by the service layer). It takes the
// querier so callers inside a transaction do not wait on the pool.
func singlePlan(ctx context.Context, q rowQuerier) bool {
	var raw string
	if err := q.QueryRowContext(ctx, `SELECT value_json FROM settings WHERE key = 'subscription'`).Scan(&raw); err != nil {
		return false
	}
	var v struct {
		SinglePlan bool `json:"single_plan"`
	}
	_ = json.Unmarshal([]byte(raw), &v)
	return v.SinglePlan
}

// DefaultGrantMode is what a purchase or grant without an explicit choice
// does: replace under the single-plan setting, otherwise stack.
func (s *Store) DefaultGrantMode(ctx context.Context) GrantMode {
	return defaultGrantMode(ctx, s.db)
}

func defaultGrantMode(ctx context.Context, q rowQuerier) GrantMode {
	if singlePlan(ctx, q) {
		return GrantReplace
	}
	return GrantStack
}

// chargeableTx picks the subscription a traffic sample lands on: among
// usable active ones prefer those whose plan group is one of the node's
// inbound groups, then the one expiring soonest; with none usable, the
// most recent active row keeps counting so overage still shows.
func chargeableTx(ctx context.Context, tx *sql.Tx, userID int64, groups []int64, at time.Time) (int64, bool) {
	var id int64
	if len(groups) > 0 {
		clause, args := groupClause("p.group_id", groups)
		q := `SELECT sub.id FROM subscriptions sub JOIN plans p ON p.id = sub.plan_id
			WHERE sub.user_id = ? AND sub.status = 'active' AND p.group_id IS NOT NULL AND ` + clause + ` AND ` + usableSQL + `
			ORDER BY sub.expires_at IS NULL, sub.expires_at, sub.id LIMIT 1`
		err := tx.QueryRowContext(ctx, q, append(append([]any{userID}, args...), at.Unix())...).Scan(&id)
		if err == nil {
			return id, true
		}
	}
	err := tx.QueryRowContext(ctx, `SELECT sub.id FROM subscriptions sub WHERE sub.user_id = ? AND sub.status = 'active' AND `+usableSQL+`
		ORDER BY sub.expires_at IS NULL, sub.expires_at, sub.id LIMIT 1`, userID, at.Unix()).Scan(&id)
	if err == nil {
		return id, true
	}
	err = tx.QueryRowContext(ctx, `SELECT id FROM subscriptions WHERE user_id = ? AND status = 'active' ORDER BY id DESC LIMIT 1`, userID).Scan(&id)
	return id, err == nil
}

// QuotaWindow is a user's allowance as a rolling window (what mita's own
// quotas take): the primary subscription's quota over its reset cycle, or
// its whole lifetime when it never resets.
type QuotaWindow struct {
	Bytes int64
	Days  int
}

// UserQuotas returns the quota window of every user with a usable
// subscription that has a quota (unlimited plans are left out).
func (s *Store) UserQuotas(ctx context.Context, at time.Time) (map[int64]QuotaWindow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT sub.user_id, sub.quota_bytes, sub.starts_at, sub.expires_at, sub.reset_day, p.reset_days, p.reset_mode
		FROM subscriptions sub JOIN plans p ON p.id = sub.plan_id
		WHERE sub.status = 'active' AND sub.quota_bytes > 0 AND `+usableSQL+`
		ORDER BY sub.user_id, sub.expires_at IS NULL DESC, sub.expires_at DESC, sub.id DESC`, at.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]QuotaWindow{}
	for rows.Next() {
		var uid, quota, starts int64
		var expires sql.NullInt64
		var resetDay, resetDays int
		var mode string
		if err := rows.Scan(&uid, &quota, &starts, &expires, &resetDay, &resetDays, &mode); err != nil {
			return nil, err
		}
		if _, seen := out[uid]; seen {
			continue // the first row per user is the primary subscription
		}
		days := 36500
		switch {
		case resetDay > 0 || mode == "monthly":
			days = 31
		case mode == "yearly":
			days = 366
		case mode == "days" && resetDays > 0:
			days = resetDays
		case expires.Valid:
			days = int((expires.Int64-starts)/86400) + 1
			if days < 1 {
				days = 1
			}
		}
		out[uid] = QuotaWindow{Bytes: quota, Days: days}
	}
	return out, rows.Err()
}

// userLimits folds every usable subscription's plan value with "0 means
// unlimited wins, otherwise the largest".
func (s *Store) userLimits(ctx context.Context, col string, at time.Time) (map[int64]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT sub.user_id, p.`+col+` FROM subscriptions sub JOIN plans p ON p.id = sub.plan_id
		WHERE sub.status = 'active' AND `+usableSQL, at.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	unlimited := map[int64]bool{}
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		if n <= 0 {
			unlimited[id] = true
			delete(out, id)
			continue
		}
		if !unlimited[id] && n > out[id] {
			out[id] = n
		}
	}
	return out, rows.Err()
}
