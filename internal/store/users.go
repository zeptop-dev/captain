package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
)

const userCols = "id, email, password_hash, role, uuid, sub_token, group_id, balance_cents, status, created_at, updated_at, COALESCE(invite_code, ''), invited_by, hwid_limit, first_connected_at"

func scanUser(row interface{ Scan(...any) error }) (*domain.User, error) {
	var u domain.User
	var group, invitedBy, hwid, firstConn sql.NullInt64
	var created, updated int64
	if err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.UUID, &u.SubToken, &group, &u.BalanceCents, &u.Status, &created, &updated, &u.InviteCode, &invitedBy, &hwid, &firstConn); err != nil {
		return nil, wrapNotFound(err)
	}
	u.GroupID = int64Ptr(group)
	u.InvitedBy = int64Ptr(invitedBy)
	if hwid.Valid {
		n := int(hwid.Int64)
		u.HwidLimit = &n
	}
	if firstConn.Valid {
		t := unix(firstConn.Int64)
		u.FirstConnectedAt = &t
	}
	u.CreatedAt, u.UpdatedAt = unix(created), unix(updated)
	return &u, nil
}

// CreateUser inserts a user and sets its ID.
func (s *Store) CreateUser(ctx context.Context, u *domain.User) error {
	ts := now()
	if u.InviteCode == "" {
		u.InviteCode = newInviteCode()
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO users (email, password_hash, role, uuid, sub_token, group_id, balance_cents, status, created_at, updated_at, invite_code, invited_by, register_ip)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.Email, u.PasswordHash, u.Role, u.UUID, u.SubToken, nullInt64(u.GroupID), u.BalanceCents, u.Status, ts, ts, u.InviteCode, nullInt64(u.InvitedBy), u.RegisterIP)
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
		q += ` AND (u.group_id = ? OR EXISTS (SELECT 1 FROM subscriptions sub JOIN plans p ON p.id = sub.plan_id WHERE sub.user_id = u.id AND sub.status = 'active' AND p.group_id = ?
		              AND (sub.expires_at IS NULL OR sub.expires_at > ?) AND (sub.quota_bytes = 0 OR sub.used_up_bytes + sub.used_down_bytes < sub.quota_bytes)))`
		args = append(args, *groupID, *groupID, at.Unix())
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
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (id, user_id, expires_at, created_at, admin) VALUES (?, ?, ?, ?, ?)`,
		sess.ID, sess.UserID, sess.ExpiresAt.Unix(), now(), boolInt(sess.Admin))
	return err
}

// SessionUser resolves a session id to its user if not expired; admin
// reports whether the session came from the admin login.
func (s *Store) SessionUser(ctx context.Context, id string, at time.Time) (*domain.User, bool, error) {
	var uid int64
	var admin int
	if err := s.db.QueryRowContext(ctx, `SELECT user_id, admin FROM sessions WHERE id = ? AND expires_at > ?`, id, at.Unix()).Scan(&uid, &admin); err != nil {
		return nil, false, wrapNotFound(err)
	}
	u, err := s.UserByID(ctx, uid)
	if err != nil {
		return nil, false, err
	}
	return u, admin == 1, nil
}

// DeleteUserSessions signs the user out everywhere (password change).
func (s *Store) DeleteUserSessions(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
	return err
}

func (s *Store) DeleteSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	return err
}

func (s *Store) UserByUUID(ctx context.Context, uuid string) (*domain.User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE uuid = ?`, uuid))
}

