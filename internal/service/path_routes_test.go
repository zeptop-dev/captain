package service

import (
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestServiceStatusCompletePathRoutesUseListenerAdjacency(t *testing.T) {
	r := newRuntimeRig(t)
	a := r.node("entry", "192.0.2.10")
	b := r.node("B", "198.51.100.20")
	c := r.node("shared", "198.51.100.21")
	d := r.node("landing", "203.0.113.30")
	ib := r.inbound(d, "landing")
	long := &store.ForwardPath{Name: "B then shared", Steps: []store.PathStep{{NodeID: b.ID}, {NodeID: c.ID}}}
	short := &store.ForwardPath{Name: "shared only", Steps: []store.PathStep{{NodeID: c.ID}}}
	for _, p := range []*store.ForwardPath{long, short} {
		if err := r.store.SaveForwardPath(r.ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	route := &store.PathRoute{Name: "Complete paths", Entry: store.LineMember{NodeID: a.ID, Port: 22000}, TargetInboundID: ib.ID, Branches: []store.PathBranch{{PathID: long.ID, PathRevision: 1, Ports: []int{22001, 22002}}, {PathID: short.ID, PathRevision: 1, Ports: []int{22003}}}}
	if err := r.store.SavePathRoute(r.ctx, route); err != nil {
		t.Fatal(err)
	}
	entry := &domain.Entry{Name: "Published route", LineID: &route.ID, LineMemberID: route.Entry.ID, Enabled: true, Rate: 1}
	if err := r.store.CreateEntry(r.ctx, entry); err != nil {
		t.Fatal(err)
	}
	for _, n := range []*domain.Node{a, b, c, d} {
		r.report(n, nil)
	}
	assertRuntime(t, r.evaluate(entry)[entry.ID], "healthy", "healthy")
	forwards, err := r.store.NodeForwards(r.ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	var longTag string
	for _, f := range forwards {
		if f.Port == 22002 {
			longTag = f.Tag
		}
	}
	broken := func(rep *agentproto.Report) {
		for i := range rep.Forwards {
			if rep.Forwards[i].Tag == longTag {
				rep.Forwards[i].Health = "down"
				rep.Forwards[i].Up = false
			}
		}
	}
	for i := 0; i < 2; i++ {
		r.at = r.at.Add(time.Second)
		r.report(c, broken)
		r.evaluate(entry)
	}
	assertRuntime(t, r.evaluate(entry)[entry.ID], "degraded", "partial_failure")
}
