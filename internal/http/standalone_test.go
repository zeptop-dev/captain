package http

import (
	"context"
	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
	"testing"
)

func TestStandaloneHTTPAccessAndDowngradedFirstPull(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	payload := map[string]any{"Tag": "independent", "Protocol": "socks", "Port": 1080, "Settings": map[string]any{"standalone": map[string]any{"username": "operator", "password": "secret"}}}
	accessAuditCall(t, r.c, "POST", "/api/admin/nodes/"+itoa(r.nodeID)+"/inbounds", payload, 400)
	caps := spec.CapabilitiesForCore("singbox")
	if err := r.st.TouchNode(ctx, r.nodeID, "dev", "", spec.SystemStatus{}, map[string]agentproto.CoreStatus{"singbox": {Capabilities: &caps}}, nil); err != nil {
		t.Fatal(err)
	}
	narrow := accessAuditToken(t, r, 1, "full", []string{"nodes:write"})
	accessAuditCall(t, narrow, "POST", "/api/admin/nodes/"+itoa(r.nodeID)+"/inbounds", payload, 403)
	ib := mustJSON[domain.Inbound](t, accessAuditCall(t, r.c, "POST", "/api/admin/nodes/"+itoa(r.nodeID)+"/inbounds", payload, 200))
	accessAuditCall(t, r.c, "POST", "/api/admin/entries", map[string]any{"Name": "Forbidden", "InboundID": ib.ID, "DisplayHost": "example.com", "DisplayPort": 1080}, 409)
	// The old process does not send the new header, despite a previous new report.
	code, _, _ := r.agent.do("GET", "/api/agent/state", nil, nil)
	if code != 409 {
		t.Fatal("downgraded first pull was not fenced", code)
	}
	code, raw, _ := r.agent.do("GET", "/api/agent/state", nil, map[string]string{agentproto.StandaloneAuthHeader: agentproto.StandaloneAuthVersion})
	if code != 200 {
		t.Fatal("capable process could not fetch", code, string(raw))
	}
	st := mustJSON[agentproto.State](t, raw)
	found := false
	for _, in := range st.Node.Inbounds {
		if in.Tag == "independent" {
			found = in.Standalone != nil && len(in.Users) == 0
		}
	}
	if !found {
		t.Fatal("independent credentials absent")
	}
}
