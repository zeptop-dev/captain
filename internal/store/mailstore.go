package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/zeptop-dev/captain/internal/auth"
)

// NewCode creates (or replaces) a six-digit code for email+purpose, valid
// for ttl, and returns the plain code for mailing. A code issued less than
// a minute ago is refused so the endpoint cannot be used to spam a mailbox.
func (s *Store) NewCode(ctx context.Context, email, purpose string, ttl time.Duration) (string, error) {
	var created int64
	err := s.db.QueryRowContext(ctx, `SELECT created_at FROM verification_codes WHERE email = ? AND purpose = ?`, email, purpose).Scan(&created)
	if err == nil && time.Since(time.Unix(created, 0)) < time.Minute {
		return "", errors.New("a code was sent less than a minute ago")
	}
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	code := fmt.Sprintf("%06d", (int(b[0])<<24|int(b[1])<<16|int(b[2])<<8|int(b[3]))%1000000)
	_, err = s.db.ExecContext(ctx, `INSERT INTO verification_codes (email, purpose, code_hash, expires_at, attempts, created_at) VALUES (?, ?, ?, ?, 0, ?)
		ON CONFLICT(email, purpose) DO UPDATE SET code_hash = excluded.code_hash, expires_at = excluded.expires_at, attempts = 0, created_at = excluded.created_at`,
		email, purpose, auth.SHA256Hex(code), time.Now().Add(ttl).Unix(), now())
	return code, err
}

// CheckCode verifies and consumes a code. Five wrong tries burn it.
func (s *Store) CheckCode(ctx context.Context, email, purpose, code string) error {
	var hash string
	var expires, attempts int64
	err := s.db.QueryRowContext(ctx, `SELECT code_hash, expires_at, attempts FROM verification_codes WHERE email = ? AND purpose = ?`, email, purpose).Scan(&hash, &expires, &attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("no code was sent to this address")
	}
	if err != nil {
		return err
	}
	if time.Now().Unix() > expires || attempts >= 5 {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM verification_codes WHERE email = ? AND purpose = ?`, email, purpose)
		return errors.New("code expired; request a new one")
	}
	if auth.SHA256Hex(code) != hash {
		_, _ = s.db.ExecContext(ctx, `UPDATE verification_codes SET attempts = attempts + 1 WHERE email = ? AND purpose = ?`, email, purpose)
		return errors.New("wrong code")
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM verification_codes WHERE email = ? AND purpose = ?`, email, purpose)
	return err
}

// Reminder is a user due for a notice. Ref identifies what it is about so
// each notice goes out once (MarkNotified stores it).
type Reminder struct {
	UserID    int64
	Email     string
	ExpiresAt time.Time
	UsedPct   int
	Threshold int // the traffic threshold crossed (traffic reminders)
	Ref       string
}

// ExpiringSubscriptions lists active subscriptions expiring within window
// that have not been reminded about this expiry yet.
func (s *Store) ExpiringSubscriptions(ctx context.Context, at time.Time, window time.Duration) ([]Reminder, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT u.id, u.email, sub.expires_at FROM subscriptions sub JOIN users u ON u.id = sub.user_id
		WHERE sub.status = 'active' AND sub.expires_at IS NOT NULL AND sub.expires_at > ? AND sub.expires_at <= ?
		AND NOT EXISTS (SELECT 1 FROM notifications n WHERE n.user_id = u.id AND n.kind = 'expiry' AND n.ref = CAST(sub.expires_at AS TEXT))`,
		at.Unix(), at.Add(window).Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Reminder
	for rows.Next() {
		var r Reminder
		var exp int64
		if err := rows.Scan(&r.UserID, &r.Email, &exp); err != nil {
			return nil, err
		}
		r.ExpiresAt = time.Unix(exp, 0)
		r.Ref = fmt.Sprint(exp)
		out = append(out, r)
	}
	return out, rows.Err()
}

// HighTrafficSubscriptions lists active subscriptions past pct of their
// quota that have not been reminded for that threshold during this quota
// period. The 90 % threshold also honours the pre-threshold reference so
// an upgrade does not repeat old notices.
func (s *Store) HighTrafficSubscriptions(ctx context.Context, pct int) ([]Reminder, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT u.id, u.email, (sub.used_up_bytes + sub.used_down_bytes) * 100 / sub.quota_bytes, sub.starts_at, COALESCE(sub.reset_at, 0)
		FROM subscriptions sub JOIN users u ON u.id = sub.user_id
		WHERE sub.status = 'active' AND sub.quota_bytes > 0 AND (sub.used_up_bytes + sub.used_down_bytes) * 100 / sub.quota_bytes >= ?
		AND NOT EXISTS (SELECT 1 FROM notifications n WHERE n.user_id = u.id AND n.kind = 'traffic'
			AND (n.ref = CAST(sub.starts_at AS TEXT) || '-' || CAST(COALESCE(sub.reset_at, 0) AS TEXT) || ':' || ?
			     OR (? = 90 AND n.ref = CAST(sub.starts_at AS TEXT) || '-' || CAST(COALESCE(sub.reset_at, 0) AS TEXT))))`, pct, pct, pct)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Reminder
	for rows.Next() {
		var r Reminder
		var used, starts, reset int64
		if err := rows.Scan(&r.UserID, &r.Email, &used, &starts, &reset); err != nil {
			return nil, err
		}
		r.UsedPct = int(used)
		r.Threshold = pct
		r.Ref = fmt.Sprintf("%d-%d:%d", starts, reset, pct)
		out = append(out, r)
	}
	return out, rows.Err()
}

// NeverConnectedUsers lists users created before "before" who hold a
// usable plan but whose traffic has never been seen, once each (the
// "not_connected" notification kind marks them).
func (s *Store) NeverConnectedUsers(ctx context.Context, before time.Time) ([]Reminder, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT u.id, u.email FROM users u
		WHERE u.first_connected_at IS NULL AND u.status = 'active' AND u.created_at < ?
		AND EXISTS (SELECT 1 FROM subscriptions sub WHERE sub.user_id = u.id AND sub.status = 'active' AND (sub.expires_at IS NULL OR sub.expires_at > ?))
		AND NOT EXISTS (SELECT 1 FROM notifications n WHERE n.user_id = u.id AND n.kind = 'not_connected')`, before.Unix(), before.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Reminder
	for rows.Next() {
		var r Reminder
		if err := rows.Scan(&r.UserID, &r.Email); err != nil {
			return nil, err
		}
		r.Ref = "1"
		out = append(out, r)
	}
	return out, rows.Err()
}

// MarkNotified records that a reminder went out.
func (s *Store) MarkNotified(ctx context.Context, userID int64, kind, ref string) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO notifications (user_id, kind, ref, sent_at) VALUES (?, ?, ?, ?)`, userID, kind, ref, now())
	return err
}
