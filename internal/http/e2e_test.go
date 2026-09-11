package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"gitlab.com/boyang-hu/bosun/pkg/agentproto"
	"gitlab.com/boyang-hu/bosun/pkg/spec"

	"gitlab.com/boyang-hu/captain/internal/config"
	"gitlab.com/boyang-hu/captain/internal/db"
	"gitlab.com/boyang-hu/captain/internal/http/admin"
	"gitlab.com/boyang-hu/captain/internal/store"
)

type client struct {
	t      *testing.T
	srv    *httptest.Server
	cookie *http.Cookie
	token  string
}

func (c *client) do(method, path string, body any, headers map[string]string) (int, []byte, http.Header) {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.srv.URL+path, rd)
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.srv.Client().Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	for _, ck := range resp.Cookies() {
		if ck.Name == "captain_session" && ck.Value != "" {
			c.cookie = ck
		}
	}
	return resp.StatusCode, out, resp.Header
}

func mustJSON[T any](t *testing.T, b []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("decode %s: %v", b, err)
	}
	return v
}

func TestEndToEnd(t *testing.T) {
	cfg := config.Default()
	cfg.BaseURL = "http://test"
	cfg.Agent.PullSeconds, cfg.Agent.PushSeconds = 5, 5
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
	defer srv.Close()
	c := &client{t: t, srv: srv}

	// Admin login.
	if code, _, _ := c.do("GET", "/api/admin/me", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("me before login: %d", code)
	}
	if code, b, _ := c.do("POST", "/api/admin/login", map[string]string{"Email": "admin@test", "Password": "wrong"}, nil); code != http.StatusUnauthorized {
		t.Fatalf("bad login: %d %s", code, b)
	}
	if code, b, _ := c.do("POST", "/api/admin/login", map[string]string{"Email": "admin@test", "Password": "password123"}, nil); code != 200 {
		t.Fatalf("login: %d %s", code, b)
	}

	// Node + pairing.
	_, b, _ := c.do("POST", "/api/admin/nodes", map[string]string{"Name": "jp-1", "PublicAddr": "203.0.113.5"}, nil)
	node := mustJSON[map[string]any](t, b)
	code := node["PairCode"].(string)
	nodeID := int64(node["ID"].(float64))
	agent := &client{t: t, srv: srv}
	if st, b, _ := agent.do("POST", "/api/agent/pair", agentproto.PairRequest{Code: "NOPE-NOPE"}, nil); st != http.StatusNotFound {
		t.Fatalf("bad code: %d %s", st, b)
	}
	_, b, _ = agent.do("POST", "/api/agent/pair", agentproto.PairRequest{Code: code, Hostname: "jp1", Version: "0.1.0", Platform: "linux/amd64"}, nil)
	pair := mustJSON[agentproto.PairResponse](t, b)
	if pair.Token == "" {
		t.Fatalf("pair: %s", b)
	}
	if st, _, _ := agent.do("POST", "/api/agent/pair", agentproto.PairRequest{Code: code}, nil); st != http.StatusNotFound {
		t.Fatal("pair code must be single use")
	}
	agent.token = pair.Token

	// Empty state, then inbound + user + plan grant.
	_, b, h := agent.do("GET", "/api/agent/state", nil, nil)
	st0 := mustJSON[agentproto.State](t, b)
	if len(st0.Node.Inbounds) != 0 || len(st0.Users) != 0 || st0.PullSeconds != 5 {
		t.Fatalf("empty state: %+v", st0)
	}
	etag0 := h.Get("ETag")

	_, b, _ = c.do("POST", "/api/admin/nodes/"+itoa(nodeID)+"/inbounds", map[string]any{
		"Tag": "mieru", "Protocol": "mieru", "Port": 24450,
		"Settings": map[string]any{"mieru_transport": "TCP"},
	}, nil)
	if !bytes.Contains(b, []byte(`"Tag":"mieru"`)) {
		t.Fatalf("inbound: %s", b)
	}
	_, b, _ = c.do("POST", "/api/admin/users", map[string]string{"Email": "u1@test", "Password": "password123"}, nil)
	u1 := mustJSON[map[string]any](t, b)
	_, b, _ = c.do("POST", "/api/admin/plans", map[string]any{"Name": "basic", "PriceCents": 1000, "PeriodDays": 30, "QuotaBytes": 1 << 30}, nil)
	plan := mustJSON[map[string]any](t, b)

	// A VIP inbound restricted to a group nobody is in yet.
	_, b, _ = c.do("POST", "/api/admin/groups", map[string]string{"Name": "vip"}, nil)
	vipGroup := int64(mustJSON[map[string]any](t, b)["id"].(float64))
	if st, b, _ := c.do("POST", "/api/admin/nodes/"+itoa(nodeID)+"/inbounds", map[string]any{
		"Tag": "vip", "Protocol": "vless", "Port": 443, "GroupID": vipGroup,
	}, nil); st != 200 {
		t.Fatalf("vip inbound: %d %s", st, b)
	}

	// User without a subscription is not provisioned.
	_, b, h = agent.do("GET", "/api/agent/state", nil, nil)
	st1 := mustJSON[agentproto.State](t, b)
	if len(st1.Node.Inbounds) != 2 || st1.Node.Inbounds[0].Protocol != spec.Mieru || st1.Node.Inbounds[0].MieruTransport != "TCP" || len(st1.Users) != 0 {
		t.Fatalf("state after inbound: %+v", st1)
	}
	if vip := st1.Node.Inbounds[1]; !vip.ScopedUsers || len(vip.Users) != 0 {
		t.Fatalf("vip inbound must be scoped: %+v", vip)
	}
	if h.Get("ETag") == etag0 {
		t.Fatal("etag must change with inbounds")
	}
	// Grant plan -> user appears.
	if st, b, _ := c.do("POST", "/api/admin/users/"+itoa(int64(u1["id"].(float64)))+"/grant", map[string]any{"PlanID": plan["ID"]}, nil); st != 200 {
		t.Fatalf("grant: %d %s", st, b)
	}
	_, b, h = agent.do("GET", "/api/agent/state", nil, nil)
	st2 := mustJSON[agentproto.State](t, b)
	if len(st2.Users) != 1 || st2.Users[0].UUID != u1["uuid"].(string) || st2.Users[0].Name != st2.Users[0].UUID {
		t.Fatalf("state after grant: %+v", st2)
	}
	// The basic plan has no group, so the user is on the shared list only.
	if vip := st2.Node.Inbounds[1]; len(vip.Users) != 0 {
		t.Fatalf("basic user leaked into vip inbound: %+v", vip.Users)
	}
	// A VIP plan puts its buyer on the vip inbound and on the shared list.
	_, b, _ = c.do("POST", "/api/admin/users", map[string]string{"Email": "vip@test", "Password": "password123"}, nil)
	u2 := mustJSON[map[string]any](t, b)
	_, b, _ = c.do("POST", "/api/admin/plans", map[string]any{"Name": "vip", "PriceCents": 5000, "PeriodDays": 30, "GroupID": vipGroup}, nil)
	vipPlan := mustJSON[map[string]any](t, b)
	c.do("POST", "/api/admin/users/"+itoa(int64(u2["id"].(float64)))+"/grant", map[string]any{"PlanID": vipPlan["ID"]}, nil)
	_, b, h = agent.do("GET", "/api/agent/state", nil, nil)
	stVip := mustJSON[agentproto.State](t, b)
	if len(stVip.Users) != 2 || len(stVip.Node.Inbounds[1].Users) != 1 || stVip.Node.Inbounds[1].Users[0].UUID != u2["uuid"].(string) {
		t.Fatalf("vip scoping: shared=%d vip=%+v", len(stVip.Users), stVip.Node.Inbounds[1].Users)
	}
	st2 = stVip
	etag2 := h.Get("ETag")
	if code, _, _ := agent.do("GET", "/api/agent/state", nil, map[string]string{"If-None-Match": etag2}); code != http.StatusNotModified {
		t.Fatalf("expected 304, got %d", code)
	}

	// Report traffic: charged to the subscription; quota exhaustion drops the user.
	uid := int64(u1["id"].(float64))
	_, b, _ = agent.do("POST", "/api/agent/report", agentproto.Report{
		Version: "0.1.0", Revision: st2.Revision,
		Traffic: []spec.UserTraffic{{UserID: uid, Up: 100, Down: 200}},
		Online:  map[string][]string{u1["uuid"].(string): {"198.51.100.7"}},
		Cores:   map[string]agentproto.CoreStatus{"mita": {Running: true}},
	}, nil)
	rr := mustJSON[agentproto.ReportResponse](t, b)
	if rr.StateChanged {
		t.Fatal("state should not change after a small report")
	}
	sub, err := st.ActiveSubscription(context.Background(), uid)
	if err != nil || sub.UsedUpBytes != 100 || sub.UsedDownBytes != 200 {
		t.Fatalf("subscription usage: %+v %v", sub, err)
	}
	_, b, _ = agent.do("POST", "/api/agent/report", agentproto.Report{Revision: st2.Revision, Traffic: []spec.UserTraffic{{UserID: uid, Up: 1 << 30, Down: 0}}}, nil)
	if rr := mustJSON[agentproto.ReportResponse](t, b); !rr.StateChanged {
		t.Fatal("quota exhausted: state should change")
	}
	_, b, _ = agent.do("GET", "/api/agent/state", nil, nil)
	if st3 := mustJSON[agentproto.State](t, b); len(st3.Users) != 0 {
		t.Fatalf("exhausted user still provisioned: %+v", st3.Users)
	}
	n, _ := st.NodeByID(context.Background(), nodeID)
	if n.LastSeenAt == nil || n.Version != "0.1.0" || !n.Paired {
		t.Fatalf("node after report: %+v", n)
	}
}

func itoa(i int64) string { return strconv.FormatInt(i, 10) }
