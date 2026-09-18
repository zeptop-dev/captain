package store

import (
	"context"
	"time"
)

// HwidDevice is one client device a user fetched the subscription from,
// identified by the x-hwid header Happ-class clients send.
type HwidDevice struct {
	Hwid        string    `json:"hwid"`
	Platform    string    `json:"platform,omitempty"`
	OSVersion   string    `json:"os_version,omitempty"`
	DeviceModel string    `json:"device_model,omitempty"`
	UserAgent   string    `json:"user_agent,omitempty"`
	RequestIP   string    `json:"request_ip,omitempty"`
	FirstSeenAt time.Time `json:"first_seen_at"`
	LastSeenAt  time.Time `json:"last_seen_at"`
}

// maxHwidRows is the ceiling on stored devices per user when no limit
// applies: the rows are written by anyone holding the link, and the admin
// drawer and the portal list them all. A configured limit above it wins —
// an operator who allows 100 devices means it — the ceiling only catches
// the unlimited case. The oldest device is evicted.
const maxHwidRows = 64

// MaxHwidRows is that ceiling, for callers that want to name it.
const MaxHwidRows = maxHwidRows

// ClaimHwidDevice records a subscription fetch from device dev. A known
// device is always allowed (its last-seen time moves); a new one is
// admitted while the user has fewer than limit devices (limit <= 0 =
// unlimited). It returns whether the device may have the subscription and
// how many devices the user has afterwards. One transaction, so two
// first fetches racing for the last slot cannot both win.
func (s *Store) ClaimHwidDevice(ctx context.Context, userID int64, dev HwidDevice, limit int, at time.Time) (allowed bool, count int, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, 0, err
	}
	defer tx.Rollback()
	var known int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM hwid_devices WHERE user_id = ? AND hwid = ?`, userID, dev.Hwid).Scan(&known); err != nil {
		return false, 0, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM hwid_devices WHERE user_id = ?`, userID).Scan(&count); err != nil {
		return false, 0, err
	}
	if known > 0 {
		_, err = tx.ExecContext(ctx, `UPDATE hwid_devices SET last_seen_at = ?, user_agent = ?, request_ip = ?, platform = CASE WHEN ? = '' THEN platform ELSE ? END, os_version = CASE WHEN ? = '' THEN os_version ELSE ? END, device_model = CASE WHEN ? = '' THEN device_model ELSE ? END WHERE user_id = ? AND hwid = ?`,
			at.Unix(), dev.UserAgent, dev.RequestIP, dev.Platform, dev.Platform, dev.OSVersion, dev.OSVersion, dev.DeviceModel, dev.DeviceModel, userID, dev.Hwid)
		if err != nil {
			return false, 0, err
		}
		return true, count, tx.Commit()
	}
	if limit > 0 && count >= limit {
		return false, count, tx.Commit()
	}
	// Unlimited (or a limit below the ceiling) still cannot mean unbounded
	// rows: keep the newest and drop the rest.
	ceiling := maxHwidRows
	if limit > ceiling {
		ceiling = limit
	}
	if count >= ceiling {
		if _, err := tx.ExecContext(ctx, `DELETE FROM hwid_devices WHERE user_id = ? AND hwid IN (
			SELECT hwid FROM hwid_devices WHERE user_id = ? ORDER BY last_seen_at ASC LIMIT ?)`, userID, userID, count-ceiling+1); err != nil {
			return false, 0, err
		}
		count = ceiling - 1
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO hwid_devices (user_id, hwid, platform, os_version, device_model, user_agent, request_ip, first_seen_at, last_seen_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, dev.Hwid, dev.Platform, dev.OSVersion, dev.DeviceModel, dev.UserAgent, dev.RequestIP, at.Unix(), at.Unix()); err != nil {
		return false, 0, err
	}
	return true, count + 1, tx.Commit()
}

