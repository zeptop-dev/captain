package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
)

// SubLink is a short or temporary alias of a user's subscription.
type SubLink struct {
	ID        int64      `json:"id"`
	UserID    int64      `json:"user_id"`
	Code      string     `json:"code"`
	Kind      string     `json:"kind"`
	MaxUses   int        `json:"max_uses"`
	Uses      int        `json:"uses"`
	ExpiresAt *time.Time `json:"expires_at"`
	Enabled   bool       `json:"enabled"`
	CreatedAt time.Time  `json:"created_at"`
}

// ErrLinkExhausted means the link exists but may not be used any more.
var ErrLinkExhausted = errors.New("subscription link expired or used up")

func newShortCode() string { return strings.ToLower(randomCode(8)) }

// ShortCodeForToken returns the user's permanent short code, creating it.
func (s *Store) ShortCodeForToken(ctx context.Context, token string) (string, error) {
	var code string
	err := s.db.QueryRowContext(ctx, `SELECT l.code FROM sub_links l JOIN users u ON u.id = l.user_id WHERE u.sub_token = ? AND l.kind = 'short' AND l.enabled = 1 LIMIT 1`, token).Scan(&code)
	if err == nil {
		return code, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	u, err := s.UserBySubToken(ctx, token)
	if err != nil {
		return "", err
	}
	for i := 0; i < 5; i++ {
		code = newShortCode()
		if _, err := s.db.ExecContext(ctx, `INSERT INTO sub_links (user_id, code, kind, created_at) VALUES (?, ?, 'short', ?)`, u.ID, code, now()); err == nil {
			return code, nil
		}
	}
	return "", errors.New("could not allocate a short code")
}

// RotateShortCode replaces the user's permanent short code (after a token rotation).
func (s *Store) RotateShortCode(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sub_links WHERE user_id = ? AND kind = 'short'`, userID)
	return err
}

// CreateTempLink issues a limited link for a user.
func (s *Store) CreateTempLink(ctx context.Context, userID int64, maxUses int, ttl time.Duration) (*SubLink, error) {
	var expires sql.NullInt64
	if ttl > 0 {
		expires = sql.NullInt64{Int64: time.Now().Add(ttl).Unix(), Valid: true}
	}
	code := newShortCode()
	res, err := s.db.ExecContext(ctx, `INSERT INTO sub_links (user_id, code, kind, max_uses, expires_at, created_at) VALUES (?, ?, 'temp', ?, ?, ?)`, userID, code, maxUses, expires, now())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.subLinkByID(ctx, id)
}

func (s *Store) subLinkByID(ctx context.Context, id int64) (*SubLink, error) {
	return scanSubLink(s.db.QueryRowContext(ctx, `SELECT id, user_id, code, kind, max_uses, uses, expires_at, enabled, created_at FROM sub_links WHERE id = ?`, id))
}

func scanSubLink(row interface{ Scan(...any) error }) (*SubLink, error) {
	var l SubLink
	var exp sql.NullInt64
	var en int
	var created int64
	if err := row.Scan(&l.ID, &l.UserID, &l.Code, &l.Kind, &l.MaxUses, &l.Uses, &exp, &en, &created); err != nil {
		return nil, wrapNotFound(err)
	}
	l.ExpiresAt, l.Enabled, l.CreatedAt = unixPtr(exp), en == 1, unix(created)
	return &l, nil
}

// ListSubLinks returns a user's links, newest first.
func (s *Store) ListSubLinks(ctx context.Context, userID int64) ([]SubLink, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, user_id, code, kind, max_uses, uses, expires_at, enabled, created_at FROM sub_links WHERE user_id = ? ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SubLink{}
	for rows.Next() {
		l, err := scanSubLink(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *l)
	}
	return out, rows.Err()
}

func (s *Store) DeleteSubLink(ctx context.Context, userID, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sub_links WHERE id = ? AND user_id = ?`, id, userID)
	return err
}

// UseSubLink resolves a code to its user, enforcing expiry and use count
// (temp links count every fetch).
func (s *Store) UseSubLink(ctx context.Context, code string, at time.Time) (*domain.User, error) {
	u, _, err := s.ResolveSubLink(ctx, code, at)
	if err != nil {
		return nil, err
	}
	if err := s.ConsumeSubLink(ctx, code); err != nil {
		return nil, err
	}
	return u, nil
}

// ResolveSubLink looks a link up without spending a use, and reports
// whether spending one is needed (a temporary link). The caller spends it
// with ConsumeSubLink once it actually serves a document, so a request a
// response rule refuses does not burn a use.
func (s *Store) ResolveSubLink(ctx context.Context, code string, at time.Time) (*domain.User, bool, error) {
	l, err := scanSubLink(s.db.QueryRowContext(ctx, `SELECT id, user_id, code, kind, max_uses, uses, expires_at, enabled, created_at FROM sub_links WHERE code = ?`, strings.ToLower(strings.TrimSpace(code))))
	if err != nil {
		return nil, false, err
	}
	if !l.Enabled || (l.ExpiresAt != nil && at.After(*l.ExpiresAt)) || (l.MaxUses > 0 && l.Uses >= l.MaxUses) {
		return nil, false, ErrLinkExhausted
	}
	u, err := s.UserByID(ctx, l.UserID)
	return u, l.Kind == "temp", err
}

// ConsumeSubLink spends one use of a temporary link.
func (s *Store) ConsumeSubLink(ctx context.Context, code string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE sub_links SET uses = uses + 1 WHERE code = ? AND kind = 'temp' AND (max_uses = 0 OR uses < max_uses)`, strings.ToLower(strings.TrimSpace(code)))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Not a temporary link (nothing to spend) or exhausted meanwhile.
		var kind string
		if err := s.db.QueryRowContext(ctx, `SELECT kind FROM sub_links WHERE code = ?`, strings.ToLower(strings.TrimSpace(code))).Scan(&kind); err == nil && kind == "temp" {
			return ErrLinkExhausted
		}
	}
	return nil
}

// ---- two-factor ---------------------------------------------------------------------

// TOTP returns the user's secret and whether it is enforced.
func (s *Store) TOTP(ctx context.Context, userID int64) (secret string, enabled bool, err error) {
	var en int
	err = s.db.QueryRowContext(ctx, `SELECT totp_secret, totp_enabled FROM users WHERE id = ?`, userID).Scan(&secret, &en)
	return secret, en == 1, wrapNotFound(err)
}

// SetTOTP stores a secret and its enforcement flag.
func (s *Store) SetTOTP(ctx context.Context, userID int64, secret string, enabled bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET totp_secret = ?, totp_enabled = ?, updated_at = ? WHERE id = ?`, secret, boolInt(enabled), now(), userID)
	return err
}
