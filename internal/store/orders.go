package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"gitlab.com/boyang-hu/captain/internal/domain"
)

const orderCols = "id, no, user_id, plan_id, amount_cents, gateway, gateway_ref, status, created_at, paid_at"

func scanOrder(row interface{ Scan(...any) error }) (*domain.Order, error) {
	var o domain.Order
	var ref sql.NullString
	var created int64
	var paid sql.NullInt64
	if err := row.Scan(&o.ID, &o.No, &o.UserID, &o.PlanID, &o.AmountCents, &o.Gateway, &ref, &o.Status, &created, &paid); err != nil {
		return nil, wrapNotFound(err)
	}
	o.GatewayRef = ref.String
	o.CreatedAt = unix(created)
	o.PaidAt = unixPtr(paid)
	return &o, nil
}

func (s *Store) CreateOrder(ctx context.Context, o *domain.Order) error {
	ts := now()
	res, err := s.db.ExecContext(ctx, `INSERT INTO orders (no, user_id, plan_id, amount_cents, gateway, status, created_at) VALUES (?, ?, ?, ?, ?, 'pending', ?)`,
		o.No, o.UserID, o.PlanID, o.AmountCents, o.Gateway, ts)
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

// ErrAlreadyPaid signals an idempotent repeat of a paid notification.
var ErrAlreadyPaid = errors.New("store: order already paid")

// MarkPaid flips a pending order to paid and grants the plan in one
// transaction. Repeats return ErrAlreadyPaid; cancelled orders return
// ErrNotFound.
func (s *Store) MarkPaid(ctx context.Context, no, gatewayRef string, at time.Time) (*domain.Order, error) {
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
		return nil, ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `UPDATE orders SET status = 'paid', gateway_ref = ?, paid_at = ? WHERE id = ? AND status = 'pending'`, gatewayRef, at.Unix(), o.ID); err != nil {
		return nil, err
	}
	plan, err := planByIDTx(ctx, tx, o.PlanID)
	if err != nil {
		return nil, err
	}
	if err := grantTx(ctx, tx, o.UserID, plan, at); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	o.Status, o.GatewayRef = "paid", gatewayRef
	o.PaidAt = &at
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
	if o.Status != "pending" {
		return nil, ErrAlreadyPaid
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
	if err := grantTx(ctx, tx, o.UserID, plan, at); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	o.Status, o.GatewayRef, o.PaidAt = "paid", "balance", &at
	return o, nil
}

// ErrInsufficientBalance means the user cannot cover the order.
var ErrInsufficientBalance = errors.New("store: insufficient balance")

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
	q := `SELECT id, name, price_cents, period_days, quota_bytes, device_limit, speed_limit_mbps, group_id, sort, enabled FROM plans`
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
		if err := rows.Scan(&p.ID, &p.Name, &p.PriceCents, &p.PeriodDays, &p.QuotaBytes, &p.DeviceLimit, &p.SpeedLimitMbps, &group, &p.Sort, &enabled); err != nil {
			return nil, err
		}
		p.GroupID = int64Ptr(group)
		p.Enabled = enabled == 1
		out = append(out, &p)
	}
	return out, rows.Err()
}
