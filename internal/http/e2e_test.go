package http

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"

	"github.com/zeptop-dev/captain/internal/config"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/http/admin"
	"github.com/zeptop-dev/captain/internal/jobs"
	"github.com/zeptop-dev/captain/internal/mail"
	"github.com/zeptop-dev/captain/internal/store"
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
	code := node["pair_code"].(string)
	nodeID := int64(node["id"].(float64))
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

	// Entries + subscription documents.
	if st, b, _ := c.do("POST", "/api/admin/entries", map[string]any{"Name": "JP mieru", "InboundID": 1, "DisplayHost": "entry.test", "DisplayPort": 24450}, nil); st != 200 {
		t.Fatalf("entry: %d %s", st, b)
	}
	c.do("POST", "/api/admin/entries", map[string]any{"Name": "JP vip", "InboundID": 2, "DisplayHost": "entry.test", "DisplayPort": 443}, nil)
	anon := &client{t: t, srv: srv}
	code0, b, h := anon.do("GET", "/sub/"+u1["sub_token"].(string), nil, map[string]string{"User-Agent": "clash-verge/1.0"})
	if code0 != 200 || !strings.Contains(h.Get("Content-Type"), "yaml") || !strings.Contains(string(b), "type: mieru") || strings.Contains(string(b), "JP vip") {
		t.Fatalf("clash sub for basic user: %d %s\n%s", code0, h.Get("Content-Type"), b)
	}
	if !strings.HasPrefix(h.Get("Subscription-Userinfo"), "upload=0; download=0; total=1073741824; expire=") {
		t.Fatalf("userinfo: %s", h.Get("Subscription-Userinfo"))
	}
	_, b, _ = anon.do("GET", "/sub/"+u2["sub_token"].(string)+"?client=uri", nil, nil)
	raw, _ := base64.StdEncoding.DecodeString(string(b))
	if !strings.Contains(string(raw), "mierus://") || !strings.Contains(string(raw), "vless://"+u2["uuid"].(string)+"@entry.test:443") {
		t.Fatalf("uri sub for vip user:\n%s", raw)
	}
	if code, _, _ := anon.do("GET", "/sub/nope", nil, nil); code != http.StatusNotFound {
		t.Fatalf("unknown token: %d", code)
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
	if st3 := mustJSON[agentproto.State](t, b); len(st3.Users) != 1 || st3.Users[0].UUID != u2["uuid"].(string) {
		t.Fatalf("exhausted user still provisioned (only the vip user should remain): %+v", st3.Users)
	}
	// Exhausted user gets an empty document but keeps the usage header.
	code1, b, h := anon.do("GET", "/sub/"+u1["sub_token"].(string), nil, map[string]string{"User-Agent": "mihomo"})
	if code1 != 200 || strings.Contains(string(b), "type: mieru") || !strings.Contains(h.Get("Subscription-Userinfo"), "total=1073741824") {
		t.Fatalf("exhausted sub: %d %s\n%s", code1, h.Get("Subscription-Userinfo"), b)
	}
	n, _ := st.NodeByID(context.Background(), nodeID)
	if n.LastSeenAt == nil || n.Version != "0.1.0" || !n.Paired {
		t.Fatalf("node after report: %+v", n)
	}
}

func itoa(i int64) string { return strconv.FormatInt(i, 10) }

func TestAdminLists(t *testing.T) {
	cfg := config.Default()
	cfg.BaseURL = "http://test"
	conn, _ := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	_ = db.Migrate(context.Background(), conn, "sqlite")
	st := store.New(conn)
	adminUser, _ := admin.NewUser("admin@test", "password123", "admin")
	_ = st.CreateUser(context.Background(), adminUser)
	srv := httptest.NewServer(New(cfg, st, slog.Default()).Handler())
	defer srv.Close()
	c := &client{t: t, srv: srv}
	c.do("POST", "/api/admin/login", map[string]string{"Email": "admin@test", "Password": "password123"}, nil)
	c.do("POST", "/api/admin/users", map[string]string{"Email": "a@test", "Password": "password123"}, nil)
	_, b, _ := c.do("POST", "/api/admin/plans", map[string]any{"Name": "p", "PriceCents": 100, "PeriodDays": 30}, nil)
	plan := mustJSON[map[string]any](t, b)
	c.do("POST", "/api/admin/users/2/grant", map[string]any{"PlanID": plan["ID"]}, nil)
	// Every list endpoint must answer 200 with the expected shape.
	for _, path := range []string{"/api/admin/users?q=a&page=1", "/api/admin/users", "/api/admin/orders", "/api/admin/plans", "/api/admin/groups", "/api/admin/entries", "/api/admin/nodes", "/api/admin/dashboard"} {
		code, body, _ := c.do("GET", path, nil, nil)
		if code != 200 {
			t.Fatalf("%s: %d %s", path, code, body)
		}
	}
	_, b, _ = c.do("GET", "/api/admin/users?q=a", nil, nil)
	if !strings.Contains(string(b), `"plan_name":"p"`) || !strings.Contains(string(b), `"total":1`) {
		t.Fatalf("users list: %s", b)
	}
	if code, _, _ := c.do("PATCH", "/api/admin/users/2", map[string]any{"Status": "banned"}, nil); code != 200 {
		t.Fatal("update user")
	}
	_, b, _ = c.do("GET", "/api/admin/users", nil, nil)
	if !strings.Contains(string(b), `"status":"banned"`) {
		t.Fatalf("banned not reflected: %s", b)
	}
}

