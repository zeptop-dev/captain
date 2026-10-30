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

// Configuration application and full-path eligibility are separate facts.
// A new automatic entry cannot become the published endpoint while every
// candidate is still unknown. Metadata-only plans create no new listener.
func pathRolloutHealthGateTx(ctx context.Context, tx *sql.Tx, p *LineRollout, at time.Time) (waiting bool, deadline time.Time, err error) {
	if p.Status != "running" || p.Stage != "expand" || p.AfterRoute == nil || p.AfterRoute.Health == nil || !p.AfterRoute.Health.Enabled || !p.AfterRoute.Health.AutoFailover || len(p.Targets) == 0 {
		return false, deadline, nil
	}
	current := currentRolloutTargets(p)
	for _, t := range p.Targets {
		if t.Phase != "expand" {
			continue
		}
		if t.Status != "applied" {
			return false, deadline, nil
		}
		if current[t.NodeID].ID == t.ID && (!t.Fresh || !t.ReceiptApplied) {
			return false, deadline, nil
		}
	}
	deadline = p.UpdatedAt.Add(time.Duration(p.timeoutMS) * time.Millisecond)
	earliest := p.UpdatedAt
	if t, ok := current[p.AfterRoute.Entry.NodeID]; ok {
		if t.DeadlineAt != nil {
			deadline = *t.DeadlineAt
		}
		if t.IssuedAt != nil {
			earliest = *t.IssuedAt
		}
	}
	var tag string
	var health *spec.PathHealth
	for _, n := range p.Nodes {
		if n.NodeID == p.AfterRoute.Entry.NodeID {
			for _, f := range n.After {
				if f.PathHealth != nil && f.PathHealth.AutoFailover {
					tag, health = f.Tag, f.PathHealth.Wire()
				}
			}
		}
	}
	if health == nil {
		return true, deadline, nil
	}
	capable, err := pathHealthCapabilityTx(ctx, tx, p.AfterRoute.Entry.NodeID, at)
	if err != nil || !capable {
		return true, deadline, err
	}
	// Desired and observed pauses prevent a stale pre-pause sample publishing.
	for _, d := range p.dependencies {
		paused, e := pathHealthNodePausedTx(ctx, tx, d.NodeID, at)
		if e != nil {
			return true, deadline, e
		}
		if paused {
			return true, deadline, nil
		}
	}
	var identity, session, delivery string
	err = tx.QueryRowContext(ctx, `SELECT h.token_identity,h.session,h.delivery_id FROM node_config_heads h JOIN nodes n ON n.id=h.node_id AND n.token_hash=h.token_identity JOIN node_config_sessions s ON s.node_id=h.node_id AND s.token_identity=h.token_identity AND s.session=h.session WHERE h.node_id=? AND s.retired_at IS NULL`, p.AfterRoute.Entry.NodeID).Scan(&identity, &session, &delivery)
	if errors.Is(err, sql.ErrNoRows) {
		return true, deadline, nil
	}
	if err != nil {
		return true, deadline, err
	}
	delivered, err := pathHealthDeliveryTx(ctx, tx, delivery)
	if err != nil {
		return true, deadline, err
	}
	if delivered[tag] == nil || delivered[tag].Digest() != health.Digest() {
		return true, deadline, nil
	}
	for _, candidate := range health.Candidates {
		var raw, storedIdentity, storedSession string
		var received int64
		err = tx.QueryRowContext(ctx, `SELECT sample_json,token_identity,session,received_at FROM path_health_samples WHERE node_id=? AND tag=? AND candidate_id=?`, p.AfterRoute.Entry.NodeID, tag, candidate.ID).Scan(&raw, &storedIdentity, &storedSession, &received)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return true, deadline, err
		}
		var sample agentproto.PathHealthSample
		if storedIdentity != identity || storedSession != session || json.Unmarshal([]byte(raw), &sample) != nil || sample.Tag != tag || sample.ConfigDigest != health.Digest() || sample.CandidateDigest != candidate.Digest || sample.RouteID != health.RouteID || sample.ScopeID != health.ScopeID || sample.Health != "up" {
			continue
		}
		recv := time.UnixMilli(received)
		if recv.Before(earliest) || sample.ObservedAt.Before(earliest) || recv.After(deadline) || !sample.ObservedAt.Add(health.Freshness()).After(at) || !recv.Add(health.Freshness()).After(at) || sample.ObservedAt.After(at.Add(30*time.Second)) {
			continue
		}
		return false, deadline, nil
	}
	return true, deadline, nil
}
