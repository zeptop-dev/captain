package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

var (
	ErrLineRolloutUnsupported = errors.New("staged releases require supported TCP relay configuration and fresh applied receipts")
	ErrLineRolloutUnsafe      = errors.New("line rollout cannot safely coexist with the current listeners")
	ErrLineRolloutConflict    = errors.New("line rollout conflicts with an active plan, resource reservation or changed topology")
	ErrLineRolloutState       = errors.New("line rollout action is not valid in this state")
)

type LineRollout struct {
	WaitingForPathHealth   bool                `json:"waiting_for_path_health,omitempty"`
	SnapshotVersion        string              `json:"snapshot_version"`
	ID                     string              `json:"id"`
	Cursor                 int64               `json:"cursor"`
	LineID                 int64               `json:"line_id"`
	ScopeID                string              `json:"scope_id"`
	Version                int64               `json:"version"`
	Kind                   string              `json:"kind"`
	Status                 string              `json:"status"`
	Stage                  string              `json:"stage"`
	Actor                  string              `json:"actor"`
	BeforeRoute            *PathRoute          `json:"before_route,omitempty"`
	AfterRoute             *PathRoute          `json:"after_route,omitempty"`
	Before                 *ForwardLine        `json:"before,omitempty"`
	After                  *ForwardLine        `json:"after,omitempty"`
	ErrorCode              string              `json:"error_code,omitempty"`
	Compensates            string              `json:"compensates,omitempty"`
	CreatedAt              time.Time           `json:"created_at"`
	UpdatedAt              time.Time           `json:"updated_at"`
	PublishedAt            *time.Time          `json:"published_at,omitempty"`
	Targets                []LineRolloutTarget `json:"targets"`
	Nodes                  []LineRolloutNode   `json:"nodes"`
	baseVersion, timeoutMS int64
	dependencies           []lineRolloutDependency
}
type LineRolloutNode struct {
	NodeID                  int64               `json:"node_id"`
	NodeName                string              `json:"node_name"`
	Before                  []LineChangeForward `json:"before"`
	After                   []LineChangeForward `json:"after"`
	identity, token         string
	beforeDepth, afterDepth int
}
type LineRolloutTarget struct {
	ID                 string              `json:"id"`
	NodeID             int64               `json:"node_id"`
	NodeName           string              `json:"node_name"`
	Phase              string              `json:"phase"`
	Order              int                 `json:"order"`
	Attempt            int                 `json:"attempt"`
	Status             string              `json:"status"`
	Expected           []LineChangeForward `json:"expected"`
	DeliveryID         string              `json:"delivery_id"`
	ConfigRevision     string              `json:"config_revision"`
	StateRevision      string              `json:"state_revision"`
	IssuedAt           *time.Time          `json:"issued_at,omitempty"`
	ConfirmedAt        *time.Time          `json:"confirmed_at,omitempty"`
	ObservedAt         *time.Time          `json:"observed_at,omitempty"`
	LastReceiptAt      *time.Time          `json:"last_receipt_at,omitempty"`
	DeadlineAt         *time.Time          `json:"deadline_at,omitempty"`
	ReceiptApplied     bool                `json:"receipt_applied"`
	Fresh              bool                `json:"fresh"`
	FreshForMS         int64               `json:"fresh_for_ms"`
	attemptID, session string
	sequence           int64
}
type LineRolloutPage struct {
	Items      []LineRollout `json:"items"`
	NextBefore int64         `json:"next_before,omitempty"`
}
type LineRolloutPreview struct {
	pathIntents     *pathRolloutIntents
	PreviewRevision string              `json:"preview_revision"`
	Draft           ForwardLine         `json:"draft"`
	Remove          bool                `json:"remove"`
	Nodes           []LineRolloutNode   `json:"nodes"`
	Targets         []LineRolloutTarget `json:"targets"`
}
type lineRolloutDependency struct {
	NodeID   int64  `json:"node_id"`
	Identity string `json:"identity"`
	Token    string `json:"token"`
}
type lineRolloutWriteKey struct{}

