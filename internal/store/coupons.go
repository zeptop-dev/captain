package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/zeptop-dev/captain/internal/domain"
)

const couponCols = "id, code, name, kind, value, plan_ids_json, max_uses, used, per_user, starts_at, expires_at, enabled, created_at"

func scanCoupon(row interface{ Scan(...any) error }) (*domain.Coupon, error) {
	var c domain.Coupon
	var plans string
	var starts, expires sql.NullInt64
	var enabled int
	var created int64
	if err := row.Scan(&c.ID, &c.Code, &c.Name, &c.Kind, &c.Value, &plans, &c.MaxUses, &c.Used, &c.PerUser, &starts, &expires, &enabled, &created); err != nil {
		return nil, wrapNotFound(err)
	}
	_ = json.Unmarshal([]byte(plans), &c.PlanIDs)
	c.StartsAt, c.ExpiresAt = unixPtr(starts), unixPtr(expires)
	c.Enabled = enabled == 1
	c.CreatedAt = unix(created)
	return &c, nil
}

// CouponByCode finds a coupon, case-insensitively.
func (s *Store) CouponByCode(ctx context.Context, code string) (*domain.Coupon, error) {
	return scanCoupon(s.db.QueryRowContext(ctx, `SELECT `+couponCols+` FROM coupons WHERE UPPER(code) = UPPER(?)`, strings.TrimSpace(code)))
}

// CouponUsesByUser counts paid orders of a user with the coupon.
func (s *Store) CouponUsesByUser(ctx context.Context, couponID, userID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders WHERE coupon_id = ? AND user_id = ? AND status <> 'cancelled'`, couponID, userID).Scan(&n)
	return n, err
}

// ListCoupons returns every coupon, newest first.
func (s *Store) ListCoupons(ctx context.Context) ([]*domain.Coupon, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+couponCols+` FROM coupons ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*domain.Coupon{}
	for rows.Next() {
		c, err := scanCoupon(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func validateCoupon(c *domain.Coupon) error {
	c.Code = strings.ToUpper(strings.TrimSpace(c.Code))
	if c.Code == "" || strings.ContainsAny(c.Code, " /?#") {
		return errors.New("code is required (letters and digits)")
	}
	if c.Kind != "percent" && c.Kind != "fixed" {
		return errors.New("kind must be percent or fixed")
	}
	if c.Value <= 0 || (c.Kind == "percent" && c.Value > 100) {
		return errors.New("value must be positive (and at most 100 for percent)")
	}
	return nil
}

// CreateCoupon inserts a coupon.
func (s *Store) CreateCoupon(ctx context.Context, c *domain.Coupon) error {
	if err := validateCoupon(c); err != nil {
		return err
	}
	plans, _ := json.Marshal(c.PlanIDs)
	if c.PlanIDs == nil {
		plans = []byte("[]")
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO coupons (code, name, kind, value, plan_ids_json, max_uses, used, per_user, starts_at, expires_at, enabled, created_at) VALUES (?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?, ?)`,
		c.Code, c.Name, c.Kind, c.Value, string(plans), c.MaxUses, c.PerUser, nullTime(c.StartsAt), nullTime(c.ExpiresAt), boolInt(c.Enabled), now())
	if err != nil {
		return err
	}
	c.ID, _ = res.LastInsertId()
	return nil
}

// UpdateCoupon replaces a coupon's editable fields.
func (s *Store) UpdateCoupon(ctx context.Context, c *domain.Coupon) error {
	if err := validateCoupon(c); err != nil {
		return err
	}
	plans, _ := json.Marshal(c.PlanIDs)
	if c.PlanIDs == nil {
		plans = []byte("[]")
	}
	_, err := s.db.ExecContext(ctx, `UPDATE coupons SET code = ?, name = ?, kind = ?, value = ?, plan_ids_json = ?, max_uses = ?, per_user = ?, starts_at = ?, expires_at = ?, enabled = ? WHERE id = ?`,
		c.Code, c.Name, c.Kind, c.Value, string(plans), c.MaxUses, c.PerUser, nullTime(c.StartsAt), nullTime(c.ExpiresAt), boolInt(c.Enabled), c.ID)
	return err
}

// DeleteCoupon removes a coupon; orders keep their discount.
func (s *Store) DeleteCoupon(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM coupons WHERE id = ?`, id)
	return err
}

// GenerateCouponCode returns a random 10-character code.
func GenerateCouponCode() string { return randomCode(10) }

func newInviteCode() string { return randomCode(8) }

func randomCode(n int) string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}

// ---- invites -------------------------------------------------------------------

// InviteSettings controls referral rewards.
type InviteSettings struct {
	Enabled        bool `json:"enabled"`
	Percent        int  `json:"percent"`          // share of each paid order credited to the inviter's balance
	FirstOrderOnly bool `json:"first_order_only"` // reward only the invitee's first paid order
}

// SettingInvite is the settings key.
const SettingInvite = "invite"

// UserByInviteCode resolves an invite code to its owner.
func (s *Store) UserByInviteCode(ctx context.Context, code string) (*domain.User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE invite_code = ?`, strings.ToUpper(strings.TrimSpace(code))))
}

// EnsureInviteCode gives an older account a code if it has none.
func (s *Store) EnsureInviteCode(ctx context.Context, u *domain.User) error {
	if u.InviteCode != "" {
		return nil
	}
	u.InviteCode = newInviteCode()
	_, err := s.db.ExecContext(ctx, `UPDATE users SET invite_code = ? WHERE id = ? AND invite_code IS NULL`, u.InviteCode, u.ID)
	return err
}

// SetInvitedBy records who invited a user, only once.
func (s *Store) SetInvitedBy(ctx context.Context, userID, inviterID int64) error {
	if userID == inviterID {
		return errors.New("cannot invite yourself")
	}
	res, err := s.db.ExecContext(ctx, `UPDATE users SET invited_by = ?, updated_at = ? WHERE id = ? AND invited_by IS NULL`, inviterID, now(), userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("an inviter is already recorded")
	}
	return nil
}

// InviteStats summarises a user's referrals.
type InviteStats struct {
	Invited     int   `json:"invited"`
	EarnedCents int64 `json:"earned_cents"`
}

// InviteStatsFor counts invitees and commissions of a user.
func (s *Store) InviteStatsFor(ctx context.Context, userID int64) (InviteStats, error) {
	var st InviteStats
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE invited_by = ?`, userID).Scan(&st.Invited); err != nil {
		return st, err
	}
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount_cents), 0) FROM commissions WHERE inviter_id = ?`, userID).Scan(&st.EarnedCents)
	return st, err
}
