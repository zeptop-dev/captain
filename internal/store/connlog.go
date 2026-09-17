package store

import (
	"context"
	"time"
)

// ConnLogSettings switches the per-connection log on (settings key
// "connlog"): nodes then report every accepted connection and the rows
// are kept RetentionDays days.
type ConnLogSettings struct {
	Enabled       bool `json:"enabled"`
	RetentionDays int  `json:"retention_days"`
}

const SettingConnLog = "connlog"

// Days returns the retention, 7 when unset.
func (s ConnLogSettings) Days() int {
	if s.RetentionDays <= 0 {
		return 7
	}
	return s.RetentionDays
}

// ConnRow is one accepted connection.
type ConnRow struct {
	ID         int64     `json:"id"`
	UserID     int64     `json:"user_id"`
	NodeID     int64     `json:"node_id"`
	NodeName   string    `json:"node_name,omitempty"`
	InboundID  int64     `json:"inbound_id,omitempty"`
	InboundTag string    `json:"inbound_tag,omitempty"`
	At         time.Time `json:"at"`
	ClientIP   string    `json:"client_ip"`
	Host       string    `json:"host"`
	Port       int       `json:"port"`
	Network    string    `json:"network,omitempty"`
}

// AddConnEvents stores a node report's connections in one transaction.
func (s *Store) AddConnEvents(ctx context.Context, rows []ConnRow) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, r := range rows {
		if _, err := tx.ExecContext(ctx, `INSERT INTO conn_log (user_id, node_id, inbound_id, at, client_ip, host, port, network) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			r.UserID, r.NodeID, r.InboundID, r.At.Unix(), r.ClientIP, r.Host, r.Port, r.Network); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// UserConnections returns a user's most recent connections, newest first,
// with node and inbound names.
func (s *Store) UserConnections(ctx context.Context, userID int64, limit int) ([]ConnRow, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `SELECT c.id, c.user_id, c.node_id, COALESCE(n.name, ''), c.inbound_id, COALESCE(i.tag, ''), c.at, c.client_ip, c.host, c.port, c.network
		FROM conn_log c LEFT JOIN nodes n ON n.id = c.node_id LEFT JOIN inbounds i ON i.id = c.inbound_id
		WHERE c.user_id = ? ORDER BY c.at DESC, c.id DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ConnRow
	for rows.Next() {
		var r ConnRow
		var at int64
		if err := rows.Scan(&r.ID, &r.UserID, &r.NodeID, &r.NodeName, &r.InboundID, &r.InboundTag, &at, &r.ClientIP, &r.Host, &r.Port, &r.Network); err != nil {
			return nil, err
		}
		r.At = time.Unix(at, 0)
		out = append(out, r)
	}
	return out, rows.Err()
}

// PruneConnLog deletes rows older than before (and everything when the
// log is off, so switching it off also forgets what was collected).
// PruneConnLog deletes rows older than before in batches: the table is the
// biggest one in the database and a single DELETE would hold the only
// connection (and the write lock) for as long as it takes, stalling the
// panel and every node report behind it.
func (s *Store) PruneConnLog(ctx context.Context, before time.Time) (int64, error) {
	const batch = 20000
	var total int64
	for {
		res, err := s.db.ExecContext(ctx, `DELETE FROM conn_log WHERE id IN (SELECT id FROM conn_log WHERE at < ? LIMIT ?)`, before.Unix(), batch)
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
		if n < batch {
			return total, nil
		}
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		default:
		}
	}
}