func lineRolloutWrite(ctx context.Context) bool {
	v, _ := ctx.Value(lineRolloutWriteKey{}).(bool)
	return v
}
func withLineRolloutWrite(ctx context.Context) context.Context {
	return context.WithValue(ctx, lineRolloutWriteKey{}, true)
}
func rolloutIntent(l *ForwardLine) *ForwardLine {
	if l == nil {
		return nil
	}
	n := ForwardLine{PathRoute: l.PathRoute, ID: l.ID, Name: l.Name, Version: l.Version, Protocol: l.Protocol, Backend: l.Backend, TargetInboundID: l.TargetInboundID, TargetAddressMode: l.TargetAddressMode, Groups: l.Groups}
	raw, _ := json.Marshal(n)
	_ = json.Unmarshal(raw, &n)
	return &n
}
func rolloutRaw(v any) string { raw, _ := json.Marshal(v); return string(raw) }
func rolloutTimeout(d time.Duration) (int64, error) {
	if d == 0 {
		d = 5 * time.Minute
	}
	if d < 30*time.Second || d > time.Hour {
		return 0, fmt.Errorf("%w: timeout must be 30..3600 seconds", ErrLineRolloutUnsafe)
	}
	return d.Milliseconds(), nil
}
func rolloutActiveTx(ctx context.Context, tx *sql.Tx, lineID int64) (bool, error) {
	var active bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM line_rollouts WHERE line_id=? AND status IN ('running','paused'))`, lineID).Scan(&active)
	return active, err
}
func checkLineRolloutWriteTx(ctx context.Context, tx *sql.Tx, lineID int64) error {
	if lineRolloutWrite(ctx) {
		return nil
	}
	active, err := rolloutActiveTx(ctx, tx, lineID)
	if err != nil {
		return err
	}
	if active {
		return ErrLineRolloutConflict
	}
	return nil
}
func lineRolloutDepths(l *ForwardLine) map[int64]int {
	out := map[int64]int{}
	if l != nil {
		for i, g := range l.Groups {
			for _, m := range g.Members {
				out[m.NodeID] = i
			}
		}
	}
	return out
}
func lineRolloutWire(fs []LineChangeForward) []spec.Forward {
	out := make([]spec.Forward, 0, len(fs))
	for _, f := range fs {
		v := spec.Forward{Tag: f.Tag, Listen: f.Listen, Port: f.Port, Protocol: f.Protocol, Backend: f.Backend, ProxyProtocol: f.ProxyProtocol, PreserveSource: f.PreserveSource, Balance: f.Balance, PathHealth: f.PathHealth.Wire()}
		for i, t := range f.Targets {
			if i == 0 {
				v.Target = t.Target
				v.Weight = t.Weight
			} else {
				v.Targets = append(v.Targets, spec.ForwardTarget{Target: t.Target, Weight: t.Weight})
			}
		}
		out = append(out, v)
	}
	return out
}
func lineRolloutUnion(a, b []LineChangeForward) ([]LineChangeForward, error) {
	out := append([]LineChangeForward{}, a...)
	for _, next := range b {
		same := false
		for _, old := range out {
			if reflect.DeepEqual(next, old) {
				same = true
				break
			}
			if next.Port == old.Port && lineListenOverlap(next.Listen, old.Listen) {
				return nil, fmt.Errorf("%w: listener %s:%d needs a separate port", ErrLineRolloutUnsafe, next.Listen, next.Port)
			}
		}
		if !same {
			out = append(out, next)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Tag < out[j].Tag })
	return out, nil
}
func lineRolloutSupported(l *ForwardLine) bool {
	if l == nil {
		return true
	}
	if l.Protocol != "tcp" || l.Backend != "relay" {
		return false
	}
	var flags struct {
		PathRoute bool `json:"path_route"`
	}
	_ = json.Unmarshal([]byte(rolloutRaw(l)), &flags)
	return !flags.PathRoute
}
func freshRolloutNodeTx(ctx context.Context, tx *sql.Tx, id int64, at time.Time) (lineRolloutDependency, string, error) {
	var d lineRolloutDependency
	d.NodeID = id
	var name, mode string
	var received, observed sql.NullInt64
	var applied bool
	err := tx.QueryRowContext(ctx, `SELECT n.name,p.uuid,COALESCE(n.token_hash,''),n.node_mode FROM nodes n JOIN probe_public_ids p ON p.node_id=n.id WHERE n.id=?`, id).Scan(&name, &d.Identity, &d.Token, &mode)
	if err != nil {
		return d, name, err
	}
	if d.Token == "" || mode == "monitor" {
		return d, name, ErrLineRolloutUnsupported
	}
	err = tx.QueryRowContext(ctx, `SELECT d.last_receipt_at,d.observed_at,d.applied FROM node_config_heads h JOIN node_config_deliveries d ON d.id=h.delivery_id JOIN node_config_sessions s ON s.node_id=h.node_id AND s.token_identity=h.token_identity AND s.session=h.session WHERE h.node_id=? AND h.token_identity=? AND s.retired_at IS NULL`, id, d.Token).Scan(&received, &observed, &applied)
	if errors.Is(err, sql.ErrNoRows) {
		return d, name, ErrLineRolloutUnsupported
	}
	if err != nil {
		return d, name, err
	}
	e := NodeConfigDeliveryRecord{Current: true, LastReceiptAt: lineMillisPtr(received), ObservedAt: lineMillisPtr(observed)}
	setNodeConfigFreshness(&e, at)
	if !e.Fresh || !applied {
		return d, name, ErrLineRolloutUnsupported
	}
	return d, name, nil
}

// Preview and Plan run the same compiler, identity checks and coexistence rules.
func (s *Store) PreviewLineRollout(ctx context.Context, draft *ForwardLine, remove bool, timeout time.Duration) (*LineRolloutPreview, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	p, _, err := prepareLineRolloutTx(ctx, tx, draft, remove, timeout, time.Now(), nil)
	if err != nil {
		return nil, err
	}
	return p, tx.Commit()
}
func prepareLineRolloutTx(ctx context.Context, tx *sql.Tx, draft *ForwardLine, remove bool, timeout time.Duration, at time.Time, override map[int64][]LineChangeForward) (*LineRolloutPreview, []lineRolloutDependency, error) {
	if draft == nil {
		return nil, nil, ErrLineRolloutUnsafe
	}
	ms, err := rolloutTimeout(timeout)
	if err != nil {
		return nil, nil, err
	}
	if !lineRolloutSupported(draft) && pathRolloutContext(ctx) == nil {
		return nil, nil, ErrLineRolloutUnsupported
	}
	var before *ForwardLine
	if draft.ID > 0 {
		if owned, e := isPathRouteTx(ctx, tx, draft.ID); e != nil {
			return nil, nil, e
		} else if owned && pathRolloutContext(ctx) == nil {
			return nil, nil, ErrLineRolloutUnsupported
		}
		before, err = scanLine(tx.QueryRowContext(ctx, "SELECT "+lineCols+" FROM forward_lines WHERE id=?", draft.ID))
		if err != nil {
			return nil, nil, wrapNotFound(err)
		}
		if before.Version != draft.Version {
			return nil, nil, ErrLineConflict
		}
		if !lineRolloutSupported(before) && pathRolloutContext(ctx) == nil {
			return nil, nil, ErrLineRolloutUnsupported
		}
		if err = checkLineRolloutWriteTx(ctx, tx, draft.ID); err != nil {
			return nil, nil, err
		}
	}
	if remove && before == nil {
		return nil, nil, ErrNotFound
	}
	next := rolloutIntent(draft)
	if remove {
		next = rolloutIntent(before)
	} else if pathRolloutContext(ctx) == nil {
		if err = normalizeLine(next); err != nil {
			return nil, nil, lineRolloutInputError(err)
		}
	}
	lineID := next.ID
	if lineID == 0 {
		lineID = -1
	}
	compiled := *next
	compiled.ID = lineID
	after := map[int64][]LineChangeForward{}
	var endpoints map[string]LineEndpoint
	if !remove {
		ib, e := scanInbound(tx.QueryRowContext(ctx, "SELECT "+inboundCols+" FROM inbounds WHERE id=?", next.TargetInboundID))
		if e != nil {
			return nil, nil, e
		}
		var reverse int
		if e = tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM node_wireguard_links WHERE user_inbound_id=?) + (SELECT count(*) FROM node_reverse_links WHERE user_inbound_id=?)`, ib.ID, ib.ID).Scan(&reverse); e != nil {
			return nil, nil, e
		}
		if reverse > 0 || ib.Settings.Reverse != nil {
			return nil, nil, ErrLineRolloutUnsupported
		}
		generated, ep, e := compileRolloutIntentTx(ctx, tx, &compiled, true)
		if e != nil {
			return nil, nil, lineRolloutInputError(e)
		}
		endpoints = ep
		for id, fs := range generated {
			wire := []spec.Forward{}
			for _, f := range fs {
				f.IngressID = ""
				wire = append(wire, f.Forward)
			}
			after[id] = lineChangeProjection(wire)
		}
		if before != nil {
			if _, e = linePublishedMembersTx(ctx, tx, before.ID, endpoints); e != nil {
				return nil, nil, lineRolloutInputError(e)
			}
		}
	} else {
		if err = pathSourceLineInUseTx(ctx, tx, draft.ID); err != nil {
			return nil, nil, err
		}
		var entries int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM entries WHERE line_id=?`, draft.ID).Scan(&entries); err != nil {
			return nil, nil, err
		}
		if entries > 0 {
			return nil, nil, fmt.Errorf("%w: remove subscription entries before planning deletion", ErrLineRolloutUnsafe)
		}
	}
	old := map[int64][]LineChangeForward{}
	if before != nil {
		old, err = lineOwnedSnapshotTx(ctx, tx, before.ID)
		if err != nil {
			return nil, nil, err
		}
	}
	if override != nil {
		old = override
	}
	ids := map[int64]bool{}
	for id := range old {
		ids[id] = true
	}
	for id := range after {
		ids[id] = true
	}
	bd, ad := lineRolloutDepths(before), lineRolloutDepths(next)
	deps := []lineRolloutDependency{}
	names := map[int64]string{}
	// Landing is a prerequisite, never a fictitious modified target.
	dependencyIDs := map[int64]bool{}
	for id := range ids {
		dependencyIDs[id] = true
	}
	for _, l := range []*ForwardLine{before, next} {
		if l != nil {
			var id int64
			if err = tx.QueryRowContext(ctx, `SELECT node_id FROM inbounds WHERE id=?`, l.TargetInboundID).Scan(&id); err != nil {
				return nil, nil, err
			}
			dependencyIDs[id] = true
		}
	}
	ordered := []int64{}
	for id := range dependencyIDs {
		ordered = append(ordered, id)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	for _, id := range ordered {
		d, name, e := freshRolloutNodeTx(ctx, tx, id, at)
		if e != nil && override != nil && d.Token != "" && d.Identity != "" {
			var received, observed sql.NullInt64
			check := tx.QueryRowContext(ctx, `SELECT d.last_receipt_at,d.observed_at FROM node_config_heads h JOIN node_config_deliveries d ON d.id=h.delivery_id JOIN nodes n ON n.id=h.node_id WHERE h.node_id=? AND h.token_identity=? AND n.node_mode<>'monitor'`, id, d.Token).Scan(&received, &observed)
			if check != nil && !errors.Is(check, sql.ErrNoRows) {
				return nil, nil, check
			}
			evidence := NodeConfigDeliveryRecord{Current: check == nil, LastReceiptAt: lineMillisPtr(received), ObservedAt: lineMillisPtr(observed)}
			setNodeConfigFreshness(&evidence, at)
			if evidence.Fresh {
				e = nil
			}
		}
		if e != nil {
			return nil, nil, fmt.Errorf("%w: server %d", e, id)
		}
		deps = append(deps, d)
		names[id] = name
	}
	next.Endpoints = sortedRolloutEndpoints(endpoints)
	p := &LineRolloutPreview{Draft: *next, Remove: remove, Nodes: []LineRolloutNode{}, Targets: []LineRolloutTarget{}}
	for id := range ids {
		b, a := old[id], after[id]
		if b == nil {
			b = []LineChangeForward{}
		}
		if a == nil {
			a = []LineChangeForward{}
		}
		var d lineRolloutDependency
		for _, v := range deps {
			if v.NodeID == id {
				d = v
			}
		}
		node := LineRolloutNode{NodeID: id, NodeName: names[id], Before: b, After: a, identity: d.Identity, token: d.Token, beforeDepth: bd[id], afterDepth: ad[id]}
		p.Nodes = append(p.Nodes, node)
		if reflect.DeepEqual(a, b) {
			continue
		}
		union, e := lineRolloutUnion(b, a)
		if e != nil {
			return nil, nil, e
		}
		if !reflect.DeepEqual(union, b) {
			p.Targets = append(p.Targets, LineRolloutTarget{NodeID: id, NodeName: names[id], Phase: "expand", Status: "pending", Expected: union, Order: ad[id]})
		}
		if !reflect.DeepEqual(union, a) {
			p.Targets = append(p.Targets, LineRolloutTarget{NodeID: id, NodeName: names[id], Phase: "cleanup", Status: "pending", Expected: a, Order: bd[id]})
		}
	}
	sort.Slice(p.Nodes, func(i, j int) bool { return p.Nodes[i].NodeID < p.Nodes[j].NodeID })
	sort.Slice(p.Targets, func(i, j int) bool {
		a, b := p.Targets[i], p.Targets[j]
		if a.Phase != b.Phase {
			return a.Phase == "expand"
		}
		if a.Order != b.Order {
			if a.Phase == "expand" {
				return a.Order > b.Order
			}
			return a.Order < b.Order
		}
		return a.NodeID < b.NodeID
	})
	for i := range p.Targets {
		p.Targets[i].Order = i + 1
		p.Targets[i].Attempt = 1
	}
	if pathRolloutContext(ctx) != nil {
		p.pathIntents = pathRolloutContext(ctx)
		if err = pathRolloutTargets(p, before, next); err != nil {
			return nil, nil, err
		}
	}
	// Do not couple preview identity to receipt sequences or dynamic user state.
	p.PreviewRevision = lineChangeDigest([]LineChangeForward{{Tag: rolloutRaw(struct {
		Draft   *ForwardLine
		Remove  bool
		Timeout int64
		Nodes   []LineRolloutNode
		Deps    []lineRolloutDependency
		Route   *pathRolloutIntents
	}{next, remove, ms, p.Nodes, deps, pathRolloutContext(ctx)})}})
	return p, deps, nil
}

func (s *Store) PlanLineRollout(ctx context.Context, draft *ForwardLine, remove bool, timeout time.Duration) (*LineRollout, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	plan, err := createLineRolloutTx(ctx, tx, draft, remove, timeout, time.Now(), nil, "")
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return plan, nil
}
func createLineRolloutTx(ctx context.Context, tx *sql.Tx, draft *ForwardLine, remove bool, timeout time.Duration, at time.Time, override map[int64][]LineChangeForward, compensates string) (*LineRollout, error) {
	preview, deps, err := prepareLineRolloutTx(ctx, tx, draft, remove, timeout, at, override)
	if err != nil {
		return nil, err
	}
	if draft.PreviewRevision != "" && draft.PreviewRevision != preview.PreviewRevision {
		return nil, ErrLinePreview
	}
	var before *ForwardLine
	if draft.ID > 0 {
		before, err = scanLine(tx.QueryRowContext(ctx, "SELECT "+lineCols+" FROM forward_lines WHERE id=?", draft.ID))
		if err != nil {
			return nil, err
		}
	}
	next := rolloutIntent(&preview.Draft)
	next.Endpoints = preview.Draft.Endpoints
	if before != nil {
		_, ep, e := compileRolloutIntentTx(ctx, tx, before, false)
		if e != nil {
			return nil, e
		}
		before = rolloutIntent(before)
		before.Endpoints = sortedRolloutEndpoints(ep)
	}
	kind := "edit"
	if remove {
		kind = "delete"
	}
	if before == nil {
		kind = "create"
		r, e := tx.ExecContext(ctx, `INSERT INTO forward_lines(name,version,protocol,backend,target_inbound_id,groups_json,updated_at) VALUES(?,1,?,?,?,'[]',?)`, next.Name, next.Protocol, next.Backend, next.TargetInboundID, now())
		if e != nil {
			return nil, e
		}
		next.ID, _ = r.LastInsertId()
		next.Version = 1
		if routes := pathRolloutContext(ctx); routes != nil {
			routes.After.ID, routes.After.Version = next.ID, next.Version
			if e = pathRouteSpecWriteTx(ctx, tx, routes.After); e != nil {
				return nil, e
			}
		}
		if e = saveLineTx(withLineRolloutWrite(ctx), tx, next, "save"); e != nil {
			return nil, e
		}
		// Recompile with the reserved real ID, replacing preview-only tags.
		generated, _, e := compileLine(ctx, tx, next)
		if e != nil {
			return nil, e
		}
		for i := range preview.Nodes {
			fs := []spec.Forward{}
			for _, f := range generated[preview.Nodes[i].NodeID] {
				f.IngressID = ""
				fs = append(fs, f.Forward)
			}
			preview.Nodes[i].After = lineChangeProjection(fs)
		}
		if pathRolloutContext(ctx) != nil {
			if e = retagPathRolloutTargets(preview.Targets, preview.Nodes, next.ID); e != nil {
				return nil, e
			}
		} else {
			for i := range preview.Targets {
				for _, n := range preview.Nodes {
					if n.NodeID == preview.Targets[i].NodeID {
						preview.Targets[i].Expected = n.After
					}
				}
			}
		}
	}
	var scope string
	if err = tx.QueryRowContext(ctx, `SELECT id FROM line_change_scopes WHERE line_id=?`, next.ID).Scan(&scope); err != nil {
		return nil, err
	}
	ms, _ := rolloutTimeout(timeout)
	after := next
	if remove {
		after = nil
	}
	stage := "expand"
	if remove {
		stage = "cleanup"
	}
	plan := &LineRollout{ID: rand.Text(), LineID: next.ID, ScopeID: scope, Version: 1, Kind: kind, Status: "running", Stage: stage, Actor: lineChangeActor(ctx), Before: before, After: after, CreatedAt: at, UpdatedAt: at, Compensates: compensates, Nodes: preview.Nodes, Targets: preview.Targets, baseVersion: next.Version, timeoutMS: ms, dependencies: deps}
	if _, err = tx.ExecContext(ctx, `INSERT INTO line_rollouts(id,line_id,scope_id,kind,status,stage,actor,before_json,after_json,dependencies_json,base_version,timeout_ms,compensates,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, plan.ID, plan.LineID, scope, kind, plan.Status, stage, plan.Actor, rolloutRaw(plan.Before), rolloutRaw(after), rolloutRaw(deps), plan.baseVersion, ms, compensates, at.UnixMilli(), at.UnixMilli()); err != nil {
		return nil, err
	}
	if err = savePathRolloutIntentsTx(ctx, tx, plan); err != nil {
		return nil, err
	}
	for _, n := range plan.Nodes {
		if _, err = tx.ExecContext(ctx, `INSERT INTO line_rollout_nodes(plan_id,node_id,node_name,node_identity,token_identity,before_json,after_json,before_depth,after_depth) VALUES(?,?,?,?,?,?,?,?,?)`, plan.ID, n.NodeID, n.NodeName, n.identity, n.token, rolloutRaw(n.Before), rolloutRaw(n.After), n.beforeDepth, n.afterDepth); err != nil {
			return nil, err
		}
	}
	for i := range plan.Targets {
		t := &plan.Targets[i]
		t.ID = rand.Text()
		t.attemptID = rand.Text()
		if _, err = tx.ExecContext(ctx, `INSERT INTO line_rollout_steps(id,plan_id,ordinal,node_id,phase,expected_json,status,attempt_id) VALUES(?,?,?,?,?,?,?,?)`, t.ID, plan.ID, t.Order, t.NodeID, t.Phase, rolloutRaw(t.Expected), t.Status, t.attemptID); err != nil {
			return nil, err
		}
	}
	if _, err = advanceLineRolloutTx(ctx, tx, plan, at); err != nil {
		return nil, err
	}
	return readLineRolloutTx(ctx, tx, plan.ID, at)
}

func randRolloutID() string { return rand.Text() }

func makeLineRolloutWire(fs []NodeForward) []spec.Forward {
	out := []spec.Forward{}
	for _, f := range fs {
		f.IngressID = ""
		out = append(out, f.Forward)
	}
	return out
}
func sameRolloutEndpoints(want []LineEndpoint, got map[string]LineEndpoint) bool {
	if len(want) != len(got) {
		return false
	}
	for _, e := range want {
		if got[e.MemberID] != e {
			return false
		}
	}
	return true
}
func sortedRolloutEndpoints(m map[string]LineEndpoint) []LineEndpoint {
	out := []LineEndpoint{}
	for _, e := range m {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MemberID < out[j].MemberID })
	return out
}
func timeNowRollout() time.Time { return time.Now() }

func lineRolloutInputError(err error) error {
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, ErrNotFound) {
		return ErrNotFound
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrLineRolloutConflict) || errors.Is(err, ErrLineConflict) || errors.Is(err, ErrLinePreview) {
		return err
	}
	var dbErr interface{ Code() int }
	if errors.As(err, &dbErr) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrLineRolloutUnsafe, err)
}
