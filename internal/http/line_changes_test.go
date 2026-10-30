package http

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestLineChangesDeliveryHistoryAndPermissions(t *testing.T) {
	r := newRig(t)
	call := func(c *client, method, path string, body any, want int) []byte {
		t.Helper()
		code, b, h := c.do(method, path, body, nil)
		if code != want {
			t.Fatalf("%s %s: want %d got %d %s", method, path, want, code, b)
		}
		if code == 200 && method == "GET" && bytes.Contains([]byte(path), []byte("line-changes")) && h.Get("Cache-Control") != "no-store" {
			t.Fatal("change evidence may be cached")
		}
		return b
	}
	call(r.c, "PATCH", "/api/admin/nodes/"+itoa(r.nodeID), map[string]any{"Name": "Landing", "PublicAddr": "203.0.113.30"}, 200)
	ibs, err := r.st.AllInboundsByNode(context.Background(), r.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	n := mustJSON[map[string]any](t, call(r.c, "POST", "/api/admin/nodes", map[string]any{"Name": "Relay", "PublicAddr": "192.0.2.10"}, 200))
	nodeID := int64(n["id"].(float64))
	a := &client{t: t, srv: r.srv}
	a.token = mustJSON[agentproto.PairResponse](t, call(a, "POST", "/api/agent/pair", agentproto.PairRequest{Code: n["pair_code"].(string), Version: "v0.83.0"}, 200)).Token
	input := store.ForwardLine{Name: "Tracked line", Protocol: "tcp", Backend: "relay", TargetInboundID: ibs[0].ID, Groups: []store.LineGroup{{Members: []store.LineMember{{NodeID: nodeID, Port: 18443}}}}}
	type savedLine struct {
		store.ForwardLine
		ChangeID string `json:"change_id"`
	}
	l := mustJSON[savedLine](t, call(r.c, "POST", "/api/admin/lines", input, 200))
	if l.ChangeID == "" {
		t.Fatal("save has no atomic change identity")
	}
	path := "/api/admin/lines/" + itoa(l.ID)
	listPath := "/api/admin/line-changes?line_id=" + itoa(l.ID)
	detail := func(id string) store.LineChange {
		return mustJSON[store.LineChange](t, call(r.c, "GET", "/api/admin/line-changes/"+id, nil, 200))
	}
	first := detail(l.ChangeID)
	if first.Kind != "save" || first.Actor != "admin@test" || first.Status != "pending" || len(first.Targets) != 1 || first.Targets[0].NodeID != nodeID || len(first.Targets[0].Before) != 0 || len(first.Targets[0].After) != 1 {
		t.Fatalf("wrong change: %+v", first)
	}
	// Landing dependencies are not targets: saving the line changed only the relay.
	anonymous := &client{t: t, srv: r.srv}
	call(anonymous, "GET", listPath, nil, 401)
	call(anonymous, "GET", "/api/admin/line-changes/"+first.ID, nil, 401)
	for _, role := range []string{domain.RoleOperator, domain.RoleSupport} {
		u, c := accessAuditStaff(t, r, role)
		call(c, "GET", listPath, nil, 403)
		call(accessAuditToken(t, r, u.ID, "read", []string{"line-changes:read"}), "GET", "/api/admin/line-changes/"+first.ID, nil, 403)
	}
	for _, scope := range []string{"nodes:read", "entries:read", "line-changes:write"} {
		call(accessAuditToken(t, r, 1, "full", []string{scope}), "GET", listPath, nil, 403)
	}
	reader := accessAuditToken(t, r, 1, "read", []string{"line-changes:read"})
	call(reader, "GET", listPath, nil, 200)
	call(reader, "GET", "/api/admin/line-changes/"+first.ID, nil, 200)
	call(reader, "PUT", path, l.ForwardLine, 403)
	for _, query := range []string{"line_id=-1", "line_id=no", "before=-1", "limit=0", "limit=101"} {
		call(r.c, "GET", "/api/admin/line-changes?"+query, nil, 400)
	}
	call(r.c, "GET", "/api/admin/line-changes/missing", nil, 404)
	headers := map[string]string{agentproto.ConfigSessionHeader: "0123456789abcdef0123456789abcdef"}
	fetch := func() agentproto.State {
		t.Helper()
		code, b, _ := a.do("GET", "/api/agent/state", nil, headers)
		if code != 200 {
			t.Fatalf("state: %d %s", code, b)
		}
		st := mustJSON[agentproto.State](t, b)
		if st.ConfigDelivery == nil {
			t.Fatal("missing delivery")
		}
		return st
	}
	var seq uint64
	report := func(st agentproto.State, applied bool) {
		t.Helper()
		seq++
		receipt := agentproto.ConfigReceipt{DeliveryID: st.ConfigDelivery.ID, Session: st.ConfigDelivery.Session, ConfigRevision: st.ConfigRevision, StateRevision: st.Revision, Sequence: seq, ObservedAt: time.Now(), Applied: applied}
		call(a, "POST", "/api/agent/report", agentproto.Report{Revision: st.Revision, AppliedRevision: st.Revision, TrafficEpoch: "abcdef0123456789abcdef0123456789", TrafficSeq: 1, ConfigReceipt: &receipt}, 200)
	}
	st := fetch()
	if got := detail(first.ID); got.Targets[0].Status != "delivered" {
		t.Fatal(got)
	}
	report(st, true)
	if got := detail(first.ID); got.Status != "applied" || got.Targets[0].ConfirmedAt == nil || got.Targets[0].FreshForMS <= 0 {
		t.Fatal(got)
	}
	report(st, false)
	if got := detail(first.ID); got.Targets[0].Status != "not_applied" || got.Targets[0].ConfirmedAt == nil {
		t.Fatal("negative lost history", got)
	}
	report(st, true)
	l.Name = "Renamed line"
	l = mustJSON[savedLine](t, call(r.c, "PUT", path, l.ForwardLine, 200))
	if got := detail(l.ChangeID); got.Status != "no_runtime_change" || len(got.Targets) != 0 {
		t.Fatal(got)
	}
	if got := detail(first.ID); got.Status != "applied" {
		t.Fatal("metadata superseded runtime target", got)
	}
	// Stale saves cannot leave history rows, and automatic recompiles carry the authenticated actor.
	before := mustJSON[store.LineChangePage](t, call(r.c, "GET", listPath, nil, 200))
	stale := l.ForwardLine
	stale.Version--
	call(r.c, "PUT", path, stale, 409)
	if got := mustJSON[store.LineChangePage](t, call(r.c, "GET", listPath, nil, 200)); len(got.Items) != len(before.Items) {
		t.Fatal("failed save recorded change")
	}
	call(r.c, "PATCH", "/api/admin/nodes/"+itoa(r.nodeID), map[string]any{"Name": "Landing", "PublicAddr": "198.51.100.20"}, 200)
	page := mustJSON[store.LineChangePage](t, call(r.c, "GET", listPath+"&limit=1", nil, 200))
	if len(page.Items) != 1 || page.NextBefore == 0 || page.Items[0].Kind != "recompile" || page.Items[0].Actor != "admin@test" {
		t.Fatal(page)
	}
	automatic := page.Items[0]
	older := mustJSON[store.LineChangePage](t, call(r.c, "GET", listPath+"&before="+itoa(page.NextBefore)+"&limit=1", nil, 200))
	if len(older.Items) != 1 || older.Items[0].ID == automatic.ID {
		t.Fatal("cursor repeated latest", older)
	}
	report(st, true)
	if got := detail(automatic.ID); got.Status != "pending" {
		t.Fatal("old delivery confirmed new change", got)
	}
	st = fetch()
	report(st, true)
	if got := detail(automatic.ID); got.Status != "applied" {
		t.Fatal(got)
	}
	live := mustJSON[store.ForwardLine](t, call(r.c, "GET", path, nil, 200))
	deleted := mustJSON[struct {
		OK       bool   `json:"ok"`
		ChangeID string `json:"change_id"`
	}](t, call(r.c, "DELETE", path+"?version="+itoa(live.Version), nil, 200))
	if !deleted.OK || deleted.ChangeID == "" {
		t.Fatal(deleted)
	}
	removal := detail(deleted.ChangeID)
	if removal.Kind != "delete" || len(removal.Targets) != 1 || removal.Targets[0].Action != "remove" || len(removal.Targets[0].After) != 0 {
		t.Fatal(removal)
	}
	st = fetch()
	if len(st.Forwards) != 0 {
		t.Fatal("deleted rules delivered")
	}
	report(st, true)
	if got := detail(removal.ID); got.Status != "applied" {
		t.Fatal(got)
	}
	data := call(r.c, "GET", "/api/admin/line-changes", nil, 200)
	if bytes.Contains(data, []byte(a.token)) || bytes.Contains(data, []byte("token_hash")) {
		t.Fatal("history exposed agent credential")
	}
	call(r.c, "GET", listPath, nil, 200) // Deleted lines remain inspectable.
}
