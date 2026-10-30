package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
)

// Bind only the health structures actually sent after rollout overlay and
// service-mode filtering. Draft route intent cannot authorize a sample.
func bindPathHealthDeliveryTx(ctx context.Context, tx *sql.Tx, delivery string, forwards []spec.Forward) error {
	for _, f := range forwards {
		if f.PathHealth == nil {
			continue
		}
		if err := f.ValidatePathHealth(); err != nil {
			return err
		}
		raw, err := json.Marshal(projectLinePathHealth(f.PathHealth))
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO path_health_deliveries(delivery_id,tag,health_json) VALUES(?,?,?) ON CONFLICT(delivery_id,tag) DO NOTHING`, delivery, f.Tag, string(raw)); err != nil {
			return err
		}
	}
	return nil
}

func pathHealthDeliveryTx(ctx context.Context, tx *sql.Tx, delivery string) (map[string]*spec.PathHealth, error) {
	rows, err := tx.QueryContext(ctx, `SELECT tag,health_json FROM path_health_deliveries WHERE delivery_id=?`, delivery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*spec.PathHealth{}
	for rows.Next() {
		var tag, raw string
		if err = rows.Scan(&tag, &raw); err != nil {
			return nil, err
		}
		var p LineExpectedPathHealth
		if err = json.Unmarshal([]byte(raw), &p); err != nil {
			return nil, err
		}
		out[tag] = p.Wire()
	}
	return out, rows.Err()
}

func recordPathHealthReportTx(ctx context.Context, tx *sql.Tx, nodeID int64, identity string, rep agentproto.Report, at time.Time) error {
	r, report := rep.ConfigReceipt, rep.PathHealth
	if r == nil || report == nil || r.Sequence == 0 || r.Sequence > math.MaxInt64 || len(report.Samples) > 1024 {
		return nil
	}
	// recordNodeConfigReceiptTx ran first. Exact accepted receipt identity is
	// necessary, but its fresh clock never substitutes for an old probe clock.
	var sequence, observed, received int64
	err := tx.QueryRowContext(ctx, `SELECT s.receipt_sequence,s.observed_at,s.last_receipt_at FROM node_config_heads h JOIN node_config_sessions s ON s.node_id=h.node_id AND s.token_identity=h.token_identity AND s.session=h.session JOIN node_config_deliveries d ON d.id=h.delivery_id WHERE h.node_id=? AND h.token_identity=? AND h.session=? AND h.delivery_id=? AND s.retired_at IS NULL AND d.config_revision=? AND d.state_revision=?`, nodeID, identity, r.Session, r.DeliveryID, r.ConfigRevision, r.StateRevision).Scan(&sequence, &observed, &received)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if sequence != int64(r.Sequence) || observed != r.ObservedAt.UnixMilli() || received != at.UnixMilli() || r.ObservedAt.Before(at.Add(-3*time.Minute)) || r.ObservedAt.After(at.Add(30*time.Second)) {
		return nil
	}
	var previousSession, previousIdentity, epoch string
	var previousSequence int64
	err = tx.QueryRowContext(ctx, `SELECT token_identity,session,sequence,epoch FROM path_health_capabilities WHERE node_id=?`, nodeID).Scan(&previousIdentity, &previousSession, &previousSequence, &epoch)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if previousIdentity == identity && previousSession == r.Session {
		if int64(r.Sequence) <= previousSequence {
			return nil
		}
	} else {
		epoch = ""
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO path_health_capabilities(node_id,token_identity,session,sequence,observed_at,received_at,supported,epoch) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(node_id) DO UPDATE SET token_identity=excluded.token_identity,session=excluded.session,sequence=excluded.sequence,observed_at=excluded.observed_at,received_at=excluded.received_at,supported=excluded.supported,epoch=excluded.epoch`, nodeID, identity, r.Session, int64(r.Sequence), observed, received, report.Supported, epoch); err != nil {
		return err
	}
	if !report.Supported {
		return nil
	}
	expected, err := pathHealthDeliveryTx(ctx, tx, r.DeliveryID)
	if err != nil {
		return err
	}
	for _, sample := range report.Samples {
		p := expected[sample.Tag]
		if p == nil || sample.RouteID != p.RouteID || sample.ScopeID != p.ScopeID || sample.ConfigDigest != p.Digest() || sample.Sequence == 0 || sample.Sequence > math.MaxInt64 || len(sample.Epoch) > 64 || sample.Epoch == "" || !spec.Plain(sample.Epoch) || sample.ObservedAt.IsZero() || sample.ObservedAt.Before(at.Add(-p.Freshness())) || sample.ObservedAt.After(at.Add(30*time.Second)) || sample.RTTMillis < 0 || sample.RTTMillis > int64(p.TimeoutSeconds)*1000+1000 || len(sample.Error) > 64 {
			continue
		}
		match := false
		for _, c := range p.Candidates {
			if c.ID == sample.CandidateID && c.Digest == sample.CandidateDigest {
				match = true
				break
			}
		}
		if !match {
			continue
		}
		if sample.Health != "up" && sample.Health != "down" && sample.Health != "unknown" && sample.Health != "unsupported" {
			continue
		}
		if sample.Result != "success" && sample.Result != "failure" && sample.Result != "unsupported" {
			continue
		}
		if p.Protocol == spec.PathHealthUnsupported && (sample.Health != "unsupported" || sample.Result != "unsupported") {
			continue
		}
		if epoch != "" && epoch != sample.Epoch {
			continue
		}
		var oldSession, oldIdentity, oldEpoch string
		var oldSeq, oldObserved int64
		err = tx.QueryRowContext(ctx, `SELECT token_identity,session,epoch,sequence,observed_at FROM path_health_samples WHERE node_id=? AND tag=? AND candidate_id=?`, nodeID, sample.Tag, sample.CandidateID).Scan(&oldIdentity, &oldSession, &oldEpoch, &oldSeq, &oldObserved)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if oldIdentity == identity && oldSession == r.Session && (oldEpoch != sample.Epoch || int64(sample.Sequence) <= oldSeq || sample.ObservedAt.UnixMilli() < oldObserved) {
			continue
		}
		if epoch == "" {
			epoch = sample.Epoch
			if _, err = tx.ExecContext(ctx, `UPDATE path_health_capabilities SET epoch=? WHERE node_id=?`, epoch, nodeID); err != nil {
				return err
			}
		}
		raw, err := json.Marshal(sample)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO path_health_samples(node_id,tag,candidate_id,token_identity,session,epoch,sequence,observed_at,received_at,sample_json) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(node_id,tag,candidate_id) DO UPDATE SET token_identity=excluded.token_identity,session=excluded.session,epoch=excluded.epoch,sequence=excluded.sequence,observed_at=excluded.observed_at,received_at=excluded.received_at,sample_json=excluded.sample_json`, nodeID, sample.Tag, sample.CandidateID, identity, r.Session, sample.Epoch, int64(sample.Sequence), sample.ObservedAt.UnixMilli(), at.UnixMilli(), string(raw)); err != nil {
			return err
		}
	}
	// Keep at most the currently delivered entry rules, including both old and
	// new tags during staged publication. Deletion/disable clears old samples.
	_, err = tx.ExecContext(ctx, `DELETE FROM path_health_samples WHERE node_id=? AND tag NOT IN (SELECT tag FROM path_health_deliveries WHERE delivery_id=?)`, nodeID, r.DeliveryID)
	return err
}
