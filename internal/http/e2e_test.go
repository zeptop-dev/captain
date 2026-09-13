package http

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/zeptop-dev/captain/internal/captcha"
	"github.com/zeptop-dev/captain/internal/webhook"
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
	// Profile name: unquoted ASCII filename, UTF-8 filename*, and Clash profile-title.
	_ = st.SetSetting(context.Background(), "site", map[string]any{"name": "深度 Proxy"})
	_, _, h = anon.do("GET", "/sub/"+u1["sub_token"].(string), nil, map[string]string{"User-Agent": "clash-verge/1.0"})
	if cd := h.Get("Content-Disposition"); cd != "attachment; filename=Proxy; filename*=UTF-8''%E6%B7%B1%E5%BA%A6%20Proxy" {
		t.Fatalf("content-disposition: %s", cd)
	}
	if pt := h.Get("Profile-Title"); pt != "base64:5rex5bqmIFByb3h5" {
		t.Fatalf("profile-title: %s", pt)
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

func TestRegistrationLimits(t *testing.T) {
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

	reg := func(email, ip, invite, cap string) (int, string) {
		c := &client{t: t, srv: srv}
		code, b, _ := c.do("POST", "/api/portal/register", map[string]string{"Email": email, "Password": "password123", "Invite": invite, "Captcha": cap}, map[string]string{"X-Real-IP": ip})
		return code, string(b)
	}
	put := func(v map[string]any) {
		if code, b, _ := ac.do("PUT", "/api/admin/settings/registration", v, nil); code != 200 {
			t.Fatalf("put registration: %d %s", code, b)
		}
	}

	// Email whitelist: suffixes are normalised, subdomains accepted.
	put(map[string]any{"email_suffixes": []string{"@Example.com "}})
	if code, b := reg("a@gmail.com", "1.1.1.1", "", ""); code != 403 || !strings.Contains(b, "domain") {
		t.Fatalf("whitelist: %d %s", code, b)
	}
	if code, b := reg("a@mail.example.com", "1.1.1.1", "", ""); code != 200 {
		t.Fatalf("whitelist ok: %d %s", code, b)
	}
	_, b, _ := ac.do("GET", "/api/portal/register/policy", nil, nil)
	if !strings.Contains(string(b), `"email_suffixes":["example.com"]`) {
		t.Fatalf("policy: %s", b)
	}

	// Per-IP cap: 2 per window from the same address; another address is fine.
	put(map[string]any{"ip_limit": 2, "ip_window_hours": 1})
	reg("b@test", "2.2.2.2", "", "")
	reg("c@test", "2.2.2.2", "", "")
	if code, b := reg("d@test", "2.2.2.2", "", ""); code != 403 || !strings.Contains(b, "address") {
		t.Fatalf("ip cap: %d %s", code, b)
	}
	if code, _ := reg("d@test", "3.3.3.3", "", ""); code != 200 {
		t.Fatal("other ip should pass")
	}
	if n, _ := st.RegistrationsFromIP(context.Background(), "2.2.2.2", time.Now().Add(-time.Hour)); n != 2 {
		t.Fatalf("count %d", n)
	}

	// Invite-only: needs a valid code; the policy tells the SPA.
	put(map[string]any{"invite_only": true})
	_, b, _ = ac.do("GET", "/api/portal/register/policy", nil, nil)
	if !strings.Contains(string(b), `"invite_only":true`) {
		t.Fatalf("policy: %s", b)
	}
	if code, b := reg("e@test", "4.4.4.4", "", ""); code != 403 || !strings.Contains(b, "invite") {
		t.Fatalf("invite only: %d %s", code, b)
	}
	if code, b := reg("e@test", "4.4.4.4", "nope", ""); code != 403 {
		t.Fatalf("bad invite: %d %s", code, b)
	}
	ac.do("PUT", "/api/admin/settings/invite", map[string]any{"enabled": true, "percent": 10}, nil)
	inviter := &client{t: t, srv: srv}
	inviter.cookie = nil
	inviter.do("POST", "/api/portal/login", map[string]string{"Email": "d@test", "Password": "password123"}, nil)
	_, b, _ = inviter.do("GET", "/api/portal/invite", nil, nil)
	var inv struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(b, &inv)
	if code, b := reg("e@test", "4.4.4.4", inv.Code, ""); code != 200 {
		t.Fatalf("with invite: %d %s", code, b)
	}

	// Captcha: verified through the provider; the secret never leaves the admin API.
	var seen []string
	orig := captcha.VerifyFunc
	captcha.VerifyFunc = func(_ context.Context, s captcha.Settings, token, ip string) error {
		seen = append(seen, s.Provider+":"+s.SecretKey+":"+token+":"+ip)
		if token != "good" {
			return errors.New("captcha failed")
		}
		return nil
	}
	defer func() { captcha.VerifyFunc = orig }()
	put(map[string]any{"invite_only": false, "captcha": map[string]string{"provider": "turnstile", "site_key": "sk", "secret_key": "sec"}})
	_, b, _ = ac.do("GET", "/api/admin/settings/registration", nil, nil)
	if strings.Contains(string(b), `"sec"`) || !strings.Contains(string(b), `"has_captcha_secret":true`) {
		t.Fatalf("secret leaked: %s", b)
	}
	// Saving without a secret keeps the old one.
	put(map[string]any{"captcha": map[string]string{"provider": "turnstile", "site_key": "sk2", "secret_key": ""}})
	_, b, _ = ac.do("GET", "/api/portal/register/policy", nil, nil)
	if !strings.Contains(string(b), `"captcha":{"provider":"turnstile","site_key":"sk2"}`) {
		t.Fatalf("policy captcha: %s", b)
	}
	if code, b := reg("f@test", "5.5.5.5", "", ""); code != 403 || !strings.Contains(b, "captcha required") {
		t.Fatalf("missing captcha: %d %s", code, b)
	}
	if code, _ := reg("f@test", "5.5.5.5", "", "bad"); code != 403 {
		t.Fatal("bad captcha should fail")
	}
	if code, b := reg("f@test", "5.5.5.5", "", "good"); code != 200 {
		t.Fatalf("good captcha: %d %s", code, b)
	}
	if len(seen) != 2 || seen[1] != "turnstile:sec:good:5.5.5.5" {
		t.Fatalf("verify calls: %v", seen)
	}
}

func TestNodeInstallScript(t *testing.T) {
	cfg := config.Default()
	cfg.BaseURL = "https://panel.test"
	conn, _ := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	_ = db.Migrate(context.Background(), conn, "sqlite")
	st := store.New(conn)
	adminUser, _ := admin.NewUser("admin@test", "password123", "admin")
	_ = st.CreateUser(context.Background(), adminUser)
	srv := httptest.NewServer(New(cfg, st, slog.Default()).Handler())
	defer srv.Close()
	ac := &client{t: t, srv: srv}
	ac.do("POST", "/api/admin/login", map[string]string{"Email": "admin@test", "Password": "password123"}, nil)
	_, b, _ := ac.do("POST", "/api/admin/nodes", map[string]string{"Name": "jp1", "PublicAddr": "jp1.test"}, nil)
	code := mustJSON[map[string]any](t, b)["pair_code"].(string)

	anon := &client{t: t, srv: srv}
	status, body, hdr := anon.do("GET", "/api/agent/install.sh?pair="+code, nil, nil)
	if status != 200 || !strings.Contains(hdr.Get("Content-Type"), "shellscript") {
		t.Fatalf("install.sh: %d %s", status, body)
	}
	want := `sh -s -- --captain "https://panel.test" --pair "` + code + `"`
	if !strings.Contains(string(body), want) || !strings.Contains(string(body), "zeptop-dev/bosun/master/scripts/install.sh") {
		t.Fatalf("script body:\n%s", body)
	}
	if status, _, _ = anon.do("GET", "/api/agent/install.sh?pair=NOPE-0000", nil, nil); status != 404 {
		t.Fatalf("bad code: %d", status)
	}
	if status, _, _ = anon.do("GET", "/api/agent/install.sh", nil, nil); status != 400 {
		t.Fatalf("no code: %d", status)
	}
	// Once redeemed the script disappears.
	anon.do("POST", "/api/agent/pair", map[string]string{"Code": code, "Hostname": "jp1", "Version": "v0", "Platform": "linux/amd64"}, nil)
	if status, _, _ = anon.do("GET", "/api/agent/install.sh?pair="+code, nil, nil); status != 404 {
		t.Fatalf("redeemed code still served: %d", status)
	}
}

func TestOpsBatch(t *testing.T) {
	cfg := config.Default()
	cfg.BaseURL = "https://panel.test"
	cfg.Portal.Registration = true
	cfg.SiteName = "Captain"
	conn, _ := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	_ = db.Migrate(context.Background(), conn, "sqlite")
	st := store.New(conn)
	adminUser, _ := admin.NewUser("admin@test", "password123", "admin")
	_ = st.CreateUser(context.Background(), adminUser)
	srv := httptest.NewServer(New(cfg, st, slog.Default()).Handler())
	defer srv.Close()
	ac := &client{t: t, srv: srv}
	ac.do("POST", "/api/admin/login", map[string]string{"Email": "admin@test", "Password": "password123"}, nil)

	// Captured mail for ticket replies.
	var mails []mail.Message
	mail.SendFunc = func(_ context.Context, _ mail.Settings, m mail.Message) error { mails = append(mails, m); return nil }
	defer func() { mail.SendFunc = nil }()
	ms := mail.Settings{Provider: "resend", FromAddress: "no@test"}
	ms.Resend.APIKey = "k"
	_ = st.SetSetting(context.Background(), mail.SettingKey, ms)

	// Trial: a 3-day plan handed to every new account.
	_, b, _ := ac.do("POST", "/api/admin/plans", map[string]any{"Name": "trial", "PriceCents": 0, "PeriodDays": 30, "QuotaBytes": 1 << 30}, nil)
	trialID := int64(mustJSON[map[string]any](t, b)["ID"].(float64))
	_, b, _ = ac.do("POST", "/api/admin/plans", map[string]any{"Name": "pro", "PriceCents": 1000, "PeriodDays": 30, "QuotaBytes": 10 << 30}, nil)
	proID := int64(mustJSON[map[string]any](t, b)["ID"].(float64))
	if code, b, _ := ac.do("PUT", "/api/admin/settings/trial", map[string]any{"plan_id": trialID, "period_days": 3}, nil); code != 200 {
		t.Fatalf("trial settings: %d %s", code, b)
	}
	uc := &client{t: t, srv: srv}
	uc.do("POST", "/api/portal/register", map[string]string{"Email": "u@test", "Password": "password123"}, nil)
	_, b, _ = uc.do("GET", "/api/portal/me", nil, nil)
	me := mustJSON[map[string]any](t, b)
	sub, _ := me["subscription"].(map[string]any)
	if sub == nil || int64(sub["plan_id"].(float64)) != trialID {
		t.Fatalf("trial not granted: %s", b)
	}
	exp, _ := time.Parse(time.RFC3339, sub["expires_at"].(string))
	if d := time.Until(exp); d < 2*24*time.Hour || d > 4*24*time.Hour {
		t.Fatalf("trial expiry %v", exp)
	}

	// Tickets: user opens, admin replies (mail goes out), user replies, admin closes.
	code, b, _ := uc.do("POST", "/api/portal/tickets", map[string]string{"Subject": "Cannot connect", "Body": "help", "Priority": "high"}, nil)
	if code != 200 {
		t.Fatalf("create ticket: %d %s", code, b)
	}
	tk := mustJSON[map[string]any](t, b)
	tid := itoa(int64(tk["id"].(float64)))
	_, b, _ = ac.do("GET", "/api/admin/dashboard", nil, nil)
	if !strings.Contains(string(b), `"open_tickets":1`) {
		t.Fatalf("dashboard: %s", b)
	}
	code, b, _ = ac.do("POST", "/api/admin/tickets/"+tid+"/reply", map[string]string{"Body": "Try again"}, nil)
	if code != 200 || !strings.Contains(string(b), `"status":"replied"`) {
		t.Fatalf("admin reply: %d %s", code, b)
	}
	if len(mails) != 1 || mails[0].To != "u@test" || !strings.Contains(mails[0].Subject, "Cannot connect") {
		t.Fatalf("ticket mail: %+v", mails)
	}
	code, b, _ = uc.do("POST", "/api/portal/tickets/"+tid+"/reply", map[string]string{"Body": "still broken"}, nil)
	if code != 200 || !strings.Contains(string(b), `"status":"open"`) || strings.Count(string(b), `"body"`) != 3 {
		t.Fatalf("user reply: %d %s", code, b)
	}
	other := &client{t: t, srv: srv}
	other.do("POST", "/api/portal/register", map[string]string{"Email": "o@test", "Password": "password123"}, nil)
	if code, _, _ := other.do("GET", "/api/portal/tickets/"+tid, nil, nil); code != 404 {
		t.Fatalf("other user sees ticket: %d", code)
	}
	ac.do("POST", "/api/admin/tickets/"+tid+"/status", map[string]string{"Status": "closed"}, nil)
	if code, _, _ := uc.do("POST", "/api/portal/tickets/"+tid+"/reply", map[string]string{"Body": "x"}, nil); code != 409 {
		t.Fatalf("reply to closed: %d", code)
	}
	_, b, _ = ac.do("GET", "/api/admin/tickets?status=closed", nil, nil)
	if !strings.Contains(string(b), `"total":1`) || !strings.Contains(string(b), `"email":"u@test"`) {
		t.Fatalf("list tickets: %s", b)
	}

	// Gift codes: balance, plan, traffic; single use; unknown code rejected.
	code, b, _ = ac.do("POST", "/api/admin/gifts", map[string]any{"Kind": "balance", "Value": 500, "Count": 2, "Prefix": "GIFT-", "Batch": "promo"}, nil)
	if code != 200 {
		t.Fatalf("create gifts: %d %s", code, b)
	}
	var gen struct {
		Codes []string `json:"codes"`
	}
	_ = json.Unmarshal(b, &gen)
	if len(gen.Codes) != 2 || !strings.HasPrefix(gen.Codes[0], "GIFT-") {
		t.Fatalf("codes %v", gen.Codes)
	}
	if code, b, _ := uc.do("POST", "/api/portal/redeem", map[string]string{"Code": strings.ToLower(gen.Codes[0])}, nil); code != 200 || !strings.Contains(string(b), `"kind":"balance"`) {
		t.Fatalf("redeem: %d %s", code, b)
	}
	if code, _, _ := uc.do("POST", "/api/portal/redeem", map[string]string{"Code": gen.Codes[0]}, nil); code != 400 {
		t.Fatalf("reuse should fail: %d", code)
	}
	if code, _, _ := uc.do("POST", "/api/portal/redeem", map[string]string{"Code": "NOPE"}, nil); code != 400 {
		t.Fatalf("unknown should fail: %d", code)
	}
	_, b, _ = uc.do("GET", "/api/portal/me", nil, nil)
	if !strings.Contains(string(b), `"balance_cents":500`) {
		t.Fatalf("balance: %s", b)
	}
	_, b, _ = ac.do("POST", "/api/admin/gifts", map[string]any{"Kind": "plan", "PlanID": proID, "PeriodDays": 90, "Count": 1}, nil)
	_ = json.Unmarshal(b, &gen)
	uc.do("POST", "/api/portal/redeem", map[string]string{"Code": gen.Codes[0]}, nil)
	_, b, _ = uc.do("GET", "/api/portal/me", nil, nil)
	me = mustJSON[map[string]any](t, b)
	sub = me["subscription"].(map[string]any)
	if int64(sub["plan_id"].(float64)) != proID || int64(sub["quota_bytes"].(float64)) != 10<<30 {
		t.Fatalf("plan gift: %s", b)
	}
	_, b, _ = ac.do("POST", "/api/admin/gifts", map[string]any{"Kind": "traffic", "Value": 1 << 30, "Count": 1}, nil)
	_ = json.Unmarshal(b, &gen)
	uc.do("POST", "/api/portal/redeem", map[string]string{"Code": gen.Codes[0]}, nil)
	_, b, _ = uc.do("GET", "/api/portal/me", nil, nil)
	if !strings.Contains(string(b), `"quota_bytes":11811160064`) {
		t.Fatalf("traffic gift: %s", b)
	}
	_, b, _ = ac.do("GET", "/api/admin/gifts/batches", nil, nil)
	if !strings.Contains(string(b), `"batch":"promo"`) || !strings.Contains(string(b), `"redeemed":1`) {
		t.Fatalf("batches: %s", b)
	}
	_, b, _ = ac.do("DELETE", "/api/admin/gifts/batches/promo", nil, nil)
	if !strings.Contains(string(b), `"deleted":1`) {
		t.Fatalf("delete batch: %s", b)
	}

	// Knowledge base: placeholders substituted per user; drafts hidden.
	_, b, _ = ac.do("POST", "/api/admin/articles", map[string]any{"Title": "Clash setup", "Category": "Windows", "Body": "Import {{sub_url}} as {{email}}"}, nil)
	aid := itoa(int64(mustJSON[map[string]any](t, b)["id"].(float64)))
	ac.do("POST", "/api/admin/articles", map[string]any{"Title": "Draft", "Body": "x", "Published": false}, nil)
	_, b, _ = uc.do("GET", "/api/portal/articles", nil, nil)
	if strings.Contains(string(b), "Draft") || !strings.Contains(string(b), "Clash setup") {
		t.Fatalf("articles: %s", b)
	}
	_, b, _ = uc.do("GET", "/api/portal/articles/"+aid, nil, nil)
	if !strings.Contains(string(b), "https://panel.test/sub/") || !strings.Contains(string(b), "as u@test") {
		t.Fatalf("article body: %s", b)
	}

	// Client downloads list.
	ac.do("PUT", "/api/admin/settings/clients", map[string]any{"items": []map[string]string{{"name": "Clash Verge", "platform": "windows", "url": "https://x/y.exe"}, {"name": "", "url": ""}}}, nil)
	_, b, _ = uc.do("GET", "/api/portal/clients", nil, nil)
	if !strings.Contains(string(b), "Clash Verge") || strings.Count(string(b), `"name"`) != 1 {
		t.Fatalf("clients: %s", b)
	}

	// Telegram off: portal reports disabled.
	_, b, _ = uc.do("GET", "/api/portal/telegram", nil, nil)
	if !strings.Contains(string(b), `"enabled":false`) {
		t.Fatalf("telegram: %s", b)
	}
}

func TestSurplusAndMultiLevelCommission(t *testing.T) {
	cfg := config.Default()
	cfg.BaseURL = "https://panel.test"
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
	planID := func(name string, price int64, days int) int64 {
		_, b, _ := ac.do("POST", "/api/admin/plans", map[string]any{"Name": name, "PriceCents": price, "PeriodDays": days, "QuotaBytes": 1 << 30}, nil)
		return int64(mustJSON[map[string]any](t, b)["ID"].(float64))
	}
	basic, pro := planID("basic", 3000, 30), planID("pro", 9000, 30)

	// Three-level referral chain: a invites b invites c. Rewards go to the commission balance.
	ac.do("PUT", "/api/admin/settings/invite", map[string]any{"enabled": true, "percent": 20, "multi_level": true, "level2": 10, "level3": 5, "payout": "commission", "min_withdraw_cents": 500, "withdraw_methods": []string{"USDT", "Alipay"}}, nil)
	ac.do("PUT", "/api/admin/settings/surplus", map[string]any{"enabled": true}, nil)
	signup := func(email, ref string) (*client, map[string]any) {
		c := &client{t: t, srv: srv}
		hdr := map[string]string{}
		if ref != "" {
			hdr["Cookie"] = "captain_ref=" + ref
		}
		c.do("POST", "/api/portal/register", map[string]string{"Email": email, "Password": "password123"}, hdr)
		_, b, _ := c.do("GET", "/api/portal/invite", nil, nil)
		return c, mustJSON[map[string]any](t, b)
	}
	a, ai := signup("a@test", "")
	b, bi := signup("b@test", ai["code"].(string))
	c, _ := signup("c@test", bi["code"].(string))
	if lv := ai["levels"].([]any); len(lv) != 3 {
		t.Fatalf("levels %v", lv)
	}
	// c buys basic (3000) with balance: b (level 1) gets 20%, a (level 2) gets 10%.
	cu, _ := st.UserByEmail(context.Background(), "c@test")
	_ = st.AdjustBalance(context.Background(), cu.ID, 20000)
	if code, body, _ := c.do("POST", "/api/portal/orders", map[string]any{"plan_id": basic, "gateway": "balance"}, nil); code != 200 {
		t.Fatalf("c buys basic: %d %s", code, body)
	}
	_, body, _ := b.do("GET", "/api/portal/invite", nil, nil)
	if !strings.Contains(string(body), `"commission_cents":600`) {
		t.Fatalf("b commission: %s", body)
	}
	_, body, _ = a.do("GET", "/api/portal/invite", nil, nil)
	if !strings.Contains(string(body), `"commission_cents":300`) {
		t.Fatalf("a level-2 commission: %s", body)
	}
	au, _ := st.UserByEmail(context.Background(), "a@test")
	if au.BalanceCents != 0 {
		t.Fatalf("commission leaked into balance: %d", au.BalanceCents)
	}

	// Surplus: c switches to pro right away; ~100% of basic's 3000 is credited.
	_, body, _ = c.do("POST", "/api/portal/orders/quote", map[string]any{"plan_id": pro}, nil)
	q := mustJSON[map[string]any](t, body)
	surplus := int64(q["surplus_cents"].(float64))
	if surplus < 2990 || surplus > 3000 || int64(q["amount_cents"].(float64)) != 9000-surplus {
		t.Fatalf("surplus quote: %s", body)
	}
	// Renewing the same plan gets no surplus.
	_, body, _ = c.do("POST", "/api/portal/orders/quote", map[string]any{"plan_id": basic}, nil)
	if !strings.Contains(string(body), `"surplus_cents":0`) {
		t.Fatalf("renewal quote: %s", body)
	}
	code, body, _ := c.do("POST", "/api/portal/orders", map[string]any{"plan_id": pro, "gateway": "balance"}, nil)
	if code != 200 {
		t.Fatalf("c switches: %d %s", code, body)
	}
	paid := int64(mustJSON[map[string]any](t, body)["amount_cents"].(float64))
	if credit := 9000 - paid; credit > surplus || credit < surplus-5 { // a second may have passed since the quote
		t.Fatalf("credit at purchase %d vs quote %d", credit, surplus)
	}
	cu, _ = st.UserByEmail(context.Background(), "c@test")
	if cu.BalanceCents != 20000-3000-paid {
		t.Fatalf("balance after switch: %d", cu.BalanceCents)
	}
	_, body, _ = c.do("GET", "/api/portal/me", nil, nil)
	if !strings.Contains(string(body), `"plan_id":`+itoa(pro)) {
		t.Fatalf("plan after switch: %s", body)
	}

	// Commission: transfer part to balance, withdraw the rest; admin rejects then pays.
	if code, body, _ := b.do("POST", "/api/portal/invite/transfer", map[string]any{"AmountCents": 100}, nil); code != 200 || !strings.Contains(string(body), `"commission_cents":`) {
		t.Fatalf("transfer: %d %s", code, body)
	}
	bu, _ := st.UserByEmail(context.Background(), "b@test")
	if bu.BalanceCents != 100 {
		t.Fatalf("transfer balance %d", bu.BalanceCents)
	}
	if code, _, _ := b.do("POST", "/api/portal/invite/withdraw", map[string]any{"AmountCents": 300, "Method": "USDT", "Account": "T..."}, nil); code != 400 {
		t.Fatal("below minimum should fail")
	}
	if code, _, _ := b.do("POST", "/api/portal/invite/withdraw", map[string]any{"AmountCents": 9999, "Method": "USDT", "Account": "T..."}, nil); code != 400 {
		t.Fatal("over balance should fail")
	}
	code, body, _ = b.do("POST", "/api/portal/invite/withdraw", map[string]any{"AmountCents": 500, "Method": "PayPal", "Account": "x"}, nil)
	if code != 400 {
		t.Fatalf("unknown method: %d %s", code, body)
	}
	_, body, _ = b.do("GET", "/api/portal/invite", nil, nil)
	before := int64(mustJSON[map[string]any](t, body)["commission_cents"].(float64)) // b also earned on c's second order
	code, body, _ = b.do("POST", "/api/portal/invite/withdraw", map[string]any{"AmountCents": 1000, "Method": "USDT", "Account": "TXYZ"}, nil)
	if code != 200 {
		t.Fatalf("withdraw: %d %s", code, body)
	}
	wid := itoa(int64(mustJSON[map[string]any](t, body)["id"].(float64)))
	_, body, _ = b.do("GET", "/api/portal/invite", nil, nil)
	if !strings.Contains(string(body), `"commission_cents":`+itoa(before-1000)+`,`) {
		t.Fatalf("reserved on withdraw: %s", body)
	}
	_, body, _ = ac.do("GET", "/api/admin/withdrawals?status=pending", nil, nil)
	if !strings.Contains(string(body), `"email":"b@test"`) || !strings.Contains(string(body), `"amount_cents":1000`) {
		t.Fatalf("admin withdrawals: %s", body)
	}
	ac.do("POST", "/api/admin/withdrawals/"+wid+"/status", map[string]string{"Status": "rejected", "Note": "wrong address"}, nil)
	_, body, _ = b.do("GET", "/api/portal/invite", nil, nil)
	if !strings.Contains(string(body), `"commission_cents":`+itoa(before)+`,`) {
		t.Fatalf("refund after reject: %s", body)
	}
	if code, _, _ := ac.do("POST", "/api/admin/withdrawals/"+wid+"/status", map[string]string{"Status": "paid"}, nil); code != 404 {
		t.Fatalf("re-deciding should 404: %d", code)
	}
	_, body, _ = b.do("POST", "/api/portal/invite/withdraw", map[string]any{"AmountCents": 1000, "Method": "Alipay", "Account": "b@test"}, nil)
	wid = itoa(int64(mustJSON[map[string]any](t, body)["id"].(float64)))
	ac.do("POST", "/api/admin/withdrawals/"+wid+"/status", map[string]string{"Status": "paid", "Note": "sent"}, nil)
	_, body, _ = b.do("GET", "/api/portal/invite/withdrawals", nil, nil)
	if !strings.Contains(string(body), `"status":"paid"`) || !strings.Contains(string(body), `"status":"rejected"`) {
		t.Fatalf("withdrawal history: %s", body)
	}
	_, body, _ = b.do("GET", "/api/portal/invite", nil, nil)
	if !strings.Contains(string(body), `"commission_cents":`+itoa(before-1000)+`,`) {
		t.Fatalf("commission after payout: %s", body)
	}
}

func TestStaffRolesAndWebhooks(t *testing.T) {
	cfg := config.Default()
	cfg.BaseURL = "https://panel.test"
	cfg.Portal.Registration = true
	conn, _ := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	_ = db.Migrate(context.Background(), conn, "sqlite")
	st := store.New(conn)
	adminUser, _ := admin.NewUser("admin@test", "password123", "admin")
	_ = st.CreateUser(context.Background(), adminUser)
	web := New(cfg, st, slog.Default())
	web.Hooks().Sync = true
	srv := httptest.NewServer(web.Handler())
	defer srv.Close()
	ac := &client{t: t, srv: srv}
	ac.do("POST", "/api/admin/login", map[string]string{"Email": "admin@test", "Password": "password123"}, nil)

	// Webhook endpoint capturing signed deliveries.
	type hit struct {
		event, sig string
		body       string
	}
	var hits []hit
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		hits = append(hits, hit{r.Header.Get("X-Captain-Event"), r.Header.Get("X-Captain-Signature"), string(b)})
	}))
	defer sink.Close()
	if code, b, _ := ac.do("PUT", "/api/admin/settings/webhooks", map[string]any{"endpoints": []map[string]any{{"url": sink.URL, "secret": "sec", "enabled": true}, {"url": "ftp://x", "enabled": true}}}, nil); code != 400 {
		t.Fatalf("bad scheme accepted: %d %s", code, b)
	}
	ac.do("PUT", "/api/admin/settings/webhooks", map[string]any{"endpoints": []map[string]any{{"url": sink.URL, "secret": "sec", "enabled": true}}}, nil)
	if code, _, _ := ac.do("POST", "/api/admin/settings/webhooks/test", map[string]string{"URL": sink.URL, "Secret": "sec"}, nil); code != 200 {
		t.Fatal("test delivery failed")
	}

	// Staff: operator and support accounts.
	code, b, _ := ac.do("POST", "/api/admin/admins", map[string]string{"Email": "op@test", "Password": "password123", "Role": "operator"}, nil)
	if code != 200 {
		t.Fatalf("create operator: %d %s", code, b)
	}
	ac.do("POST", "/api/admin/admins", map[string]string{"Email": "sup@test", "Password": "password123", "Role": "support"}, nil)
	if code, _, _ := ac.do("POST", "/api/admin/admins", map[string]string{"Email": "x@test", "Password": "password123", "Role": "user"}, nil); code != 400 {
		t.Fatal("role user must be rejected")
	}
	op := &client{t: t, srv: srv}
	if code, b, _ := op.do("POST", "/api/admin/login", map[string]string{"Email": "op@test", "Password": "password123"}, nil); code != 200 {
		t.Fatalf("operator login: %d %s", code, b)
	}
	if code, _, _ := op.do("GET", "/api/admin/nodes", nil, nil); code != 200 {
		t.Fatal("operator should list nodes")
	}
	if code, _, _ := op.do("GET", "/api/admin/settings/mail", nil, nil); code != 403 {
		t.Fatal("operator must not read settings")
	}
	if code, _, _ := op.do("GET", "/api/admin/admins", nil, nil); code != 403 {
		t.Fatal("operator must not manage staff")
	}
	sup := &client{t: t, srv: srv}
	sup.do("POST", "/api/admin/login", map[string]string{"Email": "sup@test", "Password": "password123"}, nil)
	if code, _, _ := sup.do("GET", "/api/admin/tickets", nil, nil); code != 200 {
		t.Fatal("support should list tickets")
	}
	if code, _, _ := sup.do("GET", "/api/admin/users", nil, nil); code != 200 {
		t.Fatal("support should read users")
	}
	if code, _, _ := sup.do("POST", "/api/admin/users", map[string]string{"Email": "n@test", "Password": "password123"}, nil); code != 403 {
		t.Fatal("support must not create users")
	}
	if code, _, _ := sup.do("GET", "/api/admin/nodes", nil, nil); code != 403 {
		t.Fatal("support must not see nodes")
	}
	// The last admin is protected.
	if code, _, _ := ac.do("PATCH", "/api/admin/admins/"+itoa(adminUser.ID), map[string]string{"Role": "support"}, nil); code != 409 {
		t.Fatal("last admin demotion must fail")
	}
	if code, _, _ := ac.do("DELETE", "/api/admin/admins/"+itoa(adminUser.ID), nil, nil); code != 409 {
		t.Fatal("deleting yourself must fail")
	}
	_, b, _ = ac.do("GET", "/api/admin/admins", nil, nil)
	var staff []map[string]any
	_ = json.Unmarshal(b, &staff)
	var opID int64
	for _, s := range staff {
		if s["email"] == "op@test" {
			opID = int64(s["id"].(float64))
		}
	}
	ac.do("PATCH", "/api/admin/admins/"+itoa(opID), map[string]string{"Role": "admin"}, nil)
	if code, _, _ := ac.do("PATCH", "/api/admin/admins/"+itoa(adminUser.ID), map[string]string{"Role": "support"}, nil); code != 200 {
		t.Fatal("demotion with a second admin present should work")
	}
	// The demoted account cannot restore itself; the other admin does it.
	if code, _, _ := ac.do("PATCH", "/api/admin/admins/"+itoa(adminUser.ID), map[string]string{"Role": "admin"}, nil); code != 403 {
		t.Fatal("support session must not manage staff")
	}
	if code, _, _ := op.do("PATCH", "/api/admin/admins/"+itoa(adminUser.ID), map[string]string{"Role": "admin"}, nil); code != 200 {
		t.Fatal("promoted admin should restore the original")
	}

	// Events: registration, ticket, balance-paid order.
	uc := &client{t: t, srv: srv}
	uc.do("POST", "/api/portal/register", map[string]string{"Email": "u@test", "Password": "password123"}, nil)
	uc.do("POST", "/api/portal/tickets", map[string]string{"Subject": "hi", "Body": "help"}, nil)
	_, b, _ = ac.do("POST", "/api/admin/plans", map[string]any{"Name": "p", "PriceCents": 100, "PeriodDays": 30, "QuotaBytes": 1}, nil)
	planID := int64(mustJSON[map[string]any](t, b)["ID"].(float64))
	uu, _ := st.UserByEmail(context.Background(), "u@test")
	_ = st.AdjustBalance(context.Background(), uu.ID, 100)
	uc.do("POST", "/api/portal/orders", map[string]any{"plan_id": planID, "gateway": "balance"}, nil)
	events := []string{}
	for _, h := range hits {
		events = append(events, h.event)
		if h.sig != "sha256="+webhook.Sign("sec", []byte(h.body)) {
			t.Fatalf("bad signature on %s", h.event)
		}
	}
	want := []string{"test", "user.registered", "ticket.created", "order.paid"}
	if strings.Join(events, ",") != strings.Join(want, ",") {
		t.Fatalf("events %v, want %v", events, want)
	}
	if !strings.Contains(hits[3].body, `"order_no"`) || !strings.Contains(hits[1].body, `"u@test"`) {
		t.Fatalf("payloads: %+v", hits)
	}
}

