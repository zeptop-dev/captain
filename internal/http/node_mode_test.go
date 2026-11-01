package http

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/auth"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/http/admin"
)

func TestMonitorNodeCreationPairingAndState(t *testing.T) {
	r := newRig(t)
	code, b, _ := r.c.do("POST", "/api/admin/nodes", map[string]any{"Name": "monitor", "Mode": "monitor"}, nil)
	if code != 200 {
		t.Fatalf("create %d %s", code, b)
	}
	n := mustJSON[map[string]any](t, b)
	id := int64(n["id"].(float64))
	pair := n["pair_code"].(string)
	if n["mode"] != "monitor" || n["proxy_allowed"] != false {
		t.Fatal(n)
	}
	base := "/api/admin/nodes/" + itoa(id)
	code, b, _ = r.c.do("GET", "/api/agent/install.sh?pair="+pair, nil, nil)
	if code != 200 || !strings.Contains(string(b), `--monitor-only "$@"`) {
		t.Fatalf("installer %d %s", code, b)
	}
	nc := &client{t: t, srv: r.srv}
	if c, _, _ := nc.do("POST", "/api/agent/pair", agentproto.PairRequest{Code: pair}, nil); c != 409 {
		t.Fatal("old node paired", c)
	}
	code, b, _ = nc.do("POST", "/api/agent/pair", agentproto.PairRequest{Code: pair, NodeModes: true, Version: "dev"}, nil)
	if code != 200 {
		t.Fatalf("pair %d %s", code, b)
	}
	nc.token = mustJSON[agentproto.PairResponse](t, b).Token
	if c, b, _ := nc.do("POST", "/api/agent/report", agentproto.Report{NodeMode: spec.NodeMonitor, NodeModeRevision: 1}, nil); c != 200 {
		t.Fatalf("report %d %s", c, b)
	}
	for _, req := range []struct {
		method, path string
		body         any
	}{
		{"POST", base + "/inbounds", map[string]any{"Tag": "no", "Protocol": "vless", "Port": 443}},
		{"PUT", base + "/forwards", []any{}},
		{"PUT", base + "/routing", map[string]any{}},
		{"PUT", base + "/overrides", map[string]any{}},
		{"PATCH", base, map[string]any{"Name": "monitor", "DecoyEnabled": true}},
		{"POST", base + "/jobs", map[string]any{"kind": "core_manage"}},
		{"POST", base + "/jobs", map[string]any{"kind": "warp_register"}},
		{"POST", base + "/jobs", map[string]any{"kind": "reality_scan"}},
	} {
		if c, b, _ := r.c.do(req.method, req.path, req.body, nil); c != 409 {
			t.Fatalf("proxy API bypass %s: %d %s", req.path, c, b)
		}
	}
	code, b, _ = nc.do("GET", "/api/agent/state", nil, nil)
	st := mustJSON[agentproto.State](t, b)
	if code != 200 || st.Node.Mode != spec.NodeMonitor || len(st.Users) != 0 || spec.ValidateMonitorState(&st.Node, st.Users, st.Forwards) != nil {
		t.Fatalf("unsafe state %d %s", code, b)
	}
	if c, b, _ := r.c.do("PUT", base+"/mode", map[string]any{"mode": "full", "expected": "monitor", "expected_revision": 1}, nil); c != 200 {
		t.Fatalf("switch %d %s", c, b)
	}
	if c, _, _ := r.c.do("POST", base+"/inbounds", map[string]any{"Tag": "no", "Protocol": "vless", "Port": 443}, nil); c != 409 {
		t.Fatal("pending full accepted inbound", c)
	}
	// Downgrading to an agent without the field must not reopen the guard.
	nc.do("POST", "/api/agent/report", agentproto.Report{}, nil)
	if c, _, _ := r.c.do("POST", base+"/inbounds", map[string]any{"Tag": "no", "Protocol": "vless", "Port": 443}, nil); c != 409 {
		t.Fatal("old report reopened proxy mode", c)
	}
	nc.do("POST", "/api/agent/report", agentproto.Report{NodeMode: spec.NodeFull, NodeModeRevision: 2}, nil)
	if c, b, _ := r.c.do("POST", base+"/inbounds", map[string]any{"Tag": "ready", "Protocol": "vless", "Port": 443}, nil); c != 200 {
		t.Fatalf("confirmed full %d %s", c, b)
	}
	// A delayed report cannot roll the node back to an older observation.
	nc.do("POST", "/api/agent/report", agentproto.Report{NodeMode: spec.NodeMonitor, NodeModeRevision: 1}, nil)
	saved, err := r.st.NodeByID(context.Background(), id)
	if err != nil || saved.ReportedMode != spec.NodeFull || saved.ReportedModeRevision != 2 {
		t.Fatal("stale report replaced confirmed mode", err)
	}
	code, b, _ = r.c.do("POST", base+"/repair", nil, nil)
	if code != 200 {
		t.Fatal("repair", code)
	}
	newPair := mustJSON[map[string]string](t, b)["pair_code"]
	if err := r.st.ReportNodeMode(context.Background(), id, auth.SHA256Hex(nc.token), spec.NodeFull, 2, ""); err != nil {
		t.Fatal(err)
	}
	saved, err = r.st.NodeByID(context.Background(), id)
	if err != nil || saved.ReportedMode != "" {
		t.Fatal("in-flight old token acknowledged repaired node", err)
	}
	if c, _, _ := nc.do("GET", "/api/agent/state", nil, nil); c != 401 {
		t.Fatal("repair retained old token", c)
	}
	nc.token = ""
	code, b, _ = nc.do("POST", "/api/agent/pair", agentproto.PairRequest{Code: newPair, NodeModes: true}, nil)
	if code != 200 {
		t.Fatal("new pairing", code)
	}
	nc.token = mustJSON[agentproto.PairResponse](t, b).Token
	code, b, _ = nc.do("GET", "/api/agent/state", nil, nil)
	st = mustJSON[agentproto.State](t, b)
	if code != 200 || st.Node.ModeRevision != 2 || len(st.Node.Inbounds) != 0 || len(st.Users) != 0 {
		t.Fatal("new pairing received old proxy authorization")
	}
	nc.do("POST", "/api/agent/report", agentproto.Report{NodeMode: spec.NodeFull, NodeModeRevision: 2}, nil)
	code, b, _ = nc.do("GET", "/api/agent/state", nil, nil)
	st = mustJSON[agentproto.State](t, b)
	if code != 200 || len(st.Node.Inbounds) != 1 {
		t.Fatal("new mode confirmation did not restore proxy state")
	}
}

