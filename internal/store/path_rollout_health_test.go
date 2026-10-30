package store

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
)

func TestPathRolloutHealthWireOwnsSnapshot(t *testing.T) {
	policy := &spec.PathHealth{RouteID: 1, ScopeID: "scope", Protocol: spec.PathHealthTLS, ServerName: "example.com", ALPN: []string{"h2"}, Candidates: []spec.PathHealthCandidate{{ID: "one", Target: "192.0.2.10:443", Digest: "digest"}}}
	expected := []LineChangeForward{{Tag: "line-1-entry-hash", Port: 1443, Protocol: "tcp", PathHealth: projectLinePathHealth(policy)}}
	wire := lineRolloutWire(expected)
	if !reflect.DeepEqual(wire[0].PathHealth, policy) {
		t.Fatal("overlay dropped health intent", wire)
	}
	wire[0].PathHealth.ALPN[0] = "http/1.1"
	wire[0].PathHealth.Candidates[0].Digest = "changed"
	if expected[0].PathHealth.ALPN[0] != "h2" || expected[0].PathHealth.Candidates[0].Digest != "digest" {
		t.Fatal("overlay mutated durable intent")
	}
}

func TestPathRolloutHistoricalHealthOmissionMeansDisabled(t *testing.T) {
	ctx := context.Background()
	r, draft := pathRolloutRig(t)
	created, err := r.s.PlanPathRollout(ctx, draft, false, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	created = r.drain(t, created.ID)
	next, err := r.s.PathRoute(ctx, created.LineID)
	if err != nil {
		t.Fatal(err)
	}
	next.Entry.Port += 1000
	edited, err := r.s.PlanPathRollout(ctx, next, false, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	// Recreate the persisted shape written before the optional health field.
	if _, err = r.s.db.Exec(`UPDATE path_rollout_intents SET before_json=json_remove(before_json,'$.health'),after_json=json_remove(after_json,'$.health') WHERE plan_id=?`, edited.ID); err != nil {
		t.Fatal(err)
	}
	read := r.read(t, edited.ID)
	for _, intent := range []*PathRoute{read.BeforeRoute, read.AfterRoute} {
		if intent == nil || intent.Health == nil || intent.Health.Enabled {
			t.Fatal("history treated as an omitted update", intent)
		}
	}
	enabled := clonePathRolloutIntent(read.AfterRoute)
	enabled.Health.Enabled = true
	changed, err := PathRouteHealthChanged(enabled, read.BeforeRoute)
	if err != nil || !changed {
		t.Fatal("legacy compensation would evade health authorization", changed, err)
	}
	undo, err := r.s.CompensateLineRollout(ctx, edited.ID, edited.Version)
	if err != nil {
		t.Fatal(err)
	}
	if undo.AfterRoute.Health == nil || undo.AfterRoute.Health.Enabled {
		t.Fatal("legacy compensation lost explicit disabled policy")
	}
}

func (r *rolloutRig) healthCapability(t *testing.T, nodeID int64) {
	t.Helper()
	ctx := context.Background()
	if _, err := r.s.db.Exec(`UPDATE nodes SET version='v0.85.0' WHERE id=?`, nodeID); err != nil {
		t.Fatal(err)
	}
	fs := r.wire(t, nodeID)
	hash := lineChangeDigest(lineChangeProjection(fs))
	d, err := r.s.PrepareNodeConfigDeliveryForLines(ctx, nodeID, r.token(nodeID), configSessionA, hash, hash, fs, r.at)
	if err != nil {
		t.Fatal(err)
	}
	r.seq[nodeID]++
	report := agentproto.Report{AppliedRevision: hash, PathHealth: &agentproto.PathHealthReport{Supported: true}, ConfigReceipt: &agentproto.ConfigReceipt{DeliveryID: d.ID, Session: d.Session, ConfigRevision: hash, StateRevision: hash, Sequence: r.seq[nodeID], ObservedAt: r.at, Applied: true}}
	if _, err = r.s.RecordNodeReport(ctx, nodeID, r.token(nodeID), report, false, r.at); err != nil {
		t.Fatal(err)
	}
}

func TestPathRolloutHealthUsesReleasedEntryAndRealIdentity(t *testing.T) {
	ctx := context.Background()
	r, draft := pathRolloutRig(t)
	r.healthCapability(t, draft.Entry.NodeID)
	draft.Health = &PathRouteHealth{Enabled: true}
	preview, err := r.s.PreviewPathRollout(ctx, draft, false, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var previewScope string
	for _, n := range preview.Nodes {
		for _, f := range n.After {
			if f.PathHealth != nil {
				if f.PathHealth.RouteID != -1 {
					t.Fatal("preview used live route identity", f.PathHealth)
				}
				previewScope = f.PathHealth.ScopeID
			}
		}
	}
	if previewScope == "" {
		t.Fatal("enabled preview missing health")
	}
	draft = &preview.Draft
	draft.PreviewRevision = preview.PreviewRevision
	plan, err := r.s.PlanPathRollout(ctx, draft, false, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range plan.Targets {
		for _, f := range step.Expected {
			if f.PathHealth != nil {
				if f.PathHealth.RouteID != plan.LineID || f.PathHealth.ScopeID != plan.ScopeID || f.PathHealth.ScopeID == previewScope {
					t.Fatal("preview health identity escaped into real step", f.PathHealth, plan.ScopeID)
				}
			}
		}
	}
	if len(r.wire(t, draft.Entry.NodeID)) != 0 {
		t.Fatal("future entry released before downstream")
	}
	plan = r.drain(t, plan.ID)
	current, err := r.s.PathRoute(ctx, plan.LineID)
	if err != nil {
		t.Fatal(err)
	}
	oldPort := current.Entry.Port
	current.Entry.Port += 1000
	for i := range current.Branches {
		for j := range current.Branches[i].Ports {
			current.Branches[i].Ports[j] += 1000
		}
	}
	current.Health.IntervalSeconds = 20
	r.healthCapability(t, current.Entry.NodeID)
	edit, err := r.s.PlanPathRollout(ctx, current, false, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	unionSeen, cleanSeen := false, false
	for i := 0; i < 20; i++ {
		edit = r.read(t, edit.ID)
		if edit.Status == "completed" {
			break
		}
		if edit.Status != "running" {
			t.Fatal(edit.ErrorCode)
		}
		var step *LineRolloutTarget
		for j := range edit.Targets {
			if edit.Targets[j].Status == "released" {
				step = &edit.Targets[j]
				break
			}
		}
		if step == nil {
			t.Fatal("missing released step")
		}
		fs := r.wire(t, current.Entry.NodeID)
		if step.Phase == "expand" && step.NodeID != current.Entry.NodeID {
			if len(fs) != 1 || fs[0].Port != oldPort || fs[0].PathHealth.IntervalSeconds != 15 {
				t.Fatal("future graph health escaped before entry step", fs)
			}
		}
		if step.Phase == "expand" && step.NodeID == current.Entry.NodeID {
			if len(fs) != 2 || fs[0].Tag == fs[1].Tag {
				t.Fatal("entry union not independently identified", fs)
			}
			seen := map[int]int{}
			for _, f := range fs {
				if f.PathHealth == nil {
					t.Fatal("union dropped health")
				}
				seen[f.Port] = f.PathHealth.IntervalSeconds
			}
			if seen[oldPort] != 15 || seen[current.Entry.Port] != 20 {
				t.Fatal("old and new policies collapsed", seen)
			}
			stored, err := r.s.PathRoute(ctx, edit.LineID)
			if err != nil || stored.Health.IntervalSeconds != 15 || stored.Entry.Port != oldPort {
				t.Fatal("path intent published before entry receipt", stored, err)
			}
			unionSeen = true
		}
		if step.Phase == "cleanup" {
			if len(fs) != 1 || fs[0].Port != current.Entry.Port || fs[0].PathHealth.IntervalSeconds != 20 {
				t.Fatal("cleanup retained obsolete health", fs)
			}
			cleanSeen = true
		}
		r.ack(t, *step, true)
		r.advance(t)
	}
	if edit.Status != "completed" || !unionSeen || !cleanSeen {
		t.Fatal("incomplete health release", edit.Status, unionSeen, cleanSeen)
	}
	undo, err := r.s.CompensateLineRollout(ctx, edit.ID, edit.Version)
	if err != nil {
		t.Fatal(err)
	}
	undo = r.drain(t, undo.ID)
	fs := r.wire(t, current.Entry.NodeID)
	if len(fs) != 1 || fs[0].Port != oldPort || fs[0].PathHealth.IntervalSeconds != 15 {
		t.Fatal("compensation did not restore typed health intent", fs)
	}
}

func TestPathRolloutHealthRequiresFreshCapabilityForNewPolicy(t *testing.T) {
	ctx := context.Background()
	r, draft := pathRolloutRig(t)
	if _, err := r.s.db.Exec(`UPDATE nodes SET version='v0.85.0' WHERE id=?`, draft.Entry.NodeID); err != nil {
		t.Fatal(err)
	}
	draft.Health = &PathRouteHealth{Enabled: true}
	if _, err := r.s.PreviewPathRollout(ctx, draft, false, time.Minute); err == nil {
		t.Fatal("preview accepted health without capability proof")
	}
	if _, err := r.s.PlanPathRollout(ctx, draft, false, time.Minute); err == nil {
		t.Fatal("plan accepted health without capability proof")
	}
	var count int
	if err := r.s.db.QueryRow(`SELECT count(*) FROM line_rollouts`).Scan(&count); err != nil || count != 0 {
		t.Fatal("rejected policy wrote plan", count, err)
	}
	r.healthCapability(t, draft.Entry.NodeID)
	created, err := r.s.PlanPathRollout(ctx, draft, false, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	created = r.drain(t, created.ID)
	current, err := r.s.PathRoute(ctx, created.LineID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.s.db.Exec(`UPDATE path_health_capabilities SET observed_at=?,received_at=? WHERE node_id=?`, r.at.Add(-4*time.Minute).UnixMilli(), r.at.Add(-4*time.Minute).UnixMilli(), draft.Entry.NodeID); err != nil {
		t.Fatal(err)
	}
	current.Health.IntervalSeconds = 20
	if _, err = r.s.PlanPathRollout(ctx, current, false, time.Minute); err == nil {
		t.Fatal("changed health accepted expired capability")
	}
	current.Health = nil
	current.Name = "Rename preserves health"
	metadata, err := r.s.PlanPathRollout(ctx, current, false, time.Minute)
	if err != nil {
		t.Fatal("unchanged policy required fresh enable permission", err)
	}
	if metadata.AfterRoute.Health == nil || !metadata.AfterRoute.Health.Enabled || metadata.AfterRoute.Health.IntervalSeconds != 15 {
		t.Fatal("old editor omission lost health")
	}
}
