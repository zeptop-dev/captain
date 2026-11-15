package service

import (
	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
	"testing"
)

func TestStandaloneDeliveryRequiresCurrentAdapterAndNoUsers(t *testing.T) {
	r := newRuntimeRig(t)
	n := r.node("standalone", "192.0.2.10")
	ib := &domain.Inbound{NodeID: n.ID, Tag: "standalone", Protocol: spec.SOCKS, Port: 1080, Enabled: true, Settings: spec.Inbound{Standalone: &spec.StandaloneAuth{Username: "operator", Password: "secret"}}}
	if err := r.store.CreateInbound(r.ctx, ib); err != nil {
		t.Fatal(err)
	}
	set := func(enabled bool) {
		caps := spec.CapabilitiesForCore("singbox")
		caps.StandaloneAuth = enabled
		if err := r.store.TouchNode(r.ctx, n.ID, "dev", "", spec.SystemStatus{}, map[string]agentproto.CoreStatus{"singbox": {Capabilities: &caps}}, nil); err != nil {
			t.Fatal(err)
		}
		var err error
		n, err = r.store.NodeByID(r.ctx, n.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	set(true)
	st, err := r.state.Build(r.ctx, n, r.at)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Node.Inbounds) != 1 || st.Node.Inbounds[0].Standalone == nil || len(st.Node.Inbounds[0].Users) != 0 || len(st.Users) != 0 {
		t.Fatal("standalone state lost auth or included customer accounts")
	}
	set(false)
	st, err = r.state.Build(r.ctx, n, r.at)
	if err != nil || len(st.Node.Inbounds) != 0 {
		t.Fatal("unsupported adapter received standalone listener", err)
	}
}
