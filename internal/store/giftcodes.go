package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
)

// Gift code kinds.
const (
	GiftBalance = "balance" // Value = cents
	GiftPlan    = "plan"    // PlanID + PeriodDays
	GiftTraffic = "traffic" // Value = bytes added to the active subscription
	GiftDays    = "days"    // Value = days added to the active subscription
)

// ErrGiftInvalid is returned for unknown, used or expired codes.
var ErrGiftInvalid = errors.New("code is invalid, expired or already used")

// ErrNoSubscription is returned when a traffic/days code needs an active plan.
var ErrNoSubscription = errors.New("this code tops up an active subscription; buy a plan first")

// CreateGiftCodes generates count codes sharing the given template.
func (s *Store) CreateGiftCodes(ctx context.Context, tpl domain.GiftCode, count int, prefix string) ([]string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var expires sql.NullInt64
	if tpl.ExpiresAt != nil {
		expires = sql.NullInt64{Int64: tpl.ExpiresAt.Unix(), Valid: true}
	}
	codes := make([]string, 0, count)
	for i := 0; i < count; i++ {
		code := prefix + randomCode(12)
		if _, err := tx.ExecContext(ctx, `INSERT INTO gift_codes (code, batch, kind, value, plan_id, period_days, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			code, tpl.Batch, tpl.Kind, tpl.Value, nullInt64(tpl.PlanID), tpl.PeriodDays, expires, now()); err != nil {
			return nil, err
		}
		codes = append(codes, code)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return codes, nil
}

const giftCols = "g.id, g.code, g.batch, g.kind, g.value, g.plan_id, g.period_days, g.expires_at, g.redeemed_by, g.redeemed_at, g.created_at, COALESCE(u.email, '')"

// GiftRow is a code with the redeemer's email.
type GiftRow struct {
	domain.GiftCode
	RedeemedEmail string
}

// ListGiftCodes returns codes newest first, optionally one batch.
func (s *Store) ListGiftCodes(ctx context.Context, batch string, limit, offset int) ([]GiftRow, int, error) {
	where, args := "", []any{}
	if batch != "" {
		where, args = " WHERE g.batch = ?", []any{batch}
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM gift_codes g`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+giftCols+` FROM gift_codes g LEFT JOIN users u ON u.id = g.redeemed_by`+where+` ORDER BY g.id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []GiftRow
	for rows.Next() {
		var g GiftRow
		var plan, expires, by, at sql.NullInt64
		var created int64
		if err := rows.Scan(&g.ID, &g.Code, &g.Batch, &g.Kind, &g.Value, &plan, &g.PeriodDays, &expires, &by, &at, &created, &g.RedeemedEmail); err != nil {
			return nil, 0, err
		}
		g.PlanID, g.ExpiresAt, g.RedeemedBy, g.RedeemedAt, g.CreatedAt = int64Ptr(plan), unixPtr(expires), int64Ptr(by), unixPtr(at), unix(created)
		out = append(out, g)
	}
	return out, total, rows.Err()
}

// GiftBatches lists batch names with counts (total, redeemed).
func (s *Store) GiftBatches(ctx context.Context) ([]GiftBatch, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT batch, kind, COUNT(*), SUM(CASE WHEN redeemed_by IS NULL THEN 0 ELSE 1 END), MIN(created_at) FROM gift_codes GROUP BY batch, kind ORDER BY MIN(created_at) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GiftBatch
	for rows.Next() {
		var b GiftBatch
		var created int64
		if err := rows.Scan(&b.Batch, &b.Kind, &b.Total, &b.Redeemed, &created); err != nil {
			return nil, err
		}
		b.CreatedAt = unix(created)
		out = append(out, b)
	}
	return out, rows.Err()
}

// GiftBatch summarises one generation run.
type GiftBatch struct {
	Batch     string
	Kind      string
	Total     int
	Redeemed  int
	CreatedAt time.Time
}

// DeleteUnredeemedGiftCodes removes the unused codes of a batch.
func (s *Store) DeleteUnredeemedGiftCodes(ctx context.Context, batch string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM gift_codes WHERE batch = ? AND redeemed_by IS NULL`, batch)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// RedeemGiftCode applies a code to the user atomically and returns it.
func (s *Store) RedeemGiftCode(ctx context.Context, userID int64, code string, at time.Time) (*domain.GiftCode, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var g domain.GiftCode
	var plan, expires sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT id, code, batch, kind, value, plan_id, period_days, expires_at FROM gift_codes WHERE code = ? AND redeemed_by IS NULL`, code).
		Scan(&g.ID, &g.Code, &g.Batch, &g.Kind, &g.Value, &plan, &g.PeriodDays, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrGiftInvalid
	}
	if err != nil {
		return nil, err
	}
	if expires.Valid && expires.Int64 <= at.Unix() {
		return nil, ErrGiftInvalid
	}
	g.PlanID = int64Ptr(plan)
	switch g.Kind {
	case GiftBalance:
		if _, err := tx.ExecContext(ctx, `UPDATE users SET balance_cents = balance_cents + ?, updated_at = ? WHERE id = ?`, g.Value, now(), userID); err != nil {
			return nil, err
		}
	case GiftPlan:
		if g.PlanID == nil {
			return nil, fmt.Errorf("gift code %s has no plan", code)
		}
		p, err := planByIDTx(ctx, tx, *g.PlanID)
		if err != nil {
			return nil, err
		}
		if err := grantTx(ctx, tx, userID, p, g.PeriodDays, at, defaultGrantMode(ctx, tx), 0); err != nil {
			return nil, err
		}
	case GiftTraffic, GiftDays:
		set := `quota_bytes = CASE WHEN quota_bytes > 0 THEN quota_bytes + ? ELSE 0 END`
		arg := g.Value
		if g.Kind == GiftDays {
			set = `expires_at = CASE WHEN expires_at IS NULL THEN NULL ELSE expires_at + ? END`
			arg = g.Value * 86400
		}
		res, err := tx.ExecContext(ctx, `UPDATE subscriptions SET `+set+`, updated_at = ? WHERE id = (SELECT id FROM subscriptions sub WHERE user_id = ? AND status = 'active' ORDER BY (`+usableSQL+`) DESC, expires_at IS NULL DESC, expires_at DESC, id DESC LIMIT 1)`, arg, now(), userID, at.Unix())
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil, ErrNoSubscription
		}
	default:
		return nil, fmt.Errorf("unknown gift kind %q", g.Kind)
	}
	res, err := tx.ExecContext(ctx, `UPDATE gift_codes SET redeemed_by = ?, redeemed_at = ? WHERE id = ? AND redeemed_by IS NULL`, userID, at.Unix(), g.ID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrGiftInvalid
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	g.RedeemedBy, g.RedeemedAt = &userID, &at
	return &g, nil
}
