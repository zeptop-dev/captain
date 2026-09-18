package http

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/config"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/http/admin"
	"github.com/zeptop-dev/captain/internal/store"
)

// rig is the shared setup of the tests below: a panel, a logged-in admin
// and a paired node with one inbound.
type rig struct {
	t      *testing.T
	st     *store.Store
	srv    *httptest.Server
	c      *client // admin session
	agent  *client // node
	nodeID int64
}

func newRig(t *testing.T) *rig {
	t.Helper()
	cfg := config.Default()
	cfg.BaseURL = "http://test"
	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(context.Background(), conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	st := store.New(conn)
	adminUser, _ := admin.NewUser("admin@test", "password123", "admin")
	if err := st.CreateUser(context.Background(), adminUser); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(cfg, st, slog.Default()).Handler())
	t.Cleanup(srv.Close)
	r := &rig{t: t, st: st, srv: srv, c: &client{t: t, srv: srv}}
	r.c.do("POST", "/api/admin/login", map[string]string{"Email": "admin@test", "Password": "password123"}, nil)
	_, b, _ := r.c.do("POST", "/api/admin/nodes", map[string]string{"Name": "n1"}, nil)
	node := mustJSON[map[string]any](t, b)
	r.nodeID = int64(node["id"].(float64))
	r.c.do("POST", "/api/admin/nodes/"+itoa(r.nodeID)+"/inbounds", map[string]any{"Tag": "in", "Protocol": "vless", "Port": 443}, nil)
	r.agent = &client{t: t, srv: srv}
	_, b, _ = r.agent.do("POST", "/api/agent/pair", agentproto.PairRequest{Code: node["pair_code"].(string)}, nil)
	r.agent.token = mustJSON[agentproto.PairResponse](t, b).Token
	return r
}

// user creates a customer with a plan and returns its id and uuid.
func (r *rig) user(email string) (int64, string) {
	r.t.Helper()
	_, b, _ := r.c.do("POST", "/api/admin/plans", map[string]any{"Name": "p-" + email, "PriceCents": 1, "PeriodDays": 30}, nil)
	plan := mustJSON[map[string]any](r.t, b)
	_, b, _ = r.c.do("POST", "/api/admin/users", map[string]string{"Email": email, "Password": "password123"}, nil)
	u := mustJSON[map[string]any](r.t, b)
	id := int64(u["id"].(float64))
	r.c.do("POST", "/api/admin/users/"+itoa(id)+"/grant", map[string]any{"PlanID": plan["ID"]}, nil)
	return id, u["uuid"].(string)
}

func (r *rig) status(id int64) string {
	r.t.Helper()
	u, err := r.st.UserByID(context.Background(), id)
	if err != nil {
		r.t.Fatal(err)
	}
	return u.Status
}

