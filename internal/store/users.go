package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
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

// UserRow is a user with subscription summary for admin lists.
type UserRow struct {
	User       domain.User
	PlanName   string
	ExpiresAt  *time.Time
	QuotaBytes int64
	UsedBytes  int64
	SubUsable  bool
}

// ListUsers returns users matching q (email substring) with their active
// subscription summary, newest first.
func (s *Store) ListUsers(ctx context.Context, q string, limit, offset int, at time.Time) ([]UserRow, int, error) {
	where := `WHERE u.role = 'user'`
	args := []any{}
	if q != "" {
		where += ` AND u.email LIKE ?`
		args = append(args, "%"+q+"%")
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users u `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT u.id, u.email, u.password_hash, u.role, u.uuid, u.sub_token, u.group_id, u.balance_cents, u.status, u.created_at, u.updated_at, p.name, sub.expires_at, sub.quota_bytes, sub.used_up_bytes + sub.used_down_bytes
		FROM users u
		LEFT JOIN subscriptions sub ON sub.user_id = u.id AND sub.status = 'active'
		LEFT JOIN plans p ON p.id = sub.plan_id
		`+where+` ORDER BY u.id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []UserRow
	for rows.Next() {
		var r UserRow
		var group, expires, quota, used sql.NullInt64
		var plan sql.NullString
		var created, updated int64
		if err := rows.Scan(&r.User.ID, &r.User.Email, &r.User.PasswordHash, &r.User.Role, &r.User.UUID, &r.User.SubToken, &group, &r.User.BalanceCents, &r.User.Status, &created, &updated,
			&plan, &expires, &quota, &used); err != nil {
			return nil, 0, err
		}
		r.User.GroupID = int64Ptr(group)
		r.User.CreatedAt, r.User.UpdatedAt = unix(created), unix(updated)
		r.PlanName = plan.String
		r.ExpiresAt = unixPtr(expires)
		r.QuotaBytes, r.UsedBytes = quota.Int64, used.Int64
		if plan.Valid {
			r.SubUsable = (r.ExpiresAt == nil || at.Before(*r.ExpiresAt)) && (r.QuotaBytes == 0 || r.UsedBytes < r.QuotaBytes)
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// UpdateUser changes status, group and password (when non-empty).
func (s *Store) UpdateUser(ctx context.Context, id int64, status string, groupID *int64, passwordHash string) error {
	if passwordHash != "" {
		if _, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`, passwordHash, now(), id); err != nil {
			return err
		}
	}
	_, err := s.db.ExecContext(ctx, `UPDATE users SET status = ?, group_id = ?, updated_at = ? WHERE id = ?`, status, nullInt64(groupID), now(), id)
	return err
}

// RotateSubToken issues a new subscription token.
func (s *Store) RotateSubToken(ctx context.Context, id int64, token string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET sub_token = ?, updated_at = ? WHERE id = ?`, token, now(), id)
	return err
}

// DeleteUser removes a user and, via cascades, sessions and subscriptions.
func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ? AND role = 'user'`, id)
	return err
}

// ListGroups returns all user groups.
func (s *Store) ListGroups(ctx context.Context) ([]domain.Group, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name FROM user_groups ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Group
	for rows.Next() {
		var g domain.Group
		if err := rows.Scan(&g.ID, &g.Name); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// UpdateUserEmail changes a user's login email.
func (s *Store) UpdateUserEmail(ctx context.Context, id int64, email string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET email = ?, updated_at = ? WHERE id = ?`, email, now(), id)
	return err
}
