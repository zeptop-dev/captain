package service

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestAgentStateRolloutKeepsCurrentAuthorizationJobsAndControl(t *testing.T) {
	r := newRuntimeRig(t)
	a := r.node("entry", "192.0.2.10")
	b := r.node("relay", "198.51.100.20")
	c := r.node("landing", "203.0.113.30")
	local := r.inbound(a, "local-proxy")
	landing := r.inbound(c, "landing-proxy")
	caps := spec.CapabilitiesForCore("xray")
	for _, n := range []*domain.Node{a, b, c} {
		if err := r.store.TouchNode(r.ctx, n.ID, "v0.83.0", "", spec.SystemStatus{}, map[string]agentproto.CoreStatus{"xray": {Capabilities: &caps}}, nil); err != nil {
			t.Fatal(err)
		}
	}
	user := &domain.User{Email: "rollout@example.com", UUID: "rollout-user", SubToken: "rollout-sub", Status: "active"}
	if err := r.store.CreateUser(r.ctx, user); err != nil {
		t.Fatal(err)
	}
	plan := &domain.Plan{Name: "current authorization", PeriodDays: 30, QuotaBytes: 1 << 30, Enabled: true}
	if err := r.store.CreatePlan(r.ctx, plan); err != nil {
		t.Fatal(err)
	}
	if _, err := r.store.GrantSubscriptionMode(r.ctx, user.ID, plan, r.at, store.GrantStack); err != nil {
		t.Fatal(err)
	}
	for _, n := range []*domain.Node{a, b, c} {
		st, err := r.state.Build(r.ctx, n, r.at)
		if err != nil {
			t.Fatal(err)
		}
		token := "token-" + n.Name
		d, err := r.store.PrepareNodeConfigDeliveryForLines(r.ctx, n.ID, token, "11111111111111111111111111111111", st.ConfigRevision, st.Revision, st.Forwards, r.at)
		if err != nil {
			t.Fatal(err)
		}
		report := agentproto.Report{Version: "v0.83.0", AppliedRevision: st.Revision, TrafficEpoch: "rollout", TrafficSeq: 1, ConfigReceipt: &agentproto.ConfigReceipt{DeliveryID: d.ID, Session: d.Session, ConfigRevision: st.ConfigRevision, StateRevision: st.Revision, Sequence: 1, ObservedAt: r.at, Applied: true}, ServiceControl: &agentproto.ServiceControlReport{Supported: true, Mode: "normal", CheckedAt: r.at.Unix()}, Cores: map[string]agentproto.CoreStatus{"xray": {Capabilities: &caps}}}
		if _, err = r.store.RecordNodeReport(r.ctx, n.ID, token, report, true, r.at); err != nil {
			t.Fatal(err)
		}
	}
	line := &store.ForwardLine{Name: "staged", Protocol: "tcp", Backend: "relay", TargetInboundID: landing.ID, Groups: []store.LineGroup{{Strategy: "failover", Members: []store.LineMember{{NodeID: a.ID, Port: 18443}}}, {Strategy: "failover", Members: []store.LineMember{{NodeID: b.ID, Port: 19443}}}}}
	rollout, err := r.store.PlanLineRollout(r.ctx, line, false, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.store.PauseLineRollout(r.ctx, rollout.ID, rollout.Version); err != nil {
		t.Fatal(err)
	}
	before, err := r.state.Build(r.ctx, a, r.at)
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Users) != 1 || len(before.Forwards) != 0 || len(before.Node.Inbounds) != 1 || before.Node.Inbounds[0].Tag != local.Tag {
		t.Fatal("unexpected staged entry state", before)
	}
	if err = r.store.UpdateUser(r.ctx, user.ID, "banned", nil, ""); err != nil {
		t.Fatal(err)
	}
	if err = r.store.CreateNodeJob(r.ctx, "new-live-job", a.ID, "doctor", json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	control, err := r.store.NodeServiceControl(r.ctx, a.ID, r.at)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.store.UpdateNodeServiceControl(r.ctx, a.ID, store.ServiceControlChange{Revision: control.Revision, Manual: &store.ServiceControlManual{Mode: "pause"}}, r.at); err != nil {
		t.Fatal(err)
	}
	after, err := r.state.Build(r.ctx, a, r.at)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Users) != 0 || len(after.Node.Inbounds[0].Users) != 0 || len(after.Jobs) != 1 || after.Jobs[0].ID != "new-live-job" || after.Node.ServiceControl == nil || after.Node.ServiceControl.Mode != "pause" {
		t.Fatal("rollout froze dynamic security or Jobs", after)
	}
	if after.Revision == before.Revision || !reflect.DeepEqual(after.Forwards, before.Forwards) {
		t.Fatal("dynamic changes altered held forwarding or failed to revise State")
	}
}
