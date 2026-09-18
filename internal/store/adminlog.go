package store

import (
	"context"
	"time"
)

// AdminEntry is one console action: who, what route, which target, and
// what the API answered.
type AdminEntry struct {
	ID     int64     `json:"id"`
	At     time.Time `json:"at"`
	UserID int64     `json:"user_id"`
	Email  string    `json:"email"`
	Role   string    `json:"role"`
	Method string    `json:"method"`
	Path   string    `json:"path"`
	Target string    `json:"target,omitempty"`
	Status int       `json:"status"`
	Via    string    `json:"via"`
	IP     string    `json:"ip"`
}

// AddAdminEntry records one action. Failures are the caller's to log and
// never block the request that is being recorded.
func (s *Store) AddAdminEntry(ctx context.Context, e AdminEntry) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO admin_log (at, user_id, email, role, method, path, target, status, via, ip)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.At.Unix(), nullID(e.UserID), e.Email, e.Role, e.Method, e.Path, e.Target, e.Status, e.Via, e.IP)
	return err
}

// AdminLog lists the newest entries, optionally for one staff account.
func (s *Store) AdminLog(ctx context.Context, userID int64, limit int) ([]AdminEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT id, at, COALESCE(user_id, 0), email, role, method, path, target, status, via, ip FROM admin_log`
	args := []any{}
	if userID > 0 {
		q += ` WHERE user_id = ?`
		args = append(args, userID)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AdminEntry{}
	for rows.Next() {
		var e AdminEntry
		var at int64
		if err := rows.Scan(&e.ID, &at, &e.UserID, &e.Email, &e.Role, &e.Method, &e.Path, &e.Target, &e.Status, &e.Via, &e.IP); err != nil {
			return nil, err
		}
		e.At = unix(at)
		out = append(out, e)
	}
	return out, rows.Err()
}

// PruneAdminLog drops entries older than before.
func (s *Store) PruneAdminLog(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM admin_log WHERE at < ?`, before.Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// nullID turns 0 into NULL so a deleted account leaves the row readable.
func nullID(id int64) any {
	if id <= 0 {
		return nil
	}
	return id
}
