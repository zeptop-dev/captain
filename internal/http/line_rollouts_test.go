package http

import (
	"bytes"
	"context"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/captain/internal/config"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

// Use the real admin, state and authenticated receipt HTTP paths. The scheduler
// runs explicitly so the test also verifies its cache invalidation without sleeps.
func TestLineRolloutHTTPPermissionsPublicationAndRevocation(t *testing.T) {
	r := newRig(t)
	web := New(config.Default(), r.st, slog.Default())
	srv := httptest.NewServer(web.Handler())
	t.Cleanup(srv.Close)
	r.srv, r.c.srv, r.agent.srv = srv, srv, srv
	ctx := context.Background()
	call := func(c *client, method, path string, body any, want int) []byte {
		t.Helper()
		code, b, h := c.do(method, path, body, nil)
		if code != want {
			t.Fatalf("%s %s: want %d got %d %s", method, path, want, code, b)
		}
		if code == 200 && bytes.Contains([]byte(path), []byte("line-rollouts")) && h.Get("Cache-Control") != "no-store" {
			t.Fatal("release history may be cached")
		}
		return b
	}
	call(r.c, "PATCH", "/api/admin/nodes/"+itoa(r.nodeID), map[string]any{"Name": "Landing", "PublicAddr": "203.0.113.30"}, 200)
	ibs, err := r.st.AllInboundsByNode(ctx, r.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	userID, _ := r.user("rollout@example.com")
	agents := map[int64]*client{r.nodeID: r.agent}
	for _, addr := range []string{"192.0.2.10", "198.51.100.20"} {
		n := mustJSON[map[string]any](t, call(r.c, "POST", "/api/admin/nodes", map[string]any{"Name": "Relay", "PublicAddr": addr}, 200))
		a := &client{t: t, srv: srv}
		a.token = mustJSON[agentproto.PairResponse](t, call(a, "POST", "/api/agent/pair", agentproto.PairRequest{Code: n["pair_code"].(string), Version: "v0.84.0"}, 200)).Token
		agents[int64(n["id"].(float64))] = a
	}
	seq := map[int64]uint64{}
	fetch := func(id int64) agentproto.State {
		t.Helper()
		code, b, _ := agents[id].do("GET", "/api/agent/state", nil, map[string]string{agentproto.ConfigSessionHeader: "0123456789abcdef0123456789abcdef"})
		if code != 200 {
			t.Fatalf("state: %d %s", code, b)
		}
		st := mustJSON[agentproto.State](t, b)
		if st.ConfigDelivery == nil {
			t.Fatal("no configuration challenge")
		}
		return st
	}
	receipt := func(id int64, st agentproto.State, applied bool) {
		t.Helper()
		seq[id]++
		v := agentproto.ConfigReceipt{DeliveryID: st.ConfigDelivery.ID, Session: st.ConfigDelivery.Session, ConfigRevision: st.ConfigRevision, StateRevision: st.Revision, Sequence: seq[id], ObservedAt: time.Now(), Applied: applied}
		call(agents[id], "POST", "/api/agent/report", agentproto.Report{Revision: st.Revision, AppliedRevision: st.Revision, ConfigReceipt: &v}, 200)
	}
	for id := range agents {
		receipt(id, fetch(id), true)
	}
	groups := []store.LineGroup{}
	for id := r.nodeID + 1; id <= r.nodeID+2; id++ {
		groups = append(groups, store.LineGroup{Members: []store.LineMember{{NodeID: id, Port: 18443}}})
	}
	draft := store.ForwardLine{Name: "Staged route", Protocol: "tcp", Backend: "relay", TargetInboundID: ibs[0].ID, Groups: groups}
	input := map[string]any{"line": draft, "timeout_seconds": 60}
	anonymous := &client{t: t, srv: srv}
	for _, path := range []string{"/api/admin/line-rollouts", "/api/admin/line-rollouts/missing"} {
		call(anonymous, "GET", path, nil, 401)
	}
	call(anonymous, "POST", "/api/admin/line-rollouts/preview", input, 401)
	for _, role := range []string{domain.RoleOperator, domain.RoleSupport} {
		u, c := accessAuditStaff(t, r, role)
		call(c, "POST", "/api/admin/line-rollouts/preview", input, 403)
		call(accessAuditToken(t, r, u.ID, "full", []string{"line-rollouts:read", "line-rollouts:write"}), "GET", "/api/admin/line-rollouts", nil, 403)
	}
	reader := accessAuditToken(t, r, 1, "read", []string{"line-rollouts:read"})
	writer := accessAuditToken(t, r, 1, "full", []string{"line-rollouts:write"})
	call(reader, "GET", "/api/admin/line-rollouts", nil, 200)
	call(reader, "POST", "/api/admin/line-rollouts/preview", input, 403)
	call(writer, "GET", "/api/admin/line-rollouts", nil, 403)
	call(accessAuditToken(t, r, 1, "full", []string{"lines:write"}), "POST", "/api/admin/line-rollouts", input, 403)
	code, _, _ := r.c.do("POST", "/api/admin/line-rollouts/preview", input, map[string]string{"Origin": "https://other.example.com"})
	if code != 403 {
		t.Fatal("cross-origin preview accepted", code)
	}
	logs, _ := r.st.AdminLog(ctx, 0, 500)
	p := mustJSON[store.LineRolloutPreview](t, call(writer, "POST", "/api/admin/line-rollouts/preview", input, 200))
	after, _ := r.st.AdminLog(ctx, 0, 500)
	if len(logs) != len(after) {
		t.Fatal("preview wrote audit")
	}
	if lines, e := r.st.ListLines(ctx); e != nil || len(lines) != 0 {
		t.Fatal("preview wrote configuration", e)
	}
	draft = p.Draft
	draft.PreviewRevision = p.PreviewRevision
	input["line"] = draft
	plan := mustJSON[store.LineRollout](t, call(writer, "POST", "/api/admin/line-rollouts", input, 200))
	base := "/api/admin/line-rollouts/" + plan.ID
	read := func() store.LineRollout { return mustJSON[store.LineRollout](t, call(reader, "GET", base, nil, 200)) }
	if len(plan.Targets) != 2 || plan.Targets[0].NodeID != r.nodeID+2 {
		t.Fatal("wrong dependency order", plan)
	}
	if len(fetch(r.nodeID+1).Forwards) != 0 || len(fetch(r.nodeID+2).Forwards) != 1 {
		t.Fatal("future target escaped snapshot hold")
	}
	call(r.c, "POST", "/api/admin/entries", map[string]any{"Name": "Too early", "LineID": plan.LineID, "Enabled": true}, 409)
	call(reader, "POST", base+"/pause", map[string]any{"version": plan.Version}, 403)
	plan = mustJSON[store.LineRollout](t, call(writer, "POST", base+"/pause", map[string]any{"version": plan.Version}, 200))
	call(writer, "POST", base+"/retry", map[string]any{"version": plan.Version - 1}, 409)
	// Authorizations are always built from current users, even while structural
	// publication is paused. No snapshot contains the customer's credentials.
	if len(fetch(r.nodeID).Users) == 0 {
		t.Fatal("fixture has no active user")
	}
	call(r.c, "PATCH", "/api/admin/users/"+itoa(userID), map[string]any{"Status": "banned"}, 200)
	st := fetch(r.nodeID)
	if len(st.Users) != 0 {
		t.Fatal("paused rollout froze revoked user")
	}
	receipt(r.nodeID, st, true)
	if read().Status != "paused" {
		t.Fatal("unrelated authorization resumed plan")
	}
	plan = mustJSON[store.LineRollout](t, call(writer, "POST", base+"/retry", map[string]any{"version": plan.Version}, 200))
	for i := 0; i < 8 && plan.Status != "completed"; i++ {
		for _, step := range plan.Targets {
			if step.Status == "released" {
				receipt(step.NodeID, fetch(step.NodeID), true)
			}
		}
		if e := web.advanceLineRollouts(ctx); e != nil {
			t.Fatal(e)
		}
		plan = read()
	}
	if plan.Status != "completed" || plan.PublishedAt == nil {
		t.Fatal("plan did not finish", plan)
	}
	call(r.c, "POST", "/api/admin/entries", map[string]any{"Name": "Published", "LineID": plan.LineID, "Enabled": true}, 200)
	for _, query := range []string{"limit=0", "before=-1", "line_id=bad"} {
		call(reader, "GET", "/api/admin/line-rollouts?"+query, nil, 400)
	}
	call(reader, "GET", "/api/admin/line-rollouts/missing", nil, 404)
	data := call(reader, "GET", base, nil, 200)
	for _, a := range agents {
		if bytes.Contains(data, []byte(a.token)) || bytes.Contains(data, []byte("token_hash")) {
			t.Fatal("snapshot exposed node credential")
		}
	}
}
