package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

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
	Percent        int  `json:"percent"`          // level-1 share of each paid order
	FirstOrderOnly bool `json:"first_order_only"` // reward only the invitee's first paid order
	// MultiLevel also rewards the inviter's inviter (Level2) and theirs (Level3).
	MultiLevel bool `json:"multi_level"`
	Level2     int  `json:"level2"`
	Level3     int  `json:"level3"`
	// Payout is "balance" (spendable at once) or "commission" (a separate
	// balance the user can move to balance or withdraw).
	Payout           string   `json:"payout"`
	MinWithdrawCents int64    `json:"min_withdraw_cents"`
	WithdrawMethods  []string `json:"withdraw_methods"` // e.g. USDT-TRC20, Alipay
}

// Payout modes.
const (
	PayoutBalance    = "balance"
	PayoutCommission = "commission"
)

// LevelPercents lists the reward per referral level, level 1 first.
func (i InviteSettings) LevelPercents() []int {
	out := []int{i.Percent}
	if i.MultiLevel {
		out = append(out, i.Level2, i.Level3)
		for len(out) > 1 && out[len(out)-1] <= 0 {
			out = out[:len(out)-1]
		}
	}
	return out
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

// SurplusSettings enables crediting the unused part of the current plan when
// a user switches to a different one.
type SurplusSettings struct {
	Enabled bool `json:"enabled"`
}

// SettingSurplus is the settings key.
const SettingSurplus = "surplus"

// SurplusFor values the unused remainder of the user's active subscription
// when it is for a plan other than newPlanID: by remaining time, or by
// remaining traffic when the plan never expires. 0 when nothing applies.
func (s *Store) SurplusFor(ctx context.Context, userID, newPlanID int64, at time.Time) (int64, error) {
	var ss SurplusSettings
	if err := s.GetSetting(ctx, SettingSurplus, &ss); err != nil || !ss.Enabled {
		return 0, err
	}
	sub, err := s.ActiveSubscription(ctx, userID)
	if err != nil || sub == nil || sub.PlanID == newPlanID || !sub.Usable(at) {
		return 0, nil
	}
	plan, err := s.PlanByID(ctx, sub.PlanID)
	if err != nil {
		return 0, nil
	}
	var fraction float64
	var value int64
	if sub.ExpiresAt != nil {
		total := sub.ExpiresAt.Sub(sub.StartsAt)
		if total <= 0 {
			return 0, nil
		}
		fraction = float64(sub.ExpiresAt.Sub(at)) / float64(total)
		days := int(total.Hours()/24 + 0.5)
		if p, ok := plan.PriceFor(days); ok {
			value = p
		} else if plan.PeriodDays > 0 {
			value = plan.PriceCents * int64(days) / int64(plan.PeriodDays)
		}
	} else if sub.QuotaBytes > 0 {
		fraction = float64(sub.QuotaBytes-sub.UsedUpBytes-sub.UsedDownBytes) / float64(sub.QuotaBytes)
		value = plan.PriceCents
	}
	if fraction <= 0 || fraction > 1 || value <= 0 {
		return 0, nil
	}
	return int64(float64(value) * fraction), nil
}

// ---- commission balance and withdrawals ------------------------------------------

// CommissionCents returns the user's withdrawable referral balance.
func (s *Store) CommissionCents(ctx context.Context, userID int64) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT commission_cents FROM users WHERE id = ?`, userID).Scan(&n)
	return n, err
}

// ErrInsufficientCommission is returned when the commission balance is too low.
var ErrInsufficientCommission = errors.New("commission balance too low")

// TransferCommission moves amount from the commission balance to the balance.
func (s *Store) TransferCommission(ctx context.Context, userID, amount int64) error {
	if amount <= 0 {
		return errors.New("amount must be positive")
	}
	res, err := s.db.ExecContext(ctx, `UPDATE users SET commission_cents = commission_cents - ?, balance_cents = balance_cents + ?, updated_at = ? WHERE id = ? AND commission_cents >= ?`, amount, amount, now(), userID, amount)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrInsufficientCommission
	}
	return nil
}

// Withdrawal is a payout request against the commission balance.
type Withdrawal struct {
	ID          int64     `json:"id"`
	UserID      int64     `json:"user_id"`
	Email       string    `json:"email,omitempty"`
	AmountCents int64     `json:"amount_cents"`
	Method      string    `json:"method"`
	Account     string    `json:"account"`
	Status      string    `json:"status"`
	Note        string    `json:"note"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CreateWithdrawal reserves amount from the commission balance and files a
// pending request.
func (s *Store) CreateWithdrawal(ctx context.Context, userID, amount int64, method, account string) (*Withdrawal, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE users SET commission_cents = commission_cents - ?, updated_at = ? WHERE id = ? AND commission_cents >= ?`, amount, now(), userID, amount)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrInsufficientCommission
	}
	ts := now()
	res, err = tx.ExecContext(ctx, `INSERT INTO withdrawals (user_id, amount_cents, method, account, status, created_at, updated_at) VALUES (?, ?, ?, ?, 'pending', ?, ?)`, userID, amount, method, account, ts, ts)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &Withdrawal{ID: id, UserID: userID, AmountCents: amount, Method: method, Account: account, Status: "pending", CreatedAt: unix(ts), UpdatedAt: unix(ts)}, nil
}

// ListWithdrawals returns requests newest first; userID 0 = all, status "" = any.
func (s *Store) ListWithdrawals(ctx context.Context, userID int64, status string, limit int) ([]Withdrawal, error) {
	where, args := "WHERE 1=1", []any{}
	if userID > 0 {
		where, args = where+" AND w.user_id = ?", append(args, userID)
	}
	if status != "" {
		where, args = where+" AND w.status = ?", append(args, status)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT w.id, w.user_id, u.email, w.amount_cents, w.method, w.account, w.status, w.note, w.created_at, w.updated_at
		FROM withdrawals w JOIN users u ON u.id = w.user_id `+where+` ORDER BY w.id DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Withdrawal
	for rows.Next() {
		var w Withdrawal
		var created, updated int64
		if err := rows.Scan(&w.ID, &w.UserID, &w.Email, &w.AmountCents, &w.Method, &w.Account, &w.Status, &w.Note, &created, &updated); err != nil {
			return nil, err
		}
		w.CreatedAt, w.UpdatedAt = unix(created), unix(updated)
		out = append(out, w)
	}
	return out, rows.Err()
}

// SetWithdrawalStatus marks a pending request paid or rejected; rejecting
// returns the amount to the commission balance.
func (s *Store) SetWithdrawalStatus(ctx context.Context, id int64, status, note string) error {
	if status != "paid" && status != "rejected" {
		return errors.New("status must be paid or rejected")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var userID, amount int64
	if err := tx.QueryRowContext(ctx, `SELECT user_id, amount_cents FROM withdrawals WHERE id = ? AND status = 'pending'`, id).Scan(&userID, &amount); err != nil {
		return wrapNotFound(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE withdrawals SET status = ?, note = ?, updated_at = ? WHERE id = ?`, status, note, now(), id); err != nil {
		return err
	}
	if status == "rejected" {
		if _, err := tx.ExecContext(ctx, `UPDATE users SET commission_cents = commission_cents + ?, updated_at = ? WHERE id = ?`, amount, now(), userID); err != nil {
			return err
		}
	}
	return tx.Commit()
}