func TestDeviceLimit(t *testing.T) {
	cfg := config.Default()
	cfg.BaseURL = "http://test"
	conn, _ := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	_ = db.Migrate(context.Background(), conn, "sqlite")
	st := store.New(conn)
	adminUser, _ := admin.NewUser("admin@test", "password123", "admin")
	_ = st.CreateUser(context.Background(), adminUser)
	srv := httptest.NewServer(New(cfg, st, slog.Default()).Handler())
	defer srv.Close()
	c := &client{t: t, srv: srv}
	c.do("POST", "/api/admin/login", map[string]string{"Email": "admin@test", "Password": "password123"}, nil)
	_, b, _ := c.do("POST", "/api/admin/nodes", map[string]string{"Name": "n"}, nil)
	node := mustJSON[map[string]any](t, b)
	c.do("POST", "/api/admin/nodes/"+itoa(int64(node["id"].(float64)))+"/inbounds", map[string]any{"Tag": "t", "Protocol": "vless", "Port": 1}, nil)
	_, b, _ = c.do("POST", "/api/admin/users", map[string]string{"Email": "u@test", "Password": "password123"}, nil)
	u := mustJSON[map[string]any](t, b)
	_, b, _ = c.do("POST", "/api/admin/plans", map[string]any{"Name": "two-devices", "PriceCents": 1, "PeriodDays": 30, "DeviceLimit": 2}, nil)
	plan := mustJSON[map[string]any](t, b)
	c.do("POST", "/api/admin/users/"+itoa(int64(u["id"].(float64)))+"/grant", map[string]any{"PlanID": plan["ID"]}, nil)
	agent := &client{t: t, srv: srv}
	_, b, _ = agent.do("POST", "/api/agent/pair", agentproto.PairRequest{Code: node["pair_code"].(string)}, nil)
	agent.token = mustJSON[agentproto.PairResponse](t, b).Token

	uuid := u["uuid"].(string)
	// Two devices: fine.
	agent.do("POST", "/api/agent/report", agentproto.Report{Online: map[string][]string{uuid: {"1.1.1.1", "2.2.2.2"}}}, nil)
	_, b, _ = agent.do("GET", "/api/agent/state", nil, nil)
	if stt := mustJSON[agentproto.State](t, b); len(stt.Users) != 1 {
		t.Fatalf("two devices should be allowed: %+v", stt.Users)
	}
	// Third device: the user is withheld from nodes until the extra IP ages out.
	_, b, _ = agent.do("POST", "/api/agent/report", agentproto.Report{Online: map[string][]string{uuid: {"3.3.3.3"}}}, nil)
	if rr := mustJSON[agentproto.ReportResponse](t, b); !rr.StateChanged {
		t.Fatal("exceeding the device limit should change state")
	}
	_, b, _ = agent.do("GET", "/api/agent/state", nil, nil)
	if stt := mustJSON[agentproto.State](t, b); len(stt.Users) != 0 {
		t.Fatalf("over-limit user still provisioned: %+v", stt.Users)
	}
	_, b, _ = c.do("GET", "/api/admin/users/"+itoa(int64(u["id"].(float64))), nil, nil)
	if !strings.Contains(string(b), `"3.3.3.3"`) {
		t.Fatalf("devices missing from user detail: %s", b)
	}
	// Age the IPs out and the user returns.
	conn.Exec(`UPDATE online_devices SET last_seen_at = last_seen_at - 600`)
	_, b, _ = agent.do("GET", "/api/agent/state", nil, nil)
	if stt := mustJSON[agentproto.State](t, b); len(stt.Users) != 1 {
		t.Fatalf("user should return after IPs age out: %+v", stt.Users)
	}
}