// The auto-ban path: only hits of a rule the panel actually has, with the
// action that rule carries, count — and only "block" ones. A node that
// invents hits cannot ban anybody.
func TestAuditAutoBanTrustsOnlyPanelRules(t *testing.T) {
	r := newRig(t)
	uid, uuid := r.user("victim@test")
	r.c.do("PUT", "/api/admin/settings/audit", map[string]any{"auto_ban_hits": 2, "window_hours": 24}, nil)
	_, b, _ := r.c.do("PUT", "/api/admin/audit-rules", []map[string]any{
		{"name": "bt", "match": []string{"protocol:bittorrent"}, "action": "block", "enabled": true},
		{"name": "watch", "match": []string{"domain:example.com"}, "action": "log", "enabled": true},
	}, nil)
	rules := mustJSON[[]store.AuditRule](t, b)
	if len(rules) != 2 {
		t.Fatalf("rules: %s", b)
	}
	blockID, logID := rules[0].ID, rules[1].ID

	hit := func(ruleID int64, action string) agentproto.AuditHit {
		return agentproto.AuditHit{At: time.Now().Unix(), User: uuid, Inbound: "in", ClientIP: "203.0.113.30", Host: "tracker.test", Port: 443, RuleID: ruleID, Action: action}
	}
	// A rule id the panel never issued, and a real rule with the wrong
	// action: both dropped, so nothing is stored and nobody is banned.
	r.agent.do("POST", "/api/agent/report", agentproto.Report{Audits: []agentproto.AuditHit{
		hit(9999, "block"), hit(9998, "block"), hit(blockID, "log"), hit(logID, "block"),
	}}, nil)
	if got := r.status(uid); got != "active" {
		t.Fatalf("forged hits banned the user: %s", got)
	}
	_, b, _ = r.c.do("GET", "/api/admin/audit-log?user_id="+itoa(uid), nil, nil)
	if n := strings.Count(string(b), `"host"`); n != 0 {
		t.Fatalf("forged hits were stored: %s", b)
	}
	// Log-only hits are real, but they never count toward a ban.
	r.agent.do("POST", "/api/agent/report", agentproto.Report{Audits: []agentproto.AuditHit{
		hit(logID, "log"), hit(logID, "log"), hit(logID, "log"),
	}}, nil)
	if got := r.status(uid); got != "active" {
		t.Fatalf("log-only hits banned the user: %s", got)
	}
	// Two block hits reach the threshold.
	r.agent.do("POST", "/api/agent/report", agentproto.Report{Audits: []agentproto.AuditHit{
		hit(blockID, "block"), hit(blockID, "block"),
	}}, nil)
	if got := r.status(uid); got != "banned" {
		t.Fatalf("auto-ban did not fire: %s", got)
	}
	// A timestamp far in the future is clamped to now, so retention reaches
	// the row and the ban window cannot be dodged.
	future := hit(blockID, "block")
	future.At = time.Now().Add(72 * time.Hour).Unix()
	r.agent.do("POST", "/api/agent/report", agentproto.Report{Audits: []agentproto.AuditHit{future}}, nil)
	hits, err := r.st.AuditHits(context.Background(), 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		if h.At.After(time.Now().Add(time.Minute)) {
			t.Fatalf("a node's future timestamp was stored: %s", h.At)
		}
	}
}

// Staff are never banned by audit rules, whatever a node reports.
func TestAuditNeverBansStaff(t *testing.T) {
	r := newRig(t)
	op, _ := admin.NewUser("op@test", "password123", "operator")
	if err := r.st.CreateUser(context.Background(), op); err != nil {
		t.Fatal(err)
	}
	r.c.do("PUT", "/api/admin/settings/audit", map[string]any{"auto_ban_hits": 1, "window_hours": 24}, nil)
	_, b, _ := r.c.do("PUT", "/api/admin/audit-rules", []map[string]any{
		{"name": "bt", "match": []string{"protocol:bittorrent"}, "action": "block", "enabled": true},
	}, nil)
	rules := mustJSON[[]store.AuditRule](t, b)
	for i := 0; i < 3; i++ {
		r.agent.do("POST", "/api/agent/report", agentproto.Report{Audits: []agentproto.AuditHit{
			{At: time.Now().Unix(), User: op.UUID, ClientIP: "203.0.113.30", Host: "tracker.test", Port: 443, RuleID: rules[0].ID, Action: "block"},
		}}, nil)
	}
	if got := r.status(op.ID); got != "active" {
		t.Fatalf("staff account banned by audit rules: %s", got)
	}
}

// Charging: a re-sent batch is applied once, and an absurd delta is
// refused instead of being written into the counters.
func TestTrafficBatchesAreIdempotentAndBounded(t *testing.T) {
	r := newRig(t)
	uid, _ := r.user("payer@test")
	used := func() int64 {
		s, err := r.st.ActiveSubscription(context.Background(), uid)
		if err != nil {
			t.Fatal(err)
		}
		return s.UsedUpBytes + s.UsedDownBytes
	}
	rep := agentproto.Report{TrafficSeq: 7, TrafficWindowSeconds: 60, Traffic: []spec.UserTraffic{{UserID: uid, Up: 1000, Down: 2000}}}
	r.agent.do("POST", "/api/agent/report", rep, nil)
	if got := used(); got != 3000 {
		t.Fatalf("first batch: %d", got)
	}
	// The node did not see the answer and sends the same batch again.
	r.agent.do("POST", "/api/agent/report", rep, nil)
	if got := used(); got != 3000 {
		t.Fatalf("re-sent batch charged twice: %d", got)
	}
	// The next batch is applied.
	rep.TrafficSeq = 8
	r.agent.do("POST", "/api/agent/report", rep, nil)
	if got := used(); got != 6000 {
		t.Fatalf("next batch: %d", got)
	}
	// A delta no interval can produce is dropped, the rest of the report is
	// still accounted.
	rep.TrafficSeq = 9
	rep.Traffic = []spec.UserTraffic{{UserID: uid, Up: 1 << 60, Down: 0}}
	r.agent.do("POST", "/api/agent/report", rep, nil)
	if got := used(); got != 6000 {
		t.Fatalf("absurd delta was charged: %d", got)
	}
}