// HwidDevices lists a user's devices, most recently seen first.
func (s *Store) HwidDevices(ctx context.Context, userID int64) ([]HwidDevice, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT hwid, platform, os_version, device_model, user_agent, request_ip, first_seen_at, last_seen_at FROM hwid_devices WHERE user_id = ? ORDER BY last_seen_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HwidDevice{}
	for rows.Next() {
		var d HwidDevice
		var first, last int64
		if err := rows.Scan(&d.Hwid, &d.Platform, &d.OSVersion, &d.DeviceModel, &d.UserAgent, &d.RequestIP, &first, &last); err != nil {
			return nil, err
		}
		d.FirstSeenAt, d.LastSeenAt = time.Unix(first, 0), time.Unix(last, 0)
		out = append(out, d)
	}
	return out, rows.Err()
}

// DeleteHwidDevice forgets one device (the next fetch from it counts as new).
func (s *Store) DeleteHwidDevice(ctx context.Context, userID int64, hwid string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM hwid_devices WHERE user_id = ? AND hwid = ?`, userID, hwid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetUserHwidLimit overrides the plan's device limit for HWID counting:
// nil = plan limit, 0 = no HWID limit for this user.
func (s *Store) SetUserHwidLimit(ctx context.Context, userID int64, limit *int) error {
	var v any
	if limit != nil {
		v = *limit
	}
	_, err := s.db.ExecContext(ctx, `UPDATE users SET hwid_limit = ?, updated_at = ? WHERE id = ?`, v, now(), userID)
	return err
}

// SubRequest is one fetch of a user's subscription.
type SubRequest struct {
	At        time.Time `json:"at"`
	RequestIP string    `json:"request_ip"`
	UserAgent string    `json:"user_agent"`
	Hwid      string    `json:"hwid,omitempty"`
	Rule      string    `json:"rule,omitempty"` // response rule that matched, if any
	Response  string    `json:"response"`       // format served, or "hwid-denied", "blocked" …
}

// RecordSubRequest appends to the user's fetch history.
func (s *Store) RecordSubRequest(ctx context.Context, userID int64, r SubRequest) error {
	if r.At.IsZero() {
		r.At = time.Now()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO sub_requests (user_id, at, request_ip, user_agent, hwid, rule, response) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		userID, r.At.Unix(), r.RequestIP, r.UserAgent, r.Hwid, r.Rule, r.Response)
	return err
}

// SubRequests returns the user's most recent fetches, newest first.
func (s *Store) SubRequests(ctx context.Context, userID int64, limit int) ([]SubRequest, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT at, request_ip, user_agent, hwid, rule, response FROM sub_requests WHERE user_id = ? ORDER BY at DESC, id DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SubRequest{}
	for rows.Next() {
		var r SubRequest
		var at int64
		if err := rows.Scan(&at, &r.RequestIP, &r.UserAgent, &r.Hwid, &r.Rule, &r.Response); err != nil {
			return nil, err
		}
		r.At = time.Unix(at, 0)
		out = append(out, r)
	}
	return out, rows.Err()
}

// PruneSubRequests drops fetch history older than before.
func (s *Store) PruneSubRequests(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sub_requests WHERE at < ?`, before.Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// PruneHwidDevices forgets devices that have not fetched the subscription
// for a long time: a device row holds a slot of the user's limit for ever
// otherwise, and a client that is gone should not keep one.
func (s *Store) PruneHwidDevices(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM hwid_devices WHERE last_seen_at < ?`, before.Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// TrimSubRequests keeps the newest keep rows per user: the history is for
// support, and a link that is being hammered must not fill the database
// before the age-based prune runs.
func (s *Store) TrimSubRequests(ctx context.Context, keep int) (int64, error) {
	if keep <= 0 {
		keep = 200
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM sub_requests WHERE id IN (
		SELECT id FROM sub_requests s WHERE (
			SELECT COUNT(*) FROM sub_requests n WHERE n.user_id = s.user_id AND n.id > s.id
		) >= ?)`, keep)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