func TestNodeUpgradeRequest(t *testing.T) {
	cfg := config.Default()
	cfg.BaseURL = "http://test"
	conn, _ := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	_ = db.Migrate(context.Background(), conn, "sqlite")
	st := store.New(conn)
	adminUser, _ := admin.NewUser("admin@test", "password123", "admin")
	_ = st.CreateUser(context.Background(), adminUser)
	srv := httptest.NewServer(New(cfg, st, slog.Default()).Handler())
	defer srv.Close()
	c := &client{t: t, srv: srv}
	c.do("POST", "/api/admin/login", map[string]string{"Email": "admin@test", "Password": "password123"}, nil)
	_, b, _ := c.do("POST", "/api/admin/nodes", map[string]string{"Name": "n"}, nil)
	node := mustJSON[map[string]any](t, b)
	nodeID := itoa(int64(node["id"].(float64)))
	agent := &client{t: t, srv: srv}
	_, b, _ = agent.do("POST", "/api/agent/pair", agentproto.PairRequest{Code: node["pair_code"].(string), Version: "v0.5.0"}, nil)
	agent.token = mustJSON[agentproto.PairResponse](t, b).Token

	// Nothing requested: no upgrade in the response.
	_, b, _ = agent.do("POST", "/api/agent/report", agentproto.Report{Version: "v0.5.0"}, nil)
	if rr := mustJSON[agentproto.ReportResponse](t, b); rr.UpgradeTo != "" {
		t.Fatalf("unexpected upgrade request: %+v", rr)
	}
	// Explicit version (the latest-release lookup needs the network, so pass one).
	if code, b, _ := c.do("POST", "/api/admin/nodes/"+nodeID+"/upgrade", map[string]string{"Version": "v0.6.0"}, nil); code != 200 {
		t.Fatalf("upgrade: %d %s", code, b)
	}
	if code, b, _ := c.do("POST", "/api/admin/nodes/"+nodeID+"/upgrade", map[string]string{"Version": "0.6.0"}, nil); code != 400 {
		t.Fatalf("bad tag should be rejected: %d %s", code, b)
	}
	_, b, _ = agent.do("POST", "/api/agent/report", agentproto.Report{Version: "v0.5.0"}, nil)
	if rr := mustJSON[agentproto.ReportResponse](t, b); rr.UpgradeTo != "v0.6.0" {
		t.Fatalf("upgrade should be requested: %+v", rr)
	}
	_, b, _ = c.do("GET", "/api/admin/nodes", nil, nil)
	if !strings.Contains(string(b), `"upgrade_to":"v0.6.0"`) {
		t.Fatalf("node list should show the pending upgrade: %s", b)
	}
	// Once the node reports the target version the request clears.
	_, b, _ = agent.do("POST", "/api/agent/report", agentproto.Report{Version: "v0.6.0"}, nil)
	if rr := mustJSON[agentproto.ReportResponse](t, b); rr.UpgradeTo != "" {
		t.Fatalf("request should clear after the node reports the version: %+v", rr)
	}
	_, b, _ = c.do("GET", "/api/admin/nodes", nil, nil)
	if strings.Contains(string(b), `"upgrade_to"`) {
		t.Fatalf("pending upgrade should be gone: %s", b)
	}
	// Upgrade-all skips nodes already on the version.
	_, b, _ = c.do("POST", "/api/admin/nodes/upgrade-all", map[string]string{"Version": "v0.6.0"}, nil)
	if !strings.Contains(string(b), `"nodes":0`) {
		t.Fatalf("upgrade-all should skip up-to-date nodes: %s", b)
	}
}

func TestLoginRateLimit(t *testing.T) {
	cfg := config.Default()
	cfg.BaseURL = "https://test"
	conn, _ := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	_ = db.Migrate(context.Background(), conn, "sqlite")
	st := store.New(conn)
	adminUser, _ := admin.NewUser("admin@test", "password123", "admin")
	_ = st.CreateUser(context.Background(), adminUser)
	srv := httptest.NewServer(New(cfg, st, slog.Default()).Handler())
	defer srv.Close()
	c := &client{t: t, srv: srv}
	for i := 0; i < 5; i++ {
		if code, _, _ := c.do("POST", "/api/admin/login", map[string]string{"Email": "admin@test", "Password": "wrong"}, nil); code != 401 {
			t.Fatalf("attempt %d: %d", i, code)
		}
	}
	if code, b, _ := c.do("POST", "/api/admin/login", map[string]string{"Email": "admin@test", "Password": "password123"}, nil); code != 429 {
		t.Fatalf("sixth attempt should be locked even with the right password: %d %s", code, b)
	}
	// The portal shares the limiter, so it is locked too.
	if code, _, _ := c.do("POST", "/api/portal/login", map[string]string{"Email": "admin@test", "Password": "password123"}, nil); code != 429 {
		t.Fatalf("portal should be locked as well: %d", code)
	}
}

