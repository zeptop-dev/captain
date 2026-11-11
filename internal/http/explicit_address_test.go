package http

import (
	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/store"
	"testing"
)

func TestExplicitIngressForwardAPIReachesAgentState(t *testing.T) {
	r := newRig(t)
	call := func(method, path string, body any, want int) []byte {
		t.Helper()
		code, b, _ := r.c.do(method, path, body, nil)
		if code != want {
			t.Fatalf("%s %s: %d %s", method, path, code, b)
		}
		return b
	}
	n := mustJSON[map[string]any](t, call("POST", "/api/admin/nodes", map[string]any{"Name": "Landing", "PublicAddr": "192.0.2.10"}, 200))
	id := int64(n["id"].(float64))
	base := "/api/admin/nodes/" + itoa(id)
	g := mustJSON[struct {
		Ingress store.Ingress `json:"ingress"`
	}](t, call("POST", base+"/ingresses", map[string]any{"Name": "Path", "Kind": "iplc", "LineIP": "198.51.100.20", "EntryHost": "203.0.113.30", "PortFrom": 1443, "PortTo": 1443, "PortOffset": 10000}, 200)).Ingress
	ib := mustJSON[struct{ ID int64 }](t, call("POST", base+"/inbounds", map[string]any{"Tag": "line-user", "Protocol": "vless", "Port": 1443, "IngressID": g.ID}, 200))
	f := store.NodeForward{Forward: spec.Forward{Tag: "explicit", Protocol: "tcp", Port: 18443, Target: "invalid.example.com:1"}, InboundID: ib.ID}
	for _, tc := range []struct{ mode, want string }{{"line", "198.51.100.20:1443"}, {"public", "203.0.113.30:11443"}} {
		f.TargetAddressMode = tc.mode
		call("PUT", "/api/admin/nodes/"+itoa(r.nodeID)+"/forwards", map[string]any{"Forwards": []store.NodeForward{f}}, 200)
		code, b, _ := r.agent.do("GET", "/api/agent/state", nil, nil)
		if code != 200 {
			t.Fatal(code, string(b))
		}
		state := mustJSON[agentproto.State](t, b)
		if len(state.Forwards) != 1 || state.Forwards[0].Target != tc.want || state.Node.AllowPrivateDest || len(state.Node.EgressUpstreams) != 0 {
			t.Fatalf("%+v", state.Forwards)
		}
	}
	// A managed path requires its chosen ingress; no server-public fallback.
	plain := mustJSON[struct{ ID int64 }](t, call("POST", base+"/inbounds", map[string]any{"Tag": "plain", "Protocol": "vless", "Port": 2443}, 200))
	f.InboundID = plain.ID
	f.TargetAddressMode = "line"
	call("PUT", "/api/admin/nodes/"+itoa(r.nodeID)+"/forwards", map[string]any{"Forwards": []store.NodeForward{f}}, 400)
}
