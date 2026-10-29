package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
)

type lineRolloutBinding struct {
	plan   *LineRollout
	target LineRolloutTarget
}

func matchingLineRolloutStepsTx(ctx context.Context, tx *sql.Tx, nodeID int64, token string, forwards []spec.Forward) ([]lineRolloutBinding, error) {
	rows, err := tx.QueryContext(ctx, `SELECT p.id FROM line_rollouts p JOIN line_rollout_nodes n ON n.plan_id=p.id JOIN probe_public_ids i ON i.node_id=n.node_id WHERE n.node_id=? AND n.token_identity=? AND n.node_identity=i.uuid AND p.status IN ('running','paused')`, nodeID, token)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := []lineRolloutBinding{}
	for _, id := range ids {
		p, e := readLineRolloutTx(ctx, tx, id, time.Now())
		if e != nil {
			return nil, e
		}
		var current *LineRolloutTarget
		for i := range p.Targets {
			t := &p.Targets[i]
			if t.NodeID == nodeID && t.Status != "pending" {
				current = t
			}
		}
		if current == nil {
			continue
		}
		delivered := []spec.Forward{}
		prefix := fmt.Sprintf("line-%d-", p.LineID)
		for _, f := range forwards {
			if strings.HasPrefix(f.Tag, prefix) {
				delivered = append(delivered, f)
			}
		}
		if lineChangeDigest(lineChangeProjection(delivered)) != lineChangeDigest(current.Expected) {
			continue
		}
		out = append(out, lineRolloutBinding{p, *current})
	}
	return out, nil
}
func lineRolloutsNeedDeliveryTx(ctx context.Context, tx *sql.Tx, bindings []lineRolloutBinding, delivery string) (bool, error) {
	for _, b := range bindings {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM line_rollout_deliveries WHERE delivery_id=? AND step_id=? AND attempt_id=?)`, delivery, b.target.ID, b.target.attemptID).Scan(&exists); err != nil {
			return false, err
		}
		if !exists {
			return true, nil
		}
	}
	return false, nil
}
func mapLineRolloutDeliveryTx(ctx context.Context, tx *sql.Tx, bindings []lineRolloutBinding, d *agentproto.ConfigDelivery) error {
	for _, b := range bindings {
		if _, err := tx.ExecContext(ctx, `INSERT INTO line_rollout_deliveries(delivery_id,step_id,attempt_id) VALUES(?,?,?) ON CONFLICT DO NOTHING`, d.ID, b.target.ID, b.target.attemptID); err != nil {
			return err
		}
	}
	return nil
}
func bindLineRolloutDeliveryTx(ctx context.Context, tx *sql.Tx, bindings []lineRolloutBinding, d *agentproto.ConfigDelivery) error {
	var active bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM node_config_heads WHERE delivery_id=? AND session=?)`, d.ID, d.Session).Scan(&active); err != nil {
		return err
	}
	if !active {
		return nil
	}
	var cfg, state string
	var issued int64
	if err := tx.QueryRowContext(ctx, `SELECT config_revision,state_revision,issued_at FROM node_config_deliveries WHERE id=?`, d.ID).Scan(&cfg, &state, &issued); err != nil {
		return err
	}
	for _, b := range bindings {
		t := b.target
		if t.DeliveryID == d.ID {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE line_rollout_steps SET status='released',delivery_id=?,session=?,config_revision=?,state_revision=?,issued_at=?,observed_at=NULL,last_receipt_at=NULL,receipt_sequence=0,receipt_applied=0 WHERE id=? AND attempt_id=?`, d.ID, d.Session, cfg, state, issued, t.ID, t.attemptID); err != nil {
			return err
		}
	}
	return nil
}
func recordLineRolloutReceiptTx(ctx context.Context, tx *sql.Tx, nodeID int64, token string, r *agentproto.ConfigReceipt, applied bool, at time.Time) error {
	rows, err := tx.QueryContext(ctx, `SELECT p.id,s.id FROM line_rollout_deliveries m JOIN line_rollout_steps s ON s.id=m.step_id AND s.attempt_id=m.attempt_id JOIN line_rollouts p ON p.id=s.plan_id JOIN line_rollout_nodes n ON n.plan_id=p.id AND n.node_id=s.node_id JOIN probe_public_ids i ON i.node_id=n.node_id WHERE m.delivery_id=? AND s.node_id=? AND n.token_identity=? AND n.node_identity=i.uuid AND p.status IN ('running','paused')`, r.DeliveryID, nodeID, token)
	if err != nil {
		return err
	}
	type key struct{ plan, step string }
	keys := []key{}
	for rows.Next() {
		var k key
		if err = rows.Scan(&k.plan, &k.step); err != nil {
			break
		}
		keys = append(keys, k)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, k := range keys {
		p, e := readLineRolloutTx(ctx, tx, k.plan, at)
		if e != nil {
			return e
		}
		var t *LineRolloutTarget
		for i := range p.Targets {
			v := &p.Targets[i]
			if v.NodeID == nodeID && v.Status != "pending" {
				t = v
			}
		}
		if t == nil || t.ID != k.step {
			continue
		}
		if e = bindLineRolloutDeliveryTx(ctx, tx, []lineRolloutBinding{{p, *t}}, &agentproto.ConfigDelivery{ID: r.DeliveryID, Session: r.Session}); e != nil {
			return e
		}
		status := "released"
		if applied {
			status = "applied"
		}
		if _, e = tx.ExecContext(ctx, `UPDATE line_rollout_steps SET status=?,confirmed_at=CASE WHEN ? THEN COALESCE(confirmed_at,?) ELSE confirmed_at END,observed_at=?,last_receipt_at=?,receipt_sequence=?,receipt_applied=? WHERE id=? AND attempt_id=?`, status, applied, at.UnixMilli(), r.ObservedAt.UnixMilli(), at.UnixMilli(), int64(r.Sequence), applied, t.ID, t.attemptID); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO line_rollout_attempts(step_id,attempt,delivery_id,config_revision,state_revision,observed_at,received_at,applied) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(step_id,attempt) DO UPDATE SET delivery_id=excluded.delivery_id,config_revision=excluded.config_revision,state_revision=excluded.state_revision,observed_at=excluded.observed_at,received_at=excluded.received_at,applied=excluded.applied`, t.ID, t.Attempt, r.DeliveryID, r.ConfigRevision, r.StateRevision, r.ObservedAt.UnixMilli(), at.UnixMilli(), applied); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `DELETE FROM line_rollout_attempts WHERE step_id=? AND attempt NOT IN(SELECT attempt FROM line_rollout_attempts WHERE step_id=? ORDER BY attempt DESC LIMIT 20)`, t.ID, t.ID); e != nil {
			return e
		}
		if !applied {
			if e = pauseLineRolloutTx(ctx, tx, p, "apply_not_confirmed", at); e != nil {
				return e
			}
		}
	}
	return nil
}