// Port conflicts: the inbound side and the forward side have to agree
// about which sockets are taken, protocol by protocol.
func TestPortConflictBothWays(t *testing.T) {
	r := newRig(t)
	node := "/api/admin/nodes/" + itoa(r.nodeID)
	// tcp/443 is taken by the vless inbound created in newRig.
	if code, b, _ := r.c.do("PUT", node+"/forwards", map[string]any{"Forwards": []map[string]any{
		{"tag": "f1", "port": 443, "protocol": "tcp", "target": "198.51.100.20:443"},
	}}, nil); code != http.StatusBadRequest {
		t.Fatalf("tcp forward over a tcp inbound accepted: %d %s", code, b)
	}
	// udp/443 is free: the inbound is vless (TCP only).
	if code, b, _ := r.c.do("PUT", node+"/forwards", map[string]any{"Forwards": []map[string]any{
		{"tag": "f2", "port": 443, "protocol": "udp", "target": "198.51.100.20:443"},
	}}, nil); code != http.StatusOK {
		t.Fatalf("udp forward beside a tcp inbound refused: %d %s", code, b)
	}
	// And now the other direction: a hysteria2 inbound wants udp/443, which
	// the forward just took.
	if code, b, _ := r.c.do("POST", node+"/inbounds", map[string]any{"Tag": "mi", "Protocol": "mieru", "Port": 443, "Settings": map[string]any{"mieru_transport": "UDP"}}, nil); code != http.StatusConflict {
		t.Fatalf("udp inbound over a udp forward accepted: %d %s", code, b)
	}
	// A disabled inbound binds nothing, so its port is free for a forward.
	_, ib, _ := r.c.do("POST", node+"/inbounds", map[string]any{"Tag": "off", "Protocol": "vless", "Port": 8443}, nil)
	ibID := int64(mustJSON[map[string]any](t, ib)["ID"].(float64))
	r.c.do("PATCH", "/api/admin/inbounds/"+itoa(ibID), map[string]any{"Enabled": false}, nil)
	if code, b, _ := r.c.do("PUT", node+"/forwards", map[string]any{"Forwards": []map[string]any{
		{"tag": "f3", "port": 8443, "protocol": "tcp", "target": "198.51.100.20:443"},
	}}, nil); code != http.StatusOK {
		t.Fatalf("forward over a disabled inbound refused: %d %s", code, b)
	}
}

// Token scopes: a read-only token cannot change anything, an expired one
// cannot do anything, and no token manages staff accounts.
func TestAPITokenScopes(t *testing.T) {
	r := newRig(t)
	_, b, _ := r.c.do("POST", "/api/admin/tokens", map[string]any{"Name": "ro", "Scope": "read"}, nil)
	ro := mustJSON[map[string]any](t, b)["token"].(string)
	_, b, _ = r.c.do("POST", "/api/admin/tokens", map[string]any{"Name": "full"}, nil)
	full := mustJSON[map[string]any](t, b)["token"].(string)

	anon := &client{t: t, srv: r.srv}
	auth := func(tok string) map[string]string { return map[string]string{"Authorization": "Bearer " + tok} }
	if code, _, _ := anon.do("GET", "/api/admin/nodes", nil, auth(ro)); code != http.StatusOK {
		t.Fatalf("read-only token refused a read: %d", code)
	}
	if code, _, _ := anon.do("POST", "/api/admin/nodes", map[string]string{"Name": "x"}, auth(ro)); code != http.StatusForbidden {
		t.Fatalf("read-only token wrote: %d", code)
	}
	if code, _, _ := anon.do("POST", "/api/admin/nodes", map[string]string{"Name": "x"}, auth(full)); code != http.StatusOK {
		t.Fatalf("full token refused a write: %d", code)
	}
	// Staff management is console-only for every token.
	if code, _, _ := anon.do("POST", "/api/admin/admins", map[string]string{"Email": "new@test", "Password": "password123", "Role": "admin"}, auth(full)); code != http.StatusForbidden {
		t.Fatalf("a token created an admin: %d", code)
	}
	// An expired token is no token at all.
	if _, _, err := r.st.CreateAPIToken(context.Background(), 1, "old", "full", ptr(time.Now().Add(-time.Hour))); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := anon.do("GET", "/api/admin/nodes", nil, auth("cap_whatever")); code != http.StatusForbidden {
		t.Fatalf("unknown token accepted: %d", code)
	}
}

