package http

import (
	"context"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestCompletePathRoutesAPIAndPermissions(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	call := func(c *client, method, path string, body any, want int) []byte {
		t.Helper()
		code, b, _ := c.do(method, path, body, nil)
		if code != want {
			t.Fatalf("%s %s =%d want%d %s", method, path, code, want, b)
		}
		return b
	}
	call(r.c, "PATCH", "/api/admin/nodes/"+itoa(r.nodeID), map[string]any{"Name": "Landing", "PublicAddr": "203.0.113.30"}, 200)
	ibs, e := r.st.AllInboundsByNode(ctx, r.nodeID)
	if e != nil {
		t.Fatal(e)
	}
	var nodes []int64
	var agents []*client
	for i, ip := range []string{"192.0.2.10", "198.51.100.20", "198.51.100.21"} {
		n := mustJSON[map[string]any](t, call(r.c, "POST", "/api/admin/nodes", map[string]any{"Name": "Relay " + itoa(int64(i)), "PublicAddr": ip}, 200))
		nodes = append(nodes, int64(n["id"].(float64)))
		a := &client{t: t, srv: r.srv}
		a.token = mustJSON[agentproto.PairResponse](t, call(a, "POST", "/api/agent/pair", agentproto.PairRequest{Code: n["pair_code"].(string), Version: "v0.83.0"}, 200)).Token
		agents = append(agents, a)
	}
	p := mustJSON[store.ForwardPath](t, call(r.c, "POST", "/api/admin/forward-paths", store.ForwardPath{Name: "Pinned B", Steps: []store.PathStep{{NodeID: nodes[1]}, {NodeID: nodes[2]}}}, 200))
	q := mustJSON[store.ForwardPath](t, call(r.c, "POST", "/api/admin/forward-paths", store.ForwardPath{Name: "Pinned C", Steps: []store.PathStep{{NodeID: nodes[2]}}}, 200))
	input := store.PathRoute{Name: "Complete alternatives", Entry: store.LineMember{NodeID: nodes[0], Port: 23000}, TargetInboundID: ibs[0].ID, Branches: []store.PathBranch{{PathID: p.ID, PathRevision: 1, Ports: []int{23001, 23002}}, {PathID: q.ID, PathRevision: 1, Ports: []int{23003}}}}
	preview := mustJSON[store.PathRoutePreview](t, call(r.c, "POST", "/api/admin/path-routes/preview", input, 200))
	if len(preview.Changes) != 3 || len(preview.Edges) != 5 {
		t.Fatal(preview)
	}
	input = preview.Draft
	input.PreviewRevision = preview.Revision
	route := mustJSON[store.PathRoute](t, call(r.c, "POST", "/api/admin/path-routes", input, 200))
	if route.ChangeID == "" {
		t.Fatal("missing atomic change")
	}
	change, e := r.st.LineChangeByID(ctx, route.ChangeID, time.Now())
	if e != nil || change.Actor != "admin@test" {
		t.Fatal(change, e)
	}
	line := mustJSON[store.ForwardLine](t, call(r.c, "GET", "/api/admin/lines/"+itoa(route.ID), nil, 200))
	if !line.PathRoute {
		t.Fatal("backing line lacks ownership marker")
	}
	line.PathRoute = false
	call(r.c, "PUT", "/api/admin/lines/"+itoa(route.ID), line, 409)
	call(r.c, "POST", "/api/admin/lines/"+itoa(route.ID)+"/preview", line, 409)
	call(r.c, "DELETE", "/api/admin/lines/"+itoa(route.ID)+"?version="+itoa(route.Version), nil, 409)
	for i, a := range agents {
		st := mustJSON[agentproto.State](t, call(a, "GET", "/api/agent/state", nil, 200))
		want := 1
		if i == 2 {
			want = 2
		}
		if len(st.Forwards) != want {
			t.Fatal("incorrect compiled delivery", i, st.Forwards)
		}
	}
	call(r.c, "DELETE", "/api/admin/forward-paths/"+itoa(p.ID)+"?version=1", nil, 409)
	uses := mustJSON[[]store.PathUse](t, call(r.c, "GET", "/api/admin/forward-paths/"+itoa(p.ID)+"/uses", nil, 200))
	if len(uses) != 1 || uses[0].ID != route.ID {
		t.Fatal(uses)
	}
	for _, role := range []string{domain.RoleOperator, domain.RoleSupport} {
		u, c := accessAuditStaff(t, r, role)
		call(c, "GET", "/api/admin/path-routes", nil, 403)
		call(accessAuditToken(t, r, u.ID, "read", []string{"forward-paths:read"}), "GET", "/api/admin/forward-paths", nil, 403)
	}
	for _, resource := range []string{"nodes:read", "entries:read", "line-changes:read"} {
		c := accessAuditToken(t, r, 1, "read", []string{resource})
		call(c, "GET", "/api/admin/path-routes", nil, 403)
		call(c, "GET", "/api/admin/forward-paths", nil, 403)
	}
	reader := accessAuditToken(t, r, 1, "read", []string{"path-routes:read", "forward-paths:read"})
	call(reader, "GET", "/api/admin/path-routes", nil, 200)
	call(reader, "GET", "/api/admin/forward-paths", nil, 200)
	call(reader, "POST", "/api/admin/path-routes", input, 403)
	code, _, headers := r.c.do("GET", "/api/admin/path-routes", nil, nil)
	if code != 200 || headers.Get("Cache-Control") != "no-store" {
		t.Fatal("route definitions cached")
	}
	stale := route
	stale.Version = 0
	call(r.c, "PUT", "/api/admin/path-routes/"+itoa(route.ID), stale, 409)
	result := mustJSON[map[string]any](t, call(r.c, "DELETE", "/api/admin/path-routes/"+itoa(route.ID)+"?version="+itoa(route.Version), nil, 200))
	if result["change_id"] == "" {
		t.Fatal(result)
	}
	call(r.c, "DELETE", "/api/admin/forward-paths/"+itoa(p.ID)+"?version=1", nil, 200)
}