func TestSecureCookie(t *testing.T) {
	cfg := config.Default()
	cfg.BaseURL = "https://test"
	conn, _ := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	_ = db.Migrate(context.Background(), conn, "sqlite")
	st := store.New(conn)
	adminUser, _ := admin.NewUser("admin@test", "password123", "admin")
	_ = st.CreateUser(context.Background(), adminUser)
	srv := httptest.NewServer(New(cfg, st, slog.Default()).Handler())
	defer srv.Close()
	body, _ := json.Marshal(map[string]string{"Email": "admin@test", "Password": "password123"})
	resp, err := http.Post(srv.URL+"/api/admin/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var found bool
	for _, ck := range resp.Cookies() {
		if ck.Name == "captain_session" {
			found = true
			if !ck.Secure || !ck.HttpOnly {
				t.Fatalf("cookie flags: secure=%v httponly=%v", ck.Secure, ck.HttpOnly)
			}
		}
	}
	if !found {
		t.Fatal("no session cookie")
	}
}

func TestACMESettingsReachNodes(t *testing.T) {
	cfg := config.Default()
	cfg.BaseURL = "http://test"
	conn, _ := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	_ = db.Migrate(context.Background(), conn, "sqlite")
	st := store.New(conn)
	adminUser, _ := admin.NewUser("admin@test", "password123", "admin")
	_ = st.CreateUser(context.Background(), adminUser)
	srv := httptest.NewServer(New(cfg, st, slog.Default()).Handler())
	defer srv.Close()
	c := &client{t: t, srv: srv}
	c.do("POST", "/api/admin/login", map[string]string{"Email": "admin@test", "Password": "password123"}, nil)
	_, b, _ := c.do("POST", "/api/admin/nodes", map[string]string{"Name": "n"}, nil)
	node := mustJSON[map[string]any](t, b)
	agent := &client{t: t, srv: srv}
	_, b, _ = agent.do("POST", "/api/agent/pair", agentproto.PairRequest{Code: node["pair_code"].(string)}, nil)
	agent.token = mustJSON[agentproto.PairResponse](t, b).Token

	_, b, _ = agent.do("GET", "/api/agent/state", nil, nil)
	before := mustJSON[agentproto.State](t, b)
	if before.Node.ACME != nil {
		t.Fatal("no acme settings yet")
	}
	if code, b, _ := c.do("PUT", "/api/admin/settings/acme", map[string]string{"Email": "ops@test", "CloudflareToken": "cf-secret"}, nil); code != 200 || !strings.Contains(string(b), `"has_cloudflare_token":true`) || strings.Contains(string(b), "cf-secret") {
		t.Fatalf("put acme: %d %s", code, b)
	}
	_, b, _ = agent.do("GET", "/api/agent/state", nil, nil)
	after := mustJSON[agentproto.State](t, b)
	if after.Node.ACME == nil || after.Node.ACME.Email != "ops@test" || after.Node.ACME.CloudflareToken != "cf-secret" || after.Revision == before.Revision {
		t.Fatalf("acme settings should reach the node with a new revision: %+v", after.Node.ACME)
	}
	// Blank token keeps it, "-" clears it.
	c.do("PUT", "/api/admin/settings/acme", map[string]string{"Email": "ops@test", "CloudflareToken": ""}, nil)
	_, b, _ = agent.do("GET", "/api/agent/state", nil, nil)
	if mustJSON[agentproto.State](t, b).Node.ACME.CloudflareToken != "cf-secret" {
		t.Fatal("blank token must keep the current one")
	}
	c.do("PUT", "/api/admin/settings/acme", map[string]string{"Email": "ops@test", "CloudflareToken": "-"}, nil)
	_, b, _ = agent.do("GET", "/api/agent/state", nil, nil)
	if mustJSON[agentproto.State](t, b).Node.ACME.CloudflareToken != "" {
		t.Fatal("- must clear the token")
	}
	// Cert status from the report lands in the node detail and flags problems.
	agent.do("POST", "/api/agent/report", agentproto.Report{Certs: []agentproto.CertStatus{{Domain: "jp.test", Method: "http", Error: "port 80 unreachable"}}}, nil)
	_, b, _ = c.do("GET", "/api/admin/nodes", nil, nil)
	if !strings.Contains(string(b), `"cert_problem":true`) {
		t.Fatalf("cert problem should be flagged: %s", b)
	}
	_, b, _ = c.do("GET", "/api/admin/nodes/"+itoa(int64(node["id"].(float64))), nil, nil)
	if !strings.Contains(string(b), `"jp.test"`) {
		t.Fatalf("node detail should list certs: %s", b)
	}
}

func TestSubscriptionURLsAndHosts(t *testing.T) {
	cfg := config.Default()
	cfg.BaseURL = "https://my.test"
	conn, _ := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	_ = db.Migrate(context.Background(), conn, "sqlite")
	st := store.New(conn)
	adminUser, _ := admin.NewUser("admin@test", "password123", "admin")
	_ = st.CreateUser(context.Background(), adminUser)
	server := New(cfg, st, slog.Default())
	srv := httptest.NewServer(server.Handler())
	defer srv.Close()
	c := &client{t: t, srv: srv}
	c.do("POST", "/api/admin/login", map[string]string{"Email": "admin@test", "Password": "password123"}, nil)
	_, b, _ := c.do("POST", "/api/admin/users", map[string]string{"Email": "u@test", "Password": "password123"}, nil)
	u := mustJSON[map[string]any](t, b)
	uid := itoa(int64(u["id"].(float64)))
	tok := u["sub_token"].(string)

	_, b, _ = c.do("GET", "/api/admin/users/"+uid, nil, nil)
	if !strings.Contains(string(b), `"sub_url":"https://my.test/sub/`+tok+`"`) {
		t.Fatalf("default sub url should use base_url: %s", b)
	}
	if code, b, _ := c.do("PUT", "/api/admin/settings/subscription", map[string]any{"URLs": []string{"sub.test"}}, nil); code != 400 {
		t.Fatalf("scheme-less url should be rejected: %d %s", code, b)
	}
	if code, b, _ := c.do("PUT", "/api/admin/settings/subscription", map[string]any{"URLs": []string{"https://s[1-3].test/", "https://[uuid].sub.test"}}, nil); code != 200 {
		t.Fatalf("put: %d %s", code, b)
	}
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		_, b, _ = c.do("GET", "/api/admin/users/"+uid, nil, nil)
		var v struct {
			SubURL string `json:"sub_url"`
		}
		_ = json.Unmarshal(b, &v)
		if !strings.HasSuffix(v.SubURL, "/sub/"+tok) {
			t.Fatalf("bad sub url %s", v.SubURL)
		}
		host := strings.TrimPrefix(strings.SplitN(v.SubURL, "/sub/", 2)[0], "https://")
		seen[host] = true
		if !server.SubLinks().IsSubscriptionHost(context.Background(), host) {
			t.Fatalf("generated host %q should be recognised as a subscription host", host)
		}
	}
	if len(seen) < 2 {
		t.Fatalf("placeholders should vary the host: %v", seen)
	}
	if server.SubLinks().IsSubscriptionHost(context.Background(), "my.test") || server.SubLinks().IsSubscriptionHost(context.Background(), "evil.test") {
		t.Fatal("panel host and strangers are not subscription hosts")
	}
	// On a subscription host only /sub/ is served.
	req, _ := http.NewRequest("GET", srv.URL+"/admin/", nil)
	req.Host = "s2.test"
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != 404 {
		t.Fatalf("admin on a subscription host should be 404, got %d", resp.StatusCode)
	}
	req, _ = http.NewRequest("GET", srv.URL+"/sub/"+tok, nil)
	req.Host = "s2.test"
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode == 404 {
		t.Fatal("subscription must be served on the subscription host")
	}
	req, _ = http.NewRequest("GET", srv.URL+"/admin/", nil)
	req.Host = "my.test"
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode == 404 {
		t.Fatal("the panel host keeps serving the console")
	}
}

