package store

import (
	"context"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
)

func TestPathHealthEvidenceRejectsStaleReplayIdentityAndDraft(t *testing.T) {
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
	d, err := s.PrepareNodeConfigDeliveryForLines(ctx, ns[0].ID, configTestToken, configSessionA, "health-config", "health-state", []spec.Forward{f.Forward}, at)
	if err != nil {
		t.Fatal(err)
	}
	samples := []agentproto.PathHealthSample{}
	for i, c := range f.PathHealth.Candidates {
		samples = append(samples, agentproto.PathHealthSample{Tag: f.Tag, RouteID: route.ID, ScopeID: f.PathHealth.ScopeID, CandidateID: c.ID, ConfigDigest: f.PathHealth.Digest(), CandidateDigest: c.Digest, Epoch: "first-process", Sequence: uint64(i + 1), ObservedAt: at, Health: "up", Result: "success", RTTMillis: 1})
	}
	seq := uint64(1)
	push := func(sample []agentproto.PathHealthSample, observed time.Time) {
		t.Helper()
		seq++
		r := configReceipt(d, "health-config", "health-state", seq, observed)
		_, err := s.RecordNodeReport(ctx, ns[0].ID, configTestToken, agentproto.Report{Version: "v0.85.0", ConfigReceipt: r, AppliedRevision: r.StateRevision, PathHealth: &agentproto.PathHealthReport{Supported: true, Samples: sample}}, false, observed)
		if err != nil {
			t.Fatal(err)
		}
	}
	view := func(when time.Time) *PathHealthView {
		t.Helper()
		v, e := s.PathHealth(ctx, route.ID, when)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	push(samples, at)
	if v := view(at); v.Status != "healthy" || len(v.Candidates) != 2 || v.Candidates[0].FreshForMS != 90000 {
		t.Fatal("accepted sample not shown", v)
	}
	push(samples, at.Add(time.Minute))
	if v := view(at.Add(91 * time.Second)); v.Status != "unknown" {
		t.Fatal("fresh report renewed old probe", v)
	}
	bad := append([]agentproto.PathHealthSample(nil), samples...)
	for i := range bad {
		bad[i].Sequence = uint64(i + 100)
		bad[i].ObservedAt = at.Add(100 * time.Second)
		bad[i].ConfigDigest = "draft-config"
	}
	push(bad, at.Add(100*time.Second))
	if v := view(at.Add(100 * time.Second)); v.Status != "unknown" {
		t.Fatal("draft digest accepted", v)
	}
	for i := range samples {
		samples[i].Sequence = uint64(i + 10)
		samples[i].ObservedAt = at.Add(110 * time.Second)
		samples[i].Health = "down"
		samples[i].Result = "failure"
	}
	push(samples, at.Add(110*time.Second))
	if v := view(at.Add(110 * time.Second)); v.Status != "down" {
		t.Fatal(v)
	}
	for i := range bad {
		bad[i] = samples[i]
		bad[i].Health = "up"
		bad[i].Result = "success"
		bad[i].Sequence = 1
		bad[i].ObservedAt = at.Add(115 * time.Second)
	}
	push(bad, at.Add(115*time.Second))
	if v := view(at.Add(115 * time.Second)); v.Status != "down" {
		t.Fatal("replayed sequence renewed green", v)
	}
	for i := range bad {
		bad[i].Epoch = "different-manager"
		bad[i].Sequence = 100
		bad[i].ObservedAt = at.Add(120 * time.Second)
	}
	push(bad, at.Add(120*time.Second))
	if v := view(at.Add(120 * time.Second)); v.Status != "down" {
		t.Fatal("same-session epoch reset accepted", v)
	}
	// A restarted current process cannot borrow the retired process samples.
	d, err = s.PrepareNodeConfigDeliveryForLines(ctx, ns[0].ID, configTestToken, configSessionB, "health-config", "health-state", []spec.Forward{f.Forward}, at.Add(125*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	seq = 0
	push(nil, at.Add(126*time.Second))
	if v := view(at.Add(126 * time.Second)); v.Status != "unknown" {
		t.Fatal("new process borrowed old evidence", v)
	}
	if _, err = s.db.Exec(`UPDATE nodes SET token_hash='repaired-identity' WHERE id=?`, ns[0].ID); err != nil {
		t.Fatal(err)
	}
	if v := view(at.Add(127 * time.Second)); v.Capable || v.Status != "unknown" {
		t.Fatal("repaired identity borrowed capability", v)
	}
}
