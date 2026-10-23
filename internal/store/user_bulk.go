package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/zeptop-dev/captain/internal/auth"
)

var ErrBulkRequest = errors.New("invalid or expired bulk operation")

type UserBulkRequest struct {
	IDs     []int64 `json:"ids"`
	Action  string  `json:"action"`
	GroupID *int64  `json:"group_id"`
	PlanID  int64   `json:"plan_id"`
	Days    int     `json:"days"`
}

func (r UserBulkRequest) Validate() error {
	if len(r.IDs) == 0 || len(r.IDs) > 200 {
		return ErrBulkRequest
	}
	seen := map[int64]bool{}
	for _, id := range r.IDs {
		if id <= 0 || id > MaxAccountID || seen[id] {
			return ErrBulkRequest
		}
		seen[id] = true
	}
	switch r.Action {
	case "ban", "unban", "rotate_subscription", "delete":
	case "set_group":
		if r.GroupID != nil && *r.GroupID <= 0 {
			return ErrBulkRequest
		}
	case "extend":
		if r.PlanID <= 0 || r.Days < 1 || r.Days > 3650 {
			return ErrBulkRequest
		}
	case "reset_usage":
		if r.PlanID <= 0 {
			return ErrBulkRequest
		}
	default:
		return ErrBulkRequest
	}
	return nil
}

type UserBulkRow struct {
	ID             int64  `json:"id"`
	Email          string `json:"email"`
	Outcome        string `json:"outcome"`
	Fingerprint    string `json:"fingerprint,omitempty"`
	SubscriptionID int64  `json:"subscription_id,omitempty"`
	ExpiresAt      *int64 `json:"expires_at,omitempty"`
}
type UserBulkJob struct {
	ID        string          `json:"id"`
	Request   UserBulkRequest `json:"request"`
	Rows      []UserBulkRow   `json:"rows"`
	ExpiresAt time.Time       `json:"expires_at"`
	Done      bool            `json:"done"`
}

