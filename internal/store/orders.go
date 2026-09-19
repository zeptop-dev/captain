package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
)

const orderCols = "id, no, user_id, plan_id, amount_cents, gateway, gateway_ref, status, created_at, paid_at, period_days, coupon_id, discount_cents, surplus_cents, activation"

func scanOrder(row interface{ Scan(...any) error }) (*domain.Order, error) {
	var o domain.Order
	var ref sql.NullString
	var created int64
	var paid, coupon sql.NullInt64
	if err := row.Scan(&o.ID, &o.No, &o.UserID, &o.PlanID, &o.AmountCents, &o.Gateway, &ref, &o.Status, &created, &paid, &o.PeriodDays, &coupon, &o.DiscountCents, &o.SurplusCents, &o.Activation); err != nil {
		return nil, wrapNotFound(err)
	}
	o.GatewayRef = ref.String
	o.CreatedAt = unix(created)
	o.PaidAt = unixPtr(paid)
	o.CouponID = int64Ptr(coupon)
	return &o, nil
}

func (s *Store) CreateOrder(ctx context.Context, o *domain.Order) error {
	ts := now()
	res, err := s.db.ExecContext(ctx, `INSERT INTO orders (no, user_id, plan_id, amount_cents, gateway, status, created_at, period_days, coupon_id, discount_cents, surplus_cents, activation) VALUES (?, ?, ?, ?, ?, 'pending', ?, ?, ?, ?, ?, ?)`,
		o.No, o.UserID, o.PlanID, o.AmountCents, o.Gateway, ts, o.PeriodDays, nullInt64(o.CouponID), o.DiscountCents, o.SurplusCents, o.Activation)
	if err != nil {
		return err
	}
	o.ID, _ = res.LastInsertId()
	o.Status = "pending"
	o.CreatedAt = unix(ts)
	return nil
}

func (s *Store) OrderByNo(ctx context.Context, no string) (*domain.Order, error) {
	return scanOrder(s.db.QueryRowContext(ctx, `SELECT `+orderCols+` FROM orders WHERE no = ?`, no))
}

func (s *Store) OrdersByUser(ctx context.Context, userID int64, limit int) ([]*domain.Order, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+orderCols+` FROM orders WHERE user_id = ? ORDER BY id DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Order
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// ErrRevived is returned with the (now paid) order when a notification
// arrived for an order the stale-order job had already cancelled; callers
// log it so the operator can see late payments.
var ErrRevived = errors.New("store: cancelled order revived by a late payment")

// ErrAlreadyPaid signals an idempotent repeat of a paid notification.
var ErrAlreadyPaid = errors.New("store: order already paid")

// MarkPaid flips a pending order to paid and grants the plan in one
// transaction. Repeats return ErrAlreadyPaid; cancelled orders return
// ErrNotFound.
func (s *Store) MarkPaid(ctx context.Context, no, gatewayRef string, at time.Time) (*domain.Order, error) {
	revived := false
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	o, err := scanOrder(tx.QueryRowContext(ctx, `SELECT `+orderCols+` FROM orders WHERE no = ?`, no))
	if err != nil {
		return nil, err
	}
	switch o.Status {
	case "paid":
		return o, ErrAlreadyPaid
	case "cancelled":
		// The customer paid after the pending window closed (3-DS, a
		// checkout tab left open): the money is real, so grant anyway.
		revived = true
	}
	if _, err := tx.ExecContext(ctx, `UPDATE orders SET status = 'paid', gateway_ref = ?, paid_at = ? WHERE id = ? AND status IN ('pending', 'cancelled')`, gatewayRef, at.Unix(), o.ID); err != nil {
		return nil, err
	}
	plan, err := planByIDTx(ctx, tx, o.PlanID)
	if err != nil {
		return nil, err
	}
	if err := grantTx(ctx, tx, o.UserID, plan, o.PeriodDays, at, orderGrantMode(ctx, tx, o), o.ID); err != nil {
		return nil, err
	}
	if err := s.paidHooksTx(ctx, tx, o); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	o.Status, o.GatewayRef = "paid", gatewayRef
	o.PaidAt = &at
	if revived {
		return o, ErrRevived
	}
	return o, nil
}

// PayWithBalance debits the user's balance and marks the order paid atomically.
func (s *Store) PayWithBalance(ctx context.Context, no string, at time.Time) (*domain.Order, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	o, err := scanOrder(tx.QueryRowContext(ctx, `SELECT `+orderCols+` FROM orders WHERE no = ?`, no))
	if err != nil {
		return nil, err
	}
	switch o.Status {
	case "paid":
		return nil, ErrAlreadyPaid
	case "pending":
	default:
		return nil, ErrNotFound
	}
	res, err := tx.ExecContext(ctx, `UPDATE users SET balance_cents = balance_cents - ?, updated_at = ? WHERE id = ? AND balance_cents >= ?`, o.AmountCents, now(), o.UserID, o.AmountCents)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrInsufficientBalance
	}
	if _, err := tx.ExecContext(ctx, `UPDATE orders SET status = 'paid', gateway_ref = 'balance', paid_at = ? WHERE id = ?`, at.Unix(), o.ID); err != nil {
		return nil, err
	}
	plan, err := planByIDTx(ctx, tx, o.PlanID)
	if err != nil {
		return nil, err
	}
	if err := grantTx(ctx, tx, o.UserID, plan, o.PeriodDays, at, orderGrantMode(ctx, tx, o), o.ID); err != nil {
		return nil, err
	}
	if err := s.paidHooksTx(ctx, tx, o); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	o.Status, o.GatewayRef, o.PaidAt = "paid", "balance", &at
	return o, nil
}

// orderGrantMode honours the buyer's activation choice unless the panel
// runs single-plan, where a different plan always replaces.
func orderGrantMode(ctx context.Context, tx *sql.Tx, o *domain.Order) GrantMode {
	if mode := defaultGrantMode(ctx, tx); mode == GrantReplace {
		return mode
	}
	if o.Activation == "queue" {
		return GrantQueue
	}
	return GrantStack
}

// ErrInsufficientBalance means the user cannot cover the order.
var ErrInsufficientBalance = errors.New("store: insufficient balance")

// DiscardPendingOrder deletes an order that was never paid and that the
// customer was told failed, releasing what a pending order holds (its
// coupon use, its carried-over surplus). Nothing references an unpaid
// order: commissions only exist for paid ones.
func (s *Store) DiscardPendingOrder(ctx context.Context, no string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM orders WHERE no = ? AND status = 'pending'`, no)
	return err
}

