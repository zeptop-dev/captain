package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

const lineRolloutCols = `id,ordinal,line_id,scope_id,version,snapshot_version,kind,status,stage,actor,before_json,after_json,dependencies_json,base_version,timeout_ms,error_code,compensates,created_at,updated_at,published_at`

func readLineRolloutTx(ctx context.Context, tx *sql.Tx, id string, at time.Time) (*LineRollout, error) {
	p := &LineRollout{Nodes: []LineRolloutNode{}, Targets: []LineRolloutTarget{}}
	var before, after, deps string
	var created, updated int64
	var published sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT `+lineRolloutCols+` FROM line_rollouts WHERE id=?`, id).Scan(&p.ID, &p.Cursor, &p.LineID, &p.ScopeID, &p.Version, &p.SnapshotVersion, &p.Kind, &p.Status, &p.Stage, &p.Actor, &before, &after, &deps, &p.baseVersion, &p.timeoutMS, &p.ErrorCode, &p.Compensates, &created, &updated, &published)
	if err != nil {
		return nil, wrapNotFound(err)
	}
	for _, v := range []struct {
		raw string
		dst any
	}{{before, &p.Before}, {after, &p.After}, {deps, &p.dependencies}} {
		if err = json.Unmarshal([]byte(v.raw), v.dst); err != nil {
			return nil, err
		}
	}
	p.CreatedAt = time.UnixMilli(created)
	p.UpdatedAt = time.UnixMilli(updated)
	p.PublishedAt = lineMillisPtr(published)
	rows, err := tx.QueryContext(ctx, `SELECT node_id,node_name,node_identity,token_identity,before_json,after_json,before_depth,after_depth FROM line_rollout_nodes WHERE plan_id=? ORDER BY node_id`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var n LineRolloutNode
		var b, a string
		if err = rows.Scan(&n.NodeID, &n.NodeName, &n.identity, &n.token, &b, &a, &n.beforeDepth, &n.afterDepth); err != nil {
			break
		}
		if err = json.Unmarshal([]byte(b), &n.Before); err != nil {
			break
		}
		if err = json.Unmarshal([]byte(a), &n.After); err != nil {
			break
		}
		p.Nodes = append(p.Nodes, n)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT id,ordinal,node_id,phase,expected_json,status,attempt,attempt_id,deadline_at,delivery_id,session,config_revision,state_revision,issued_at,confirmed_at,observed_at,last_receipt_at,receipt_sequence,receipt_applied FROM line_rollout_steps WHERE plan_id=? ORDER BY ordinal`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var t LineRolloutTarget
		var expected string
		var deadline, issued, confirmed, observed, received sql.NullInt64
		if err = rows.Scan(&t.ID, &t.Order, &t.NodeID, &t.Phase, &expected, &t.Status, &t.Attempt, &t.attemptID, &deadline, &t.DeliveryID, &t.session, &t.ConfigRevision, &t.StateRevision, &issued, &confirmed, &observed, &received, &t.sequence, &t.ReceiptApplied); err != nil {
			break
		}
		if err = json.Unmarshal([]byte(expected), &t.Expected); err != nil {
			break
		}
		t.DeadlineAt = lineMillisPtr(deadline)
		t.IssuedAt = lineMillisPtr(issued)
		t.ConfirmedAt = lineMillisPtr(confirmed)
		t.ObservedAt = lineMillisPtr(observed)
		t.LastReceiptAt = lineMillisPtr(received)
		for _, n := range p.Nodes {
			if n.NodeID == t.NodeID {
				t.NodeName = n.NodeName
			}
		}
		p.Targets = append(p.Targets, t)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range p.Targets {
		t := &p.Targets[i]
		var current bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM node_config_heads h JOIN nodes n ON n.id=h.node_id JOIN probe_public_ids i ON i.node_id=n.id JOIN line_rollout_nodes rn ON rn.node_id=n.id AND rn.plan_id=? WHERE h.delivery_id=? AND h.node_id=? AND h.token_identity=n.token_hash AND rn.token_identity=n.token_hash AND rn.node_identity=i.uuid)`, id, t.DeliveryID, t.NodeID).Scan(&current); err != nil {
			return nil, err
		}
		e := NodeConfigDeliveryRecord{Current: current, LastReceiptAt: t.LastReceiptAt, ObservedAt: t.ObservedAt}
		setNodeConfigFreshness(&e, at)
		t.Fresh = e.Fresh
		t.FreshForMS = e.FreshForMS
	}
	return p, nil
}
func (s *Store) LineRolloutByID(ctx context.Context, id string) (*LineRollout, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	p, err := readLineRolloutTx(ctx, tx, id, time.Now())
	if err != nil {
		return nil, err
	}
	return p, tx.Commit()
}
func (s *Store) ListLineRollouts(ctx context.Context, lineID, before int64, limit int) (*LineRolloutPage, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id FROM line_rollouts WHERE (?=0 OR line_id=?) AND (?=0 OR ordinal<?) ORDER BY ordinal DESC LIMIT ?`, lineID, lineID, before, before, limit+1)
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
	out := &LineRolloutPage{Items: []LineRollout{}}
	more := len(ids) > limit
	if more {
		ids = ids[:limit]
	}
	at := time.Now()
	for _, id := range ids {
		p, e := readLineRolloutTx(ctx, tx, id, at)
		if e != nil {
			return nil, e
		}
		out.Items = append(out.Items, *p)
	}
	if more {
		out.NextBefore = out.Items[len(out.Items)-1].Cursor
	}
	return out, tx.Commit()
}
func pauseLineRolloutTx(ctx context.Context, tx *sql.Tx, p *LineRollout, code string, at time.Time) error {
	if p.Status != "running" {
		return nil
	}
	_, err := tx.ExecContext(ctx, `UPDATE line_rollouts SET status='paused',error_code=?,version=version+1,updated_at=? WHERE id=? AND status='running'`, code, at.UnixMilli(), p.ID)
	p.Status = "paused"
	p.ErrorCode = code
	p.Version++
	p.UpdatedAt = at
	return err
}
func (s *Store) PauseLineRollout(ctx context.Context, id string, version int64) (*LineRollout, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	at := time.Now()
	p, err := readLineRolloutTx(ctx, tx, id, at)
	if err != nil {
		return nil, err
	}
	if p.Version != version {
		return nil, ErrLineRolloutConflict
	}
	if p.Status != "running" {
		return nil, ErrLineRolloutState
	}
	if err = pauseLineRolloutTx(ctx, tx, p, "operator_pause", at); err != nil {
		return nil, err
	}
	return p, tx.Commit()
}
func lineRolloutCurrent(p *LineRollout, nodeID int64) []LineChangeForward {
	out := []LineChangeForward{}
	for _, n := range p.Nodes {
		if n.NodeID == nodeID {
			out = n.Before
		}
	}
	for _, t := range p.Targets {
		if t.NodeID == nodeID && t.Status != "pending" {
			out = t.Expected
		}
	}
	return out
}
func lineRolloutValidationTx(ctx context.Context, tx *sql.Tx, p *LineRollout, at time.Time, requireFresh bool) error {
	var version int64
	var scope string
	if err := tx.QueryRowContext(ctx, `SELECT l.version,s.id FROM forward_lines l JOIN line_change_scopes s ON s.line_id=l.id WHERE l.id=?`, p.LineID).Scan(&version, &scope); err != nil {
		return fmt.Errorf("%w: line removed", ErrLineRolloutConflict)
	}
	if version != p.baseVersion || scope != p.ScopeID {
		return fmt.Errorf("%w: line changed", ErrLineRolloutConflict)
	}
	for _, d := range p.dependencies {
		var identity, token, mode string
		if err := tx.QueryRowContext(ctx, `SELECT i.uuid,COALESCE(n.token_hash,''),n.node_mode FROM nodes n JOIN probe_public_ids i ON i.node_id=n.id WHERE n.id=?`, d.NodeID).Scan(&identity, &token, &mode); err != nil {
			return fmt.Errorf("%w: server removed", ErrLineRolloutConflict)
		}
		if identity != d.Identity || token != d.Token || mode == "monitor" {
			return fmt.Errorf("%w: server identity or mode changed", ErrLineRolloutConflict)
		}
		modified := false
		for _, step := range p.Targets {
			if step.NodeID == d.NodeID {
				modified = true
			}
		}
		if requireFresh || !modified {
			if _, _, err := freshRolloutNodeTx(ctx, tx, d.NodeID, at); err != nil {
				return err
			}
		}
	}
	// Recompile intent against current topology; never replay stale endpoints.
	for _, v := range []struct {
		intent *ForwardLine
		after  bool
	}{{p.Before, false}, {p.After, true}} {
		if v.intent == nil {
			continue
		}
		generated, endpoints, err := compileLine(withLineRolloutWrite(ctx), tx, v.intent)
		if err != nil {
			return fmt.Errorf("%w: topology no longer compiles", ErrLineRolloutConflict)
		}
		if !sameRolloutEndpoints(v.intent.Endpoints, endpoints) {
			return fmt.Errorf("%w: published endpoint changed", ErrLineRolloutConflict)
		}
		for _, n := range p.Nodes {
			expected := n.Before
			if v.after {
				expected = n.After
			}
			fs := []spec.Forward{}
			for _, f := range generated[n.NodeID] {
				f.IngressID = ""
				fs = append(fs, f.Forward)
			}
			// Compensation's before snapshot can include both prior listeners. Only its
			// destination intent is recompiled; the union is owned, immutable cleanup.
			if p.Compensates != "" && !v.after {
				continue
			}
			if !reflect.DeepEqual(lineChangeProjection(fs), expected) {
				return fmt.Errorf("%w: endpoint changed", ErrLineRolloutConflict)
			}
		}
	}
	return nil
}
func (s *Store) RetryLineRollout(ctx context.Context, id string, version int64) (*LineRollout, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	at := time.Now()
	p, err := readLineRolloutTx(ctx, tx, id, at)
	if err != nil {
		return nil, err
	}
	if p.Version != version {
		return nil, ErrLineRolloutConflict
	}
	if p.Status != "paused" {
		return nil, ErrLineRolloutState
	}
	if err = lineRolloutValidationTx(ctx, tx, p, at, false); err != nil {
		return nil, err
	}
	// Previously released steps keep the same pointer, but every resumed attempt
	// needs its own new delivery. Neither old success nor late failure can win.
	for _, t := range p.Targets {
		if t.Phase != p.Stage || t.Status == "pending" {
			continue
		}
		if _, err = tx.ExecContext(ctx, `UPDATE line_rollout_steps SET attempt=attempt+1,attempt_id=?,status='released',deadline_at=?,delivery_id='',session='',config_revision='',state_revision='',issued_at=NULL,observed_at=NULL,last_receipt_at=NULL,receipt_sequence=0,receipt_applied=0 WHERE id=?`, randRolloutID(), at.UnixMilli()+p.timeoutMS, t.ID); err != nil {
			return nil, err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE line_rollouts SET status='running',error_code='',version=version+1,updated_at=? WHERE id=?`, at.UnixMilli(), id); err != nil {
		return nil, err
	}
	out, err := readLineRolloutTx(ctx, tx, id, at)
	if err != nil {
		return nil, err
	}
	return out, tx.Commit()
}
func (s *Store) AdvanceLineRollouts(ctx context.Context, at time.Time, limit int) ([]int64, error) {
	if limit <= 0 || limit > 20 {
		limit = 20
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id FROM line_rollouts WHERE status='running' ORDER BY checked_at,ordinal LIMIT ?`, limit)
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
	changed := map[int64]bool{}
	for _, id := range ids {
		if _, e := tx.ExecContext(ctx, `UPDATE line_rollouts SET checked_at=? WHERE id=?`, at.UnixMilli(), id); e != nil {
			return nil, e
		}
		p, e := readLineRolloutTx(ctx, tx, id, at)
		if e != nil {
			return nil, e
		}
		nodes, e := advanceLineRolloutTx(ctx, tx, p, at)
		if e != nil {
			return nil, e
		}
		for _, n := range nodes {
			changed[n] = true
		}
	}
	out := []int64{}
	for n := range changed {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, tx.Commit()
}
func advanceLineRolloutTx(ctx context.Context, tx *sql.Tx, p *LineRollout, at time.Time) ([]int64, error) {
	changed := []int64{}
	if p.Status != "running" {
		return changed, nil
	}
	if err := lineRolloutValidationTx(ctx, tx, p, at, false); err != nil {
		return changed, pauseLineRolloutTx(ctx, tx, p, "dependency_changed", at)
	}
	// A node that only expanded remains a live prerequisite during cleanup.
	// Inspect its latest released target: an older expansion receipt must not
	// override the evidence for a newer cleanup target on the same node.
	current := map[int64]LineRolloutTarget{}
	for _, t := range p.Targets {
		if t.Status != "pending" {
			current[t.NodeID] = t
		}
	}
	for _, t := range current {
		if t.Phase != p.Stage && (t.Status != "applied" || !t.Fresh || !t.ReceiptApplied) {
			return changed, pauseLineRolloutTx(ctx, tx, p, "confirmation_stale", at)
		}
	}
	for _, t := range p.Targets {
		if t.Phase != p.Stage {
			continue
		}
		switch t.Status {
		case "applied":
			if !t.Fresh || !t.ReceiptApplied {
				return changed, pauseLineRolloutTx(ctx, tx, p, "confirmation_stale", at)
			}
		case "released":
			if t.DeadlineAt != nil && !at.Before(*t.DeadlineAt) {
				return changed, pauseLineRolloutTx(ctx, tx, p, "deadline_exceeded", at)
			}
			return changed, nil
		case "pending":
			if _, err := tx.ExecContext(ctx, `UPDATE line_rollout_steps SET status='released',deadline_at=? WHERE id=? AND status='pending'`, at.UnixMilli()+p.timeoutMS, t.ID); err != nil {
				return nil, err
			}
			_, err := tx.ExecContext(ctx, `UPDATE line_rollouts SET version=version+1,updated_at=? WHERE id=?`, at.UnixMilli(), p.ID)
			return []int64{t.NodeID}, err
		}
	}
	if p.Stage == "expand" {
		if err := publishLineRolloutTx(ctx, tx, p, at); err != nil {
			return nil, err
		}
		p.Stage = "cleanup"
		p.PublishedAt = &at
		for _, n := range p.Nodes {
			changed = append(changed, n.NodeID)
		}
		// Read newly persisted metadata and release only the first cleanup step.
		fresh, err := readLineRolloutTx(ctx, tx, p.ID, at)
		if err != nil {
			return nil, err
		}
		next, err := advanceLineRolloutTx(ctx, tx, fresh, at)
		return append(changed, next...), err
	}
	if p.Kind == "delete" {
		if err := deleteLineRolloutTx(ctx, tx, p); err != nil {
			return nil, err
		}
		for _, n := range p.Nodes {
			changed = append(changed, n.NodeID)
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE line_rollouts SET status='completed',error_code='',version=version+1,updated_at=?,published_at=COALESCE(published_at,?) WHERE id=?`, at.UnixMilli(), at.UnixMilli(), p.ID); err != nil {
		return nil, err
	}
	// Keep active plans plus the latest 100 terminal structural histories.
	_, err := tx.ExecContext(ctx, `DELETE FROM line_rollouts WHERE status IN ('completed','compensated') AND ordinal NOT IN(SELECT ordinal FROM line_rollouts WHERE status IN ('completed','compensated') ORDER BY ordinal DESC LIMIT 100)`)
	return changed, err
}
func publishLineRolloutTx(ctx context.Context, tx *sql.Tx, p *LineRollout, at time.Time) error {
	if p.After == nil {
		return ErrLineRolloutState
	}
	if p.Kind != "create" {
		next := rolloutIntent(p.After)
		next.Version = p.baseVersion + 1
		if err := saveLineTx(withLineRolloutWrite(ctx), tx, next, "save"); err != nil {
			return err
		}
		p.baseVersion = next.Version
	}
	_, err := tx.ExecContext(ctx, `UPDATE line_rollouts SET stage='cleanup',base_version=?,published_at=?,version=version+1,updated_at=? WHERE id=?`, p.baseVersion, at.UnixMilli(), at.UnixMilli(), p.ID)
	return err
}
func deleteLineRolloutTx(ctx context.Context, tx *sql.Tx, p *LineRollout) error {
	if err := pathSourceLineInUseTx(ctx, tx, p.LineID); err != nil {
		return err
	}
	var refs int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM entries WHERE line_id=?`, p.LineID).Scan(&refs); err != nil {
		return err
	}
	if refs > 0 {
		return ErrLineRolloutConflict
	}
	before, err := lineOwnedSnapshotTx(ctx, tx, p.LineID)
	if err != nil {
		return err
	}
	if err = replaceLineForwardsTx(ctx, tx, p.LineID, nil); err != nil {
		return err
	}
	old := rolloutIntent(p.Before)
	old.Version = p.baseVersion + 1
	if err = recordLineChangeTx(ctx, tx, old, "delete", before); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM forward_lines WHERE id=?`, p.LineID)
	return err
}

// ApplyLineRolloutForwards replaces only the plan's owned prefix. Everything
// else (including all current users, credentials, Jobs and controls) stays live.
func (s *Store) ApplyLineRolloutForwards(ctx context.Context, nodeID int64, current []spec.Forward) ([]spec.Forward, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT p.id FROM line_rollouts p JOIN line_rollout_nodes n ON n.plan_id=p.id WHERE n.node_id=? AND p.status IN ('running','paused') ORDER BY p.ordinal`, nodeID)
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
	out := append([]spec.Forward{}, current...)
	for _, id := range ids {
		p, e := readLineRolloutTx(ctx, tx, id, time.Now())
		if e != nil {
			return nil, e
		}
		var identity, token string
		if e = tx.QueryRowContext(ctx, `SELECT i.uuid,COALESCE(n.token_hash,'') FROM nodes n JOIN probe_public_ids i ON i.node_id=n.id WHERE n.id=?`, nodeID).Scan(&identity, &token); e != nil {
			return nil, e
		}
		owned := false
		for _, n := range p.Nodes {
			if n.NodeID == nodeID && n.identity == identity && n.token == token {
				owned = true
			}
		}
		prefix := fmt.Sprintf("line-%d-", p.LineID)
		keep := out[:0]
		for _, f := range out {
			if !strings.HasPrefix(f.Tag, prefix) {
				keep = append(keep, f)
			}
		}
		out = keep
		// A re-paired server must never inherit an old generation's held listeners.
		if owned {
			out = append(out, lineRolloutWire(lineRolloutCurrent(p, nodeID))...)
		}
	}
	return out, tx.Commit()
}

// Compensation is another compiled plan. It never restores a complete State,
// credentials, executed Jobs, an old token generation or a historical line ID.
func (s *Store) CompensateLineRollout(ctx context.Context, id string, version int64) (*LineRollout, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	at := time.Now()
	old, err := readLineRolloutTx(ctx, tx, id, at)
	if err != nil {
		return nil, err
	}
	if old.Version != version {
		return nil, ErrLineRolloutConflict
	}
	if old.Status == "compensated" {
		return nil, ErrLineRolloutState
	}
	changedIdentity := map[int64][]LineChangeForward{}
	missing := map[int64]bool{}
	for _, d := range old.dependencies {
		var identity, token string
		e := tx.QueryRowContext(ctx, `SELECT i.uuid,COALESCE(n.token_hash,'') FROM nodes n JOIN probe_public_ids i ON i.node_id=n.id WHERE n.id=?`, d.NodeID).Scan(&identity, &token)
		if errors.Is(e, sql.ErrNoRows) {
			missing[d.NodeID] = true
			continue
		}
		if e != nil {
			return nil, e
		}
		if identity != d.Identity || token != d.Token {
			fs, e := currentLineRolloutObservationTx(ctx, tx, old.ID, d.NodeID, identity, token, at)
			if e != nil {
				return nil, e
			}
			changedIdentity[d.NodeID] = fs
		}
	}
	var current *ForwardLine
	current, err = scanLine(tx.QueryRowContext(ctx, "SELECT "+lineCols+" FROM forward_lines WHERE id=?", old.LineID))
	if err != nil && !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	if current != nil && current.Version != old.baseVersion {
		return nil, fmt.Errorf("%w: a later edit must not be overwritten", ErrLineRolloutConflict)
	}
	restore := rolloutIntent(old.Before)
	remove := restore == nil
	if remove && current == nil {
		return nil, ErrLineRolloutState
	}
	if restore == nil {
		restore = rolloutIntent(current)
	} else if current == nil {
		restore.ID = 0
		restore.Version = 0
	} else {
		restore.ID = current.ID
		restore.Version = current.Version
	}
	held := map[int64][]LineChangeForward{}
	if current != nil {
		if old.Status == "completed" {
			held, err = lineOwnedSnapshotTx(ctx, tx, old.LineID)
			if err != nil {
				return nil, err
			}
		} else {
			for _, n := range old.Nodes {
				held[n.NodeID] = lineRolloutCurrent(old, n.NodeID)
			}
		}
	}
	for id, fs := range changedIdentity {
		held[id] = fs
	}
	for id := range missing {
		delete(held, id)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE line_rollouts SET status='compensated',version=version+1,updated_at=? WHERE id=?`, at.UnixMilli(), old.ID); err != nil {
		return nil, err
	}
	plan, err := createLineRolloutTx(withLineRolloutWrite(ctx), tx, restore, remove, time.Duration(old.timeoutMS)*time.Millisecond, at, held, old.ID)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return plan, nil
}
