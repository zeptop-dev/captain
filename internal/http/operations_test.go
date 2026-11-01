package http

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestOperationsHTTPReadScopeIsolationPaginationAndNoExecution(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	for _, id := range []string{"job-a", "job-b"} {
		if e := r.st.CreateNodeJob(ctx, id, r.nodeID, "host_manage", json.RawMessage(`{"action":"apply","password":"private-never-return"}`)); e != nil {
			t.Fatal(e)
		}
		if e := r.st.CompleteNodeJob(ctx, r.nodeID, id, json.RawMessage(`{"outcome":"waiting","message":"private-never-return"}`), ""); e != nil {
			t.Fatal(e)
		}
	}
	if e := r.st.SetNodeUpgrade(ctx, r.nodeID, "v0.90.0"); e != nil {
		t.Fatal(e)
	}
	call := func(c *client, path string, want int) []byte {
		t.Helper()
		code, b, h := c.do("GET", path, nil, nil)
		if code != want {
			t.Fatalf("%s got %d want %d: %s", path, code, want, b)
		}
		if code == 200 && h.Get("Cache-Control") != "no-store" {
			t.Fatal("cacheable operation history")
		}
		return b
	}
	base := "/api/admin/operations"
	detail := base + "/node_job:job-a"
	anon := &client{t: t, srv: r.srv}
	call(anon, base, 401)
	for _, role := range []string{domain.RoleOperator, domain.RoleSupport} {
		u, c := accessAuditStaff(t, r, role)
		call(c, base, 403)
		call(accessAuditToken(t, r, u.ID, "full", []string{"operations:*", "nodes:*"}), detail, 403)
	}
	for _, scopes := range [][]string{{"nodes:read"}, {"operations:write"}} {
		call(accessAuditToken(t, r, 1, "full", scopes), base, 403)
	}
	narrow := accessAuditToken(t, r, 1, "read", []string{"operations:read"})
	empty := mustJSON[store.OperationPage](t, call(narrow, base, 200))
	if len(empty.Items) != 0 || empty.NextBefore != "" {
		t.Fatal(empty)
	}
	hidden := call(narrow, detail, 404)
	missing := call(narrow, base+"/node_job:missing", 404)
	if string(hidden) != string(missing) {
		t.Fatal("existence leak", string(hidden), string(missing))
	}
	// Endpoint grants match router patterns, never a caller-selected concrete ID.
	literal := accessAuditToken(t, r, 1, "read", []string{"operations:read", "GET /api/admin/nodes/1", "GET /api/admin/nodes/{id}/jobs/{job}"})
	if got := mustJSON[store.OperationPage](t, call(literal, base, 200)); len(got.Items) != 0 {
		t.Fatal("literal endpoint widened", got)
	}
	reader := accessAuditToken(t, r, 1, "read", []string{"operations:read", "GET /api/admin/nodes/{id}", "GET /api/admin/nodes/{id}/jobs/{job}"})
	p := mustJSON[store.OperationPage](t, call(reader, base+"?source=node_job&limit=1", 200))
	if len(p.Items) != 1 || p.NextBefore == "" {
		t.Fatal(p)
	}
	second := mustJSON[store.OperationPage](t, call(reader, base+"?source=node_job&limit=1&before="+url.QueryEscape(p.NextBefore), 200))
	if len(second.Items) != 1 || second.Items[0].ID == p.Items[0].ID || second.NextBefore != "" {
		t.Fatal(second)
	}
	body := call(reader, detail, 200)
	v := mustJSON[store.Operation](t, body)
	if v.Status != "waiting" || v.Outcome != "waiting" || v.Action != "apply" || v.Evidence != "historical" || strings.Contains(string(body), "private-never-return") {
		t.Fatal(string(body))
	}
	for _, q := range []string{"limit=101", "before=not-a-cursor", "source=unknown", "status=success", "node_id=-1", "line_id=0"} {
		call(reader, base+"?"+q, 400)
	}
	for _, method := range []string{"POST", "PUT", "DELETE"} {
		code, _, _ := r.c.do(method, detail, nil, nil)
		if code >= 200 && code < 300 {
			t.Fatal("operation view executes", method)
		}
	}
	call(r.c, detail, 200)
}

func TestOperationsHTTPTopologyRequiresAllOriginalReadPermissions(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	var e error
	port := 4443
	name := "New offer"
	d := &store.TopologyChangeset{Name: "Private draft", Operations: []store.TopologyOperation{
		{Key: "entry-key-pretending-to-be-inbound", Kind: "inbound", Inbound: &store.TopologyInboundPatch{NodeID: r.nodeID, Tag: &name, Port: &port, Protocol: spec.Shadowsocks, Settings: &spec.Inbound{Cipher: "2022-blake3-aes-128-gcm"}}},
		{Key: "inbound-key-pretending-to-be-entry", Kind: "entry", Entry: &store.TopologyEntryPatch{Name: &name, InboundRef: "entry-key-pretending-to-be-inbound"}},
	}}
	if e = r.st.SaveTopologyChangeset(ctx, d); e != nil {
		t.Fatal(e)
	}
	base := "/api/admin/operations"
	path := base + "/topology_changeset:" + d.ID
	get := func(c *client, p string, want int) []byte {
		t.Helper()
		code, b, _ := c.do("GET", p, nil, nil)
		if code != want {
			t.Fatalf("%s %d: %s", p, code, b)
		}
		return b
	}
	scopes := []string{"operations:read", "topology-changesets:read", "GET /api/admin/nodes/{id}"}
	narrow := accessAuditToken(t, r, 1, "read", scopes)
	if page := mustJSON[store.OperationPage](t, get(narrow, base, 200)); len(page.Items) != 0 || page.NextBefore != "" {
		t.Fatal("missing entry read leaked existence", page)
	}
	get(narrow, path, 404)
	full := accessAuditToken(t, r, 1, "read", append(scopes, "entries:read"))
	page := mustJSON[store.OperationPage](t, get(full, base+"?source=topology_changeset", 200))
	if len(page.Items) != 1 || page.Items[0].ID != "topology_changeset:"+d.ID {
		t.Fatal(page)
	}
	get(full, path, 200)
	get(r.c, path, 200)
}
