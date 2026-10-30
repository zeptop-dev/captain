package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
)

type PathHealthView struct {
	RouteID      int64                     `json:"route_id"`
	Version      int64                     `json:"version"`
	Enabled      bool                      `json:"enabled"`
	AutoFailover bool                      `json:"auto_failover"`
	EntryNodeID  int64                     `json:"entry_node_id"`
	Capable      bool                      `json:"capable"`
	Protocol     string                    `json:"protocol"`
	ServerName   string                    `json:"server_name"`
	Status       string                    `json:"status"`
	Reason       string                    `json:"reason,omitempty"`
	Candidates   []PathHealthCandidateView `json:"candidates"`
}
type PathHealthCandidateView struct {
	ID              string     `json:"id"`
	Tag             string     `json:"tag"`
	Target          string     `json:"target"`
	ConfigDigest    string     `json:"config_digest"`
	CandidateDigest string     `json:"candidate_digest"`
	Status          string     `json:"status"`
	Result          string     `json:"result"`
	Eligible        bool       `json:"eligible"`
	ObservedAt      *time.Time `json:"observed_at,omitempty"`
	ReceivedAt      *time.Time `json:"received_at,omitempty"`
	FreshForMS      int64      `json:"fresh_for_ms"`
	Epoch           string     `json:"epoch"`
	Sequence        uint64     `json:"sequence"`
	RTTMillis       int64      `json:"rtt_ms"`
	Error           string     `json:"error,omitempty"`
}