func TestEntryDefaultsFromInbound(t *testing.T) {
	cfg := config.Default()
	cfg.BaseURL = "http://test"
	conn, _ := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	_ = db.Migrate(context.Background(), conn, "sqlite")
	st := store.New(conn)
	adminUser, _ := admin.NewUser("admin@test", "password123", "admin")
	_ = st.CreateUser(context.Background(), adminUser)
	srv := httptest.NewServer(New(cfg, st, slog.Default()).Handler())
	defer srv.Close()
	c := &client{t: t, srv: srv}
	c.do("POST", "/api/admin/login", map[string]string{"Email": "admin@test", "Password": "password123"}, nil)
	_, b, _ := c.do("POST", "/api/admin/nodes", map[string]any{"Name": "jp", "PublicAddr": "203.0.113.5"}, nil)
	node := mustJSON[map[string]any](t, b)
	nid := itoa(int64(node["id"].(float64)))
	_, b, _ = c.do("POST", "/api/admin/nodes/"+nid+"/inbounds", map[string]any{"Tag": "hy2", "Protocol": "hysteria2", "Port": 8443, "Settings": map[string]any{"tls": map[string]any{"mode": 1, "server_name": "jp.test", "auto_cert": true}}}, nil)
	tlsIB := mustJSON[map[string]any](t, b)
	_, b, _ = c.do("POST", "/api/admin/nodes/"+nid+"/inbounds", map[string]any{"Tag": "ss", "Protocol": "shadowsocks", "Port": 8388, "Settings": map[string]any{"cipher": "aes-128-gcm"}}, nil)
	plainIB := mustJSON[map[string]any](t, b)

	_, b, _ = c.do("POST", "/api/admin/entries", map[string]any{"Name": "jp", "InboundID": tlsIB["ID"]}, nil)
	if !strings.Contains(string(b), `"DisplayHost":"jp.test"`) || !strings.Contains(string(b), `"DisplayPort":8443`) {
		t.Fatalf("entry should default to the TLS domain and inbound port: %s", b)
	}
	_, b, _ = c.do("POST", "/api/admin/entries", map[string]any{"Name": "ss", "InboundID": plainIB["ID"]}, nil)
	if !strings.Contains(string(b), `"DisplayHost":"203.0.113.5"`) {
		t.Fatalf("entry should fall back to the node address: %s", b)
	}
	_, b, _ = c.do("POST", "/api/admin/entries", map[string]any{"Name": "iplc", "InboundID": tlsIB["ID"], "DisplayHost": "entrance.provider.net", "DisplayPort": 30001}, nil)
	if !strings.Contains(string(b), `"DisplayHost":"entrance.provider.net"`) {
		t.Fatalf("explicit address must win: %s", b)
	}
}

