package store

import (
	"context"
	"reflect"
	"testing"
	"time"

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
