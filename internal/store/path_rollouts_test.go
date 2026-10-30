package store

import (
	"context"
	"errors"
	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
	"reflect"
	"sync"
	"testing"
	"time"
)

func pathRolloutRig(t *testing.T) (*rolloutRig, *PathRoute) {
	t.Helper()
	s, ns, ib, p := pathFixture(t)
	r := &rolloutRig{s: s, nodes: ns, ib: ib, at: time.Now().UTC().Truncate(time.Millisecond), seq: map[int64]uint64{}}
	for _, n := range ns {
		if _, e := s.db.Exec(`UPDATE nodes SET token_hash=? WHERE id=?`, r.token(n.ID), n.ID); e != nil {
			t.Fatal(e)
		}
		d, e := s.PrepareNodeConfigDelivery(context.Background(), n.ID, r.token(n.ID), configSessionA, "base", "base", r.at)
		if e != nil {
			t.Fatal(e)
		}
		r.receipt(t, n.ID, d, "base", "base", true)
	}
	return r, p
}
func TestPathRolloutGraphRepeatedServerCreateEditCompensateDelete(t *testing.T) {
	ctx := context.Background()
	r, draft := pathRolloutRig(t)
	preview, e := r.s.PreviewPathRollout(ctx, draft, false, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	draft = &preview.Draft
	draft.PreviewRevision = preview.PreviewRevision
	p, e := r.s.PlanPathRollout(ctx, draft, false, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Targets) != 4 || p.Targets[3].NodeID != draft.Entry.NodeID || p.AfterRoute == nil {
		t.Fatal(p)
	}
	// The shared physical node receives two independent cumulative listener steps.
	shared := r.nodes[2].ID
	counts := []int{}
	for _, step := range p.Targets {
		if step.NodeID == shared {
			counts = append(counts, len(step.Expected))
		}
	}
	if !reflect.DeepEqual(counts, []int{1, 2}) {
		t.Fatal(counts)
	}
	p = r.drain(t, p.ID)
	saved, e := r.s.PathRoute(ctx, p.LineID)
	if e != nil {
		t.Fatal(e)
	}
	before := clonePathRolloutIntent(saved)
	saved.Entry.Port += 1000
	for i := range saved.Branches {
		for j := range saved.Branches[i].Ports {
			saved.Branches[i].Ports[j] += 1000
		}
	}
	edit, e := r.s.PlanPathRollout(ctx, saved, false, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	held, e := r.s.PathRoute(ctx, p.LineID)
	if e != nil || held.Entry.Port != before.Entry.Port {
		t.Fatal("draft leaked before publication", held, e)
	}
	edit = r.drain(t, edit.ID)
	held, e = r.s.PathRoute(ctx, p.LineID)
	if e != nil || held.Entry.Port != saved.Entry.Port {
		t.Fatal(held, e)
	}
	undo, e := r.s.CompensateLineRollout(ctx, edit.ID, edit.Version)
	if e != nil {
		t.Fatal(e)
	}
	undo = r.drain(t, undo.ID)
	held, e = r.s.PathRoute(ctx, p.LineID)
	if e != nil || held.Entry.Port != before.Entry.Port {
		t.Fatal(held, e)
	}
	del, e := r.s.PlanPathRollout(ctx, held, true, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	del = r.drain(t, del.ID)
	if del.Kind != "delete" {
		t.Fatal(del)
	}
	for _, n := range r.nodes {
		if len(pathOwned(t, r.s, n.ID, p.LineID)) != 0 {
			t.Fatal("deleted path left forwards")
		}
	}
}

func TestPathRolloutRepeatedNodeRetryRejectsOldStepReceipt(t *testing.T) {
	ctx := context.Background()
	r, draft := pathRolloutRig(t)
	p, e := r.s.PlanPathRollout(ctx, draft, false, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	previous := map[int64]LineRolloutTarget{}
	deliveries := map[int64]*agentproto.ConfigDelivery{}
	for {
		p = r.read(t, p.ID)
		var step LineRolloutTarget
		for _, s := range p.Targets {
			if s.Status == "released" {
				step = s
				break
			}
		}
		if step.ID == "" {
			t.Fatal("fixture never revisited a node")
		}
		if old, ok := previous[step.NodeID]; ok {
			d := r.ack(t, step, false)
			p = r.read(t, p.ID)
			if p.Status != "paused" {
				t.Fatal(p)
			}
			resumed, e := r.s.RetryLineRollout(ctx, p.ID, p.Version)
			if e != nil {
				t.Fatal(e)
			}
			for _, s := range resumed.Targets {
				if s.ID == old.ID && s.Attempt != old.Attempt {
					t.Fatal("retry reset obsolete listener step")
				}
			}
			oldDelivery := deliveries[step.NodeID]
			var cfg, state string
			if e = r.s.db.QueryRow(`SELECT config_revision,state_revision FROM node_config_deliveries WHERE id=?`, oldDelivery.ID).Scan(&cfg, &state); e != nil {
				t.Fatal(e)
			}
			r.receipt(t, step.NodeID, oldDelivery, cfg, state, true)
			r.advance(t)
			p = r.read(t, p.ID)
			if p.Status != "running" || p.PublishedAt != nil {
				t.Fatal("old step receipt advanced retry", p)
			}
			fresh := r.ack(t, step, true)
			if fresh.ID == d.ID || fresh.ID == oldDelivery.ID {
				t.Fatal("retry reused delivery")
			}
			r.advance(t)
			r.drain(t, p.ID)
			return
		}
		previous[step.NodeID] = step
		deliveries[step.NodeID] = r.ack(t, step, true)
		r.advance(t)
	}
}

func TestPathRolloutPinsFutureTemplatesAndRollsBackAtomically(t *testing.T) {
	ctx := context.Background()
	r, draft := pathRolloutRig(t)
	if e := r.s.SavePathRoute(ctx, draft); e != nil {
		t.Fatal(e)
	}
	future := &ForwardPath{Name: "Future fixed path", Steps: []PathStep{{NodeID: r.nodes[2].ID}}}
	if e := r.s.SaveForwardPath(ctx, future); e != nil {
		t.Fatal(e)
	}
	draft.Entry.Port += 1000
	draft.Branches = []PathBranch{{PathID: future.ID, PathRevision: future.Version, Ports: []int{25001}}}
	p, e := r.s.PlanPathRollout(ctx, draft, false, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	if e = r.s.DeleteForwardPath(ctx, future.ID, future.Version); !errors.Is(e, ErrPathInUse) {
		t.Fatal("future template not pinned", e)
	}
	if _, e = r.s.DeletePathRoute(ctx, p.LineID, p.baseVersion); !errors.Is(e, ErrLineRolloutConflict) {
		t.Fatal("immediate delete bypassed stage", e)
	}
	if e = r.s.SavePathRoute(ctx, draft); !errors.Is(e, ErrLineRolloutConflict) {
		t.Fatal("immediate save bypassed stage", e)
	}
	r.drain(t, p.ID)
	r2, d2 := pathRolloutRig(t)
	if _, e = r2.s.db.Exec(`CREATE TRIGGER reject_path_plan BEFORE INSERT ON path_rollout_refs BEGIN SELECT RAISE(ABORT,'fixture rollback'); END`); e != nil {
		t.Fatal(e)
	}
	if _, e = r2.s.PlanPathRollout(ctx, d2, false, time.Minute); e == nil {
		t.Fatal("injected transaction committed")
	}
	for _, table := range []string{"forward_lines", "forward_path_routes", "line_rollouts", "path_rollout_intents", "path_rollout_refs"} {
		var n int
		if e = r2.s.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); e != nil || n != 0 {
			t.Fatal("partial planned save", table, n, e)
		}
	}
}

func TestPathRolloutReopenAndConcurrentPlanners(t *testing.T) {
	ctx := context.Background()
	r, draft := pathRolloutRig(t)
	var wg sync.WaitGroup
	results := make(chan *LineRollout, 2)
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, e := r.s.PlanPathRollout(ctx, clonePathRolloutIntent(draft), false, time.Minute)
			results <- p
			errs <- e
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	var p *LineRollout
	success := 0
	for v := range results {
		if v != nil {
			p = v
			success++
		}
	}
	if success != 1 {
		t.Fatal("overlapping concurrent plans", success)
	}
	// Persist an accepted first step, then close/reopen the actual SQLite file.
	r.ack(t, p.Targets[0], true)
	var seq int
	var name, file string
	if e := r.s.db.QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &file); e != nil {
		t.Fatal(e)
	}
	if e := r.s.db.Close(); e != nil {
		t.Fatal(e)
	}
	conn, e := db.Open("sqlite", file)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { conn.Close() })
	r.s = New(conn)
	loaded := r.read(t, p.ID)
	if loaded.Targets[0].Status != "applied" || loaded.AfterRoute == nil {
		t.Fatal("restart lost accepted evidence", loaded)
	}
	r.advance(t)
	r.drain(t, p.ID)
}

func TestPathRolloutIncarnationFenceAndReset(t *testing.T) {
	ctx := context.Background()
	r, draft := pathRolloutRig(t)
	p, e := r.s.PlanPathRollout(ctx, draft, false, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	p = r.drain(t, p.ID)
	route, e := r.s.PathRoute(ctx, p.LineID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = r.s.DeletePathRoute(ctx, route.ID, route.Version); e != nil {
		t.Fatal(e)
	}
	// Force a numeric ID reuse as can happen after manual data recovery.
	if _, e = r.s.db.Exec(`UPDATE sqlite_sequence SET seq=? WHERE name='forward_lines'`, p.LineID-1); e != nil {
		t.Fatal(e)
	}
	route.ID, route.Version = 0, 0
	if e = r.s.SavePathRoute(ctx, route); e != nil {
		t.Fatal(e)
	}
	if route.ID != p.LineID {
		t.Fatal("fixture did not reuse route id")
	}
	if _, e = r.s.CompensateLineRollout(ctx, p.ID, p.Version); !errors.Is(e, ErrLineRolloutConflict) {
		t.Fatal("historical compensation overwrote reused id", e)
	}
	route.Entry.Port += 1000
	for i := range route.Branches {
		for j := range route.Branches[i].Ports {
			route.Branches[i].Ports[j] += 1000
		}
	}
	if _, e = r.s.PlanPathRollout(ctx, route, false, time.Minute); e != nil {
		t.Fatal(e)
	}
	admin := &domain.User{Email: "path-reset@example.com", PasswordHash: "synthetic-test", Role: "admin", Status: "active"}
	if e = r.s.CreateStaff(ctx, admin); e != nil {
		t.Fatal(e)
	}
	if _, e = r.s.ResetSite(ctx, admin.ID); e != nil {
		t.Fatal(e)
	}
	for _, table := range []string{"path_rollout_refs", "path_rollout_intents", "line_rollouts", "forward_path_routes"} {
		var n int
		if e = r.s.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); e != nil || n != 0 {
			t.Fatal("reset retained path plans", table, n, e)
		}
	}
}

func TestPathRolloutPhysicalNodeCycleKeepsListenerDAG(t *testing.T) {
	ctx := context.Background()
	r, draft := pathRolloutRig(t)
	reversed := &ForwardPath{Name: "Reverse physical order", Steps: []PathStep{{NodeID: r.nodes[2].ID}, {NodeID: r.nodes[1].ID}}}
	if e := r.s.SaveForwardPath(ctx, reversed); e != nil {
		t.Fatal(e)
	}
	draft.Branches[1] = PathBranch{PathID: reversed.ID, PathRevision: reversed.Version, Ports: []int{23001, 23002}}
	p, e := r.s.PlanPathRollout(ctx, draft, false, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Targets) != 5 {
		t.Fatal("physical nodes were collapsed", p.Targets)
	}
	counts := map[int64][]int{}
	for _, step := range p.Targets {
		counts[step.NodeID] = append(counts[step.NodeID], len(step.Expected))
	}
	for _, id := range []int64{r.nodes[1].ID, r.nodes[2].ID} {
		if !reflect.DeepEqual(counts[id], []int{1, 2}) {
			t.Fatal("shared physical server lost cumulative listener state", counts)
		}
	}
	r.drain(t, p.ID)
}