func TestNodeModeTransitionChecksResourcesRolesAndCompatibility(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	base := "/api/admin/nodes/" + itoa(r.nodeID)
	req := map[string]string{"mode": "monitor", "expected": "full"}
	if c, _, _ := r.c.do("PUT", base+"/mode", req, nil); c != 409 {
		t.Fatal("old node switched", c)
	}
	r.agent.do("POST", "/api/agent/report", agentproto.Report{NodeMode: spec.NodeFull}, nil)
	ibs, err := r.st.AllInboundsByNode(ctx, r.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ib := range ibs {
		ib.Enabled = false
		if err = r.st.UpdateInbound(ctx, ib); err != nil {
			t.Fatal(err)
		}
	}
	if c, _, _ := r.c.do("PUT", base+"/mode", req, nil); c != 409 {
		t.Fatal("disabled inbounds ignored", c)
	}
	for _, ib := range ibs {
		if err = r.st.DeleteInbound(ctx, ib.ID); err != nil {
			t.Fatal(err)
		}
	}
	for _, role := range []string{"operator", "support"} {
		u, _ := admin.NewUser(role+"@example.com", "password123", role)
		r.st.CreateStaff(ctx, u)
		c := &client{t: t, srv: r.srv}
		c.do("POST", "/api/admin/login", map[string]string{"Email": u.Email, "Password": "password123"}, nil)
		if status, _, _ := c.do("PUT", base+"/mode", req, nil); status != 403 {
			t.Fatal("role switched", role, status)
		}
	}
	if _, err = r.st.DB().Exec(`UPDATE nodes SET forwards_json = '[{"tag":"relay","port":443,"target":"192.0.2.10:443"}]' WHERE id = ?`, r.nodeID); err != nil {
		t.Fatal(err)
	}
	if c, _, _ := r.c.do("PUT", base+"/mode", req, nil); c != 409 {
		t.Fatal("forwards ignored", c)
	}
	r.st.DB().Exec(`UPDATE nodes SET forwards_json = '[]' WHERE id = ?`, r.nodeID)
	if c, b, _ := r.c.do("PUT", base+"/mode", req, nil); c != 200 {
		t.Fatalf("switch %d %s", c, b)
	}
	if err = r.st.CreateInbound(ctx, &domain.Inbound{NodeID: r.nodeID, Tag: "bypass", Protocol: spec.VLESS, Port: 443}); err == nil {
		t.Fatal("internal writer bypassed guard")
	}
	// Old clients omit/null the new field when editing unrelated metadata.
	if c, b, _ := r.c.do("PATCH", base, map[string]any{"Name": "renamed", "Mode": nil}, nil); c != 200 {
		t.Fatalf("legacy edit %d %s", c, b)
	}
	n, _ := r.st.NodeByID(ctx, r.nodeID)
	if n.Mode != spec.NodeMonitor {
		t.Fatal("legacy edit cleared mode")
	}
	if c, _, _ := r.c.do("PUT", base+"/mode", req, nil); c != 409 {
		t.Fatal("stale expected mode accepted", c)
	}
	// Fresh beats must not refresh a stale mode capability report.
	r.st.DB().Exec(`UPDATE nodes SET mode_reported_at = ?, last_seen_at = ? WHERE id = ?`, time.Now().Add(-time.Hour).Unix(), time.Now().Unix(), r.nodeID)
	if c, _, _ := r.c.do("PUT", base+"/mode", map[string]any{"mode": "full", "expected": "monitor", "expected_revision": 1}, nil); c != 409 {
		t.Fatal("stale capability accepted", c)
	}
}