// CancelStaleOrders cancels pending orders created before cutoff.
func (s *Store) CancelStaleOrders(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE orders SET status = 'cancelled' WHERE status = 'pending' AND created_at < ?`, cutoff.Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ListPlans returns enabled plans for the portal.
func (s *Store) ListPlans(ctx context.Context, enabledOnly bool) ([]*domain.Plan, error) {
	q := `SELECT id, name, price_cents, period_days, quota_bytes, device_limit, speed_limit_mbps, reset_days, reset_mode, prices_json, group_id, sort, enabled FROM plans`
	if enabledOnly {
		q += ` WHERE enabled = 1`
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY sort, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Plan
	for rows.Next() {
		var p domain.Plan
		var group sql.NullInt64
		var enabled int
		var prices string
		if err := rows.Scan(&p.ID, &p.Name, &p.PriceCents, &p.PeriodDays, &p.QuotaBytes, &p.DeviceLimit, &p.SpeedLimitMbps, &p.ResetDays, &p.ResetMode, &prices, &group, &p.Sort, &enabled); err != nil {
			return nil, err
		}
		p.GroupID = int64Ptr(group)
		p.Enabled = enabled == 1
		p.Prices = decodePrices(prices)
		out = append(out, &p)
	}
	return out, rows.Err()
}

// OrderRow is an order with user email and plan name for admin lists.
type OrderRow struct {
	Order    domain.Order
	Email    string
	PlanName string
}

func (s *Store) ListOrders(ctx context.Context, status string, limit, offset int) ([]OrderRow, int, error) {
	where := ""
	args := []any{}
	if status != "" {
		where = ` WHERE o.status = ?`
		args = append(args, status)
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders o`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT o.id, o.no, o.user_id, o.plan_id, o.amount_cents, o.gateway, o.gateway_ref, o.status, o.created_at, o.paid_at, o.period_days, o.discount_cents, o.surplus_cents, u.email, p.name
		FROM orders o JOIN users u ON u.id = o.user_id JOIN plans p ON p.id = o.plan_id`+where+` ORDER BY o.id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []OrderRow
	for rows.Next() {
		var r OrderRow
		var ref sql.NullString
		var created int64
		var paid sql.NullInt64
		if err := rows.Scan(&r.Order.ID, &r.Order.No, &r.Order.UserID, &r.Order.PlanID, &r.Order.AmountCents, &r.Order.Gateway, &ref, &r.Order.Status, &created, &paid, &r.Order.PeriodDays, &r.Order.DiscountCents, &r.Order.SurplusCents, &r.Email, &r.PlanName); err != nil {
			return nil, 0, err
		}
		r.Order.GatewayRef = ref.String
		r.Order.CreatedAt, r.Order.PaidAt = unix(created), unixPtr(paid)
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// DashboardStats are the headline numbers.
type DashboardStats struct {
	Users         int   `json:"users"`
	ActiveSubs    int   `json:"active_subs"`
	Nodes         int   `json:"nodes"`
	NodesOnline   int   `json:"nodes_online"`
	RevenueToday  int64 `json:"revenue_today_cents"`
	RevenueMonth  int64 `json:"revenue_month_cents"`
	OrdersPending int   `json:"orders_pending"`
	TrafficToday  int64 `json:"traffic_today_bytes"`
	OnlineDevices int   `json:"online_devices"`
}

func (s *Store) Dashboard(ctx context.Context, at time.Time) (*DashboardStats, error) {
	d := &DashboardStats{}
	day := at.UTC().Truncate(24 * time.Hour)
	month := time.Date(at.UTC().Year(), at.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	q := func(dst any, query string, args ...any) error {
		return s.db.QueryRowContext(ctx, query, args...).Scan(dst)
	}
	if err := q(&d.Users, `SELECT COUNT(*) FROM users WHERE role = 'user'`); err != nil {
		return nil, err
	}
	if err := q(&d.ActiveSubs, `SELECT COUNT(*) FROM subscriptions WHERE status = 'active' AND (expires_at IS NULL OR expires_at > ?) AND (quota_bytes = 0 OR used_up_bytes + used_down_bytes < quota_bytes)`, at.Unix()); err != nil {
		return nil, err
	}
	if err := q(&d.Nodes, `SELECT COUNT(*) FROM nodes`); err != nil {
		return nil, err
	}
	if err := q(&d.NodesOnline, `SELECT COUNT(*) FROM nodes WHERE last_seen_at > ?`, at.Add(-3*time.Minute).Unix()); err != nil {
		return nil, err
	}
	if err := q(&d.RevenueToday, `SELECT COALESCE(SUM(amount_cents),0) FROM orders WHERE status = 'paid' AND paid_at >= ?`, day.Unix()); err != nil {
		return nil, err
	}
	if err := q(&d.RevenueMonth, `SELECT COALESCE(SUM(amount_cents),0) FROM orders WHERE status = 'paid' AND paid_at >= ?`, month.Unix()); err != nil {
		return nil, err
	}
	if err := q(&d.OrdersPending, `SELECT COUNT(*) FROM orders WHERE status = 'pending'`); err != nil {
		return nil, err
	}
	if err := q(&d.TrafficToday, `SELECT COALESCE(SUM(up_bytes + down_bytes),0) FROM traffic_daily WHERE day = ?`, day.Unix()); err != nil {
		return nil, err
	}
	if err := q(&d.OnlineDevices, `SELECT COUNT(*) FROM online_devices WHERE last_seen_at > ?`, at.Add(-5*time.Minute).Unix()); err != nil {
		return nil, err
	}
	return d, nil
}

// TrafficSeries returns daily totals for the last n days, oldest first.
func (s *Store) TrafficSeries(ctx context.Context, at time.Time, days int) ([]DayTraffic, error) {
	start := at.UTC().Truncate(24*time.Hour).AddDate(0, 0, -(days - 1))
	rows, err := s.db.QueryContext(ctx, `SELECT day, SUM(up_bytes), SUM(down_bytes) FROM traffic_daily WHERE day >= ? GROUP BY day ORDER BY day`, start.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byDay := map[int64]DayTraffic{}
	for rows.Next() {
		var d DayTraffic
		var day int64
		if err := rows.Scan(&day, &d.Up, &d.Down); err != nil {
			return nil, err
		}
		d.Day = day
		byDay[day] = d
	}
	out := make([]DayTraffic, 0, days)
	for i := 0; i < days; i++ {
		day := start.AddDate(0, 0, i).Unix()
		d := byDay[day]
		d.Day = day
		out = append(out, d)
	}
	return out, rows.Err()
}

// DayTraffic is one day's totals.
type DayTraffic struct {
	Day  int64 `json:"day"`
	Up   int64 `json:"up"`
	Down int64 `json:"down"`
}

// paidHooksTx runs the bookkeeping that follows a payment: coupon usage and
// the inviter's commission.
func (s *Store) paidHooksTx(ctx context.Context, tx *sql.Tx, o *domain.Order) error {
	if o.CouponID != nil {
		// Never count past max_uses; the discount was already granted at
		// checkout, this just keeps the counter honest.
		if _, err := tx.ExecContext(ctx, `UPDATE coupons SET used = used + 1 WHERE id = ? AND (max_uses = 0 OR used < max_uses)`, *o.CouponID); err != nil {
			return err
		}
	}
	// Read through the transaction: the pool may hold a single connection
	// and a query on s.db would wait on our own lock.
	var inv InviteSettings
	var raw string
	if err := tx.QueryRowContext(ctx, `SELECT value_json FROM settings WHERE key = ?`, SettingInvite).Scan(&raw); err == nil {
		_ = json.Unmarshal([]byte(raw), &inv)
	}
	if !inv.Enabled || o.AmountCents <= 0 {
		return nil
	}
	if inv.FirstOrderOnly {
		var n int
		_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM commissions WHERE invitee_id = ?`, o.UserID).Scan(&n)
		if n > 0 {
			return nil
		}
	}
	column, payout := "balance_cents", PayoutBalance
	if inv.Payout == PayoutCommission {
		column, payout = "commission_cents", PayoutCommission
	}
	// Walk up the referral chain, one level per configured percentage. A
	// chain that comes back round (the buyer, or someone already paid for
	// this order) ends the walk: nobody earns on their own purchase.
	current := o.UserID
	seen := map[int64]bool{o.UserID: true}
	for level, pct := range inv.LevelPercents() {
		var inviter sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT invited_by FROM users WHERE id = ?`, current).Scan(&inviter); err != nil || !inviter.Valid || seen[inviter.Int64] {
			return nil
		}
		seen[inviter.Int64] = true
		current = inviter.Int64
		amount := o.AmountCents * int64(pct) / 100
		if amount <= 0 {
			continue
		}
		res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO commissions (order_id, inviter_id, invitee_id, level, amount_cents, created_at, payout) VALUES (?, ?, ?, ?, ?, ?, ?)`, o.ID, inviter.Int64, o.UserID, level+1, amount, now(), payout)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 1 {
			if _, err := tx.ExecContext(ctx, `UPDATE users SET `+column+` = `+column+` + ?, updated_at = ? WHERE id = ?`, amount, now(), inviter.Int64); err != nil {
				return err
			}
		}
	}
	return nil
}

// reverseOrderHooksTx undoes what paidHooksTx granted for an order that is
// being refunded: the inviter's commission goes back (the column may end
// up negative, which is the debt an already-withdrawn commission leaves
// behind) and the coupon use is released. Without this, buying a queued
// plan and cancelling it minted commission on every round.
func reverseOrderHooksTx(ctx context.Context, tx *sql.Tx, orderID int64, couponID *int64) error {
	rows, err := tx.QueryContext(ctx, `SELECT inviter_id, amount_cents, payout FROM commissions WHERE order_id = ?`, orderID)
	if err != nil {
		return err
	}
	type entry struct {
		inviter int64
		amount  int64
		payout  string
	}
	var list []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.inviter, &e.amount, &e.payout); err != nil {
			rows.Close()
			return err
		}
		list = append(list, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, e := range list {
		column := "balance_cents"
		if e.payout == PayoutCommission {
			column = "commission_cents"
		}
		if _, err := tx.ExecContext(ctx, `UPDATE users SET `+column+` = `+column+` - ?, updated_at = ? WHERE id = ?`, e.amount, now(), e.inviter); err != nil {
			return err
		}
	}
	if len(list) > 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM commissions WHERE order_id = ?`, orderID); err != nil {
			return err
		}
	}
	if couponID != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE coupons SET used = used - 1 WHERE id = ? AND used > 0`, *couponID); err != nil {
			return err
		}
	}
	return nil
}