// NodeUserIDs is the set of users a node may report traffic or client
// addresses for: active users with an active subscription who can reach
// at least one of the node's enabled inbounds (ungrouped inbound = anyone,
// grouped = the user's own group or a group of one of their plans).
func (s *Store) NodeUserIDs(ctx context.Context, nodeID int64) (map[int64]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT u.id FROM users u
		WHERE u.status = 'active' AND u.role = 'user'
		  AND EXISTS (SELECT 1 FROM subscriptions sub WHERE sub.user_id = u.id AND sub.status = 'active')
		  AND (EXISTS (SELECT 1 FROM inbounds i WHERE i.node_id = ? AND i.enabled = 1 AND i.group_id IS NULL)
		    OR u.group_id IN (SELECT group_id FROM inbounds WHERE node_id = ? AND enabled = 1 AND group_id IS NOT NULL)
		    OR EXISTS (SELECT 1 FROM subscriptions sub JOIN plans p ON p.id = sub.plan_id WHERE sub.user_id = u.id AND sub.status = 'active'
		               AND p.group_id IN (SELECT group_id FROM inbounds WHERE node_id = ? AND enabled = 1 AND group_id IS NOT NULL)))`, nodeID, nodeID, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// AdjustBalance adds delta (may be negative) to a user's balance.
func (s *Store) AdjustBalance(ctx context.Context, userID, deltaCents int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET balance_cents = balance_cents + ?, updated_at = ? WHERE id = ? AND role = 'user'`, deltaCents, now(), userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UserRow is a user with subscription summary for admin lists.
type UserRow struct {
	User       domain.User
	PlanName   string
	ExpiresAt  *time.Time
	QuotaBytes int64
	UsedBytes  int64
	SubUsable  bool
	SubCount   int // active subscriptions (the summary above is the primary one)
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
	rows, err := s.db.QueryContext(ctx, `SELECT u.id, u.email, u.password_hash, u.role, u.uuid, u.sub_token, u.group_id, u.balance_cents, u.status, u.created_at, u.updated_at, p.name, sub.expires_at, sub.quota_bytes, (SELECT COUNT(*) FROM subscriptions c WHERE c.user_id = u.id AND c.status = 'active'), sub.used_up_bytes + sub.used_down_bytes
		FROM users u
		LEFT JOIN subscriptions sub ON sub.id = (SELECT id FROM subscriptions x WHERE x.user_id = u.id AND x.status = 'active'
			ORDER BY ((x.expires_at IS NULL OR x.expires_at > ?) AND (x.quota_bytes = 0 OR x.used_up_bytes + x.used_down_bytes < x.quota_bytes)) DESC, x.expires_at IS NULL DESC, x.expires_at DESC, x.id DESC LIMIT 1)
		LEFT JOIN plans p ON p.id = sub.plan_id
		`+where+` ORDER BY u.id DESC LIMIT ? OFFSET ?`, append(append([]any{at.Unix()}, args...), limit, offset)...)
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
			&plan, &expires, &quota, &r.SubCount, &used); err != nil {
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
// UpdateUser changes a customer's status, group and password. Staff
// accounts are out of reach here (ErrNotFound): they are managed through
// the admins API, so an operator cannot reset an admin's password or ban
// the last admin through the user list.
func (s *Store) UpdateUser(ctx context.Context, id int64, status string, groupID *int64, passwordHash string) error {
	if passwordHash != "" {
		if _, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ? AND role = 'user'`, passwordHash, now(), id); err != nil {
			return err
		}
	}
	res, err := s.db.ExecContext(ctx, `UPDATE users SET status = ?, group_id = ?, updated_at = ? WHERE id = ? AND role = 'user'`, status, nullInt64(groupID), now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateStaff is UpdateUser for a staff account: the admins API guards
// who may call it (and keeps the last admin), so the role filter that
// protects staff from the customer endpoints does not apply here.
func (s *Store) UpdateStaff(ctx context.Context, id int64, status string, passwordHash string) error {
	if passwordHash != "" {
		if _, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ? AND role <> 'user'`, passwordHash, now(), id); err != nil {
			return err
		}
	}
	res, err := s.db.ExecContext(ctx, `UPDATE users SET status = ?, updated_at = ? WHERE id = ?`, status, now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// RotateSubToken issues a new subscription token.
func (s *Store) RotateSubToken(ctx context.Context, id int64, token string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET sub_token = ?, updated_at = ? WHERE id = ? AND role = 'user'`, token, now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteUser removes a user and, via cascades, sessions and subscriptions.
// The tables the nodes fill have no foreign key (they are written on a hot
// path), so they are cleared here: SQLite reuses row ids, and the next
// registrant must not inherit somebody's connection log, audit hits or
// throttle.
func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = ? AND role = 'user'`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	for _, q := range []string{
		`DELETE FROM conn_log WHERE user_id = ?`,
		`DELETE FROM audit_log WHERE user_id = ?`,
		`DELETE FROM dyn_limits WHERE user_id = ?`,
		`DELETE FROM hwid_devices WHERE user_id = ?`,
		`DELETE FROM sub_requests WHERE user_id = ?`,
		`DELETE FROM traffic_daily WHERE user_id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, q, id); err != nil {
			return err
		}
	}
	return tx.Commit()
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

// ListStaff returns console accounts (every role but "user").
func (s *Store) ListStaff(ctx context.Context) ([]*domain.User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+userCols+` FROM users WHERE role != 'user' ORDER BY id`)
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

// SetRole changes a user's role.
func (s *Store) SetRole(ctx context.Context, id int64, role string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET role = ?, updated_at = ? WHERE id = ?`, role, now(), id)
	return err
}

// CountAdmins returns how many full admins exist (the last one cannot go).
func (s *Store) CountAdmins(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role = 'admin' AND status = 'active'`).Scan(&n)
	return n, err
}

// DeleteStaff removes a console account (never a plain user).
func (s *Store) DeleteStaff(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ? AND role != 'user'`, id)
	return err
}
