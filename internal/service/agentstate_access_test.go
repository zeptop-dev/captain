package service

import (
	"testing"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestAgentStateExplicitAccessAndWireGuardPeers(t *testing.T) {
	r := newRuntimeRig(t)
	n := r.node("access", "192.0.2.10")
	cores := map[string]agentproto.CoreStatus{"xray": {Capabilities: &spec.CoreCapabilities{WireGuardDevices: true}}}
	if err := r.store.TouchNode(r.ctx, n.ID, "v0.82.0", "", spec.SystemStatus{}, cores, nil); err != nil {
		t.Fatal(err)
	}
	ib := r.inbound(n, "scoped-wg")
	ib.Protocol = spec.WireGuard
	if err := r.store.UpdateInbound(r.ctx, ib); err != nil {
		t.Fatal(err)
	}
	groups := []int64{}
	for _, name := range []string{"basic", "premium", "unrelated"} {
		g, err := r.store.CreateGroup(r.ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		groups = append(groups, g)
		p := &domain.Plan{Name: name, PeriodDays: 30, QuotaBytes: 10000, GroupID: &g, Enabled: true}
		if err = r.store.CreatePlan(r.ctx, p); err != nil {
			t.Fatal(err)
		}
		u := &domain.User{Email: name + "@example.com", UUID: name, SubToken: name, Status: "active"}
		if err = r.store.CreateUser(r.ctx, u); err != nil {
			t.Fatal(err)
		}
		if _, err = r.store.GrantSubscriptionMode(r.ctx, u.ID, p, r.at, store.GrantStack); err != nil {
			t.Fatal(err)
		}
		if _, err = r.store.CreateWireGuardDevice(r.ctx, u.ID, "phone", 4); err != nil {
			t.Fatal(err)
		}
	}
	set := func(policy string, selected []int64) {
		t.Helper()
		catalog, err := r.store.AccessCatalog(r.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err = r.store.UpdateInboundAccess(r.ctx, ib.ID, domain.InboundAccessChange{InboundID: ib.ID, Policy: policy, GroupIDs: selected, Revision: catalog.Revision}); err != nil {
			t.Fatal(err)
		}
	}
	set(domain.AccessGroups, groups[:2])
	st, err := r.state.Build(r.ctx, n, r.at)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Node.Inbounds) != 1 || !st.Node.Inbounds[0].ScopedUsers || len(st.Node.Inbounds[0].Users) != 2 || len(st.Users) != 2 {
		t.Fatalf("scoped state users=%d inbounds=%+v", len(st.Users), st.Node.Inbounds)
	}
	for _, u := range st.Node.Inbounds[0].Users {
		if len(u.WireGuardPeers) != 1 {
			t.Fatalf("authorized device peers missing: %d", len(u.WireGuardPeers))
		}
	}
	for _, u := range st.Users {
		if len(u.WireGuardPeers) != 0 {
			t.Fatal("device keys escaped listener scope")
		}
	}
	set(domain.AccessNone, nil)
	closed, err := r.state.Build(r.ctx, n, r.at)
	if err != nil {
		t.Fatal(err)
	}
	if len(closed.Node.Inbounds) != 1 || !closed.Node.Inbounds[0].ScopedUsers || len(closed.Node.Inbounds[0].Users) != 0 || len(closed.Users) != 0 {
		t.Fatal("none policy fell back to node credentials")
	}
	if closed.Revision == st.Revision {
		t.Fatal("access change did not revise delivered state")
	}
	set(domain.AccessAll, nil)
	open, err := r.state.Build(r.ctx, n, r.at)
	if err != nil {
		t.Fatal(err)
	}
	if len(open.Users) != 3 || len(open.Node.Inbounds[0].Users) != 3 {
		t.Fatal("all policy did not restore eligible customers")
	}
}