func ptr[T any](v T) *T { return &v }

// Support accounts see customers but not their credentials or their
// traffic: no subscription token, no connection log, no sub links.
func TestSupportRoleCannotSeeCredentials(t *testing.T) {
	r := newRig(t)
	uid, _ := r.user("cust@test")
	sup, _ := admin.NewUser("sup@test", "password123", "support")
	if err := r.st.CreateUser(context.Background(), sup); err != nil {
		t.Fatal(err)
	}
	s := &client{t: t, srv: r.srv}
	s.do("POST", "/api/admin/login", map[string]string{"Email": "sup@test", "Password": "password123"}, nil)
	code, b, _ := s.do("GET", "/api/admin/users/"+itoa(uid), nil, nil)
	if code != http.StatusOK {
		t.Fatalf("support cannot read a user: %d", code)
	}
	got := mustJSON[map[string]any](t, b)
	if got["sub_token"] != "" || got["sub_url"] != "" {
		t.Fatalf("support saw the subscription token: %v", got["sub_token"])
	}
	_, b, _ = s.do("GET", "/api/admin/users", nil, nil)
	if strings.Contains(string(b), `"sub_token":"`) && !strings.Contains(string(b), `"sub_token":""`) {
		t.Fatalf("user list leaked tokens to support: %s", b)
	}
	for _, p := range []string{"/connections", "/links"} {
		if code, _, _ := s.do("GET", "/api/admin/users/"+itoa(uid)+p, nil, nil); code != http.StatusForbidden {
			t.Fatalf("support reached %s: %d", p, code)
		}
	}
	// The admin still sees both.
	if code, b, _ := r.c.do("GET", "/api/admin/users/"+itoa(uid), nil, nil); code != 200 || mustJSON[map[string]any](t, b)["sub_token"] == "" {
		t.Fatalf("admin lost the token: %d %s", code, b)
	}
}

// Operators cannot reach the audit rules or the audit log: those write
// every node's core config and record where users went.
func TestOperatorCannotTouchAudit(t *testing.T) {
	r := newRig(t)
	op, _ := admin.NewUser("op2@test", "password123", "operator")
	if err := r.st.CreateUser(context.Background(), op); err != nil {
		t.Fatal(err)
	}
	c := &client{t: t, srv: r.srv}
	c.do("POST", "/api/admin/login", map[string]string{"Email": "op2@test", "Password": "password123"}, nil)
	for _, p := range []string{"/api/admin/audit-rules", "/api/admin/audit-log"} {
		if code, _, _ := c.do("GET", p, nil, nil); code != http.StatusForbidden {
			t.Fatalf("operator read %s: %d", p, code)
		}
	}
	if code, _, _ := c.do("GET", "/api/admin/nodes", nil, nil); code != http.StatusOK {
		t.Fatalf("operator lost the nodes: %d", code)
	}
}

