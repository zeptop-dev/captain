package http

import (
	"bytes"
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

func serviceControlHTTPGet(t *testing.T, r *rig) store.NodeServiceControl {
	t.Helper()
	return mustJSON[store.NodeServiceControl](t, accessAuditCall(t, r.c, "GET", "/api/admin/nodes/"+itoa(r.nodeID)+"/service-control", nil, 200))
}
func serviceControlHTTPReport(t *testing.T, r *rig, seq uint64, control *agentproto.ServiceControlReport) {
	t.Helper()
	accessAuditCall(t, r.agent, "POST", "/api/agent/report", agentproto.Report{Version: "test", TrafficEpoch: "0123456789abcdef0123456789abcdef", TrafficSeq: seq, ServiceControl: control}, 200)
}
func serviceControlHTTPReady(t *testing.T, r *rig) {
	t.Helper()
	accessAuditCall(t, r.c, "PUT", "/api/admin/settings/probe", map[string]any{"enabled": true, "page_enabled": true, "visibility": "public", "public_sections": []string{"traffic", "info"}}, 200)
	accessAuditCall(t, r.c, "PUT", "/api/admin/nodes/"+itoa(r.nodeID)+"/probe", map[string]any{"LimitBytes": 1000, "ResetDay": 1, "Mode": "sum"}, 200)
	serviceControlHTTPReport(t, r, 1, &agentproto.ServiceControlReport{Supported: true, CheckedAt: time.Now().Unix(), Mode: "normal"})
	for i := uint64(1); i <= 2; i++ {
		accessAuditCall(t, r.agent, "POST", "/api/agent/beat", agentproto.Beat{Version: "test", Host: spec.SystemStatus{Resources: &spec.Resources{Epoch: "test", Sequence: i, Networks: []spec.NetworkResource{{Name: "eth0", ID: "boot:1", Included: true, Up: i * 100, Down: i * 100}}}}}, 204)
	}
}
func TestNodeServiceControlHTTPAuthorizationAndScopes(t *testing.T) {
	r := newRig(t)
	path := "/api/admin/nodes/" + itoa(r.nodeID) + "/service-control"
	v := serviceControlHTTPGet(t, r)
	change := store.ServiceControlChange{Revision: v.Revision, Manual: &store.ServiceControlManual{Mode: "normal"}}
	for _, role := range []string{domain.RoleOperator, domain.RoleSupport} {
		u, c := accessAuditStaff(t, r, role)
		accessAuditCall(t, c, "GET", path, nil, 403)
		accessAuditCall(t, c, "PUT", path, change, 403)
		accessAuditCall(t, accessAuditToken(t, r, u.ID, "full", []string{"*"}), "PUT", path, change, 403)
	}
	anon := &client{t: t, srv: r.srv}
	accessAuditCall(t, anon, "GET", path, nil, 401)
	accessAuditCall(t, anon, "PUT", path, change, 401)
	for _, scope := range [][]string{{"nodes:*"}, {"settings:*"}, {"entries:*"}} {
		c := accessAuditToken(t, r, 1, "full", scope)
		accessAuditCall(t, c, "GET", path, nil, 403)
		accessAuditCall(t, c, "PUT", path, change, 403)
	}
	reader := accessAuditToken(t, r, 1, "read", []string{"node-service-control:*"})
	accessAuditCall(t, reader, "GET", path, nil, 200)
	accessAuditCall(t, reader, "PUT", path, change, 403)
	writer := accessAuditToken(t, r, 1, "full", []string{"node-service-control:*"})
	accessAuditCall(t, writer, "PUT", path, change, 200)
	if code, _, _ := r.c.do("PUT", path, change, map[string]string{"Origin": "https://other.example.com"}); code != 403 {
		t.Fatal("cross-origin control write", code)
	}
	accessAuditCall(t, writer, "PUT", path, store.ServiceControlChange{Revision: v.Revision, Manual: &store.ServiceControlManual{Mode: "pause"}}, 400) // old agents explicitly rejected
}
func TestNodeServiceControlHTTPDesiredAppliedAndPrivate(t *testing.T) {
	r := newRig(t)
	path := "/api/admin/nodes/" + itoa(r.nodeID) + "/service-control"
	serviceControlHTTPReady(t, r)
	before := mustJSON[agentproto.State](t, accessAuditCall(t, r.agent, "GET", "/api/agent/state", nil, 200))
	v := serviceControlHTTPGet(t, r)
	pause := store.ServiceControlChange{Revision: v.Revision, Manual: &store.ServiceControlManual{Mode: "pause"}}
	v = mustJSON[store.NodeServiceControl](t, accessAuditCall(t, r.c, "PUT", path, pause, 200))
	if v.Runtime.Status != "pending" {
		t.Fatal("save claimed applied", v)
	}
	state := mustJSON[agentproto.State](t, accessAuditCall(t, r.agent, "GET", "/api/agent/state", nil, 200))
	if state.Node.ServiceControl == nil || state.Node.ServiceControl.Mode != "pause" || state.Node.ServiceControl.Revision != v.Effective.Revision || state.Revision == before.Revision {
		t.Fatal("control not delivered", state)
	}
	if !reflect.DeepEqual(state.Node.Inbounds, before.Node.Inbounds) || !reflect.DeepEqual(state.Users, before.Users) || !reflect.DeepEqual(state.Forwards, before.Forwards) {
		t.Fatal("control destroyed listener/authorization state")
	}
	serviceControlHTTPReport(t, r, 2, &agentproto.ServiceControlReport{Supported: true, CheckedAt: time.Now().Unix(), Mode: "pause", DesiredRevision: v.Effective.Revision, AppliedRevision: v.Effective.Revision})
	if got := serviceControlHTTPGet(t, r); got.Runtime.Status != "applied" {
		t.Fatal("actual independent confirmation lost", got)
	}
	// Receipt retry cannot lend a fresh outer timestamp to a changed payload.
	serviceControlHTTPReport(t, r, 2, &agentproto.ServiceControlReport{Supported: true, CheckedAt: time.Now().Unix(), Mode: "normal", Error: "forged retry"})
	if got := serviceControlHTTPGet(t, r); got.Runtime.Status != "applied" {
		t.Fatal("receipt retry replaced runtime", got)
	}
	anon := &client{t: t, srv: r.srv}
	public := accessAuditCall(t, anon, "GET", "/api/probe", nil, 200)
	for _, private := range []string{"service_control", "threshold_percent", "exempt_current_period", v.Revision, v.Effective.Revision} {
		if bytes.Contains(public, []byte(private)) {
			t.Fatal("private control leaked", private)
		}
	}
	accessAuditCall(t, r.c, "PUT", path, pause, 409)
	// Restoring normal uses a distinct command; it never re-enables a saved disabled listener.
	restored := mustJSON[store.NodeServiceControl](t, accessAuditCall(t, r.c, "PUT", path, store.ServiceControlChange{Revision: v.Revision, Manual: &store.ServiceControlManual{Mode: "normal"}}, 200))
	if restored.Effective.Revision == v.Effective.Revision || restored.Runtime.Status != "pending" {
		t.Fatal(restored)
	}
}
func TestNodeServiceControlHTTPAutomaticBoundariesAndLegacyProbe(t *testing.T) {
	r := newRig(t)
	path := "/api/admin/nodes/" + itoa(r.nodeID) + "/service-control"
	serviceControlHTTPReady(t, r)
	v := serviceControlHTTPGet(t, r)
	policy := store.ServiceControlPolicy{Action: "pause", ThresholdPercent: 90, AutoResume: true}
	v = mustJSON[store.NodeServiceControl](t, accessAuditCall(t, r.c, "PUT", path, store.ServiceControlChange{Revision: v.Revision, Policy: &policy}, 200))
	if v.Automatic.Active {
		t.Fatal(v)
	}
	probePath := "/api/admin/nodes/" + itoa(r.nodeID) + "/probe"
	echo := map[string]any{"LimitBytes": 1000, "ResetDay": 1, "Mode": "sum", "Info": map[string]string{"note": "still editable"}}
	accessAuditCall(t, r.c, "PUT", probePath, echo, 200)
	if got := serviceControlHTTPGet(t, r); got.Policy != policy || got.Revision != v.Revision {
		t.Fatal("legacy probe save changed policy", got)
	}
	restricted := accessAuditToken(t, r, 1, "full", []string{"nodes:*"})
	accessAuditCall(t, restricted, "PUT", probePath, echo, 200) // unrelated information remains editable
	accessAuditCall(t, restricted, "POST", probePath+"/reset-traffic", nil, 403)
	echo["LimitBytes"] = 10000
	accessAuditCall(t, restricted, "PUT", probePath, echo, 403)
	accessAuditCall(t, r.c, "PUT", "/api/admin/settings/probe", map[string]any{"enabled": false}, 409)
	echo["LimitBytes"] = 0
	accessAuditCall(t, r.c, "PUT", probePath, echo, 400)
	// The existing report interval and user billing ledger remain independent.
	accessAuditCall(t, r.agent, "POST", "/api/agent/beat", agentproto.Beat{Version: "test", Host: spec.SystemStatus{Resources: &spec.Resources{Epoch: "test", Sequence: 3, Networks: []spec.NetworkResource{{Name: "eth0", ID: "boot:1", Included: true, Up: 600, Down: 600}}}}}, 204)
	got := serviceControlHTTPGet(t, r)
	if !got.Automatic.Active || got.Effective.Mode != "pause" {
		t.Fatal("beat did not trigger automatic protection", got)
	}
	state := mustJSON[agentproto.State](t, accessAuditCall(t, r.agent, "GET", "/api/agent/state", nil, 200))
	if state.Node.ServiceControl == nil || state.Node.ServiceControl.Mode != "pause" {
		t.Fatal("beat left cached agent state unchanged", state)
	}
	if err := r.st.SetNodeMode(context.Background(), r.nodeID, spec.NodeMonitor, "", 0); err == nil {
		t.Fatal("monitor switch bypassed protection")
	}
}
