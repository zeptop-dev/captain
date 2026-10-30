package store

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/captain/internal/domain"
)

func TestResetSiteClearsPinnedPathsActiveRolloutAndOldIdentity(t *testing.T) {
	ctx := context.Background()
	r := newRolloutRig(t)
	r.line.Groups = r.line.Groups[1:]
	if err := r.s.SaveLine(ctx, r.line); err != nil {
		t.Fatal(err)
	}
	p := &ForwardPath{Name: "Pinned source", Steps: []PathStep{{LineID: r.line.ID, LineVersion: r.line.Version}}}
	if err := r.s.SaveForwardPath(ctx, p); err != nil {
		t.Fatal(err)
	}
	nested := &ForwardPath{Name: "Nested pin", Steps: []PathStep{{PathID: p.ID, PathRevision: p.Version}}}
	if err := r.s.SaveForwardPath(ctx, nested); err != nil {
		t.Fatal(err)
	}
	p.Name = "New source revision"
	if err := r.s.SaveForwardPath(ctx, p); err != nil {
		t.Fatal(err)
	}
	route := &PathRoute{Name: "Complete route", Entry: LineMember{NodeID: r.nodes[0].ID, Port: 28000}, TargetInboundID: r.ib.ID, Strategy: "failover", Branches: []PathBranch{{PathID: nested.ID, PathRevision: nested.Version, Ports: []int{28001, 28002}}}}
	if err := r.s.SavePathRoute(ctx, route); err != nil {
		t.Fatal(err)
	}
	draft := cloneLine(r.line)
	for i := range draft.Groups {
		draft.Groups[i].Members[0].Port += 2000
	}
	plan, err := r.s.PlanLineRollout(ctx, draft, false, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	step := plan.Targets[0]
	delivery := r.ack(t, step, true)
	var cfg, state, oldUUID string
	if err = r.s.db.QueryRow(`SELECT config_revision,state_revision FROM node_config_deliveries WHERE id=?`, delivery.ID).Scan(&cfg, &state); err != nil {
		t.Fatal(err)
	}
	if err = r.s.db.QueryRow(`SELECT uuid FROM probe_public_ids WHERE node_id=?`, step.NodeID).Scan(&oldUUID); err != nil {
		t.Fatal(err)
	}
	admin := &domain.User{Email: "reset-paths@example.test", PasswordHash: "test-password-hash", Role: "admin", Status: "active"}
	if err = r.s.CreateStaff(ctx, admin); err != nil {
		t.Fatal(err)
	}
	tables := []string{"forward_paths", "forward_path_revisions", "forward_path_hops", "forward_path_line_versions", "forward_path_refs", "forward_path_routes", "forward_path_route_refs", "line_rollouts", "line_rollout_nodes", "line_rollout_steps", "line_rollout_deliveries", "line_rollout_attempts", "line_rollout_observations", "line_changes", "line_change_scopes", "node_config_deliveries", "node_config_heads", "node_config_sessions"}
	for _, table := range tables {
		var n int
		if err = r.s.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil || n == 0 {
			t.Fatal("fixture did not exercise table", table, n, err)
		}
	}
	if _, err = r.s.ResetSite(ctx, admin.ID); err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		var n int
		if err = r.s.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil || n != 0 {
			t.Fatal("reset retained state", table, n, err)
		}
	}
	if _, err = r.s.NodeByTokenHash(ctx, r.token(step.NodeID)); !errors.Is(err, ErrNotFound) {
		t.Fatal("old node token survived", err)
	}
	var replacement *domain.Node
	for i := int64(1); i <= step.NodeID; i++ {
		replacement = &domain.Node{Name: "new incarnation", PublicAddr: "192.0.2.123"}
		if err = r.s.CreateNode(ctx, replacement, "new-pair-"+strconv.FormatInt(i, 10), time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	if replacement.ID != step.NodeID {
		t.Fatal("test did not reuse numeric node ID")
	}
	if _, err = r.s.db.Exec(`UPDATE nodes SET token_hash='new-incarnation-token' WHERE id=?`, replacement.ID); err != nil {
		t.Fatal(err)
	}
	var newUUID string
	if err = r.s.db.QueryRow(`SELECT uuid FROM probe_public_ids WHERE node_id=?`, replacement.ID).Scan(&newUUID); err != nil || newUUID == oldUUID {
		t.Fatal("node identity reused", err)
	}
	report := agentproto.Report{AppliedRevision: state, ConfigReceipt: &agentproto.ConfigReceipt{DeliveryID: delivery.ID, Session: delivery.Session, ConfigRevision: cfg, StateRevision: state, Sequence: 100, ObservedAt: r.at, Applied: true}}
	if _, err = r.s.RecordNodeReport(ctx, replacement.ID, r.token(step.NodeID), report, false, r.at); !errors.Is(err, ErrNotFound) {
		t.Fatal("old token report accepted by new incarnation", err)
	}
	if _, err = r.s.PrepareNodeConfigDelivery(ctx, replacement.ID, r.token(step.NodeID), configSessionA, cfg, state, r.at); !errors.Is(err, ErrNotFound) {
		t.Fatal("old token delivery accepted", err)
	}
	for _, table := range []string{"line_rollouts", "node_config_deliveries", "node_config_heads", "node_config_sessions"} {
		var n int
		if err = r.s.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil || n != 0 {
			t.Fatal("old request resurrected state", table, n, err)
		}
	}
	var broken int
	if err = r.s.db.QueryRow(`SELECT count(*) FROM pragma_foreign_key_check`).Scan(&broken); err != nil || broken != 0 {
		t.Fatal("foreign key violations", broken, err)
	}
}

func TestStructuralHistoryAndPathReadsWorkOnReadOnlyConnection(t *testing.T) {
	ctx := context.Background()
	r := newRolloutRig(t)
	change, err := r.s.SaveLineWithChange(ctx, r.line)
	if err != nil {
		t.Fatal(err)
	}
	path := &ForwardPath{Name: "Read-only pin", Steps: []PathStep{{NodeID: r.nodes[1].ID}}}
	if err = r.s.SaveForwardPath(ctx, path); err != nil {
		t.Fatal(err)
	}
	route := &PathRoute{Name: "Read-only route", Entry: LineMember{NodeID: r.nodes[0].ID, Port: 30000}, TargetInboundID: r.ib.ID, Strategy: "failover", Branches: []PathBranch{{PathID: path.ID, PathRevision: path.Version, Ports: []int{30001}}}}
	if err = r.s.SavePathRoute(ctx, route); err != nil {
		t.Fatal(err)
	}
	draft := cloneLine(r.line)
	draft.Groups[0].Members[0].Port += 1000
	plan, err := r.s.PlanLineRollout(ctx, draft, false, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var before, after int64
	if err = r.s.db.QueryRow(`SELECT total_changes()`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err = r.s.db.Exec(`PRAGMA query_only=ON`); err != nil {
		t.Fatal(err)
	}
	defer r.s.db.Exec(`PRAGMA query_only=OFF`)
	for _, check := range []struct {
		name string
		read func() error
	}{
		{"path list", func() error { _, e := r.s.ListForwardPaths(ctx); return e }},
		{"current path", func() error { _, e := r.s.ForwardPath(ctx, path.ID, 0); return e }},
		{"pinned path revision", func() error { _, e := r.s.ForwardPath(ctx, path.ID, path.Version); return e }},
		{"path uses", func() error { _, e := r.s.ForwardPathUses(ctx, path.ID); return e }},
		{"route list", func() error { _, e := r.s.ListPathRoutes(ctx); return e }},
		{"route detail", func() error { _, e := r.s.PathRoute(ctx, route.ID); return e }},
		{"node evidence", func() error { _, e := r.s.NodeConfigHistory(ctx, r.nodes[0].ID, r.at); return e }},
		{"line change list", func() error { _, e := r.s.ListLineChanges(ctx, r.line.ID, 0, 20); return e }},
		{"line change detail", func() error { _, e := r.s.LineChangeByID(ctx, change, r.at); return e }},
		{"rollout history", func() error { _, e := r.s.ListLineRollouts(ctx, r.line.ID, 0, 20); return e }},
		{"rollout detail", func() error { _, e := r.s.LineRolloutByID(ctx, plan.ID); return e }},
	} {
		t.Run(check.name, func(t *testing.T) {
			if e := check.read(); e != nil {
				t.Fatal(e)
			}
		})
	}
	if err = r.s.db.QueryRow(`SELECT total_changes()`).Scan(&after); err != nil || before != after {
		t.Fatal("read endpoints changed database", before, after, err)
	}
}