func TestMailVerificationAndReset(t *testing.T) {
	cfg := config.Default()
	cfg.BaseURL = "http://test"
	cfg.Portal.Registration = true
	conn, _ := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	_ = db.Migrate(context.Background(), conn, "sqlite")
	st := store.New(conn)
	adminUser, _ := admin.NewUser("admin@test", "password123", "admin")
	_ = st.CreateUser(context.Background(), adminUser)
	ms := mail.Settings{Provider: "smtp", FromAddress: "noreply@test", VerifyRegistration: true, Reminders: true}
	ms.SMTP.Host = "smtp.test"
	_ = st.SetSetting(context.Background(), mail.SettingKey, ms)
	var sent []mail.Message
	mail.SendFunc = func(ctx context.Context, s mail.Settings, m mail.Message) error { sent = append(sent, m); return nil }
	defer func() { mail.SendFunc = nil }()
	srv := httptest.NewServer(New(cfg, st, slog.Default()).Handler())
	defer srv.Close()
	c := &client{t: t, srv: srv}

	if code, b, _ := c.do("GET", "/api/portal/register/policy", nil, nil); code != 200 || !strings.Contains(string(b), `"verify":true`) {
		t.Fatalf("policy: %d %s", code, b)
	}
	// Registration without a code is refused.
	if code, _, _ := c.do("POST", "/api/portal/register", map[string]string{"Email": "new@test", "Password": "password123"}, nil); code != 400 {
		t.Fatalf("register without code: %d", code)
	}
	if code, b, _ := c.do("POST", "/api/portal/verify/send", map[string]string{"Email": "new@test", "Purpose": "register"}, nil); code != 200 {
		t.Fatalf("send code: %d %s", code, b)
	}
	if len(sent) != 1 || sent[0].To != "new@test" {
		t.Fatalf("code mail: %+v", sent)
	}
	code := extractCode(sent[0].Subject)
	if code2, _, _ := c.do("POST", "/api/portal/verify/send", map[string]string{"Email": "new@test", "Purpose": "register"}, nil); code2 != 429 {
		t.Fatalf("a second code within a minute should be refused: %d", code2)
	}
	if c2, _, _ := c.do("POST", "/api/portal/register", map[string]string{"Email": "new@test", "Password": "password123", "Code": "000000"}, nil); c2 != 400 {
		t.Fatalf("wrong code: %d", c2)
	}
	if c2, b, _ := c.do("POST", "/api/portal/register", map[string]string{"Email": "new@test", "Password": "password123", "Code": code}, nil); c2 != 200 {
		t.Fatalf("register with code: %d %s", c2, b)
	}
	c.do("POST", "/api/portal/logout", nil, nil)

	// Password reset by code.
	if c2, _, _ := c.do("POST", "/api/portal/verify/send", map[string]string{"Email": "nobody@test", "Purpose": "reset"}, nil); c2 != 200 {
		t.Fatalf("unknown address must look the same: %d", c2)
	}
	if len(sent) != 1 {
		t.Fatal("no mail to unknown addresses")
	}
	c.do("POST", "/api/portal/verify/send", map[string]string{"Email": "new@test", "Purpose": "reset"}, nil)
	reset := extractCode(sent[1].Subject)
	if c2, b, _ := c.do("POST", "/api/portal/password/reset", map[string]string{"Email": "new@test", "Code": reset, "Password": "newpassword9"}, nil); c2 != 200 {
		t.Fatalf("reset: %d %s", c2, b)
	}
	if c2, _, _ := c.do("POST", "/api/portal/login", map[string]string{"Email": "new@test", "Password": "password123"}, nil); c2 != 401 {
		t.Fatal("old password must stop working")
	}
	if c2, _, _ := c.do("POST", "/api/portal/login", map[string]string{"Email": "new@test", "Password": "newpassword9"}, nil); c2 != 200 {
		t.Fatal("new password should work")
	}

	// Reminders: expiring within 3 days and 90% traffic, each once.
	u, _ := st.UserByEmail(context.Background(), "new@test")
	_, b, _ := (&client{t: t, srv: srv}).do("GET", "/api/health", nil, nil)
	_ = b
	ac := &client{t: t, srv: srv}
	ac.do("POST", "/api/admin/login", map[string]string{"Email": "admin@test", "Password": "password123"}, nil)
	_, b, _ = ac.do("POST", "/api/admin/plans", map[string]any{"Name": "p", "PriceCents": 1, "PeriodDays": 2, "QuotaBytes": 1000}, nil)
	plan := mustJSON[map[string]any](t, b)
	ac.do("POST", "/api/admin/users/"+itoa(u.ID)+"/grant", map[string]any{"PlanID": plan["ID"]}, nil)
	conn.Exec(`UPDATE subscriptions SET used_up_bytes = 950 WHERE user_id = ?`, u.ID)
	r := &jobs.Runner{Store: st, Log: slog.Default(), Mail: &mail.Loader{Store: st}, SiteName: "T", PortalURL: "http://test/portal/"}
	before := len(sent)
	r.Tick(context.Background())
	r.Tick(context.Background()) // an hour has not passed; nothing new
	subjects := []string{}
	for _, m := range sent[before:] {
		subjects = append(subjects, m.Subject)
	}
	if len(subjects) != 2 || !strings.Contains(subjects[0], "到期") || !strings.Contains(subjects[1], "95%") {
		t.Fatalf("reminders: %v", subjects)
	}
}

