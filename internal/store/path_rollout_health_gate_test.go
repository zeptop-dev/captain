package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
)

func pathHealthGateFixture(t *testing.T, originalAuto ...bool) (*rolloutRig, *LineRollout, *domain.Entry) {
	t.Helper()
	ctx := context.Background()
	r, draft := pathRolloutRig(t)
	r.healthCapability(t, draft.Entry.NodeID)
	r.ib.Settings.TLS = &spec.TLS{Mode: spec.TLSStandard, ServerName: "example.com"}
	if err := r.s.UpdateInbound(ctx, r.ib); err != nil {
		t.Fatal(err)
	}
	draft.Strategy = "failover"
	draft.Health = &PathRouteHealth{Enabled: true, ServerName: "example.com"}
	original, err := r.s.PlanPathRollout(ctx, draft, false, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	original = r.drain(t, original.ID)
	next, err := r.s.PathRoute(ctx, original.LineID)
	if err != nil {
		t.Fatal(err)
	}
	if len(originalAuto) > 0 && originalAuto[0] {
		next.Health.AutoFailover = true
		if err = r.s.SavePathRoute(ctx, next); err != nil {
			t.Fatal(err)
		}
		r.healthCapability(t, next.Entry.NodeID)
	}
	entry := &domain.Entry{Name: "Published endpoint", LineID: &original.LineID, LineMemberID: next.Entry.ID, Enabled: true}
	if err = r.s.CreateEntry(ctx, entry); err != nil {
		t.Fatal(err)
	}
	next.Entry.Port += 1000
	for i := range next.Branches {
		for j := range next.Branches[i].Ports {
			next.Branches[i].Ports[j] += 1000
		}
	}
	next.Health.AutoFailover = true
	r.healthCapability(t, next.Entry.NodeID)
	p, err := r.s.PlanPathRollout(ctx, next, false, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return r, p, entry
}
func (r *rolloutRig) drainToHealthGate(t *testing.T, id string) *LineRollout {
	t.Helper()
	for i := 0; i < 30; i++ {
		p := r.read(t, id)
		if p.WaitingForPathHealth {
			return p
		}
		if p.Status != "running" || p.PublishedAt != nil {
			t.Fatal("published or paused before health gate", p.Status, p.ErrorCode)
		}
		for _, step := range p.Targets {
			if step.Status == "released" {
				r.ack(t, step, true)
				break
			}
		}
		r.advance(t)
	}
	t.Fatal("health gate not reached")
	return nil
}
func (r *rolloutRig) gateSample(t *testing.T, p *LineRollout, sequence uint64, mutate func(*agentproto.PathHealthSample)) {
	t.Helper()
	ctx := context.Background()
	node := p.AfterRoute.Entry.NodeID
	var chosen spec.Forward
	for _, f := range r.wire(t, node) {
		if f.Port == p.AfterRoute.Entry.Port {
			chosen = f
		}
	}
	if chosen.PathHealth == nil {
		t.Fatal("new entry not released")
	}
	h := chosen.PathHealth
	c := h.Candidates[0]
	sample := agentproto.PathHealthSample{Tag: chosen.Tag, RouteID: h.RouteID, ScopeID: h.ScopeID, CandidateID: c.ID, ConfigDigest: h.Digest(), CandidateDigest: c.Digest, Epoch: "gate-process", Sequence: sequence, ObservedAt: r.at, Health: "up", Result: "success"}
	if mutate != nil {
		mutate(&sample)
	}
	fs := r.wire(t, node)
	hash := lineChangeDigest(lineChangeProjection(fs))
	d, err := r.s.PrepareNodeConfigDeliveryForLines(ctx, node, r.token(node), configSessionA, hash, hash, fs, r.at)
	if err != nil {
		t.Fatal(err)
	}
	r.seq[node]++
	report := agentproto.Report{Version: "v0.85.0", AppliedRevision: hash, PathHealth: &agentproto.PathHealthReport{Supported: true, Samples: []agentproto.PathHealthSample{sample}}, ConfigReceipt: &agentproto.ConfigReceipt{DeliveryID: d.ID, Session: d.Session, ConfigRevision: hash, StateRevision: hash, Sequence: r.seq[node], ObservedAt: r.at, Applied: true}}
	if _, err = r.s.RecordNodeReport(ctx, node, r.token(node), report, false, r.at); err != nil {
		t.Fatal(err)
	}
}
func TestPathRolloutAutoHealthGatePreservesEntryUntilFreshEligibility(t *testing.T) {
	ctx := context.Background()
	r, p, entry := pathHealthGateFixture(t)
	p = r.drainToHealthGate(t, p.ID)
	checkHeld := func() {
		t.Helper()
		r.advance(t)
		p = r.read(t, p.ID)
		if !p.WaitingForPathHealth || p.PublishedAt != nil {
			t.Fatal("unhealthy new entry published", p)
		}
		e, err := r.s.EntryByID(ctx, entry.ID)
		if err != nil || e.DisplayPort == p.AfterRoute.Entry.Port {
			t.Fatal("subscription endpoint changed while waiting", e, err)
		}
	}
	checkHeld()
	// Waiting is reconstructed from durable snapshots and evidence after restart.
	var seq int
	var name, filename string
	if err := r.s.db.QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &filename); err != nil {
		t.Fatal(err)
	}
	if err := r.s.db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := db.Open("sqlite", filename)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reopened.Close() })
	r.s = New(reopened)
	checkHeld()
	for i, mutate := range []func(*agentproto.PathHealthSample){
		func(s *agentproto.PathHealthSample) { s.Health = "unknown" },
		func(s *agentproto.PathHealthSample) { s.Health = "down"; s.Result = "failure" },
		func(s *agentproto.PathHealthSample) { s.ConfigDigest = "wrong-entry-digest" },
		func(s *agentproto.PathHealthSample) { s.ObservedAt = r.at.Add(-4 * time.Minute) },
	} {
		r.gateSample(t, p, uint64(i+1), mutate)
		checkHeld()
	}
	r.gateSample(t, p, 5, nil)
	r.advance(t)
	p = r.read(t, p.ID)
	if p.PublishedAt == nil || p.WaitingForPathHealth {
		t.Fatal("fresh eligible path did not publish", p)
	}
	saved, err := r.s.EntryByID(ctx, entry.ID)
	if err != nil || saved.DisplayPort != p.AfterRoute.Entry.Port {
		t.Fatal("publication not atomic with entry", saved, err)
	}
	r.drain(t, p.ID)
}
func TestPathRolloutAutoHealthTimeoutRetryNeedsNewEvidence(t *testing.T) {
	ctx := context.Background()
	r, p, _ := pathHealthGateFixture(t)
	p = r.drainToHealthGate(t, p.ID)
	step := currentRolloutTargets(p)[p.AfterRoute.Entry.NodeID]
	if _, err := r.s.db.Exec(`UPDATE line_rollout_steps SET deadline_at=? WHERE id=?`, r.at.Add(-time.Millisecond).UnixMilli(), step.ID); err != nil {
		t.Fatal(err)
	}
	r.advance(t)
	p = r.read(t, p.ID)
	if p.Status != "paused" || p.ErrorCode != "path_health_deadline_exceeded" || p.PublishedAt != nil {
		t.Fatal("missing explicit health timeout", p)
	}
	// A healthy sample for the expired attempt must not silently satisfy a retry.
	r.gateSample(t, p, 1, nil)
	oldDelivery := step.DeliveryID
	retried, err := r.s.RetryLineRollout(ctx, p.ID, p.Version)
	if err != nil {
		t.Fatal(err)
	}
	r.at = r.at.Add(time.Second)
	retried = r.drainToHealthGate(t, retried.ID)
	if currentRolloutTargets(retried)[p.AfterRoute.Entry.NodeID].DeliveryID == oldDelivery {
		t.Fatal("retry reused delivery")
	}
	// This sample was collected during the old attempt, but arrives in a new
	// report after the retry delivery. Receive time alone must not release it.
	r.gateSample(t, retried, 2, func(s *agentproto.PathHealthSample) { s.ObservedAt = r.at.Add(-time.Second) })
	r.advance(t)
	if got := r.read(t, p.ID); !got.WaitingForPathHealth || got.PublishedAt != nil {
		t.Fatal("late old sample released new attempt", got)
	}
	r.gateSample(t, retried, 3, nil)
	r.advance(t)
	if got := r.read(t, p.ID); got.PublishedAt == nil {
		t.Fatal("new evidence did not release retried plan", got.ErrorCode)
	}
}

