package http

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestTopologyChangesetPermissionsPreviewAndAtomicCommit(t *testing.T) {
	r := newRig(t)
	call := func(c *client, method, path string, body any, want int) []byte {
		t.Helper()
		code, b, h := c.do(method, path, body, nil)
		if code != want {
			t.Fatalf("%s %s: %d want %d %s", method, path, code, want, b)
		}
		if code == 200 && method == "GET" && h.Get("Cache-Control") != "no-store" {
			t.Fatal("cacheable draft")
		}
		return b
	}
	base := "/api/admin/topology-changesets"
	body := map[string]any{"name": "Atomic topology", "operations": []any{
		map[string]any{"key": "ib", "kind": "inbound", "inbound": map[string]any{"node_id": r.nodeID, "tag": "draft-ss", "port": 2443, "protocol": "shadowsocks", "settings": map[string]any{"cipher": "2022-blake3-aes-128-gcm", "server_key": "YWJjZGVmZ2hpamtsbW5vcA=="}}},
		map[string]any{"key": "entry", "kind": "entry", "entry": map[string]any{"name": "Draft subscription", "inbound_ref": "ib", "display_host": "192.0.2.9"}},
	}}
	for _, role := range []string{domain.RoleOperator, domain.RoleSupport} {
		u, c := accessAuditStaff(t, r, role)
		call(c, "GET", base, nil, 403)
		call(accessAuditToken(t, r, u.ID, "full", []string{"*"}), "POST", base, body, 403)
	}
	call(accessAuditToken(t, r, 1, "full", []string{"nodes:write", "entries:write"}), "POST", base, body, 403)
	restricted := accessAuditToken(t, r, 1, "full", []string{"topology-changesets:*"})
	call(restricted, "POST", base, body, 403)
	call(restricted, "GET", base+"/catalog", nil, 403)
	writer := accessAuditToken(t, r, 1, "full", []string{"topology-changesets:*", "nodes:write", "entries:write"})
	raw := call(writer, "POST", base, body, 200)
	if bytes.Contains(raw, []byte("YWJjZGVmZ2hpamtsbW5vcA==")) || bytes.Contains(raw, []byte(`"settings":`)) {
		t.Fatal("creation secret in response", string(raw))
	}
	draft := mustJSON[store.TopologyChangeset](t, raw)
	path := base + "/" + draft.ID
	call(restricted, "GET", path, nil, 403)
	call(restricted, "POST", path+"/preview", map[string]any{"version": draft.Version}, 403)
	beforeLog, err := r.st.AdminLog(context.Background(), 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	preview := mustJSON[store.TopologyPreview](t, call(writer, "POST", path+"/preview", map[string]any{"version": draft.Version}, 200))
	afterLog, err := r.st.AdminLog(context.Background(), 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(beforeLog) != len(afterLog) {
		t.Fatal("preview wrote admin audit")
	}
	// Revoking either the new or old grant is honored again at commit, rather
	// than trusting the authority that originally saved or previewed the draft.
	call(restricted, "POST", path+"/commit", map[string]any{"version": draft.Version, "preview_revision": preview.Revision}, 403)
	result := mustJSON[store.TopologyChangeset](t, call(writer, "POST", path+"/commit", map[string]any{"version": draft.Version, "preview_revision": preview.Revision}, 200))
	if result.Status != "committed" || len(result.Result) != 2 {
		t.Fatal("incomplete commit", result)
	}
	ib, err := r.st.InboundByID(context.Background(), result.Result[0].ID)
	if err != nil || ib.Protocol != spec.Shadowsocks || ib.Settings.ServerKey != "YWJjZGVmZ2hpamtsbW5vcA==" {
		t.Fatal("wrong live settings", err)
	}
	entry, err := r.st.EntryByID(context.Background(), result.Result[1].ID)
	if err != nil || entry.InboundID != ib.ID {
		t.Fatal("wrong entry ref", err)
	}
	call(writer, "POST", path+"/commit", map[string]any{"version": draft.Version, "preview_revision": preview.Revision}, 409)
	call(r.c, "POST", base, map[string]any{"name": "Forbidden", "operations": []any{map[string]any{"key": "acl", "kind": "entry", "entry": map[string]any{"access": map[string]any{"mode": "all"}}}}}, 400)
	call(r.c, "POST", base, map[string]any{"name": "Forbidden", "operations": []any{map[string]any{"key": "delete", "kind": "delete", "entry": map[string]any{}}}}, 400)
}

func TestTopologyChangesetLineUsesActualEndpointGrant(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	ibs, err := r.st.AllInboundsByNode(ctx, r.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ibs) == 0 {
		t.Fatal("fixture missing landing")
	}
	n := &domain.Node{Name: "Relay", PublicAddr: "192.0.2.10"}
	if err = r.st.CreateNode(ctx, n, "TOPOLOGY-PAIR", time.Hour); err != nil {
		t.Fatal(err)
	}
	// Pair through the real protocol so line-version eligibility is meaningful.
	a := &client{t: t, srv: r.srv}
	code, b, _ := a.do("POST", "/api/agent/pair", agentproto.PairRequest{Code: "TOPOLOGY-PAIR", Version: "v0.84.0"}, nil)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	landing, _ := r.st.NodeByID(ctx, r.nodeID)
	landing.PublicAddr = "203.0.113.9"
	if err = r.st.UpdateNode(ctx, landing); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"name": "Scoped line", "operations": []store.TopologyOperation{{Key: "line", Kind: "line", Line: &store.ForwardLine{Name: "Relay", TargetInboundID: ibs[0].ID, Protocol: "tcp", Backend: "relay", Groups: []store.LineGroup{{Members: []store.LineMember{{NodeID: n.ID, Port: 18443}}}}}}}}
	restricted := accessAuditToken(t, r, 1, "full", []string{"topology-changesets:write", "nodes:write"})
	code, b, _ = restricted.do("POST", "/api/admin/topology-changesets", body, nil)
	if code != 403 {
		t.Fatal("nodes permission granted line mutation", code, string(b))
	}
	writer := accessAuditToken(t, r, 1, "full", []string{"topology-changesets:write", "POST /api/admin/lines"})
	code, b, _ = writer.do("POST", "/api/admin/topology-changesets", body, nil)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	d := mustJSON[store.TopologyChangeset](t, b)
	path := "/api/admin/topology-changesets/" + d.ID
	code, b, _ = writer.do("POST", path+"/preview", map[string]any{"version": d.Version}, nil)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	p := mustJSON[store.TopologyPreview](t, b)
	code, b, _ = writer.do("POST", path+"/commit", map[string]any{"version": d.Version, "preview_revision": p.Revision}, nil)
	if code != 200 {
		t.Fatal(code, string(b))
	}
}
