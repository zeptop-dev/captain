package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

// NodeForward is one port-forward rule on a node plus the inbound it points
// at when the target is another managed node (so an entry can be made from
// it with one click).
type NodeForward struct {
	spec.Forward
	InboundID int64 `json:"inbound_id,omitempty"`
}

// ForwardStatus is what the node last reported for one rule.
type ForwardStatus struct {
	Tag        string    `json:"tag"`
	Up         bool      `json:"up"`
	RTTMillis  int64     `json:"rtt_ms"`
	LastError  string    `json:"last_error"`
	ActiveConn int64     `json:"active_conn"`
	TotalConn  int64     `json:"total_conn"`
	BytesIn    int64     `json:"bytes_in"`
	BytesOut   int64     `json:"bytes_out"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (s *Store) NodeForwards(ctx context.Context, nodeID int64) ([]NodeForward, error) {
	var raw string
	if err := s.db.QueryRowContext(ctx, `SELECT forwards_json FROM nodes WHERE id = ?`, nodeID).Scan(&raw); err != nil {
		return nil, wrapNotFound(err)
	}
	out := []NodeForward{}
	_ = json.Unmarshal([]byte(raw), &out)
	return out, nil
}

func (s *Store) SetNodeForwards(ctx context.Context, nodeID int64, list []NodeForward) error {
	if list == nil {
		list = []NodeForward{}
	}
	b, _ := json.Marshal(list)
	_, err := s.db.ExecContext(ctx, `UPDATE nodes SET forwards_json = ?, updated_at = ? WHERE id = ?`, string(b), now(), nodeID)
	if err == nil {
		// Statuses of rules that no longer exist are noise.
		keep := map[string]bool{}
		for _, f := range list {
			keep[f.Tag] = true
		}
		if rows, err := s.db.QueryContext(ctx, `SELECT tag FROM forward_status WHERE node_id = ?`, nodeID); err == nil {
			var stale []string
			for rows.Next() {
				var t string
				if rows.Scan(&t) == nil && !keep[t] {
					stale = append(stale, t)
				}
			}
			rows.Close()
			for _, t := range stale {
				_, _ = s.db.ExecContext(ctx, `DELETE FROM forward_status WHERE node_id = ? AND tag = ?`, nodeID, t)
			}
		}
	}
	return err
}

func (s *Store) ForwardStatuses(ctx context.Context, nodeID int64) (map[string]ForwardStatus, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT tag, up, rtt_ms, last_error, active_conn, total_conn, bytes_in, bytes_out, updated_at FROM forward_status WHERE node_id = ?`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]ForwardStatus{}
	for rows.Next() {
		var f ForwardStatus
		var up int
		var at int64
		if err := rows.Scan(&f.Tag, &up, &f.RTTMillis, &f.LastError, &f.ActiveConn, &f.TotalConn, &f.BytesIn, &f.BytesOut, &at); err != nil {
			return nil, err
		}
		f.Up, f.UpdatedAt = up == 1, unix(at)
		out[f.Tag] = f
	}
	return out, rows.Err()
}
