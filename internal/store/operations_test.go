package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
)

func allOperationAccess() OperationAccess {
	return OperationAccess{true, true, true, true, true, true, true, true}
}
func operationList(t *testing.T, s *Store, q OperationQuery, a OperationAccess, at time.Time) *OperationPage {
	t.Helper()
	if q.Limit == 0 {
		q.Limit = 100
	}
	p, e := s.Operations(context.Background(), q, a, at)
	if e != nil {
		t.Fatal(e)
	}
	return p
}

func TestOperationsStablePaginationPermissionsAndReadOnly(t *testing.T) {
	ctx := context.Background()
	s, d := topologyDraftFixture(t)
	at := time.Now()
	for _, id := range []string{"a", "b", "c"} {
		if e := s.CreateNodeJob(ctx, id, 1, "host_manage", json.RawMessage(`{"expires_at":1,"password":"never-public"}`)); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := s.db.Exec(`UPDATE node_jobs SET created_at=?`, at.Unix()); e != nil {
		t.Fatal(e)
	}
	if e := s.SetNodeUpgrade(ctx, 1, "v0.90.0"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.db.Exec(`PRAGMA query_only=ON`); e != nil {
		t.Fatal(e)
	}
	defer s.db.Exec(`PRAGMA query_only=OFF`)
	a := allOperationAccess()
	a.TopologyEntries = false
	first := operationList(t, s, OperationQuery{Limit: 2}, a, at)
	if len(first.Items) != 2 || first.NextBefore == "" {
		t.Fatal(first)
	}
	seen := map[string]bool{}
	page := first
	for {
		for _, o := range page.Items {
			if seen[o.ID] {
				t.Fatal("duplicate cursor item", o.ID)
			}
			seen[o.ID] = true
			if o.Source == "topology_changeset" {
				t.Fatal("restricted draft leaked")
			}
		}
		if page.NextBefore == "" {
			break
		}
		page = operationList(t, s, OperationQuery{Limit: 2, Before: page.NextBefore}, a, at)
	}
	if len(seen) != 4 {
		t.Fatal("pagination lost records", seen)
	}
	if _, e := s.Operation(ctx, "topology_changeset:"+d.ID, a, at); !errors.Is(e, ErrNotFound) {
		t.Fatal("existence leaked", e)
	}
	if got := operationList(t, s, OperationQuery{Source: "node_job", Status: "expired", NodeID: 1, Limit: 1}, a, at); len(got.Items) != 1 || got.Items[0].Status != "expired" {
		t.Fatal(got)
	}
	if got := operationList(t, s, OperationQuery{}, OperationAccess{}, at); len(got.Items) != 0 || got.NextBefore != "" {
		t.Fatal("hidden records affect page", got)
	}
	detail, e := s.Operation(ctx, "node_job:a", a, at)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(detail)
	if strings.Contains(string(raw), "never-public") || strings.Contains(string(raw), "params") {
		t.Fatal("private job data leaked", string(raw))
	}
	var done any
	if e = s.db.QueryRow(`SELECT done_at FROM node_jobs WHERE id='a'`).Scan(&done); e != nil || done != nil {
		t.Fatal("read changed job", done, e)
	}
	for _, q := range []OperationQuery{{Limit: 101}, {Limit: 1, Before: "bad"}, {Limit: 1, Source: "other"}, {Limit: 1, Status: "success"}, {Limit: 1, NodeID: -1}} {
		if _, e = s.Operations(ctx, q, a, at); !errors.Is(e, ErrOperationQuery) {
			t.Fatal(q, e)
		}
	}
}

func TestOperationsRolloutAttemptsRemainHistoricalAfterExpiry(t *testing.T) {
	ctx := context.Background()
	r := newRolloutRig(t)
	p, e := r.s.PlanLineRollout(ctx, r.line, false, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	r.ack(t, p.Targets[0], false)
	p = r.read(t, p.ID)
	if p.Status != "paused" {
		t.Fatal(p.Status)
	}
	p, e = r.s.RetryLineRollout(ctx, p.ID, p.Version)
	if e != nil {
		t.Fatal(e)
	}
	p = r.drain(t, p.ID)
	op, e := r.s.Operation(ctx, "line_rollout:"+p.ID, allOperationAccess(), r.at)
	if e != nil {
		t.Fatal(e)
	}
	if op.Status != "completed" || op.Evidence != "historical" || len(op.Attempts) < 2 {
		t.Fatal(op)
	}
	if _, e = r.s.db.Exec(`PRAGMA query_only=ON`); e != nil {
		t.Fatal(e)
	}
	stale, e := r.s.Operation(ctx, op.ID, allOperationAccess(), r.at.Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	r.s.db.Exec(`PRAGMA query_only=OFF`)
	if stale.Status != "completed" {
		t.Fatal("historical outcome rewritten", stale)
	}
	for _, step := range stale.Steps {
		if step.FreshForMS != 0 {
			t.Fatal("old receipt renewed", step)
		}
	}
	undo, e := r.s.CompensateLineRollout(ctx, p.ID, p.Version)
	if e != nil {
		t.Fatal(e)
	}
	rel, e := r.s.Operation(ctx, "line_rollout:"+undo.ID, allOperationAccess(), r.at)
	if e != nil {
		t.Fatal(e)
	}
	if len(rel.Related) != 1 || rel.Related[0].Relation != "compensates" || rel.Related[0].OperationID != op.ID || !rel.Related[0].Available {
		t.Fatal(rel.Related)
	}
	q := operationList(t, r.s, OperationQuery{NodeID: r.nodes[0].ID, LineID: p.LineID}, allOperationAccess(), r.at)
	if len(q.Items) != 2 {
		t.Fatal(q)
	}
}

func TestOperationsPathRolloutHasSeparateSourceAndPermissions(t *testing.T) {
	ctx := context.Background()
	r, d := pathRolloutRig(t)
	p, e := r.s.PlanPathRollout(ctx, d, false, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	a := allOperationAccess()
	a.PathRollouts = false
	if got := operationList(t, r.s, OperationQuery{}, a, r.at); len(got.Items) != 0 {
		t.Fatal(got)
	}
	if _, e = r.s.Operation(ctx, "path_rollout:"+p.ID, a, r.at); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e = r.s.Operation(ctx, "line_rollout:"+p.ID, allOperationAccess(), r.at); !errors.Is(e, ErrNotFound) {
		t.Fatal("source spoof", e)
	}
	got, e := r.s.Operation(ctx, "path_rollout:"+p.ID, allOperationAccess(), r.at)
	if e != nil || len(got.Steps) == 0 {
		t.Fatal(got, e)
	}
}

func TestOperationUpgradeIdentityObservationAndAtomicity(t *testing.T) {
	ctx := context.Background()
	s, ns, _, _ := lineFixture(t)
	id := ns[0].ID
	if _, e := s.db.Exec(`UPDATE nodes SET token_hash='old-pair' WHERE id=?`, id); e != nil {
		t.Fatal(e)
	}
	if e := s.SetNodeUpgrade(ctx, id, "v0.90.0"); e != nil {
		t.Fatal(e)
	}
	first := operationList(t, s, OperationQuery{Source: "node_upgrade"}, allOperationAccess(), time.Now()).Items[0]
	if first.Status != "pending" {
		t.Fatal(first)
	}
	if e := s.SetNodeUpgrade(ctx, id, "v0.90.0"); e != nil {
		t.Fatal(e)
	}
	all := operationList(t, s, OperationQuery{Source: "node_upgrade"}, allOperationAccess(), time.Now())
	if len(all.Items) != 1 || all.Items[0].ID != first.ID {
		t.Fatal("same pending target is not idempotent", all)
	}
	if e := s.SetNodeUpgrade(ctx, id, "v0.90.1"); e != nil {
		t.Fatal(e)
	}
	all = operationList(t, s, OperationQuery{Source: "node_upgrade"}, allOperationAccess(), time.Now())
	var current Operation
	for _, o := range all.Items {
		if o.Status == "pending" {
			current = o
		}
	}
	if len(all.Items) != 2 || current.ID == first.ID {
		t.Fatal("different target reused identity", all)
	}
	if _, e := s.RecordNodeReport(ctx, id, "old-pair", agentproto.Report{Version: "v0.90.1"}, false, time.Now().Add(time.Second)); e != nil {
		t.Fatal(e)
	}
	o, e := s.Operation(ctx, current.ID, allOperationAccess(), time.Now())
	if e != nil || o.Status != "observed" {
		t.Fatal(o, e)
	}
	if e = s.SetNodeUpgrade(ctx, id, "v0.91.0"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec(`UPDATE nodes SET token_hash='new-pair' WHERE id=?`, id); e != nil {
		t.Fatal(e)
	}
	if _, e = s.RecordNodeReport(ctx, id, "old-pair", agentproto.Report{Version: "v0.91.0"}, false, time.Now()); e == nil {
		t.Fatal("revoked report accepted")
	}
	if _, e = s.RecordNodeReport(ctx, id, "new-pair", agentproto.Report{Version: "v0.91.0"}, false, time.Now()); e != nil {
		t.Fatal(e)
	}
	changed := operationList(t, s, OperationQuery{Source: "node_upgrade", Status: "identity_changed"}, allOperationAccess(), time.Now())
	if len(changed.Items) != 1 {
		t.Fatal(changed)
	}
	// An unpaired request never becomes an observation after a later enrollment.
	if e = s.SetNodeUpgrade(ctx, ns[1].ID, "v0.91.0"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec(`UPDATE nodes SET token_hash='later-pair' WHERE id=?`, ns[1].ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.RecordNodeReport(ctx, ns[1].ID, "later-pair", agentproto.Report{Version: "v0.91.0"}, false, time.Now()); e != nil {
		t.Fatal(e)
	}
	if got := operationList(t, s, OperationQuery{Source: "node_upgrade", NodeID: ns[1].ID}, allOperationAccess(), time.Now()); len(got.Items) != 1 || got.Items[0].Status != "identity_changed" {
		t.Fatal(got)
	}
	if e = s.SetNodeUpgrade(ctx, id, "v0.92.0"); e != nil {
		t.Fatal(e)
	}
	if e = s.SetNodeUpgrade(ctx, id, ""); e != nil {
		t.Fatal(e)
	}
	if got := operationList(t, s, OperationQuery{Source: "node_upgrade", Status: "cancelled"}, allOperationAccess(), time.Now()); len(got.Items) != 1 {
		t.Fatal(got)
	}
	if e = s.SetNodeUpgrade(ctx, id, "v0.92.0"); e != nil {
		t.Fatal(e)
	}
	if got := operationList(t, s, OperationQuery{Source: "node_upgrade", Status: "pending"}, allOperationAccess(), time.Now()); len(got.Items) != 1 {
		t.Fatal("same target after cancellation needs a new request", got)
	}
	if _, e = s.RecordNodeReport(ctx, id, "new-pair", agentproto.Report{Version: "v0.92.0"}, false, time.Now().Add(time.Second)); e != nil {
		t.Fatal(e)
	}
	if got := operationList(t, s, OperationQuery{Source: "node_upgrade", Status: "cancelled"}, allOperationAccess(), time.Now()); len(got.Items) != 1 {
		t.Fatal("late observation changed cancelled record", got)
	}
	// Failed history persistence also rolls back the requested delivery slot.
	if _, e = s.db.Exec(`CREATE TRIGGER reject_upgrade BEFORE INSERT ON node_upgrade_operations BEGIN SELECT RAISE(ABORT,'synthetic failure'); END`); e != nil {
		t.Fatal(e)
	}
	if e = s.SetNodeUpgrade(ctx, id, "v0.93.0"); e == nil {
		t.Fatal("history failure swallowed")
	}
	n, e := s.NodeByID(ctx, id)
	if e != nil || n.UpgradeTo != "" {
		t.Fatal("partial upgrade intent", n, e)
	}
}

func TestOperationUpgradeBulkFailureRollsBackEveryRequest(t *testing.T) {
	ctx := context.Background()
	s, ns, _, _ := lineFixture(t)
	if _, e := s.db.Exec(`UPDATE nodes SET token_hash='pair-'||id`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.db.Exec(fmt.Sprintf(`CREATE TRIGGER reject_second_upgrade BEFORE INSERT ON node_upgrade_operations WHEN NEW.node_id=%d BEGIN SELECT RAISE(ABORT,'synthetic failure'); END`, ns[1].ID)); e != nil {
		t.Fatal(e)
	}
	if _, e := s.SetAllNodesUpgrade(ctx, "v0.90.0"); e == nil {
		t.Fatal("bulk persistence failure swallowed")
	}
	var n int
	if e := s.db.QueryRow(`SELECT count(*) FROM node_upgrade_operations`).Scan(&n); e != nil || n != 0 {
		t.Fatal(n, e)
	}
	if e := s.db.QueryRow(`SELECT count(*) FROM nodes WHERE upgrade_to<>''`).Scan(&n); e != nil || n != 0 {
		t.Fatal("partial delivery on failed bulk", n, e)
	}
	if _, e := s.db.Exec(`DROP TRIGGER reject_second_upgrade`); e != nil {
		t.Fatal(e)
	}
	if n, e := s.SetAllNodesUpgrade(ctx, "v0.90.0"); e != nil || n != 4 {
		t.Fatal(n, e)
	}
	before := operationList(t, s, OperationQuery{Source: "node_upgrade"}, allOperationAccess(), time.Now())
	if _, e := s.SetAllNodesUpgrade(ctx, "v0.90.0"); e != nil {
		t.Fatal(e)
	}
	after := operationList(t, s, OperationQuery{Source: "node_upgrade"}, allOperationAccess(), time.Now())
	b, _ := json.Marshal(before)
	a, _ := json.Marshal(after)
	if string(b) != string(a) {
		t.Fatal("duplicate bulk changed identities or times")
	}
}

func TestOperationUpgradeClearedSlotDoesNotSwallowSameVersionRequest(t *testing.T) {
	ctx := context.Background()
	s, ns, _, _ := lineFixture(t)
	id := ns[0].ID
	if _, e := s.db.Exec(`UPDATE nodes SET token_hash='pair' WHERE id=?`, id); e != nil {
		t.Fatal(e)
	}
	if e := s.SetNodeUpgrade(ctx, id, "v0.90.0"); e != nil {
		t.Fatal(e)
	}
	before := operationList(t, s, OperationQuery{Source: "node_upgrade"}, allOperationAccess(), time.Now()).Items[0]
	// A restored/legacy writer may have cleared the old delivery slot without
	// a corresponding observation. A new explicit request must still deliver.
	if _, e := s.db.Exec(`UPDATE nodes SET upgrade_to='' WHERE id=?`, id); e != nil {
		t.Fatal(e)
	}
	if e := s.SetNodeUpgrade(ctx, id, "v0.90.0"); e != nil {
		t.Fatal(e)
	}
	after := operationList(t, s, OperationQuery{Source: "node_upgrade", Status: "pending"}, allOperationAccess(), time.Now())
	if len(after.Items) != 1 || after.Items[0].ID == before.ID {
		t.Fatal(after)
	}
	n, e := s.NodeByID(ctx, id)
	if e != nil || n.UpgradeTo != "v0.90.0" {
		t.Fatal(n, e)
	}
}

func TestOperationUpgradeRetentionDeleteAndReset(t *testing.T) {
	ctx := context.Background()
	s, ns, _, _ := lineFixture(t)
	id := ns[0].ID
	_, e := s.db.Exec(`UPDATE nodes SET token_hash='paired-'||id`)
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 205; i++ {
		if e = s.SetNodeUpgrade(ctx, id, fmt.Sprintf("v0.90.%d", i)); e != nil {
			t.Fatal(e)
		}
	}
	var count int
	if e = s.db.QueryRow(`SELECT count(*) FROM node_upgrade_operations WHERE node_id=?`, id).Scan(&count); e != nil || count != 201 {
		t.Fatal(count, e)
	}
	if got := operationList(t, s, OperationQuery{Status: "pending"}, allOperationAccess(), time.Now()); len(got.Items) != 1 {
		t.Fatal("active request evicted", got)
	}
	if _, e = s.db.Exec(`UPDATE nodes SET token_hash='changed-pair' WHERE id=?`, id); e != nil {
		t.Fatal(e)
	}
	if e = s.db.QueryRow(`SELECT count(*) FROM node_upgrade_operations WHERE node_id=?`, id).Scan(&count); e != nil || count != 200 {
		t.Fatal("identity retirement exceeded terminal cap", count, e)
	}
	_, e = s.db.Exec(`WITH RECURSIVE n(x) AS(SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<10005) INSERT INTO node_upgrade_operations(id,node_id,node_name,token_identity,target_version,status,actor,created_at,updated_at) SELECT 'retained-'||x,?,'synthetic','paired','v0.90.0','observed','',x,x FROM n`, ns[1].ID)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.SetNodeUpgrade(ctx, id, "v0.99.0"); e != nil {
		t.Fatal(e)
	}
	if e = s.db.QueryRow(`SELECT count(*) FROM node_upgrade_operations WHERE status<>'pending'`).Scan(&count); e != nil || count != 10000 {
		t.Fatal("global cap", count, e)
	}
	if _, e = s.db.Exec(`DELETE FROM nodes WHERE id=?`, id); e != nil {
		t.Fatal(e)
	}
	if e = s.db.QueryRow(`SELECT count(*) FROM node_upgrade_operations WHERE node_id=?`, id).Scan(&count); e != nil || count != 0 {
		t.Fatal("deleted identity retained", count, e)
	}
	if _, e = s.db.Exec(`INSERT INTO staff(id,email,password_hash,role,status,created_at,updated_at) VALUES(1,'admin@example.com','synthetic','admin','active',0,0)`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ResetSite(ctx, 1); e != nil {
		t.Fatal(e)
	}
	if e = s.db.QueryRow(`SELECT count(*) FROM node_upgrade_operations`).Scan(&count); e != nil || count != 0 {
		t.Fatal(count, e)
	}
}

func TestOperationsHostOutcomeIsNotTransportSuccess(t *testing.T) {
	ctx := context.Background()
	s, ns, _, _ := lineFixture(t)
	for _, tc := range []struct{ outcome, status string }{{"waiting", "waiting"}, {"failed", "failed"}, {"unsupported", "failed"}, {"unknown", "unknown"}, {"complete", "completed"}, {"ok", "completed"}, {"restored", "completed"}, {"other", "unknown"}, {"", "unknown"}} {
		id := "host-" + tc.outcome
		if e := s.CreateNodeJob(ctx, id, ns[0].ID, "host_manage", json.RawMessage(`{"action":"apply"}`)); e != nil {
			t.Fatal(e)
		}
		result, _ := json.Marshal(map[string]string{"outcome": tc.outcome, "message": "private-sensitive-message"})
		if e := s.CompleteNodeJob(ctx, ns[0].ID, id, result, ""); e != nil {
			t.Fatal(e)
		}
		o, e := s.Operation(ctx, "node_job:"+id, allOperationAccess(), time.Now())
		if e != nil || o.Status != tc.status {
			t.Fatalf("%q => %+v %v", tc.outcome, o, e)
		}
		raw, _ := json.Marshal(o)
		if strings.Contains(string(raw), "private-sensitive") {
			t.Fatal(string(raw))
		}
	}
	if e := s.CreateNodeJob(ctx, "broken-host", ns[0].ID, "host_manage", json.RawMessage(`{}`)); e != nil {
		t.Fatal(e)
	}
	if e := s.CompleteNodeJob(ctx, ns[0].ID, "broken-host", json.RawMessage(`malformed`), ""); e != nil {
		t.Fatal(e)
	}
	if o, e := s.Operation(ctx, "node_job:broken-host", allOperationAccess(), time.Now()); e != nil || o.Status != "unknown" {
		t.Fatal(o, e)
	}
}