func TestProbePageAndBeats(t *testing.T) {
	cfg := config.Default()
	cfg.BaseURL = "https://panel.test"
	cfg.Portal.Registration = true
	conn, _ := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	_ = db.Migrate(context.Background(), conn, "sqlite")
	st := store.New(conn)
	adminUser, _ := admin.NewUser("admin@test", "password123", "admin")
	_ = st.CreateUser(context.Background(), adminUser)
	web := New(cfg, st, slog.Default())
	srv := httptest.NewServer(web.Handler())
	defer srv.Close()
	ac := &client{t: t, srv: srv}
	ac.do("POST", "/api/admin/login", map[string]string{"Email": "admin@test", "Password": "password123"}, nil)

	// Off by default: page and API 404, and the node state carries no probe.
	if code, _, _ := ac.do("GET", "/api/probe", nil, nil); code != 404 {
		t.Fatalf("probe API while off: %d", code)
	}
	_, b, _ := ac.do("POST", "/api/admin/nodes", map[string]string{"Name": "jp1", "PublicAddr": "jp1.test"}, nil)
	node := mustJSON[map[string]any](t, b)
	nodeID := itoa(int64(node["id"].(float64)))
	nc := &client{t: t, srv: srv}
	_, b, _ = nc.do("POST", "/api/agent/pair", map[string]string{"Code": node["pair_code"].(string), "Hostname": "jp1", "Version": "v0.11.0", "Platform": "linux/amd64"}, nil)
	nc.token = mustJSON[map[string]any](t, b)["token"].(string)
	_, b, _ = nc.do("GET", "/api/agent/state", nil, nil)
	if strings.Contains(string(b), `"probe"`) {
		t.Fatalf("state has probe while off: %s", b)
	}

	// Turn it on: path /status, dedicated host, carrier pings, a tcp task, thresholds.
	if code, b, _ := ac.do("PUT", "/api/admin/settings/probe", map[string]any{"enabled": true, "path": "/admin", "visibility": "public"}, nil); code != 400 {
		t.Fatalf("reserved path accepted: %d %s", code, b)
	}
	code, b, _ := ac.do("PUT", "/api/admin/settings/probe", map[string]any{"enabled": true, "beat_seconds": 5, "carrier_ping": true, "path": "status", "hosts": []string{"Status.Example.com"}, "visibility": "public", "title": "Our Status",
		"alerts": map[string]any{"offline_seconds": 60, "cpu_pct": 80, "window_minutes": 1, "traffic": true}}, nil)
	if code != 200 || !strings.Contains(string(b), `"path":"/status"`) || !strings.Contains(string(b), `"hosts":["status.example.com"]`) {
		t.Fatalf("probe settings: %d %s", code, b)
	}
	if code, b, _ := ac.do("POST", "/api/admin/ping-tasks", map[string]any{"name": "cf", "type": "tcp", "target": "1.1.1.1:443", "interval_seconds": 30, "enabled": true}, nil); code != 200 {
		t.Fatalf("ping task: %d %s", code, b)
	}
	ac.do("PUT", "/api/admin/nodes/"+nodeID+"/probe", map[string]any{"Info": map[string]string{"region": "jp", "provider": "Vultr"}, "LimitBytes": 1000, "ResetDay": 1, "Mode": "sum"}, nil)
	_, b, _ = nc.do("GET", "/api/agent/state", nil, nil)
	if !strings.Contains(string(b), `"probe":{"enabled":true,"beat_seconds":5,"carrier_ping":true,"tasks":[{"id":1,"name":"cf","type":"tcp","target":"1.1.1.1:443","interval_seconds":30}]}`) {
		t.Fatalf("state probe config: %s", b)
	}

	// Beats from the node.
	beat := func(up, down uint64, cpu float64) {
		if code, b, _ := nc.do("POST", "/api/agent/beat", map[string]any{"version": "v0.11.0", "host": map[string]any{"cpu_percent": cpu, "mem_total": 1000, "mem_used": 400, "disk_total": 100, "disk_used": 10, "net_total_up": up, "net_total_down": down, "net_up": 50, "net_down": 90, "uptime": 3600,
			"pings": []map[string]any{{"task_id": 0, "name": "CT", "latency_ms": 35, "loss": 0}, {"task_id": 1, "name": "cf", "latency_ms": 12}}}}, nil); code != 204 {
			t.Fatalf("beat: %d %s", code, b)
		}
	}
	beat(1000, 2000, 85)
	beat(1300, 2600, 90)
	beat(1600, 3200, 95)

	anon := &client{t: t, srv: srv}
	code, b, _ = anon.do("GET", "/api/probe", nil, nil)
	if code != 200 {
		t.Fatalf("public snapshot: %d %s", code, b)
	}
	var snap struct {
		Title string `json:"title"`
		Nodes []struct {
			Name    string         `json:"name"`
			Online  bool           `json:"online"`
			Addr    string         `json:"addr"`
			Info    map[string]any `json:"info"`
			Host    map[string]any `json:"host"`
			Traffic struct {
				Used  int64 `json:"used"`
				Limit int64 `json:"limit"`
			} `json:"traffic"`
			Recent []map[string]any `json:"recent"`
		} `json:"nodes"`
		Staff bool `json:"staff"`
	}
	_ = json.Unmarshal(b, &snap)
	if snap.Title != "Our Status" || len(snap.Nodes) != 1 || !snap.Nodes[0].Online || snap.Nodes[0].Addr != "" || snap.Nodes[0].Info["region"] != "JP" || snap.Nodes[0].Traffic.Used != 1800 || snap.Nodes[0].Traffic.Limit != 1000 || len(snap.Nodes[0].Recent) != 3 || snap.Staff {
		t.Fatalf("snapshot: %s", b)
	}
	if snap.Nodes[0].Host["cpu_percent"] != 95.0 {
		t.Fatalf("live host: %v", snap.Nodes[0].Host)
	}
	// Staff see the address.
	_, b, _ = ac.do("GET", "/api/probe", nil, nil)
	if !strings.Contains(string(b), `"addr":"jp1.test"`) || !strings.Contains(string(b), `"staff":true`) {
		t.Fatalf("staff snapshot: %s", b)
	}
	// History and pings.
	_, b, _ = anon.do("GET", "/api/probe/nodes/"+nodeID+"/history?range=24h", nil, nil)
	if !strings.Contains(string(b), `"res":"m"`) || !strings.Contains(string(b), `"n":3`) {
		t.Fatalf("history: %s", b)
	}
	_, b, _ = anon.do("GET", "/api/probe/nodes/"+nodeID+"/history?range=1h", nil, nil)
	if !strings.Contains(string(b), `"res":"raw"`) || strings.Count(string(b), `"cpu"`) != 3 {
		t.Fatalf("raw history: %s", b)
	}
	_, b, _ = anon.do("GET", "/api/probe/nodes/"+nodeID+"/pings?range=24h", nil, nil)
	if !strings.Contains(string(b), `"name":"CT"`) || !strings.Contains(string(b), `"name":"cf"`) {
		t.Fatalf("pings: %s", b)
	}
	// Page routing: /status redirects to /status/, which serves the SPA; dedicated host serves it at /.
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, _ := noRedirect.Get(srv.URL + "/status")
	if resp.StatusCode != 301 || resp.Header.Get("Location") != "/status/" {
		t.Fatalf("path redirect: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	resp.Body.Close()
	resp, _ = http.Get(srv.URL + "/status/")
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(strings.ToLower(string(body)), "<html") {
		t.Fatalf("status page: %d %s", resp.StatusCode, body)
	}
	req, _ := http.NewRequest("GET", srv.URL+"/admin/", nil)
	req.Host = "status.example.com"
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("admin on probe host: %d", resp.StatusCode)
	}
	req, _ = http.NewRequest("GET", srv.URL+"/", nil)
	req.Host = "status.example.com"
	resp, _ = http.DefaultClient.Do(req)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(strings.ToLower(string(body)), "<html") {
		t.Fatalf("probe host root: %d", resp.StatusCode)
	}
	// Hidden node disappears for visitors, stays for staff; visibility=users gates anonymous access.
	ac.do("PUT", "/api/admin/nodes/"+nodeID+"/probe", map[string]any{"Hidden": true, "LimitBytes": 1000, "ResetDay": 1, "Mode": "sum"}, nil)
	_, b, _ = anon.do("GET", "/api/probe", nil, nil)
	if strings.Contains(string(b), `"jp1"`) {
		t.Fatalf("hidden node visible: %s", b)
	}
	if code, _, _ := anon.do("GET", "/api/probe/nodes/"+nodeID+"/history?range=24h", nil, nil); code != 404 {
		t.Fatal("hidden node history should 404")
	}
	_, b, _ = ac.do("GET", "/api/probe", nil, nil)
	if !strings.Contains(string(b), `"jp1"`) {
		t.Fatalf("staff must still see hidden node: %s", b)
	}
	ac.do("PUT", "/api/admin/settings/probe", map[string]any{"enabled": true, "path": "/status", "visibility": "users"}, nil)
	if code, _, _ := anon.do("GET", "/api/probe", nil, nil); code != 401 {
		t.Fatalf("users-only should 401 anonymous: %d", code)
	}
	uc := &client{t: t, srv: srv}
	uc.do("POST", "/api/portal/register", map[string]string{"Email": "u@test", "Password": "password123"}, nil)
	if code, _, _ := uc.do("GET", "/api/probe", nil, nil); code != 200 {
		t.Fatalf("signed-in user should see it: %d", code)
	}
	// Landing page advertises the URL only when public.
	_, b, _ = anon.do("GET", "/api/site", nil, nil)
	if !strings.Contains(string(b), `"probe_url":"/status/"`) {
		t.Fatalf("site probe_url: %s", b)
	}
	// Threshold alert fired once (cpu 80% over 1 minute) into the admin channel via the store record.
	if fire, _ := st.AlertOnce(context.Background(), int64(node["id"].(float64)), "cpu", time.Hour, time.Now()); fire {
		t.Fatal("cpu alert should already have fired during the beats")
	}
}

func TestExternalNodesAndRouting(t *testing.T) {
	cfg := config.Default()
	cfg.BaseURL = "https://panel.test"
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

	// A fake airport serving a base64 URI list; it records the User-Agent.
	var gotUA string
	airport := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.UserAgent()
		body := "trojan://pw@air1.test:443?sni=air1.test#Air%20HK\nss://" + base64.StdEncoding.EncodeToString([]byte("aes-256-gcm:k")) + "@air2.test:8388#Air%20JP\n"
		_, _ = w.Write([]byte(base64.StdEncoding.EncodeToString([]byte(body))))
	}))
	defer airport.Close()
	code, b, _ := ac.do("POST", "/api/admin/external/sources", map[string]any{"name": "air", "url": airport.URL + "/sub", "user_agent": "v2rayN/7.0", "rate": 2}, nil)
	if code != 200 || !strings.Contains(string(b), `"synced":2`) || gotUA != "v2rayN/7.0" {
		t.Fatalf("add source: %d %s ua=%s", code, b, gotUA)
	}
	// Manual import with one bad line; group-restricted.
	_, b, _ = ac.do("POST", "/api/admin/groups", map[string]string{"Name": "vip"}, nil)
	gid := int64(mustJSON[map[string]any](t, b)["id"].(float64))
	code, b, _ = ac.do("POST", "/api/admin/external/nodes", map[string]any{"Text": "vless://11111111-1111-1111-1111-111111111111@vip.test:443?security=reality&pbk=PUB&sid=ab&sni=www.apple.com&type=tcp&flow=xtls-rprx-vision#VIP%20Exit\nnope://x", "GroupID": gid}, nil)
	if code != 200 || !strings.Contains(string(b), `"added":1`) || !strings.Contains(string(b), `"skipped":1`) {
		t.Fatalf("import: %d %s", code, b)
	}
	_, b, _ = ac.do("GET", "/api/admin/external/nodes", nil, nil)
	if strings.Count(string(b), `"uri"`) != 3 {
		t.Fatalf("external list: %s", b)
	}

	// A plan + user: the subscription lists the airport nodes but not the vip one.
	_, b, _ = ac.do("POST", "/api/admin/plans", map[string]any{"Name": "p", "PriceCents": 0, "PeriodDays": 30, "QuotaBytes": 1 << 30}, nil)
	planID := int64(mustJSON[map[string]any](t, b)["ID"].(float64))
	uc := &client{t: t, srv: srv}
	uc.do("POST", "/api/portal/register", map[string]string{"Email": "u@test", "Password": "password123"}, nil)
	u, _ := st.UserByEmail(context.Background(), "u@test")
	ac.do("POST", "/api/admin/users/"+itoa(u.ID)+"/grant", map[string]any{"PlanID": planID}, nil)
	anon := &client{t: t, srv: srv}
	_, b, _ = anon.do("GET", "/sub/"+u.SubToken+"?client=clash", nil, nil)
	if !strings.Contains(string(b), "name: Air HK") || !strings.Contains(string(b), "server: air2.test") || strings.Contains(string(b), "VIP Exit") {
		t.Fatalf("subscription with external nodes:\n%s", b)
	}
	ac.do("PATCH", "/api/admin/users/"+itoa(u.ID), map[string]any{"Status": "active", "GroupID": gid}, nil)
	_, b, _ = anon.do("GET", "/sub/"+u.SubToken+"?client=clash", nil, nil)
	if !strings.Contains(string(b), "VIP Exit") || !strings.Contains(string(b), "public-key: PUB") {
		t.Fatalf("vip external node for group member:\n%s", b)
	}

	// Node routing: outbound from a share link, default exit, per-inbound rule; validation.
	_, b, _ = ac.do("POST", "/api/admin/nodes", map[string]string{"Name": "relay", "PublicAddr": "relay.test"}, nil)
	node := mustJSON[map[string]any](t, b)
	nodeID := itoa(int64(node["id"].(float64)))
	_, b, _ = ac.do("POST", "/api/admin/external/parse", map[string]string{"Text": "ss://" + base64.StdEncoding.EncodeToString([]byte("aes-128-gcm:pw")) + "@exit.test:8388#exit"}, nil)
	var parsed struct {
		Nodes []struct {
			Remote map[string]any `json:"remote"`
		} `json:"nodes"`
	}
	_ = json.Unmarshal(b, &parsed)
	if len(parsed.Nodes) != 1 || parsed.Nodes[0].Remote["host"] != "exit.test" {
		t.Fatalf("parse: %s", b)
	}
	if code, b, _ := ac.do("PUT", "/api/admin/nodes/"+nodeID+"/routing", map[string]any{"outbounds": []map[string]any{{"tag": "exit", "remote": parsed.Nodes[0].Remote}}, "routes": []map[string]any{{"match": []string{"inbound:in-a"}, "action": "outbound", "value": "nope"}}}, nil); code != 400 {
		t.Fatalf("unknown outbound accepted: %d %s", code, b)
	}
	code, b, _ = ac.do("PUT", "/api/admin/nodes/"+nodeID+"/routing", map[string]any{"outbounds": []map[string]any{{"tag": "exit", "remote": parsed.Nodes[0].Remote}, {"tag": "hop", "proxy_tag": "exit", "remote": parsed.Nodes[0].Remote}},
		"routes": []map[string]any{{"match": []string{"inbound:in-a"}, "action": "outbound", "value": "hop"}, {"match": []string{"domain:cn"}, "action": "direct"}}, "default_outbound": "exit"}, nil)
	if code != 200 {
		t.Fatalf("routing: %d %s", code, b)
	}
	nc := &client{t: t, srv: srv}
	_, b, _ = nc.do("POST", "/api/agent/pair", map[string]string{"Code": node["pair_code"].(string), "Hostname": "relay", "Version": "v0.12.0", "Platform": "linux/amd64"}, nil)
	nc.token = mustJSON[map[string]any](t, b)["token"].(string)
	_, b, _ = nc.do("GET", "/api/agent/state", nil, nil)
	for _, want := range []string{`"default_outbound":"exit"`, `"tag":"hop","proxy_tag":"exit"`, `"host":"exit.test"`, `"match":["inbound:in-a"],"action":"outbound","value":"hop"`} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("state missing %s:\n%s", want, b)
		}
	}
}
