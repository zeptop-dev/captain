package store

import (
	"context"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
)

func TestPathHealthViewRetainsHysteresisAndSampleAge(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "expired_sample", true: "degraded_candidates"}[failure], func(t *testing.T) {
			ctx := context.Background()
			s, ns, ib, route := pathFixture(t)
			ib.Protocol = spec.HTTP
			if err := s.UpdateInbound(ctx, ib); err != nil {
				t.Fatal(err)
			}
			at := time.Now().UTC().Truncate(time.Millisecond)
			pathHealthCapabilityFixture(t, s, ns[0].ID, at)
			route.Health = &PathRouteHealth{Enabled: true, AutoFailover: true}
			route.Strategy = spec.BalanceFailover
			if err := s.SavePathRoute(ctx, route); err != nil {
				t.Fatal(err)
			}
			f := pathOwned(t, s, ns[0].ID, route.ID)[0]
			d, err := s.PrepareNodeConfigDeliveryForLines(ctx, ns[0].ID, configTestToken, configSessionA, "health-view", "health-state", []spec.Forward{f.Forward}, at)
			if err != nil {
				t.Fatal(err)
			}
			result := "success"
			if failure {
				result = "failure"
			}
			samples := []agentproto.PathHealthSample{}
			for i, c := range f.PathHealth.Candidates {
				samples = append(samples, agentproto.PathHealthSample{Tag: f.Tag, RouteID: route.ID, ScopeID: f.PathHealth.ScopeID, CandidateID: c.ID, ConfigDigest: f.PathHealth.Digest(), CandidateDigest: c.Digest, Epoch: "view-process", Sequence: uint64(i + 1), ObservedAt: at, Health: "up", Result: result, RTTMillis: 1})
			}
			receipt := configReceipt(d, "health-view", "health-state", 2, at)
			if _, err = s.RecordNodeReport(ctx, ns[0].ID, configTestToken, agentproto.Report{Version: "v0.85.0", ConfigReceipt: receipt, AppliedRevision: receipt.StateRevision, PathHealth: &agentproto.PathHealthReport{Supported: true, Samples: samples}}, false, at); err != nil {
				t.Fatal(err)
			}
			now := at
			if !failure {
				now = at.Add(91 * time.Second)
			}
			view, err := s.PathHealth(ctx, route.ID, now)
			if err != nil {
				t.Fatal(err)
			}
			if failure {
				if view.Status != "degraded" || !view.Candidates[0].Eligible {
					t.Fatalf("healthy hysteresis candidates with a recent failure must remain visibly degraded and eligible: %+v", view)
				}
			} else if view.Status != "unknown" || view.Candidates[0].FreshForMS != 0 || view.Candidates[0].Eligible || view.Candidates[0].ObservedAt == nil || !view.Candidates[0].ObservedAt.Equal(at) {
				t.Fatalf("expired sample must retain its original observation time without retaining health: %+v", view)
			}
		})
	}
}

func TestPathHealthRuntimePauseExpiresButDesiredPauseRemains(t *testing.T) {
	ctx := context.Background()
	s, ns, ib, route := pathFixture(t)
	ib.Protocol = spec.HTTP
	if err := s.UpdateInbound(ctx, ib); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Millisecond)
	pathHealthCapabilityFixture(t, s, ns[0].ID, at)
	route.Health = &PathRouteHealth{Enabled: true}
	if err := s.SavePathRoute(ctx, route); err != nil {
		t.Fatal(err)
	}
	f := pathOwned(t, s, ns[0].ID, route.ID)[0]
	d, err := s.PrepareNodeConfigDeliveryForLines(ctx, ns[0].ID, configTestToken, configSessionA, "pause-config", "pause-state", []spec.Forward{f.Forward}, at)
	if err != nil {
		t.Fatal(err)
	}
	push := func(seq uint64, when time.Time) {
		t.Helper()
		samples := []agentproto.PathHealthSample{}
		for i, c := range f.PathHealth.Candidates {
			samples = append(samples, agentproto.PathHealthSample{Tag: f.Tag, RouteID: route.ID, ScopeID: f.PathHealth.ScopeID, CandidateID: c.ID, ConfigDigest: f.PathHealth.Digest(), CandidateDigest: c.Digest, Epoch: "pause-view", Sequence: seq*10 + uint64(i), ObservedAt: when, Health: "up", Result: "success"})
		}
		receipt := configReceipt(d, "pause-config", "pause-state", seq, when)
		if _, e := s.RecordNodeReport(ctx, ns[0].ID, configTestToken, agentproto.Report{Version: "v0.85.0", ConfigReceipt: receipt, AppliedRevision: receipt.StateRevision, PathHealth: &agentproto.PathHealthReport{Supported: true, Samples: samples}}, false, when); e != nil {
			t.Fatal(e)
		}
	}
	push(2, at)
	if _, err = s.RecordNodeReport(ctx, ns[0].ID, configTestToken, agentproto.Report{ServiceControl: &agentproto.ServiceControlReport{Mode: spec.ServiceControlPause}}, true, at); err != nil {
		t.Fatal(err)
	}
	if v, e := s.PathHealth(ctx, route.ID, at); e != nil || v.Status != "paused" {
		t.Fatal(v, e)
	}
	later := at.Add(181 * time.Second)
	push(3, later)
	if v, e := s.PathHealth(ctx, route.ID, later); e != nil || v.Status != "healthy" {
		t.Fatal("old runtime pause suppressed new handshake", v, e)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	record, err := serviceControlTx(ctx, tx, ns[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	old := record
	record.state.Manual = ServiceControlManual{Mode: spec.ServiceControlPause}
	if err = saveServiceControlTx(ctx, tx, ns[0].ID, &record, old); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if v, e := s.PathHealth(ctx, route.ID, later); e != nil || v.Status != "paused" {
		t.Fatal("desired pause expired", v, e)
	}
}
