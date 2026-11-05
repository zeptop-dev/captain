package http

import (
	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
	"testing"
)

func TestNodeProbePlanHTTPAuthorizationAndDelivery(t *testing.T) {
	r := newRig(t)
	getPath := "/api/admin/nodes/" + itoa(r.nodeID) + "/probe-plan"
	putPath := "/api/admin/settings/nodes/" + itoa(r.nodeID) + "/probe-plan"
	get := func() store.NodeProbePlanView {
		t.Helper()
		return mustJSON[store.NodeProbePlanView](t, accessAuditCall(t, r.c, "GET", getPath, nil, 200))
	}
	p := get()
	change := store.NodeProbePlanChange{Revision: p.Revision, Mode: "custom", Custom: &spec.CarrierProbeConfig{Protocol: "icmp", Family: "ipv4"}}
	for _, role := range []string{domain.RoleOperator, domain.RoleSupport} {
		_, c := accessAuditStaff(t, r, role)
		accessAuditCall(t, c, "GET", getPath, nil, 403)
		accessAuditCall(t, c, "PUT", putPath, change, 403)
	}
	anon := &client{t: t, srv: r.srv}
	accessAuditCall(t, anon, "GET", getPath, nil, 401)
	accessAuditCall(t, anon, "PUT", putPath, change, 401)
	for _, scope := range [][]string{{"nodes:*"}, {"settings:*"}, {"monitoring:*"}} {
		c := accessAuditToken(t, r, 1, "full", scope)
		accessAuditCall(t, c, "GET", getPath, nil, 403)
		accessAuditCall(t, c, "PUT", putPath, change, 403)
	}
	reader := accessAuditToken(t, r, 1, "read", []string{"node-probe-plan:*"})
	accessAuditCall(t, reader, "GET", getPath, nil, 200)
	accessAuditCall(t, reader, "PUT", putPath, change, 403)
	writer := accessAuditToken(t, r, 1, "full", []string{"node-probe-plan:*"})
	accessAuditCall(t, writer, "PUT", putPath, change, 200)
	accessAuditCall(t, writer, "PUT", putPath, change, 409)
	p = get()
	if p.Mode != "custom" || p.Custom.Protocol != "icmp" {
		t.Fatal(p)
	}
	accessAuditCall(t, r.c, "PUT", "/api/admin/settings/probe", map[string]any{"enabled": true, "carrier_ping": false}, 200)
	state := mustJSON[agentproto.State](t, accessAuditCall(t, r.agent, "GET", "/api/agent/state", nil, 200))
	if state.Probe == nil || state.Probe.CarrierProbe == nil || state.Probe.CarrierProbe.Protocol != "icmp" || state.Probe.CarrierPing {
		t.Fatal("custom was suppressed by global default", state.Probe)
	}
	accessAuditCall(t, r.c, "PUT", "/api/admin/nodes/"+itoa(r.nodeID)+"/probe", map[string]any{"LimitBytes": 1000, "ResetDay": 1, "Mode": "sum"}, 200)
	if now := get(); now.Revision != p.Revision || now.Mode != "custom" {
		t.Fatal("old editor cleared custom", now)
	}
	p = mustJSON[store.NodeProbePlanView](t, accessAuditCall(t, writer, "PUT", putPath, store.NodeProbePlanChange{Revision: p.Revision, Mode: "disabled"}, 200))
	state = mustJSON[agentproto.State](t, accessAuditCall(t, r.agent, "GET", "/api/agent/state", nil, 200))
	if state.Probe == nil || !state.Probe.Enabled || state.Probe.CarrierProbe != nil {
		t.Fatal("disabled carrier affected resources", state.Probe)
	}
	p = mustJSON[store.NodeProbePlanView](t, accessAuditCall(t, writer, "PUT", putPath, store.NodeProbePlanChange{Revision: p.Revision, Mode: "custom"}, 200))
	accessAuditCall(t, r.c, "PUT", "/api/admin/settings/probe", map[string]any{"enabled": false}, 200)
	if p = get(); p.Effective.Enabled || p.MonitoringEnabled {
		t.Fatal("global master gate bypassed", p)
	}
	accessAuditCall(t, r.c, "GET", "/api/admin/nodes/999999/probe-plan", nil, 404)
	accessAuditCall(t, r.c, "PUT", putPath, store.NodeProbePlanChange{Revision: p.Revision, Mode: "bogus"}, 400)
}