func observeLineRolloutDeliveryTx(ctx context.Context, tx *sql.Tx, nodeID int64, token string, forwards []spec.Forward, d *agentproto.ConfigDelivery) error {
	var identity string
	if err := tx.QueryRowContext(ctx, `SELECT uuid FROM probe_public_ids WHERE node_id=?`, nodeID).Scan(&identity); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT p.id,p.line_id FROM line_rollouts p JOIN line_rollout_nodes n ON n.plan_id=p.id WHERE n.node_id=? AND p.status IN ('running','paused')`, nodeID)
	if err != nil {
		return err
	}
	type scope struct {
		id   string
		line int64
	}
	plans := []scope{}
	for rows.Next() {
		var p scope
		if err = rows.Scan(&p.id, &p.line); err != nil {
			break
		}
		plans = append(plans, p)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, p := range plans {
		fs := []spec.Forward{}
		prefix := fmt.Sprintf("line-%d-", p.line)
		for _, f := range forwards {
			if strings.HasPrefix(f.Tag, prefix) {
				fs = append(fs, f)
			}
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO line_rollout_observations(delivery_id,plan_id,node_id,node_identity,token_identity,forwards_json) VALUES(?,?,?,?,?,?) ON CONFLICT DO NOTHING`, d.ID, p.id, nodeID, identity, token, rolloutRaw(lineChangeProjection(fs))); err != nil {
			return err
		}
	}
	return nil
}
func currentLineRolloutObservationTx(ctx context.Context, tx *sql.Tx, planID string, nodeID int64, identity, token string, at time.Time) ([]LineChangeForward, error) {
	var raw string
	var received, observed sql.NullInt64
	var applied bool
	err := tx.QueryRowContext(ctx, `SELECT o.forwards_json,d.last_receipt_at,d.observed_at,d.applied FROM line_rollout_observations o JOIN node_config_heads h ON h.delivery_id=o.delivery_id AND h.node_id=o.node_id AND h.token_identity=o.token_identity JOIN node_config_deliveries d ON d.id=h.delivery_id WHERE o.plan_id=? AND o.node_id=? AND o.node_identity=? AND o.token_identity=?`, planID, nodeID, identity, token).Scan(&raw, &received, &observed, &applied)
	if err != nil {
		return nil, fmt.Errorf("%w: current identity has no confirmed structural observation", ErrLineRolloutUnsafe)
	}
	fresh := NodeConfigDeliveryRecord{Current: true, ObservedAt: lineMillisPtr(observed), LastReceiptAt: lineMillisPtr(received)}
	setNodeConfigFreshness(&fresh, at)
	if !fresh.Fresh || !applied {
		return nil, fmt.Errorf("%w: current identity needs a fresh successful receipt", ErrLineRolloutUnsafe)
	}
	var fs []LineChangeForward
	if err = json.Unmarshal([]byte(raw), &fs); err != nil {
		return nil, err
	}
	return fs, nil
}
