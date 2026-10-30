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

func TestPathRolloutHTTPScopesGraphAndPublication(t *testing.T) {
	r := newRig(t)
	web := New(config.Default(), r.st, slog.Default())
	srv := httptest.NewServer(web.Handler())
	defer srv.Close()
	r.srv, r.c.srv, r.agent.srv = srv, srv, srv
	ctx := context.Background()
	call := func(c *client, method, path string, body any, want int) []byte {
		t.Helper()
		code, b, h := c.do(method, path, body, nil)
		if code != want {
			t.Fatalf("%s %s: want %d got %d %s", method, path, want, code, b)
		}
		if code == 200 && bytes.Contains([]byte(path), []byte("path-rollouts")) && h.Get("Cache-Control") != "no-store" {
			t.Fatal("path release response cached")
		}
		return b
	}
	call(r.c, "PATCH", "/api/admin/nodes/"+itoa(r.nodeID), map[string]any{"Name": "Landing", "PublicAddr": "203.0.113.30"}, 200)
	ibs, e := r.st.AllInboundsByNode(ctx, r.nodeID)
	if e != nil {
		t.Fatal(e)
	}
	agents := map[int64]*client{r.nodeID: r.agent}
	ids := []int64{}
	for i, addr := range []string{"192.0.2.10", "198.51.100.20", "192.0.2.30"} {
		n := mustJSON[map[string]any](t, call(r.c, "POST", "/api/admin/nodes", map[string]any{"Name": "Relay " + itoa(int64(i)), "PublicAddr": addr}, 200))
		a := &client{t: t, srv: srv}
		a.token = mustJSON[agentproto.PairResponse](t, call(a, "POST", "/api/agent/pair", agentproto.PairRequest{Code: n["pair_code"].(string), Version: "v0.85.0"}, 200)).Token
		id := int64(n["id"].(float64))
		ids = append(ids, id)
		agents[id] = a
	}
	seq := map[int64]uint64{}
	fetch := func(id int64) agentproto.State {
		t.Helper()
		code, b, _ := agents[id].do("GET", "/api/agent/state", nil, map[string]string{agentproto.ConfigSessionHeader: "0123456789abcdef0123456789abcdef"})
		if code != 200 {
			t.Fatal(code, string(b))
		}
		return mustJSON[agentproto.State](t, b)
	}
	ack := func(id int64) {
		t.Helper()
		s := fetch(id)
		if s.ConfigDelivery == nil {
			t.Fatal("missing delivery")
		}
		seq[id]++
		call(agents[id], "POST", "/api/agent/report", agentproto.Report{Revision: s.Revision, AppliedRevision: s.Revision, PathHealth: &agentproto.PathHealthReport{Supported: true}, ConfigReceipt: &agentproto.ConfigReceipt{DeliveryID: s.ConfigDelivery.ID, Session: s.ConfigDelivery.Session, ConfigRevision: s.ConfigRevision, StateRevision: s.Revision, Sequence: seq[id], ObservedAt: time.Now(), Applied: true}}, 200)
	}
	for id := range agents {
		ack(id)
	}
	path := mustJSON[store.ForwardPath](t, call(r.c, "POST", "/api/admin/forward-paths", store.ForwardPath{Name: "Two-hop pin", Steps: []store.PathStep{{NodeID: ids[1]}, {NodeID: ids[2]}}}, 200))
	short := mustJSON[store.ForwardPath](t, call(r.c, "POST", "/api/admin/forward-paths", store.ForwardPath{Name: "Shared server pin", Steps: []store.PathStep{{NodeID: ids[2]}}}, 200))
	draft := store.PathRoute{Name: "Staged complete route", Entry: store.LineMember{NodeID: ids[0], Port: 22000}, TargetInboundID: ibs[0].ID, Branches: []store.PathBranch{{PathID: path.ID, PathRevision: path.Version, Ports: []int{22001, 22002}}, {PathID: short.ID, PathRevision: short.Version, Ports: []int{22003}}}}
	body := map[string]any{"route": draft, "timeout_seconds": 60}
	anon := &client{t: t, srv: srv}
	for _, method := range []string{"GET", "POST"} {
		call(anon, method, "/api/admin/path-rollouts", body, 401)
	}
	for _, role := range []string{domain.RoleOperator, domain.RoleSupport} {
		u, c := accessAuditStaff(t, r, role)
		call(c, "POST", "/api/admin/path-rollouts/preview", body, 403)
		token := accessAuditToken(t, r, u.ID, "full", []string{"path-rollouts:*"})
		call(token, "GET", "/api/admin/path-rollouts", nil, 403)
	}
	reader := accessAuditToken(t, r, 1, "read", []string{"path-rollouts:read"})
	writer := accessAuditToken(t, r, 1, "full", []string{"path-rollouts:write"})
	ordinary := accessAuditToken(t, r, 1, "full", []string{"line-rollouts:*"})
	call(reader, "POST", "/api/admin/path-rollouts/preview", body, 403)
	call(writer, "GET", "/api/admin/path-rollouts", nil, 403)
	call(ordinary, "POST", "/api/admin/path-rollouts", body, 403)
	call(accessAuditToken(t, r, 1, "full", []string{"path-routes:*"}), "POST", "/api/admin/path-rollouts", body, 403)
	healthDraft := draft
	healthDraft.Health = &store.PathRouteHealth{Enabled: true}
	call(writer, "POST", "/api/admin/path-rollouts", map[string]any{"route": healthDraft}, 403)
	// Having a health-only token does not grant the separate rollout resource.
	call(accessAuditToken(t, r, 1, "full", []string{"path-health:write"}), "POST", "/api/admin/path-rollouts", map[string]any{"route": healthDraft}, 403)
	logs, _ := r.st.AdminLog(ctx, 0, 500)
	preview := mustJSON[store.PathRolloutPreview](t, call(writer, "POST", "/api/admin/path-rollouts/preview", body, 200))
	after, _ := r.st.AdminLog(ctx, 0, 500)
	if len(logs) != len(after) {
		t.Fatal("preview wrote audit")
	}
	body["route"] = preview.Draft
	draft = preview.Draft
	draft.PreviewRevision = preview.PreviewRevision
	body["route"] = draft
	p := mustJSON[store.LineRollout](t, call(writer, "POST", "/api/admin/path-rollouts", body, 200))
	base := "/api/admin/path-rollouts/" + p.ID
	call(ordinary, "GET", "/api/admin/line-rollouts/"+p.ID, nil, 404)
	call(ordinary, "POST", "/api/admin/line-rollouts/"+p.ID+"/pause", map[string]any{"version": p.Version}, 404)
	if len(mustJSON[store.LineRolloutPage](t, call(ordinary, "GET", "/api/admin/line-rollouts", nil, 200)).Items) != 0 {
		t.Fatal("ordinary history exposed path plan")
	}
	call(r.c, "POST", "/api/admin/entries", map[string]any{"Name": "Too early", "LineID": p.LineID}, 400)
	p = mustJSON[store.LineRollout](t, call(writer, "POST", base+"/pause", map[string]any{"version": p.Version}, 200))
	call(writer, "POST", base+"/retry", map[string]any{"version": p.Version - 1}, 409)
	p = mustJSON[store.LineRollout](t, call(writer, "POST", base+"/retry", map[string]any{"version": p.Version}, 200))
	for i := 0; i < 12 && p.Status != "completed"; i++ {
		for _, step := range p.Targets {
			if step.Status == "released" {
				ack(step.NodeID)
			}
		}
		if e = web.advanceLineRollouts(ctx); e != nil {
			t.Fatal(e)
		}
		p = mustJSON[store.LineRollout](t, call(reader, "GET", base, nil, 200))
	}
	if p.Status != "completed" || p.AfterRoute == nil || len(p.Targets) != 4 {
		t.Fatal("path did not publish complete graph", p)
	}
	saved := mustJSON[store.PathRoute](t, call(r.c, "GET", "/api/admin/path-routes/"+itoa(p.LineID), nil, 200))
	if saved.Entry.Port != draft.Entry.Port {
		t.Fatal(saved)
	}
	entry := mustJSON[domain.Entry](t, call(r.c, "POST", "/api/admin/entries", map[string]any{"Name": "Published route", "LineID": p.LineID}, 200))
	if entry.DisplayPort != draft.Entry.Port {
		t.Fatal(entry)
	}
	healthDraft = saved
	healthDraft.Health = &store.PathRouteHealth{Enabled: true}
	call(writer, "POST", "/api/admin/path-rollouts", map[string]any{"route": healthDraft}, 403)
	healthWriter := accessAuditToken(t, r, 1, "full", []string{"path-rollouts:write", "path-health:write"})
	healthDraft.Entry.Port += 1000
	for i := range healthDraft.Branches {
		healthDraft.Branches[i].Ports = append([]int(nil), healthDraft.Branches[i].Ports...)
		for j := range healthDraft.Branches[i].Ports {
			healthDraft.Branches[i].Ports[j] += 1000
		}
	}
	healthPlan := mustJSON[store.LineRollout](t, call(healthWriter, "POST", "/api/admin/path-rollouts", map[string]any{"route": healthDraft}, 200))
	drain := func(plan store.LineRollout) store.LineRollout {
		t.Helper()
		for i := 0; i < 20 && plan.Status != "completed"; i++ {
			for _, step := range plan.Targets {
				if step.Status == "released" {
					ack(step.NodeID)
				}
			}
			if err := web.advanceLineRollouts(ctx); err != nil {
				t.Fatal(err)
			}
			plan = mustJSON[store.LineRollout](t, call(reader, "GET", "/api/admin/path-rollouts/"+plan.ID, nil, 200))
		}
		if plan.Status != "completed" {
			t.Fatal("health release incomplete", plan.ErrorCode)
		}
		return plan
	}
	healthPlan = drain(healthPlan)
	healthCurrent := mustJSON[store.PathRoute](t, call(r.c, "GET", "/api/admin/path-routes/"+itoa(p.LineID), nil, 200))
	if healthCurrent.Health == nil || !healthCurrent.Health.Enabled {
		t.Fatal("health plan not published", healthCurrent)
	}
	call(writer, "POST", "/api/admin/path-rollouts", map[string]any{"route": healthCurrent, "remove": true}, 403)
	disabled := healthCurrent
	disabled.Health = &store.PathRouteHealth{}
	call(writer, "POST", "/api/admin/path-rollouts", map[string]any{"route": disabled}, 403)
	compensatePath := "/api/admin/path-rollouts/" + healthPlan.ID + "/compensate"
	call(writer, "POST", compensatePath, map[string]any{"version": healthPlan.Version}, 403)
	undo := mustJSON[store.LineRollout](t, call(healthWriter, "POST", compensatePath, map[string]any{"version": healthPlan.Version}, 200))
	undo = drain(undo)
	if undo.AfterRoute == nil || undo.AfterRoute.Health == nil || undo.AfterRoute.Health.Enabled {
		t.Fatal("compensation did not restore disabled intent")
	}
	// A path-scoped token cannot read or mutate an ordinary plan by guessing its ID.
	for id := range agents {
		ack(id)
	}
	plain := store.ForwardLine{Name: "Ordinary", Protocol: "tcp", Backend: "relay", TargetInboundID: ibs[0].ID, Groups: []store.LineGroup{{Members: []store.LineMember{{NodeID: ids[0], Port: 24000}}}}}
	op := mustJSON[store.LineRollout](t, call(ordinary, "POST", "/api/admin/line-rollouts", map[string]any{"line": plain}, 200))
	call(reader, "GET", "/api/admin/path-rollouts/"+op.ID, nil, 404)
	call(writer, "POST", "/api/admin/path-rollouts/"+op.ID+"/pause", map[string]any{"version": op.Version}, 404)
	if len(mustJSON[store.LineRolloutPage](t, call(reader, "GET", "/api/admin/path-rollouts", nil, 200)).Items) != 3 {
		t.Fatal("path history exposed ordinary plan")
	}
	for _, q := range []string{"route_id=bad", "before=-1", "limit=0"} {
		call(reader, "GET", "/api/admin/path-rollouts?"+q, nil, 400)
	}
}
