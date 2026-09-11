package store

import (
	"context"
	"database/sql"
	"time"

	"gitlab.com/boyang-hu/captain/internal/domain"
)

const userCols = "id, email, password_hash, role, uuid, sub_token, group_id, balance_cents, status, created_at, updated_at"

func scanUser(row interface{ Scan(...any) error }) (*domain.User, error) {
	var u domain.User
	var group sql.NullInt64
	var created, updated int64
	if err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.UUID, &u.SubToken, &group, &u.BalanceCents, &u.Status, &created, &updated); err != nil {
		return nil, wrapNotFound(err)
	}
	u.GroupID = int64Ptr(group)
	u.CreatedAt, u.UpdatedAt = unix(created), unix(updated)
	return &u, nil
}

// CreateUser inserts a user and sets its ID.
func (s *Store) CreateUser(ctx context.Context, u *domain.User) error {
	ts := now()
	res, err := s.db.ExecContext(ctx, `INSERT INTO users (email, password_hash, role, uuid, sub_token, group_id, balance_cents, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.Email, u.PasswordHash, u.Role, u.UUID, u.SubToken, nullInt64(u.GroupID), u.BalanceCents, u.Status, ts, ts)
	if err != nil {
		return err
	}
	u.ID, _ = res.LastInsertId()
	u.CreatedAt, u.UpdatedAt = unix(ts), unix(ts)
	return nil
}

func (s *Store) UserByEmail(ctx context.Context, email string) (*domain.User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE email = ?`, email))
}

func (s *Store) UserByID(ctx context.Context, id int64) (*domain.User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, id))
}

func (s *Store) UserBySubToken(ctx context.Context, token string) (*domain.User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE sub_token = ?`, token))
}

// UsersWithAccess returns active users whose group may use inbounds of
// groupID (NULL group on the inbound means everyone) and whose subscription
// is currently usable.
func (s *Store) UsersWithAccess(ctx context.Context, groupID *int64, at time.Time) ([]*domain.User, error) {
	q := `SELECT ` + userCols + ` FROM users u
		WHERE u.status = 'active'
		  AND u.role = 'user'
		  AND EXISTS (SELECT 1 FROM subscriptions sub WHERE sub.user_id = u.id AND sub.status = 'active'
		              AND (sub.expires_at IS NULL OR sub.expires_at > ?)
		              AND (sub.quota_bytes = 0 OR sub.used_up_bytes + sub.used_down_bytes < sub.quota_bytes))`
	args := []any{at.Unix()}
	if groupID != nil {
		q += ` AND u.group_id = ?`
		args = append(args, *groupID)
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY u.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// CreateSession stores a session.
func (s *Store) CreateSession(ctx context.Context, sess *domain.Session) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (id, user_id, expires_at, created_at) VALUES (?, ?, ?, ?)`,
		sess.ID, sess.UserID, sess.ExpiresAt.Unix(), now())
	return err
}

// SessionUser resolves a session id to its user if not expired.
func (s *Store) SessionUser(ctx context.Context, id string, at time.Time) (*domain.User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id = (SELECT user_id FROM sessions WHERE id = ? AND expires_at > ?)`, id, at.Unix()))
}

func (s *Store) DeleteSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	return err
}

func (s *Store) UserByUUID(ctx context.Context, uuid string) (*domain.User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE uuid = ?`, uuid))
}

// AdjustBalance adds delta (may be negative) to a user's balance.
func (s *Store) AdjustBalance(ctx context.Context, userID, deltaCents int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET balance_cents = balance_cents + ?, updated_at = ? WHERE id = ?`, deltaCents, now(), userID)
	return err
}