func (s *Store) PathHealth(ctx context.Context, id int64, at time.Time) (*PathHealthView, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	route, err := pathRouteTx(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	out := &PathHealthView{RouteID: id, Version: route.Version, EntryNodeID: route.Entry.NodeID, Status: "disabled", Candidates: []PathHealthCandidateView{}}
	if route.Health == nil || !route.Health.Enabled {
		return out, nil
	}
	out.Enabled, out.AutoFailover, out.Status = true, route.Health.AutoFailover, "unknown"
	out.Capable, err = pathHealthCapabilityTx(ctx, tx, route.Entry.NodeID, at)
	if err != nil {
		return nil, err
	}
	// Compile only the published route. A staged second entry may have valid
	// samples already, but its tag cannot attest this route's current listener.
	l, err := pathRouteLineTx(ctx, tx, route)
	if err != nil {
		out.Reason = "configuration_unavailable"
		return out, nil
	}
	generated, _, err := compilePathRouteTx(ctx, tx, l, route)
	if err != nil {
		out.Reason = "configuration_unavailable"
		return out, nil
	}
	var entry *NodeForward
	for i := range generated[route.Entry.NodeID] {
		f := &generated[route.Entry.NodeID][i]
		if f.PathHealth != nil {
			entry = f
			break
		}
	}
	if entry == nil {
		out.Reason = "configuration_unavailable"
		return out, nil
	}
	p := entry.PathHealth
	out.Protocol, out.ServerName = p.Protocol, p.ServerName
	paused := false
	nodes := map[int64]bool{}
	for node := range generated {
		nodes[node] = true
	}
	var landing int64
	if err = tx.QueryRowContext(ctx, `SELECT node_id FROM inbounds WHERE id=?`, route.TargetInboundID).Scan(&landing); err != nil {
		return nil, err
	}
	nodes[landing] = true
	for node := range nodes {
		control, e := serviceControlTx(ctx, tx, node)
		if e != nil {
			return nil, e
		}
		if control.effective().Mode == spec.ServiceControlPause {
			paused = true
		}
		var raw string
		var reported sql.NullInt64
		if e = tx.QueryRowContext(ctx, `SELECT runtime_report_json,runtime_reported_at FROM nodes WHERE id=?`, node).Scan(&raw, &reported); e != nil {
			return nil, e
		}
		var runtime NodeRuntime
		if reported.Valid && !time.Unix(reported.Int64, 0).Before(at.Add(-3*time.Minute)) && !time.Unix(reported.Int64, 0).After(at.Add(30*time.Second)) && json.Unmarshal([]byte(raw), &runtime) == nil && runtime.ServiceControl != nil && runtime.ServiceControl.Mode == spec.ServiceControlPause {
			paused = true
		}
	}
	var capabilityObserved, capabilityReceived int64
	if out.Capable {
		if err = tx.QueryRowContext(ctx, `SELECT observed_at,received_at FROM path_health_capabilities WHERE node_id=?`, route.Entry.NodeID).Scan(&capabilityObserved, &capabilityReceived); err != nil {
			return nil, err
		}
	}
	var identity, session string
	err = tx.QueryRowContext(ctx, `SELECT h.token_identity,h.session FROM node_config_heads h JOIN nodes n ON n.id=h.node_id AND n.token_hash=h.token_identity JOIN node_config_sessions s ON s.node_id=h.node_id AND s.token_identity=h.token_identity AND s.session=h.session WHERE h.node_id=? AND s.retired_at IS NULL`, route.Entry.NodeID).Scan(&identity, &session)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	for _, c := range p.Candidates {
		view := PathHealthCandidateView{ID: c.ID, Tag: entry.Tag, Target: c.Target, ConfigDigest: p.Digest(), CandidateDigest: c.Digest, Status: "unknown"}
		if p.Protocol == spec.PathHealthUnsupported {
			view.Status = "unsupported"
			view.Result = "unsupported"
		} else if identity != "" {
			var raw, storedIdentity, storedSession string
			var received int64
			e := tx.QueryRowContext(ctx, `SELECT sample_json,token_identity,session,received_at FROM path_health_samples WHERE node_id=? AND tag=? AND candidate_id=?`, route.Entry.NodeID, entry.Tag, c.ID).Scan(&raw, &storedIdentity, &storedSession, &received)
			if e != nil && !errors.Is(e, sql.ErrNoRows) {
				return nil, e
			}
			var sample agentproto.PathHealthSample
			if e == nil && storedIdentity == identity && storedSession == session && json.Unmarshal([]byte(raw), &sample) == nil && sample.ConfigDigest == p.Digest() && sample.CandidateDigest == c.Digest && sample.ScopeID == p.ScopeID && sample.RouteID == p.RouteID {
				view.ObservedAt = &sample.ObservedAt
				recv := time.UnixMilli(received).UTC()
				view.ReceivedAt = &recv
				view.Epoch, view.Sequence, view.RTTMillis, view.Result, view.Error = sample.Epoch, sample.Sequence, sample.RTTMillis, sample.Result, sample.Error
				deadline := sample.ObservedAt.Add(p.Freshness())
				for _, capTime := range []int64{capabilityObserved, capabilityReceived} {
					if capDeadline := time.UnixMilli(capTime).Add(3 * time.Minute); capDeadline.Before(deadline) {
						deadline = capDeadline
					}
				}
				if receiveDeadline := time.UnixMilli(received).Add(p.Freshness()); receiveDeadline.Before(deadline) {
					deadline = receiveDeadline
				}
				if out.Capable && !paused && deadline.After(at) && !sample.ObservedAt.After(at.Add(30*time.Second)) {
					view.Status, view.Result, view.Eligible = sample.Health, sample.Result, sample.Health == "up"
					if view.Status == "up" && view.Result != "success" {
						view.Status = "degraded"
					}
					view.FreshForMS = deadline.Sub(at).Milliseconds()
				}
			}
		}
		out.Candidates = append(out.Candidates, view)
	}
	if paused {
		out.Status, out.Reason = "paused", "service_paused"
		return out, nil
	}
	if p.Protocol == spec.PathHealthUnsupported {
		out.Status, out.Reason = "unsupported", "landing_handshake_unsupported"
		return out, nil
	}
	if !out.Capable {
		out.Reason = "capability_unavailable"
		return out, nil
	}
	up, down, degraded := 0, 0, 0
	for _, c := range out.Candidates {
		if c.Status == "up" {
			up++
		}
		if c.Status == "degraded" {
			degraded++
		}
		if c.Status == "down" {
			down++
		}
	}
	switch {
	case up == len(out.Candidates):
		out.Status = "healthy"
	case down == len(out.Candidates):
		out.Status = "down"
	case up > 0 || degraded > 0:
		out.Status = "degraded"
	default:
		out.Reason = "fresh_evidence_unavailable"
	}
	return out, nil
}