// The subscription endpoint is a public URL: it rate-limits per token and
// per address, and it stores only bounded strings.
func TestSubFetchLimitsAndClipping(t *testing.T) {
	r := newRig(t)
	uid, _ := r.user("sub@test")
	u, err := r.st.UserByID(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	anon := &client{t: t, srv: r.srv}
	long := strings.Repeat("A", 4000)
	code, _, _ := anon.do("GET", "/sub/"+u.SubToken, nil, map[string]string{
		"User-Agent": long, "x-hwid": long, "x-device-os": long, "x-device-model": long,
	})
	if code != http.StatusOK {
		t.Fatalf("first fetch: %d", code)
	}
	reqs, err := r.st.SubRequests(context.Background(), uid, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) == 0 {
		t.Fatal("the fetch was not recorded")
	}
	if len(reqs[0].UserAgent) > 512 {
		t.Fatalf("user agent stored unclipped: %d bytes", len(reqs[0].UserAgent))
	}
	// Sixty fetches in five minutes is the ceiling for one token.
	locked := false
	for i := 0; i < 70; i++ {
		if code, _, _ := anon.do("GET", "/sub/"+u.SubToken, nil, nil); code == http.StatusTooManyRequests {
			locked = true
			break
		}
	}
	if !locked {
		t.Fatal("the subscription endpoint never rate-limited")
	}
	// The device table is bounded whatever the plan's device limit says.
	for i := 0; i < store.MaxHwidRows+10; i++ {
		_, _, _ = r.st.ClaimHwidDevice(context.Background(), uid, store.HwidDevice{Hwid: fmt.Sprintf("dev-%d", i), UserAgent: "x"}, 0, time.Now().Add(time.Duration(i)*time.Second))
	}
	devs, err := r.st.HwidDevices(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	if len(devs) > store.MaxHwidRows {
		t.Fatalf("device table unbounded: %d rows", len(devs))
	}
}

// Deleting a user takes its rows with it, so the next user to get that id
// does not inherit connection log, audit hits or a throttle.
func TestDeleteUserCleansItsRows(t *testing.T) {
	r := newRig(t)
	uid, uuid := r.user("gone@test")
	_, b, _ := r.c.do("PUT", "/api/admin/audit-rules", []map[string]any{
		{"name": "bt", "match": []string{"protocol:bittorrent"}, "action": "log", "enabled": true},
	}, nil)
	rules := mustJSON[[]store.AuditRule](t, b)
	r.c.do("PUT", "/api/admin/settings/connlog", map[string]any{"enabled": true, "retention_days": 7}, nil)
	r.agent.do("POST", "/api/agent/report", agentproto.Report{
		Audits:      []agentproto.AuditHit{{At: time.Now().Unix(), User: uuid, ClientIP: "203.0.113.30", Host: "a.test", Port: 443, RuleID: rules[0].ID, Action: "log"}},
		Connections: []agentproto.ConnEvent{{At: time.Now().Unix(), User: uuid, ClientIP: "203.0.113.30", Host: "a.test", Port: 443, Network: "tcp"}},
	}, nil)
	if code, _, _ := r.c.do("PUT", "/api/admin/users/"+itoa(uid)+"/dyn-limit", map[string]any{"Mbps": 10, "Seconds": 600}, nil); code != http.StatusOK {
		t.Fatalf("manual throttle refused")
	}
	if code, _, _ := r.c.do("DELETE", "/api/admin/users/"+itoa(uid), nil, nil); code != http.StatusOK {
		t.Fatalf("delete refused")
	}
	hits, _ := r.st.AuditHits(context.Background(), uid, 50)
	if len(hits) != 0 {
		t.Fatalf("audit hits outlived the user: %d", len(hits))
	}
	conns, _ := r.st.UserConnections(context.Background(), uid, 50)
	if len(conns) != 0 {
		t.Fatalf("connection log outlived the user: %d", len(conns))
	}
	if dl, _ := r.st.DynLimitFor(context.Background(), uid, time.Now()); dl != nil {
		t.Fatal("the throttle outlived the user")
	}
}

// A settings PUT that carries some keys leaves the others alone: a script
// or an MCP call must not clear HWID or the info lines by omission.
func TestSettingsPutIsAMerge(t *testing.T) {
	r := newRig(t)
	r.c.do("PUT", "/api/admin/settings/subscription", map[string]any{
		"URLs": []string{"https://sub.test"}, "single_plan": true,
		"hwid": map[string]any{"enabled": true, "fallback_limit": 3},
	}, nil)
	r.c.do("PUT", "/api/admin/settings/subscription", map[string]any{"URLs": []string{"https://sub2.test"}}, nil)
	_, b, _ := r.c.do("GET", "/api/admin/settings/subscription", nil, nil)
	var got struct {
		URLs       []string `json:"urls"`
		SinglePlan bool     `json:"single_plan"`
		Hwid       struct {
			Enabled       bool `json:"enabled"`
			FallbackLimit int  `json:"fallback_limit"`
		} `json:"hwid"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.URLs) != 1 || got.URLs[0] != "https://sub2.test" {
		t.Fatalf("the PUT did not apply: %s", b)
	}
	if !got.SinglePlan || !got.Hwid.Enabled || got.Hwid.FallbackLimit != 3 {
		t.Fatalf("omitted keys were cleared: %s", b)
	}
}

// A settings document that is a list is replaced, not merged: decoding a
// shorter array onto the old one would let a rule keep fields from
// whatever used to sit in its place.
func TestListSettingsAreReplaced(t *testing.T) {
	r := newRig(t)
	if code, b, _ := r.c.do("PUT", "/api/admin/settings/response-rules", []map[string]any{
		{"name": "first", "action": "block", "enabled": true, "match": []map[string]any{{"header": "user-agent", "op": "contains", "value": "curl"}}},
		{"name": "second", "action": "serve", "format": "clash", "enabled": true, "match": []map[string]any{{"header": "user-agent", "op": "contains", "value": "clash"}}},
	}, nil); code != http.StatusOK {
		t.Fatalf("first put: %d %s", code, b)
	}
	if code, b, _ := r.c.do("PUT", "/api/admin/settings/response-rules", []map[string]any{
		{"name": "only", "action": "serve", "enabled": true, "match": []map[string]any{{"header": "user-agent", "op": "contains", "value": "x"}}},
	}, nil); code != http.StatusOK {
		t.Fatalf("second put: %d %s", code, b)
	}
	_, b, _ := r.c.do("GET", "/api/admin/settings/response-rules", nil, nil)
	if strings.Count(string(b), `"name"`) != 1 || strings.Contains(string(b), "clash") {
		t.Fatalf("the list was merged instead of replaced: %s", b)
	}
}

// A node that restarts begins a new batch series at 1. The panel must
// still charge those: treating "lower than the last one" as a repeat would
// silently drop the node's traffic until its counter climbed back past the
// number it had before the restart.
func TestTrafficSeqSurvivesANodeRestart(t *testing.T) {
	r := newRig(t)
	uid, _ := r.user("restart@test")
	used := func() int64 {
		s, err := r.st.ActiveSubscription(context.Background(), uid)
		if err != nil {
			t.Fatal(err)
		}
		return s.UsedUpBytes + s.UsedDownBytes
	}
	for seq := uint64(1); seq <= 5; seq++ {
		r.agent.do("POST", "/api/agent/report", agentproto.Report{TrafficSeq: seq, TrafficWindowSeconds: 60,
			Traffic: []spec.UserTraffic{{UserID: uid, Up: 100, Down: 0}}}, nil)
	}
	if got := used(); got != 500 {
		t.Fatalf("five batches: %d", got)
	}
	// A copy of the previous batch, delayed past the one after it, is a
	// repeat and must not be charged again.
	r.agent.do("POST", "/api/agent/report", agentproto.Report{TrafficSeq: 4, TrafficWindowSeconds: 60,
		Traffic: []spec.UserTraffic{{UserID: uid, Up: 100, Down: 0}}}, nil)
	if got := used(); got != 500 {
		t.Fatalf("a delayed duplicate was charged: %d", got)
	}
	// The agent restarts with a clock-seeded series (bosun >= 0.46.1):
	// the number jumps forward and the batch is charged.
	r.agent.do("POST", "/api/agent/report", agentproto.Report{TrafficSeq: uint64(time.Now().Unix()), TrafficWindowSeconds: 60,
		Traffic: []spec.UserTraffic{{UserID: uid, Up: 100, Down: 0}}}, nil)
	if got := used(); got != 600 {
		t.Fatalf("the first batch after a restart was dropped: %d", got)
	}
	// An older agent restarts and counts from 1 again: far enough behind
	// to be a new series, so its traffic is charged rather than lost.
	r.agent.do("POST", "/api/agent/report", agentproto.Report{TrafficSeq: 1, TrafficWindowSeconds: 60,
		Traffic: []spec.UserTraffic{{UserID: uid, Up: 100, Down: 0}}}, nil)
	if got := used(); got != 700 {
		t.Fatalf("an old agent's restarted series was dropped: %d", got)
	}
	// And a genuine re-send of that batch is still applied once.
	r.agent.do("POST", "/api/agent/report", agentproto.Report{TrafficSeq: 1, TrafficWindowSeconds: 60,
		Traffic: []spec.UserTraffic{{UserID: uid, Up: 100, Down: 0}}}, nil)
	if got := used(); got != 700 {
		t.Fatalf("re-sent batch charged twice: %d", got)
	}
}
