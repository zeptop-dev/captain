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
)

type pathRolloutIntents struct {
	Before  *PathRoute
	After   *PathRoute
	cleanup []*ForwardLine
}
type pathRolloutContextKey struct{}

func pathRolloutContext(ctx context.Context) *pathRolloutIntents {
	v, _ := ctx.Value(pathRolloutContextKey{}).(*pathRolloutIntents)
	return v
}
func withPlanPathIntents(ctx context.Context, p *LineRollout) context.Context {
	if p.BeforeRoute == nil && p.AfterRoute == nil {
		return ctx
	}
	return context.WithValue(ctx, pathRolloutContextKey{}, &pathRolloutIntents{Before: p.BeforeRoute, After: p.AfterRoute})
}
func clonePathRolloutIntent(p *PathRoute) *PathRoute {
	if p == nil {
		return nil
	}
	var out PathRoute
	_ = json.Unmarshal([]byte(rolloutRaw(p)), &out)
	out.PreviewRevision, out.ChangeID = "", ""
	return &out
}
func currentRolloutTargets(p *LineRollout) map[int64]LineRolloutTarget {
	out := map[int64]LineRolloutTarget{}
	for _, t := range p.Targets {
		if t.Status != "pending" {
			out[t.NodeID] = t
		}
	}
	return out
}
func compileRolloutIntentTx(ctx context.Context, tx *sql.Tx, l *ForwardLine, after bool) (map[int64][]NodeForward, map[string]LineEndpoint, error) {
	if routes := pathRolloutContext(ctx); routes != nil {
		p := routes.Before
		if after {
			p = routes.After
		}
		if p != nil {
			return compilePathRouteTx(ctx, tx, l, p)
		}
	}
	return compileLine(ctx, tx, l)
}
func readPathRolloutIntentsTx(ctx context.Context, tx *sql.Tx, p *LineRollout) error {
	var b, a string
	e := tx.QueryRowContext(ctx, `SELECT before_json,after_json FROM path_rollout_intents WHERE plan_id=?`, p.ID).Scan(&b, &a)
	if errors.Is(e, sql.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	if e = json.Unmarshal([]byte(b), &p.BeforeRoute); e != nil {
		return e
	}
	if e = json.Unmarshal([]byte(a), &p.AfterRoute); e != nil {
		return e
	}
	for _, route := range []*PathRoute{p.BeforeRoute, p.AfterRoute} {
		if route != nil {
			// An absent field in durable history means disabled at that time,
			// unlike omission in a new editor request which preserves policy.
			PreservePathRouteHealth(route, nil)
		}
	}
	return nil
}
func savePathRolloutIntentsTx(ctx context.Context, tx *sql.Tx, p *LineRollout) error {
	r := pathRolloutContext(ctx)
	if r == nil {
		return nil
	}
	p.BeforeRoute, p.AfterRoute = clonePathRolloutIntent(r.Before), clonePathRolloutIntent(r.After)
	for _, route := range []*PathRoute{p.BeforeRoute, p.AfterRoute} {
		if route != nil {
			PreservePathRouteHealth(route, nil)
		}
	}
	if p.Kind == "delete" {
		p.AfterRoute = nil
	}
	p.SnapshotVersion = "path-relay-v1"
	if _, e := tx.ExecContext(ctx, `INSERT INTO path_rollout_intents(plan_id,before_json,after_json) VALUES(?,?,?)`, p.ID, rolloutRaw(p.BeforeRoute), rolloutRaw(p.AfterRoute)); e != nil {
		return e
	}
	if _, e := tx.ExecContext(ctx, `UPDATE line_rollouts SET snapshot_version=? WHERE id=?`, p.SnapshotVersion, p.ID); e != nil {
		return e
	}
	for _, route := range []*PathRoute{p.BeforeRoute, p.AfterRoute} {
		if route == nil {
			continue
		}
		for _, b := range route.Branches {
			if _, e := tx.ExecContext(ctx, `INSERT INTO path_rollout_refs(plan_id,path_id,revision) VALUES(?,?,?) ON CONFLICT DO NOTHING`, p.ID, b.PathID, b.PathRevision); e != nil {
				return e
			}
		}
	}
	return nil
}
func retagPathRolloutTargets(ts []LineRolloutTarget, nodes []LineRolloutNode, id int64) error {
	for i := range ts {
		for j, old := range ts[i].Expected {
			member := strings.TrimPrefix(old.Tag, "line--1-")
			cut := strings.LastIndex(member, "-")
			if cut < 1 {
				return ErrLineRolloutUnsafe
			}
			prefix := fmt.Sprintf("line-%d-%s-", id, member[:cut])
			found := false
			for _, n := range nodes {
				if n.NodeID != ts[i].NodeID {
					continue
				}
				for _, actual := range n.After {
					if strings.HasPrefix(actual.Tag, prefix) {
						ts[i].Expected[j] = actual
						found = true
						break
					}
				}
			}
			if !found {
				return fmt.Errorf("%w: created listener identity changed during compilation", ErrLineRolloutUnsafe)
			}
		}
	}
	return nil
}

// The graph is made of dedicated listener identities, never physical-node
// depth. A repeated physical node can consume several cumulative snapshots.
func pathRolloutOrderedRules(lines []*ForwardLine, nodes map[int64][]LineChangeForward, reverse bool) ([]struct {
	node int64
	f    LineChangeForward
}, error) {
	type rule = struct {
		node int64
		f    LineChangeForward
	}
	all := map[string]rule{}
	edges := map[string]map[string]bool{}
	indegree := map[string]int{}
	assigned := map[string]bool{}
	for n, fs := range nodes {
		for _, f := range fs {
			all[f.Tag] = rule{n, f}
			edges[f.Tag] = map[string]bool{}
			indegree[f.Tag] = 0
		}
	}
	for _, l := range lines {
		if l == nil {
			continue
		}
		members := map[string][]string{}
		for _, g := range l.Groups {
			for _, m := range g.Members {
				prefix := fmt.Sprintf("line-%d-%s-", l.ID, m.ID)
				if l.ID == 0 {
					prefix = fmt.Sprintf("line--1-%s-", m.ID)
				}
				for _, f := range nodes[m.NodeID] {
					if strings.HasPrefix(f.Tag, prefix) {
						members[m.ID] = append(members[m.ID], f.Tag)
						assigned[f.Tag] = true
					}
				}
			}
		}
		for from, tos := range PathRouteAdjacency(l) {
			for _, to := range tos {
				for _, a := range members[from] {
					for _, b := range members[to] {
						if a != b && !edges[a][b] {
							edges[a][b] = true
							indegree[b]++
						}
					}
				}
			}
		}
	}
	if len(assigned) != len(all) {
		return nil, fmt.Errorf("%w: held listener has no immutable path identity", ErrLineRolloutUnsafe)
	}
	ordered := []rule{}
	for len(ordered) < len(all) {
		ready := []string{}
		for k, v := range indegree {
			if v == 0 {
				ready = append(ready, k)
			}
		}
		if len(ready) == 0 {
			return nil, fmt.Errorf("%w: old and new listener dependencies cannot be ordered safely", ErrLineRolloutUnsafe)
		}
		sort.Strings(ready)
		for _, k := range ready {
			ordered = append(ordered, all[k])
			delete(indegree, k)
			for next := range edges[k] {
				indegree[next]--
			}
		}
	}
	if reverse {
		for i, j := 0, len(ordered)-1; i < j; i, j = i+1, j-1 {
			ordered[i], ordered[j] = ordered[j], ordered[i]
		}
	}
	return ordered, nil
}
func pathRolloutTargets(p *LineRolloutPreview, before, after *ForwardLine) error {
	initial, final := map[int64][]LineChangeForward{}, map[int64][]LineChangeForward{}
	names := map[int64]string{}
	for _, n := range p.Nodes {
		initial[n.NodeID] = append([]LineChangeForward{}, n.Before...)
		final[n.NodeID] = append([]LineChangeForward{}, n.After...)
		names[n.NodeID] = n.NodeName
	}
	expands, e := pathRolloutOrderedRules([]*ForwardLine{after}, final, true)
	if e != nil {
		return e
	}
	cleanupLines := []*ForwardLine{before}
	// Compensation can hold listeners from both sides of its earlier plan.
	// The caller attaches those immutable graph identities, not live topology.
	if routes := p.pathIntents; routes != nil {
		cleanupLines = append(cleanupLines, routes.cleanup...)
	}
	cleans, e := pathRolloutOrderedRules(cleanupLines, initial, false)
	if e != nil {
		return e
	}
	held := map[int64][]LineChangeForward{}
	for n, fs := range initial {
		held[n] = append([]LineChangeForward{}, fs...)
	}
	p.Targets = []LineRolloutTarget{}
	add := func(node int64, phase string) {
		fs := append([]LineChangeForward{}, held[node]...)
		sort.Slice(fs, func(i, j int) bool { return fs[i].Tag < fs[j].Tag })
		held[node] = fs
		p.Targets = append(p.Targets, LineRolloutTarget{NodeID: node, NodeName: names[node], Phase: phase, Order: len(p.Targets) + 1, Attempt: 1, Status: "pending", Expected: append([]LineChangeForward{}, fs...)})
	}
	for _, r := range expands {
		exists := false
		for _, f := range held[r.node] {
			if reflect.DeepEqual(f, r.f) {
				exists = true
			}
		}
		if !exists {
			held[r.node] = append(held[r.node], r.f)
			add(r.node, "expand")
		}
	}
	for _, r := range cleans {
		keep := false
		for _, f := range final[r.node] {
			if reflect.DeepEqual(f, r.f) {
				keep = true
			}
		}
		if keep {
			continue
		}
		next := []LineChangeForward{}
		for _, f := range held[r.node] {
			if f.Tag != r.f.Tag {
				next = append(next, f)
			}
		}
		held[r.node] = next
		add(r.node, "cleanup")
	}
	return nil
}

type PathRolloutPreview struct {
	PreviewRevision string              `json:"preview_revision"`
	Draft           PathRoute           `json:"draft"`
	Remove          bool                `json:"remove"`
	Nodes           []LineRolloutNode   `json:"nodes"`
	Targets         []LineRolloutTarget `json:"targets"`
}

func pathRolloutPrepareContext(ctx context.Context, tx *sql.Tx, draft *PathRoute, remove bool, compensates string) (context.Context, *ForwardLine, error) {
	if draft == nil {
		return ctx, nil, ErrLineRolloutUnsafe
	}
	next := clonePathRolloutIntent(draft)
	var before *PathRoute
	if next.ID > 0 {
		var e error
		before, e = pathRouteTx(ctx, tx, next.ID)
		if e != nil {
			return ctx, nil, e
		}
		if before.Version != next.Version {
			return ctx, nil, ErrLineConflict
		}
	}
	PreservePathRouteHealth(next, before)
	if remove {
		if before == nil {
			return ctx, nil, ErrNotFound
		}
		next = clonePathRolloutIntent(before)
	}
	l, e := pathRouteLineTx(ctx, tx, next)
	if e != nil {
		return ctx, nil, lineRolloutInputError(e)
	}
	if !remove {
		if e = validatePathRouteHealthMutationTx(ctx, tx, before, next); e != nil {
			return ctx, nil, lineRolloutInputError(e)
		}
	}
	l.PreviewRevision = draft.PreviewRevision
	r := &pathRolloutIntents{Before: before, After: next}
	if compensates != "" {
		old, e := readLineRolloutTx(ctx, tx, compensates, time.Now())
		if e != nil {
			return ctx, nil, e
		}
		r.cleanup = []*ForwardLine{old.Before, old.After}
	}
	return context.WithValue(ctx, pathRolloutContextKey{}, r), l, nil
}
func (s *Store) PreviewPathRollout(ctx context.Context, draft *PathRoute, remove bool, timeout time.Duration) (*PathRolloutPreview, error) {
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	ctx, l, e := pathRolloutPrepareContext(ctx, tx, draft, remove, "")
	if e != nil {
		return nil, e
	}
	p, _, e := prepareLineRolloutTx(ctx, tx, l, remove, timeout, time.Now(), nil)
	if e != nil {
		return nil, e
	}
	return &PathRolloutPreview{p.PreviewRevision, *clonePathRolloutIntent(pathRolloutContext(ctx).After), remove, p.Nodes, p.Targets}, nil
}
func createPathRolloutTx(ctx context.Context, tx *sql.Tx, draft *PathRoute, remove bool, timeout time.Duration, at time.Time, override map[int64][]LineChangeForward, compensates string) (*LineRollout, error) {
	ctx, l, e := pathRolloutPrepareContext(ctx, tx, draft, remove, compensates)
	if e != nil {
		return nil, e
	}
	return createLineRolloutTx(ctx, tx, l, remove, timeout, at, override, compensates)
}
func (s *Store) PlanPathRollout(ctx context.Context, draft *PathRoute, remove bool, timeout time.Duration) (*LineRollout, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	p, e := createPathRolloutTx(ctx, tx, draft, remove, timeout, time.Now(), nil, "")
	if e != nil {
		return nil, e
	}
	return p, tx.Commit()
}
