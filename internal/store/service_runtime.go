package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
)

// NodeRuntime is a coherent, accepted full-report snapshot. Unlike LastSeenAt,
// its clock is never refreshed by resource-only heartbeats or receipt retries.
// Identity is for private cache invalidation only; never serialize this object
// into a customer response.
type NodeRuntime struct {
	ServiceControl  *agentproto.ServiceControlReport `json:"service_control,omitempty"`
	Identity        string                           `json:"-"`
	ObservedAt      time.Time                        `json:"-"`
	Sequence        int64                            `json:"-"`
	Epoch           string                           `json:"epoch"`
	Version         string                           `json:"version"`
	Revision        string                           `json:"revision"`
	AppliedRevision string                           `json:"applied_revision"`
	Cores           map[string]agentproto.CoreStatus `json:"cores"`
	Forwards        []agentproto.ForwardStatus       `json:"forwards"`
	WireGuard       *agentproto.WireGuardReport      `json:"wireguard"`
}

func recordNodeRuntime(ctx context.Context, tx *sql.Tx, nodeID int64, rep agentproto.Report, fresh bool, at time.Time) error {
	if !fresh {
		return nil
	}
	runtime := NodeRuntime{ServiceControl: rep.ServiceControl, Epoch: rep.TrafficEpoch, Version: rep.Version, Revision: rep.Revision, AppliedRevision: rep.AppliedRevision, Cores: rep.Cores, Forwards: rep.Forwards, WireGuard: rep.WireGuard}
	raw, err := json.Marshal(runtime)
	if err != nil {
		return err
	}
	// Unnumbered old reports can supply positive observations, but cannot
	// establish two independent failures. A later numbered report starts at 1.
	increment := 0
	if rep.TrafficSeq > 0 {
		increment = 1
	}
	_, err = tx.ExecContext(ctx, `UPDATE nodes SET runtime_reported_at=?,runtime_report_seq=CASE WHEN ?=0 THEN 0 ELSE runtime_report_seq+1 END,runtime_report_json=? WHERE id=?`, at.Unix(), increment, string(raw), nodeID)
	return err
}

func (s *Store) NodeRuntime(ctx context.Context, nodeID int64) (*NodeRuntime, error) {
	var raw string
	var token sql.NullString
	var at sql.NullInt64
	var seq int64
	if err := s.db.QueryRowContext(ctx, `SELECT token_hash,runtime_reported_at,runtime_report_seq,runtime_report_json FROM nodes WHERE id=?`, nodeID).Scan(&token, &at, &seq, &raw); err != nil {
		return nil, wrapNotFound(err)
	}
	if !token.Valid || token.String == "" || !at.Valid || at.Int64 <= 0 {
		return nil, nil
	}
	var out *NodeRuntime
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	if out != nil {
		out.Identity, out.ObservedAt, out.Sequence = token.String, unix(at.Int64), seq
	}
	return out, nil
}
