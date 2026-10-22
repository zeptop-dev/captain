package http

import (
	"context"
	"testing"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/http/admin"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestInternalForwardAPIAndCompatibility(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	call := func(c *client, method, path string, body any, want int) []byte {
		t.Helper()
		code, b, _ := c.do(method, path, body, nil)
		if code != want {
			t.Fatalf("%s %s = %d %s (want %d)", method, path, code, b, want)
		}
		return b
	}
	landing := mustJSON[map[string]any](t, call(r.c, "POST", "/api/admin/nodes", map[string]any{"Name": "Landing", "PublicAddr": "203.0.113.30", "InternalAddr": "10.10.0.2"}, 200))
	id := int64(landing["id"].(float64))
	path := "/api/admin/nodes/" + itoa(id)
	ib := mustJSON[struct{ ID int64 }](t, call(r.c, "POST", path+"/inbounds", map[string]any{"Tag": "private", "Protocol": "vless", "Port": 1443}, 200))
	forwardPath := "/api/admin/nodes/" + itoa(r.nodeID) + "/forwards"
	f := store.NodeForward{Forward: spec.Forward{Tag: "private", Protocol: "tcp", Port: 18443, Target: "example.com:443"}, InboundID: ib.ID, TargetAddressMode: "internal"}
	call(r.c, "PUT", forwardPath, map[string]any{"Forwards": []store.NodeForward{f}}, 200)
	state := mustJSON[agentproto.State](t, call(r.agent, "GET", "/api/agent/state", nil, 200))
	if len(state.Forwards) != 1 || state.Forwards[0].Target != "10.10.0.2:1443" || len(state.Node.EgressUpstreams) != 0 || state.Node.AllowPrivateDest {
		t.Fatal("incorrect target or widened private privileges", state.Forwards, state.Node.EgressUpstreams)
	}
	// Legacy node edits, including explicit null, cannot clear the saved IP.
	for _, body := range []map[string]any{{"Name": "Landing", "PublicAddr": "203.0.113.30"}, {"Name": "Landing", "PublicAddr": "203.0.113.30", "InternalAddr": nil}} {
		call(r.c, "PATCH", path, body, 200)
		n, _ := r.st.NodeByID(ctx, id)
		if n.InternalAddr != "10.10.0.2" {
			t.Fatal("old node edit erased private address")
		}
	}
	call(r.c, "PATCH", path, map[string]any{"Name": "Landing", "PublicAddr": "203.0.113.30", "InternalAddr": ""}, 409)
	call(r.c, "PATCH", path, map[string]any{"Name": "Landing", "PublicAddr": "203.0.113.30", "InternalAddr": "2001:db8::20"}, 200)
	state = mustJSON[agentproto.State](t, call(r.agent, "GET", "/api/agent/state", nil, 200))
	if state.Forwards[0].Target != "[2001:db8::20]:1443" {
		t.Fatal(state.Forwards)
	}
	// Stale target submitted by an older editor is re-resolved, never restored.
	f.TargetAddressMode = ""
	call(r.c, "PUT", forwardPath, map[string]any{"Forwards": []store.NodeForward{f}}, 200)
	fs, _ := r.st.NodeForwards(ctx, r.nodeID)
	if fs[0].Target != "[2001:db8::20]:1443" || fs[0].TargetAddressMode != "internal" {
		t.Fatal(fs)
	}
	usage := mustJSON[struct {
		Uses []store.InternalAddressUse `json:"uses"`
	}](t, call(r.c, "GET", path+"/internal-address-usage", nil, 200))
	if len(usage.Uses) != 1 || usage.Uses[0].Tag != "private" {
		t.Fatal(usage)
	}
	call(r.c, "DELETE", "/api/admin/inbounds/"+itoa(ib.ID), nil, 409)
	call(r.c, "POST", path+"/removal", map[string]any{"mode": "standalone"}, 409)
	for _, role := range []string{"operator", "support"} {
		u, _ := admin.NewUser(role+"@example.com", "password123", role)
		if err := r.st.CreateStaff(ctx, u); err != nil {
			t.Fatal(err)
		}
		c := &client{t: t, srv: r.srv}
		call(c, "POST", "/api/admin/login", map[string]string{"Email": u.Email, "Password": "password123"}, 200)
		want := 403
		if role == "operator" {
			want = 200
		}
		call(c, "GET", path+"/internal-address-usage", nil, want)
		call(c, "GET", "/api/admin/lines/options?inbound_id="+itoa(ib.ID), nil, want)
		call(c, "GET", "/api/admin/lines", nil, 403)
		call(c, "POST", "/api/admin/lines", map[string]any{}, 403)
		call(c, "POST", path+"/jobs", map[string]any{"kind": spec.NetworkDiagnosticKind, "params": map[string]any{}}, 403)
	}
	for _, grant := range []string{"nodes:read", "GET /api/admin/lines/options"} {
		token, _, err := r.st.CreateAPIToken(ctx, 1, "private metadata", "read", nil, []string{grant})
		if err != nil {
			t.Fatal(err)
		}
		c := &client{t: t, srv: r.srv}
		headers := map[string]string{"Authorization": "Bearer " + token}
		for _, target := range []struct {
			path string
			want int
		}{{path + "/internal-address-usage", map[bool]int{true: 200, false: 403}[grant == "nodes:read"]}, {"/api/admin/lines/options?inbound_id=" + itoa(ib.ID), map[bool]int{true: 200, false: 403}[grant == "GET /api/admin/lines/options"]}} {
			if code, b, _ := c.do("GET", target.path, nil, headers); code != target.want {
				t.Fatalf("%s scope %s: %d %s", target.path, grant, code, b)
			}
		}
		if code, _, _ := c.do("POST", path+"/jobs", map[string]any{"kind": spec.NetworkDiagnosticKind}, headers); code != 403 {
			t.Fatal("metadata token could create diagnostic", code)
		}
	}

}