func TestPathRolloutAutoHealthLateEligibilityDoesNotExtendDeadline(t *testing.T) {
	r, p, _ := pathHealthGateFixture(t)
	p = r.drainToHealthGate(t, p.ID)
	step := currentRolloutTargets(p)[p.AfterRoute.Entry.NodeID]
	if _, err := r.s.db.Exec(`UPDATE line_rollout_steps SET deadline_at=? WHERE id=?`, r.at.Add(-time.Millisecond).UnixMilli(), step.ID); err != nil {
		t.Fatal(err)
	}
	r.gateSample(t, p, 1, nil)
	r.advance(t)
	p = r.read(t, p.ID)
	if p.Status != "paused" || p.ErrorCode != "path_health_deadline_exceeded" || p.PublishedAt != nil {
		t.Fatal("late eligibility extended deadline", p)
	}
}

func TestPathRolloutHealthGateHonorsCurrentPause(t *testing.T) {
	for _, kind := range []string{"desired", "runtime_fresh", "runtime_expired"} {
		t.Run(kind, func(t *testing.T) {
			r, p, _ := pathHealthGateFixture(t)
			p = r.drainToHealthGate(t, p.ID)
			r.gateSample(t, p, 1, nil)
			ctx := context.Background()
			node := r.ib.NodeID
			if kind == "desired" {
				tx, err := r.s.db.BeginTx(ctx, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback()
				control, err := serviceControlTx(ctx, tx, node)
				if err != nil {
					t.Fatal(err)
				}
				old := control
				control.state.Manual = ServiceControlManual{Mode: spec.ServiceControlPause}
				if err = saveServiceControlTx(ctx, tx, node, &control, old); err != nil {
					t.Fatal(err)
				}
				if err = tx.Commit(); err != nil {
					t.Fatal(err)
				}
			} else {
				at := r.at
				if kind == "runtime_expired" {
					at = at.Add(-4 * time.Minute)
				}
				raw, _ := json.Marshal(NodeRuntime{ServiceControl: &agentproto.ServiceControlReport{Mode: spec.ServiceControlPause}})
				if _, err := r.s.db.Exec(`UPDATE nodes SET runtime_report_json=?,runtime_reported_at=? WHERE id=?`, string(raw), at.Unix(), node); err != nil {
					t.Fatal(err)
				}
			}
			r.advance(t)
			p = r.read(t, p.ID)
			if kind == "runtime_expired" {
				if p.PublishedAt == nil {
					t.Fatal("expired runtime pause blocked fresh health")
				}
			} else if p.PublishedAt != nil || !p.WaitingForPathHealth {
				t.Fatal("current pause did not hold publication", kind, p.Status, p.Stage, p.PublishedAt)
			}
		})
	}
}
func TestPathRolloutHealthGateCleanupOnlyCompensationRequiresNewObservation(t *testing.T) {
	r, p, _ := pathHealthGateFixture(t, true)
	p = r.drainToHealthGate(t, p.ID)
	// Both entry listeners now exist; only the original one has healthy evidence.
	old := *p
	old.AfterRoute = p.BeforeRoute
	r.gateSample(t, &old, 1, nil)
	comp, err := r.s.CompensateLineRollout(context.Background(), p.ID, p.Version)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range comp.Targets {
		if step.Phase == "expand" {
			t.Fatal("fixture unexpectedly expands a listener")
		}
	}
	if !comp.WaitingForPathHealth {
		t.Fatal("cleanup-only compensation reused pre-attempt health")
	}
	r.advance(t)
	if got := r.read(t, comp.ID); got.PublishedAt != nil {
		t.Fatal("compensation published on old evidence")
	}
	r.at = time.Now().UTC().Add(time.Millisecond).Truncate(time.Millisecond)
	r.gateSample(t, comp, 2, nil)
	r.advance(t)
	if got := r.read(t, comp.ID); got.PublishedAt == nil {
		t.Fatal("new current evidence did not release compensation", got.ErrorCode)
	}
}