func inspectBulkRow(ctx context.Context, tx *sql.Tx, req UserBulkRequest, id int64) (UserBulkRow, error) {
	row := UserBulkRow{ID: id, Outcome: "ready"}
	var agent int64
	var status, token string
	var group sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT email,agent_id,status,group_id,sub_token FROM users WHERE id=? AND role='user'`, id).Scan(&row.Email, &agent, &status, &group, &token)
	if errors.Is(err, sql.ErrNoRows) {
		row.Outcome = "missing"
		return row, nil
	}
	if err != nil {
		return row, err
	}
	var expires sql.NullInt64
	if req.Action == "extend" || req.Action == "reset_usage" {
		err = tx.QueryRowContext(ctx, `SELECT id,expires_at FROM subscriptions WHERE user_id=? AND plan_id=? AND status='active' ORDER BY id DESC LIMIT 1`, id, req.PlanID).Scan(&row.SubscriptionID, &expires)
		if errors.Is(err, sql.ErrNoRows) {
			row.Outcome = "no_subscription"
		} else if err != nil {
			return row, err
		}
		if req.Action == "extend" && row.Outcome == "ready" && !expires.Valid {
			row.Outcome = "unlimited"
		}
		if expires.Valid {
			row.ExpiresAt = &expires.Int64
		}
	}
	if req.Action == "delete" {
		var history bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM orders WHERE user_id=?) OR EXISTS(SELECT 1 FROM commissions WHERE inviter_id=? OR invitee_id=?) OR EXISTS(SELECT 1 FROM users WHERE invited_by=?)`, id, id, id, id).Scan(&history)
		if err != nil {
			return row, err
		}
		if history {
			row.Outcome = "history"
		}
	}
	// A reassigned public ID must never inherit a preview. The immutable agent ID
	// is included; mutable state guards against edits between preview and apply.
	b, _ := json.Marshal([]any{id, agent, row.Email, status, group, token, row.SubscriptionID, expires})
	sum := sha256.Sum256(b)
	row.Fingerprint = hex.EncodeToString(sum[:])
	return row, nil
}
func validateBulkGroup(ctx context.Context, tx *sql.Tx, r UserBulkRequest) error {
	if r.Action != "set_group" || r.GroupID == nil {
		return nil
	}
	var id int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM user_groups WHERE id=?`, *r.GroupID).Scan(&id); errors.Is(err, sql.ErrNoRows) {
		return ErrBulkRequest
	} else {
		return err
	}
}
func (s *Store) PreviewUserBulk(ctx context.Context, staffID int64, req UserBulkRequest, at time.Time) (*UserBulkJob, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = validateBulkGroup(ctx, tx, req); err != nil {
		return nil, err
	}
	job := &UserBulkJob{ID: auth.Token(24), Request: req, Rows: []UserBulkRow{}, ExpiresAt: at.Add(10 * time.Minute)}
	for _, id := range req.IDs {
		row, err := inspectBulkRow(ctx, tx, req, id)
		if err != nil {
			return nil, err
		}
		job.Rows = append(job.Rows, row)
	}
	request, _ := json.Marshal(req)
	preview, _ := json.Marshal(job.Rows)
	// Result retention is bounded; the normal administrative audit log remains.
	if _, err = tx.ExecContext(ctx, `DELETE FROM user_bulk_jobs WHERE created_at < ?`, at.Add(-30*24*time.Hour).Unix()); err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO user_bulk_jobs(id,staff_id,request_json,preview_json,created_at,expires_at) VALUES(?,?,?,?,?,?)`, job.ID, staffID, string(request), string(preview), at.Unix(), job.ExpiresAt.Unix())
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return job, nil
}
func readBulkJob(ctx context.Context, tx *sql.Tx, staffID int64, id string) (*UserBulkJob, error) {
	job := &UserBulkJob{ID: id}
	var req, preview, result string
	var expires int64
	var done sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT request_json,preview_json,result_json,expires_at,done_at FROM user_bulk_jobs WHERE id=? AND staff_id=?`, id, staffID).Scan(&req, &preview, &result, &expires, &done)
	if err != nil {
		return nil, wrapNotFound(err)
	}
	job.Done = done.Valid
	job.ExpiresAt = time.Unix(expires, 0)
	if err = json.Unmarshal([]byte(req), &job.Request); err != nil {
		return nil, err
	}
	if job.Done {
		preview = result
	}
	if err = json.Unmarshal([]byte(preview), &job.Rows); err != nil {
		return nil, err
	}
	return job, nil
}
func (s *Store) UserBulk(ctx context.Context, staffID int64, id string) (*UserBulkJob, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	return readBulkJob(ctx, tx, staffID, id)
}
func (s *Store) ExecuteUserBulk(ctx context.Context, staffID int64, id string, at time.Time) (*UserBulkJob, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	job, err := readBulkJob(ctx, tx, staffID, id)
	if err != nil {
		return nil, err
	}
	if job.Done {
		return job, nil
	} // Same result on retries, including after a lost response.
	if !at.Before(job.ExpiresAt) || job.Request.Validate() != nil {
		return nil, ErrBulkRequest
	}
	req := job.Request
	if err = validateBulkGroup(ctx, tx, req); err != nil {
		return nil, err
	}
	for i, expected := range job.Rows {
		if expected.Outcome != "ready" {
			continue
		}
		row, err := inspectBulkRow(ctx, tx, req, expected.ID)
		if err != nil {
			return nil, err
		}
		if row.Outcome != "ready" {
			job.Rows[i] = row
			continue
		}
		if row.Fingerprint != expected.Fingerprint {
			row.Outcome = "conflict"
			job.Rows[i] = row
			continue
		}
		var q string
		var args []any
		switch req.Action {
		case "ban", "unban":
			status := "banned"
			if req.Action == "unban" {
				status = "active"
			}
			q = `UPDATE users SET status=?,updated_at=? WHERE id=?`
			args = []any{status, at.Unix(), row.ID}
		case "set_group":
			q = `UPDATE users SET group_id=?,updated_at=? WHERE id=?`
			args = []any{nullInt64(req.GroupID), at.Unix(), row.ID}
		case "rotate_subscription":
			if _, err = tx.ExecContext(ctx, `DELETE FROM wireguard_devices WHERE user_id=?`, row.ID); err != nil {
				return nil, err
			}
			q = `UPDATE users SET sub_token=?,updated_at=? WHERE id=?`
			args = []any{auth.Token(24), at.Unix(), row.ID}
		case "extend":
			expiry := max(*row.ExpiresAt, at.Unix()) + int64(req.Days)*86400
			if expiry > 253402300799 {
				row.Outcome = "conflict"
				job.Rows[i] = row
				continue
			}
			q = `UPDATE subscriptions SET expires_at=?,updated_at=? WHERE id=?`
			args = []any{expiry, at.Unix(), row.SubscriptionID}
			row.ExpiresAt = &expiry
		case "reset_usage":
			q = `UPDATE subscriptions SET used_up_bytes=0,used_down_bytes=0,updated_at=? WHERE id=?`
			args = []any{at.Unix(), row.SubscriptionID}
		case "delete":
			err = deleteUserTx(ctx, tx, row.ID)
		}
		if q != "" {
			_, err = tx.ExecContext(ctx, q, args...)
		}
		if err != nil {
			return nil, err
		}
		row.Outcome = "applied"
		row.Fingerprint = ""
		job.Rows[i] = row
	}
	job.Done = true
	b, _ := json.Marshal(job.Rows)
	if _, err = tx.ExecContext(ctx, `UPDATE user_bulk_jobs SET result_json=?,done_at=? WHERE id=?`, string(b), at.Unix(), id); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return job, nil
}
