package store

import (
	"context"
	"encoding/json"
	"reflect"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
)

// NodeForward is one port-forward rule on a node plus the inbound it points
// at when the target is another managed node (so an entry can be made from
// it with one click).
type NodeForward struct {
	spec.Forward
	InboundID         int64  `json:"inbound_id,omitempty"`
	TargetAddressMode string `json:"target_address_mode,omitempty"`
	LineID            int64  `json:"line_id,omitempty"`
	LineMemberID      string `json:"line_member_id,omitempty"`
}

// ForwardStatus is what the node last reported for one rule.
type ForwardStatus struct {
	// Health describes only ProbeProtocol (tcp, none, or backend on apply failure).
	Health        string    `json:"health,omitempty"`
	ProbeProtocol string    `json:"probe_protocol,omitempty"`
	Tag           string    `json:"tag"`
	Up            bool      `json:"up"`
	RTTMillis     int64     `json:"rtt_ms"`
	LastError     string    `json:"last_error"`
	ActiveConn    int64     `json:"active_conn"`
	TotalConn     int64     `json:"total_conn"`
	BytesIn       int64     `json:"bytes_in"`
	BytesOut      int64     `json:"bytes_out"`
	UpdatedAt     time.Time `json:"updated_at"`
	// Targets is per-hop health of a rule with several targets.
	Targets []agentproto.ForwardTargetStatus `json:"targets,omitempty"`
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
	var err error
	list, err = s.PreserveLineForwards(ctx, nodeID, list)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	old, err := nodeForwardsTx(ctx, tx, nodeID)
	if err != nil {
		return err
	}
	if err = preserveForwardAddressModes(list, old); err != nil {
		return err
	}
	if err = resolveInternalForwardTargetsTx(ctx, tx, nodeID, list); err != nil {
		return err
	}
	if list == nil {
		list = []NodeForward{}
	}
	b, _ := json.Marshal(list)
	if _, err = tx.ExecContext(ctx, `UPDATE nodes SET forwards_json = ?, updated_at = ? WHERE id = ?`, string(b), now(), nodeID); err != nil {
		return err
	}
	keep := map[string]NodeForward{}
	for _, f := range list {
		keep[f.Tag] = f
	}
	for _, f := range old {
		next, exists := keep[f.Tag]
		if !exists || !reflect.DeepEqual(f, next) {
			if _, err = tx.ExecContext(ctx, `DELETE FROM forward_status WHERE node_id=? AND tag=?`, nodeID, f.Tag); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (s *Store) ForwardStatuses(ctx context.Context, nodeID int64) (map[string]ForwardStatus, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT tag, up, rtt_ms, last_error, active_conn, total_conn, bytes_in, bytes_out, updated_at, targets_json, health, probe_protocol FROM forward_status WHERE node_id = ?`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]ForwardStatus{}
	for rows.Next() {
		var f ForwardStatus
		var up int
		var at int64
		var targets string
		if err := rows.Scan(&f.Tag, &up, &f.RTTMillis, &f.LastError, &f.ActiveConn, &f.TotalConn, &f.BytesIn, &f.BytesOut, &at, &targets, &f.Health, &f.ProbeProtocol); err != nil {
			return nil, err
		}
		f.Up, f.UpdatedAt = up == 1, unix(at)
		if targets != "" {
			_ = json.Unmarshal([]byte(targets), &f.Targets)
		}
		out[f.Tag] = f
	}
	return out, rows.Err()
}
