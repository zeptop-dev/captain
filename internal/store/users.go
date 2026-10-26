package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
)

const userCols = "id, email, password_hash, role, uuid, sub_token, group_id, balance_cents, status, created_at, updated_at, COALESCE(invite_code, ''), invited_by, hwid_limit, first_connected_at, lang, agent_id"

func scanUser(row interface{ Scan(...any) error }) (*domain.User, error) {
	var u domain.User
	var group, invitedBy, hwid, firstConn sql.NullInt64
	var created, updated int64
	if err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.UUID, &u.SubToken, &group, &u.BalanceCents, &u.Status, &created, &updated, &u.InviteCode, &invitedBy, &hwid, &firstConn, &u.Lang, &u.AgentID); err != nil {
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

var ErrUserEmailInUse = errors.New("email already exists")

// CreateUser inserts a user with an automatically allocated ID.
func (s *Store) CreateUser(ctx context.Context, u *domain.User) error {
	return s.createUser(ctx, u, 0)
}

// CreateUserWithID is the administrator-only choice of a visible account ID.
// The node's accounting identity is always allocated separately.
func (s *Store) CreateUserWithID(ctx context.Context, u *domain.User, id int64) error {
	if id <= 0 || id > MaxAccountID {
		return ErrAccountID
	}
	return s.createUser(ctx, u, id)
}

func (s *Store) createUser(ctx context.Context, u *domain.User, id int64) error {
	if u.Role == "" {
		u.Role = domain.RoleUser
	}
	if u.Role != domain.RoleUser {
		return errors.New("customer role must be user")
	}
	ts := now()
	if u.InviteCode == "" {
		u.InviteCode = newInviteCode()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if id == 0 {
		id, err = nextCustomerID(ctx, tx)
	} else {
		err = availableAccountID(ctx, tx, "users", customerRefs, id)
	}
	if err != nil {
		return err
	}
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM users WHERE email = ?`, u.Email).Scan(&exists); err == nil {
		return ErrUserEmailInUse
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE account_sequences SET value = value + 1 WHERE name = 'agent'`); err != nil {
		return err
	}
	var agentID int64
	if err = tx.QueryRowContext(ctx, `SELECT value FROM account_sequences WHERE name = 'agent'`).Scan(&agentID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO users (id, email, password_hash, role, uuid, sub_token, group_id, balance_cents, status, created_at, updated_at, invite_code, invited_by, register_ip, agent_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, u.Email, u.PasswordHash, u.Role, u.UUID, u.SubToken, nullInt64(u.GroupID), u.BalanceCents, u.Status, ts, ts, u.InviteCode, nullInt64(u.InvitedBy), u.RegisterIP, agentID)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	u.ID, u.AgentID = id, agentID
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
	column := "user_id"
	if sess.Admin {
		column = "staff_id"
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (id, `+column+`, expires_at, created_at, admin) VALUES (?, ?, ?, ?, ?)`,
		sess.ID, sess.UserID, sess.ExpiresAt.Unix(), now(), boolInt(sess.Admin))
	return err
}

// SessionUser resolves a session id to its user if not expired; admin
// reports whether the session came from the admin login.
func (s *Store) SessionUser(ctx context.Context, id string, at time.Time) (*domain.User, bool, error) {
	var uid int64
	var admin int
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(user_id, staff_id), admin FROM sessions WHERE id = ? AND expires_at > ?`, id, at.Unix()).Scan(&uid, &admin); err != nil {
		return nil, false, wrapNotFound(err)
	}
	lookup := s.UserByID
	if admin == 1 {
		lookup = s.StaffByID
	}
	u, err := lookup(ctx, uid)
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

// NodeAgentUserIDs maps immutable wire IDs to account IDs for the users
// a node may report traffic or client addresses for: active users with an active subscription who can reach
// at least one of the node's enabled inbounds (ungrouped inbound = anyone,
// grouped = the user's own group or a group of one of their plans).
func (s *Store) NodeAgentUserIDs(ctx context.Context, nodeID int64) (map[int64]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT u.agent_id, u.id FROM users u
        WHERE u.status='active' AND u.role='user'
        AND EXISTS(SELECT 1 FROM subscriptions sub WHERE sub.user_id=u.id AND sub.status='active')
        AND EXISTS(SELECT 1 FROM inbounds i JOIN effective_inbound_access ia ON ia.inbound_id=i.id
        WHERE i.node_id=? AND i.enabled=1 AND `+inboundUserAccessSQL(false)+`)`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int64{}
	for rows.Next() {
		var wire, id int64
		if err := rows.Scan(&wire, &id); err != nil {
			return nil, err
		}
		out[wire] = id
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
	return s.ListUsersFiltered(ctx, UserFilter{Query: q, Desc: true}, limit, offset, at)
}

func (s *Store) ListUsersFiltered(ctx context.Context, f UserFilter, limit, offset int, at time.Time) ([]UserRow, int, error) {
	if err := f.Validate(); err != nil {
		return nil, 0, err
	}
	where, args, order := f.query(at)
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users u `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT u.id, u.email, u.password_hash, u.role, u.uuid, u.sub_token, u.group_id, u.balance_cents, u.status, u.created_at, u.updated_at, p.name, sub.expires_at, sub.quota_bytes, (SELECT COUNT(*) FROM subscriptions c WHERE c.user_id = u.id AND c.status = 'active'), sub.used_up_bytes + sub.used_down_bytes
		FROM users u
		LEFT JOIN subscriptions sub ON sub.id = (SELECT id FROM subscriptions x WHERE x.user_id = u.id AND x.status = 'active'
			ORDER BY ((x.expires_at IS NULL OR x.expires_at > ?) AND (x.quota_bytes = 0 OR x.used_up_bytes + x.used_down_bytes < x.quota_bytes)) DESC, x.expires_at IS NULL DESC, x.expires_at DESC, x.id DESC LIMIT 1)
		LEFT JOIN plans p ON p.id = sub.plan_id
		`+where+` ORDER BY `+order+` LIMIT ? OFFSET ?`, append(append([]any{at.Unix()}, args...), limit, offset)...)
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

// RotateSubToken issues a new subscription token.
func (s *Store) RotateSubToken(ctx context.Context, id int64, token string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE users SET sub_token = ?, updated_at = ? WHERE id = ? AND role = 'user'`, token, now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	// Resetting subscription access also revokes independently issued WG credentials.
	if _, err = tx.ExecContext(ctx, `DELETE FROM wireguard_devices WHERE user_id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// ErrUserHasHistory protects orders, commissions and other users' invitations.
var ErrUserHasHistory = errors.New("user has orders, commissions or invited users; keep the account banned to preserve history")

// DeleteUser removes a user and, via cascades, sessions and subscriptions.
// Financial history and invitations from other users prevent deletion.
// The tables the nodes fill have no foreign key (they are written on a hot
// path), so they are cleared here: administrators may reuse a deleted ID,
// and its new owner must not inherit connection logs, audit hits or throttles.
func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := deleteUserTx(ctx, tx, id); err != nil {
		return err
	}
	return tx.Commit()
}

func deleteUserTx(ctx context.Context, tx *sql.Tx, id int64) error {
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM users WHERE id = ? AND role = 'user'`, id).Scan(&exists); err != nil {
		return wrapNotFound(err)
	}
	// Check the restrictive references before deleting, in the same transaction,
	// so callers can distinguish preserved history from a storage failure.
	var hasHistory bool
	if err := tx.QueryRowContext(ctx, `SELECT
		EXISTS(SELECT 1 FROM orders WHERE user_id = ?)
		OR EXISTS(SELECT 1 FROM commissions WHERE inviter_id = ? OR invitee_id = ?)
		OR EXISTS(SELECT 1 FROM users WHERE invited_by = ?)`, id, id, id, id).Scan(&hasHistory); err != nil {
		return err
	}
	if hasHistory {
		return ErrUserHasHistory
	}
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
	return nil
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

// SetUserLang records the language a user reads, so their mail can follow
// it. "" clears it back to the panel's mail language.
func (s *Store) SetUserLang(ctx context.Context, userID int64, lang string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET lang = ?, updated_at = ? WHERE id = ?`, lang, now(), userID)
	return err
}
