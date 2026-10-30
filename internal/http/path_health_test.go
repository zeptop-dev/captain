package http

import (
	"context"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestPathHealthAPIRequiresDistinctScopeAndBindsActualDelivery(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	call := func(c *client, method, path string, body any, want int) []byte {
		t.Helper()
		code, b, _ := c.do(method, path, body, nil)
		if code != want {
			t.Fatalf("%s %s=%d want %d %s", method, path, code, want, b)
		}
		return b
	}
	call(r.c, "PATCH", "/api/admin/nodes/"+itoa(r.nodeID), map[string]any{"Name": "Health landing", "PublicAddr": "203.0.113.30"}, 200)
	ibs, err := r.st.AllInboundsByNode(ctx, r.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	ibs[0].Protocol = spec.HTTP
	ibs[0].Settings = spec.Inbound{}
	if err = r.st.UpdateInbound(ctx, ibs[0]); err != nil {
		t.Fatal(err)
	}
	var nodes []int64
	var entry *client
	for i, ip := range []string{"192.0.2.10", "198.51.100.20"} {
		n := mustJSON[map[string]any](t, call(r.c, "POST", "/api/admin/nodes", map[string]any{"Name": "Health relay " + itoa(int64(i)), "PublicAddr": ip}, 200))
		nodes = append(nodes, int64(n["id"].(float64)))
		a := &client{t: t, srv: r.srv}
		a.token = mustJSON[agentproto.PairResponse](t, call(a, "POST", "/api/agent/pair", agentproto.PairRequest{Code: n["pair_code"].(string), Version: "v0.85.0"}, 200)).Token
		if i == 0 {
			entry = a
		}
	}
	session := "11111111111111111111111111111111"
	getState := func() *agentproto.State {
		t.Helper()
		code, b, _ := entry.do("GET", "/api/agent/state", nil, map[string]string{agentproto.ConfigSessionHeader: session})
		if code != 200 {
			t.Fatal(code, string(b))
		}
		st := mustJSON[agentproto.State](t, b)
		return &st
	}
	seq := uint64(0)
	push := func(st *agentproto.State, samples []agentproto.PathHealthSample) {
		t.Helper()
		seq++
		receipt := &agentproto.ConfigReceipt{Session: session, DeliveryID: st.ConfigDelivery.ID, ConfigRevision: st.ConfigRevision, StateRevision: st.Revision, Sequence: seq, ObservedAt: time.Now().UTC(), Applied: true}
		call(entry, "POST", "/api/agent/report", agentproto.Report{Version: "v0.85.0", Revision: st.Revision, AppliedRevision: st.Revision, ConfigReceipt: receipt, PathHealth: &agentproto.PathHealthReport{Supported: true, Samples: samples}}, 200)
	}
	push(getState(), nil)
	p := mustJSON[store.ForwardPath](t, call(r.c, "POST", "/api/admin/forward-paths", store.ForwardPath{Name: "Health B", Steps: []store.PathStep{{NodeID: nodes[1]}}}, 200))
	input := store.PathRoute{Name: "Measured path", Entry: store.LineMember{NodeID: nodes[0], Port: 23000}, TargetInboundID: ibs[0].ID, Branches: []store.PathBranch{{PathID: p.ID, PathRevision: 1, Ports: []int{23001}}}, Health: &store.PathRouteHealth{Enabled: true}}
	narrow := accessAuditToken(t, r, 1, "full", []string{"path-routes:read", "path-routes:write"})
	call(narrow, "POST", "/api/admin/path-routes", input, 403)
	route := mustJSON[store.PathRoute](t, call(r.c, "POST", "/api/admin/path-routes", input, 200))
	url := "/api/admin/path-routes/" + itoa(route.ID)
	call(narrow, "GET", url+"/health", nil, 403)
	reader := accessAuditToken(t, r, 1, "read", []string{"path-health:read"})
	view := mustJSON[store.PathHealthView](t, call(reader, "GET", url+"/health", nil, 200))
	if view.Status != "unknown" {
		t.Fatal("configured means healthy", view)
	}
	st := getState()
	if len(st.Forwards) != 1 || st.Forwards[0].PathHealth == nil {
		t.Fatal(st.Forwards)
	}
	f := st.Forwards[0]
	phealth := f.PathHealth
	c := phealth.Candidates[0]
	push(st, []agentproto.PathHealthSample{{Tag: f.Tag, RouteID: route.ID, ScopeID: phealth.ScopeID, CandidateID: c.ID, ConfigDigest: phealth.Digest(), CandidateDigest: c.Digest, Epoch: "http-fixture", Sequence: 1, ObservedAt: time.Now().UTC(), Health: "up", Result: "success", RTTMillis: 1}})
	view = mustJSON[store.PathHealthView](t, call(reader, "GET", url+"/health", nil, 200))
	if view.Status != "healthy" || len(view.Candidates) != 1 || view.Candidates[0].FreshForMS <= 0 {
		t.Fatal(view)
	}
	// The old route editor can rename without receiving health mutation rights.
	route.Health = nil
	route.Name = "Renamed"
	route = mustJSON[store.PathRoute](t, call(narrow, "PUT", url, route, 200))
	if route.Health == nil || !route.Health.Enabled {
		t.Fatal("old editor disabled health")
	}
	call(narrow, "DELETE", url+"?version="+itoa(route.Version), nil, 403)
	call(reader, "PUT", url+"/health", map[string]any{"version": route.Version, "health": store.PathRouteHealth{}}, 403)
	for _, role := range []string{domain.RoleOperator, domain.RoleSupport} {
		_, c := accessAuditStaff(t, r, role)
		call(c, "GET", url+"/health", nil, 403)
	}
	writer := accessAuditToken(t, r, 1, "full", []string{"path-health:write"})
	call(writer, "PUT", url+"/health", map[string]any{"version": route.Version - 1, "health": store.PathRouteHealth{}}, 409)
	route = mustJSON[store.PathRoute](t, call(writer, "PUT", url+"/health", map[string]any{"version": route.Version, "health": store.PathRouteHealth{}}, 200))
	view = mustJSON[store.PathHealthView](t, call(reader, "GET", url+"/health", nil, 200))
	if view.Status != "disabled" || len(view.Candidates) != 0 {
		t.Fatal("disabled retained green", view)
	}
	call(narrow, "DELETE", url+"?version="+itoa(route.Version), nil, 200)
}