func extractCode(subject string) string {
	f := strings.Fields(subject)
	return f[len(f)-1]
}

func TestPeriodsCouponsInvites(t *testing.T) {
	cfg := config.Default()
	cfg.BaseURL = "http://test"
	cfg.Portal.Registration = true
	conn, _ := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	_ = db.Migrate(context.Background(), conn, "sqlite")
	st := store.New(conn)
	adminUser, _ := admin.NewUser("admin@test", "password123", "admin")
	_ = st.CreateUser(context.Background(), adminUser)
	srv := httptest.NewServer(New(cfg, st, slog.Default()).Handler())
	defer srv.Close()
	ac := &client{t: t, srv: srv}
	ac.do("POST", "/api/admin/login", map[string]string{"Email": "admin@test", "Password": "password123"}, nil)

	// A plan with a monthly base price, a cheaper quarterly period, monthly quota reset.
	_, b, _ := ac.do("POST", "/api/admin/plans", map[string]any{"Name": "pro", "PriceCents": 1000, "PeriodDays": 30, "QuotaBytes": 1 << 30, "ResetMode": "monthly",
		"Prices": []map[string]any{{"period_days": 90, "price_cents": 2700}}}, nil)
	plan := mustJSON[map[string]any](t, b)
	planID := int64(plan["ID"].(float64))
	if !strings.Contains(string(b), `"ResetMode":"monthly"`) || !strings.Contains(string(b), `"period_days":90`) {
		t.Fatalf("plan: %s", b)
	}
	_, b, _ = ac.do("POST", "/api/admin/coupons", map[string]any{"Code": "SAVE10", "Kind": "percent", "Value": 10, "PerUser": 1, "Enabled": true}, nil)
	if !strings.Contains(string(b), `"Code":"SAVE10"`) {
		t.Fatalf("coupon: %s", b)
	}
	ac.do("PUT", "/api/admin/settings/invite", map[string]any{"enabled": true, "percent": 20}, nil)

	// Inviter registers, gets a code.
	inviter := &client{t: t, srv: srv}
	inviter.do("POST", "/api/portal/register", map[string]string{"Email": "inviter@test", "Password": "password123"}, nil)
	_, b, _ = inviter.do("GET", "/api/portal/invite", nil, nil)
	var inv struct {
		Code string `json:"code"`
		URL  string `json:"url"`
	}
	_ = json.Unmarshal(b, &inv)
	if len(inv.Code) != 8 || !strings.HasSuffix(inv.URL, "/?ref="+inv.Code) {
		t.Fatalf("invite: %s", b)
	}

	// Invitee arrives through the link (cookie), registers, buys a quarter with the coupon.
	u := &client{t: t, srv: srv}
	if code, _, _ := u.do("POST", "/api/portal/ref", map[string]string{"Code": inv.Code}, nil); code != 200 {
		t.Fatal("ref cookie")
	}
	// The test client keeps only the session cookie, so hand the code over
	// the way the cookie would (the header form is what browsers send).
	u.do("POST", "/api/portal/register", map[string]string{"Email": "buyer@test", "Password": "password123"}, map[string]string{"Cookie": "captain_ref=" + inv.Code})
	_, b, _ = u.do("POST", "/api/portal/orders/quote", map[string]any{"plan_id": planID, "period_days": 90, "coupon": "save10"}, nil)
	if !strings.Contains(string(b), `"list_cents":2700`) || !strings.Contains(string(b), `"discount_cents":270`) || !strings.Contains(string(b), `"amount_cents":2430`) {
		t.Fatalf("quote: %s", b)
	}
	if code, b, _ := u.do("POST", "/api/portal/orders/quote", map[string]any{"plan_id": planID, "period_days": 45}, nil); code != 400 {
		t.Fatalf("unoffered period should be refused: %d %s", code, b)
	}
	buyer, _ := st.UserByEmail(context.Background(), "buyer@test")
	ac.do("POST", "/api/admin/users/"+itoa(buyer.ID)+"/balance", map[string]any{"DeltaCents": 10000}, nil)
	_, b, _ = u.do("POST", "/api/portal/orders", map[string]any{"plan_id": planID, "period_days": 90, "coupon": "SAVE10", "gateway": "balance"}, nil)
	if !strings.Contains(string(b), `"status":"paid"`) || !strings.Contains(string(b), `"amount_cents":2430`) {
		t.Fatalf("order: %s", b)
	}
	sub, _ := st.ActiveSubscription(context.Background(), buyer.ID)
	if d := sub.ExpiresAt.Sub(sub.StartsAt).Hours() / 24; d < 89 || d > 91 {
		t.Fatalf("quarter should last 90 days, got %.0f", d)
	}
	if sub.ResetAt == nil || sub.ResetAt.Day() != 1 {
		t.Fatalf("monthly reset should land on the 1st: %v", sub.ResetAt)
	}
	// Coupon is single-use per user.
	if code, _, _ := u.do("POST", "/api/portal/orders/quote", map[string]any{"plan_id": planID, "coupon": "SAVE10"}, nil); code != 400 {
		t.Fatal("second use of a per-user coupon should fail")
	}
	// Inviter earned 20% of 2430.
	_, b, _ = inviter.do("GET", "/api/portal/invite", nil, nil)
	if !strings.Contains(string(b), `"invited":1`) || !strings.Contains(string(b), `"earned_cents":486`) {
		t.Fatalf("commission: %s", b)
	}
	inv2, _ := st.UserByEmail(context.Background(), "inviter@test")
	if inv2.BalanceCents != 486 {
		t.Fatalf("inviter balance %d", inv2.BalanceCents)
	}
	// Renewing the same plan before expiry extends the time instead of replacing it.
	before := *sub.ExpiresAt
	u.do("POST", "/api/portal/orders", map[string]any{"plan_id": planID, "gateway": "balance"}, nil)
	sub2, _ := st.ActiveSubscription(context.Background(), buyer.ID)
	if sub2.ID != sub.ID || sub2.ExpiresAt.Sub(before).Hours()/24 < 29 {
		t.Fatalf("renewal should extend the existing subscription: %v -> %v (id %d/%d)", before, sub2.ExpiresAt, sub.ID, sub2.ID)
	}
	// Notice appears on the portal once enabled.
	ac.do("PUT", "/api/admin/settings/notice", map[string]any{"enabled": true, "title": "维护", "body": "今晚"}, nil)
	_, b, _ = u.do("GET", "/api/portal/notice", nil, nil)
	if !strings.Contains(string(b), `"title":"维护"`) {
		t.Fatalf("notice: %s", b)
	}
}

