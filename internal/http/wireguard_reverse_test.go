package http

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/auth"
	"github.com/zeptop-dev/captain/internal/http/admin"
	"github.com/zeptop-dev/captain/internal/service"
)

func TestWireGuardManagedAPI(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	call := func(method, path string, body any, want int) []byte {
		t.Helper()
		code, b, _ := r.c.do(method, path, body, nil)
		if code != want {
			t.Fatalf("%s %s: %d want %d: %s", method, path, code, want, b)
		}
		return b
	}
	transit, _ := r.st.NodeByID(ctx, r.nodeID)
	transit.PublicAddr = "198.51.100.20"
	if err := r.st.UpdateNode(ctx, transit); err != nil {
		t.Fatal(err)
	}
	created := mustJSON[map[string]any](t, call("POST", "/api/admin/nodes", map[string]any{"Name": "WG exit", "PublicAddr": "203.0.113.30"}, 200))
	exit := int64(created["id"].(float64))
	if err := r.st.ResetPairCode(ctx, exit, "wg-pair", time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := r.st.RedeemPairCode(ctx, "wg-pair", auth.SHA256Hex("wg-exit-test-token"), "test", "v0.68.0", "linux/amd64"); err != nil {
		t.Fatal(err)
	}
	path := "/api/admin/settings/nodes/" + itoa(exit) + "/wireguard-connections"
	input := map[string]any{"transit_id": r.nodeID, "name": "WG test", "port": 18443, "tunnel_port": 51820, "endpoint": "198.51.100.20:51820", "mtu": 1380, "server_name": "www.example.com", "enabled": true}
	call("POST", path, input, 409)
	for _, id := range []int64{r.nodeID, exit} {
		cores := map[string]agentproto.CoreStatus{"xray": {Capabilities: &spec.CoreCapabilities{WireGuardReverse: true, PrivateAccess: true, Protocols: []spec.Protocol{spec.VLESS, spec.Shadowsocks}}}}
		if err := r.st.TouchNode(ctx, id, "v0.68.0", "", spec.SystemStatus{}, cores, nil); err != nil {
			t.Fatal(err)
		}
		tokenHash := auth.SHA256Hex("wg-exit-test-token")
		if id == r.nodeID {
			tokenHash = auth.SHA256Hex(r.agent.token)
		}
		if err := r.st.SetWireGuardReport(ctx, id, tokenHash, &agentproto.WireGuardReport{Available: true}); err != nil {
			t.Fatal(err)
		}
	}
	response := mustJSON[map[string]any](t, call("POST", path, input, 200))
	id := response["id"].(string)
	links, err := r.st.WireGuardLinks(ctx, exit)
	if err != nil || len(links) != 1 {
		t.Fatal(links, err)
	}
	link := links[0]
	view := call("GET", path, nil, 200)
	if strings.Contains(string(view), link.TransitKey) || strings.Contains(string(view), link.ExitKey) {
		t.Fatal("transport credentials exposed")
	}
	// A regular resource request cannot take over a managed subscriber inbound.
	call("DELETE", "/api/admin/inbounds/"+itoa(link.UserInboundID), nil, 409)
	// The separate OS UDP listener is reserved against ordinary inbounds.
	call("POST", "/api/admin/nodes/"+itoa(r.nodeID)+"/inbounds", map[string]any{"Tag": "collision", "Protocol": "shadowsocks", "Port": 51820, "Enabled": true, "Settings": map[string]any{"cipher": "aes-128-gcm"}}, 400)
	state := &service.AgentState{Store: r.st}
	n, _ := r.st.NodeByID(ctx, r.nodeID)
	st, err := state.Build(ctx, n, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Node.WireGuardLinks) != 1 {
		t.Fatal("transit transport not delivered")
	}
	found := false
	for _, ib := range st.Node.Inbounds {
		found = found || ib.WireGuardSlot == link.Slot
	}
	if !found {
		t.Fatal("subscriber inbound not delivered")
	}
	b, _ := r.st.NodeByID(ctx, exit)
	bst, err := state.Build(ctx, b, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(bst.Node.WireGuardLinks) != 1 || !bst.Node.WireGuardLinks[0].Exit {
		t.Fatal("exit role missing")
	}
	// Subscriber payload is charged on A exactly once. B's tunnel counters
	// must not become a second billing source, even when reports are retried.
	uid, _ := r.user("wg-payer@example.com")
	ib, err := r.st.InboundByID(ctx, link.UserInboundID)
	if err != nil {
		t.Fatal(err)
	}
	rep := agentproto.Report{TrafficSeq: 71, TrafficWindowSeconds: 60,
		WireGuard: &agentproto.WireGuardReport{Available: true, Links: []agentproto.WireGuardLinkStatus{{Slot: link.Slot, Receive: 9000000, Transmit: 8000000}}},
		Traffic:   []spec.UserTraffic{{UserID: uid, Inbound: ib.Tag, Up: 524288, Down: 524288}}}
	for range 2 {
		if code, body, _ := r.agent.do("POST", "/api/agent/report", rep, nil); code != 200 {
			t.Fatalf("A accounting: %d %s", code, body)
		}
	}
	exitAgent := &client{t: t, srv: r.srv, token: "wg-exit-test-token"}
	for range 2 {
		if code, body, _ := exitAgent.do("POST", "/api/agent/report", agentproto.Report{TrafficSeq: 72, WireGuard: rep.WireGuard}, nil); code != 200 {
			t.Fatalf("B diagnostics: %d %s", code, body)
		}
	}
	sub, err := r.st.ActiveSubscription(ctx, uid)
	if err != nil || sub.UsedUpBytes != 524288 || sub.UsedDownBytes != 524288 {
		t.Fatalf("WireGuard billing: %+v %v", sub, err)
	}

	if err := r.st.SetWireGuardReport(ctx, exit, auth.SHA256Hex("wg-exit-test-token"), nil); err != nil {
		t.Fatal(err)
	}
	st, err = state.Build(ctx, n, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Node.WireGuardLinks) != 0 {
		t.Fatal("old peer got transport")
	}
	for _, ib := range st.Node.Inbounds {
		if ib.WireGuardSlot != 0 {
			t.Fatal("missing peer capability fell back to direct")
		}
	}
	for _, role := range []string{"operator", "support"} {
		user, err := admin.NewUser("wg-"+role+"@example.com", "password123", role)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.st.CreateStaff(ctx, user); err != nil {
			t.Fatal(err)
		}
		staff := &client{t: t, srv: r.srv}
		if code, _, _ := staff.do("POST", "/api/admin/login", map[string]string{"Email": user.Email, "Password": "password123"}, nil); code != 200 {
			t.Fatal(code)
		}
		for _, method := range []string{"GET", "POST"} {
			if code, _, _ := staff.do(method, path, input, nil); code != 403 {
				t.Fatal("role bypass", role, method, code)
			}
		}
	}
	call("DELETE", path+"/"+id, map[string]any{"version": 99}, 409)
	call("DELETE", path+"/"+id, map[string]any{"version": 1}, 200)
	if len(mustJSON[[]any](t, call("GET", path, nil, 200))) != 0 {
		t.Fatal("connection not deleted")
	}
}

func TestPanelRollbackFloor(t *testing.T) {
	for _, tc := range []struct{ configured, want string }{{"", "v1.32.0"}, {"invalid", "v1.32.0"}, {"v1.20.0", "v1.32.0"}, {"v1.29.0", "v1.32.0"}, {"v1.30.0", "v1.32.0"}, {"v1.31.0", "v1.32.0"}, {"v1.32.0", "v1.32.0"}, {"v1.33.0", "v1.33.0"}} {
		if got := captainUpdateFloor(tc.configured); got != tc.want {
			t.Fatalf("%q: %s", tc.configured, got)
		}
	}
}