func TestAgentLongPoll(t *testing.T) {
	cfg := config.Default()
	cfg.BaseURL = "http://test"
	conn, _ := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	_ = db.Migrate(context.Background(), conn, "sqlite")
	st := store.New(conn)
	adminUser, _ := admin.NewUser("admin@test", "password123", "admin")
	_ = st.CreateUser(context.Background(), adminUser)
	srv := httptest.NewServer(New(cfg, st, slog.Default()).Handler())
	defer srv.Close()
	c := &client{t: t, srv: srv}
	c.do("POST", "/api/admin/login", map[string]string{"Email": "admin@test", "Password": "password123"}, nil)
	_, b, _ := c.do("POST", "/api/admin/nodes", map[string]string{"Name": "n"}, nil)
	node := mustJSON[map[string]any](t, b)
	agent := &client{t: t, srv: srv}
	_, b, _ = agent.do("POST", "/api/agent/pair", agentproto.PairRequest{Code: node["pair_code"].(string)}, nil)
	agent.token = mustJSON[agentproto.PairResponse](t, b).Token
	_, _, hdr := agent.do("GET", "/api/agent/state", nil, nil)
	etag := hdr.Get("ETag")
	if etag == "" {
		t.Fatal("no etag")
	}
	// Unchanged: the request is held for the wait window and answers 304.
	start := time.Now()
	code, _, _ := agent.do("GET", "/api/agent/state?wait=3s", nil, map[string]string{"If-None-Match": etag})
	if code != 304 || time.Since(start) < 2*time.Second {
		t.Fatalf("expected a held 304, got %d after %v", code, time.Since(start))
	}
	// A change made while waiting returns the new state early.
	go func() {
		time.Sleep(1500 * time.Millisecond)
		c.do("POST", "/api/admin/nodes/"+itoa(int64(node["id"].(float64)))+"/inbounds", map[string]any{"Tag": "t", "Protocol": "vless", "Port": 1}, nil)
	}()
	start = time.Now()
	code, b, hdr = agent.do("GET", "/api/agent/state?wait=20s", nil, map[string]string{"If-None-Match": etag})
	if code != 200 || time.Since(start) > 10*time.Second || hdr.Get("ETag") == etag || !strings.Contains(string(b), `"tag":"t"`) {
		t.Fatalf("expected the new state promptly: %d after %v", code, time.Since(start))
	}
}
