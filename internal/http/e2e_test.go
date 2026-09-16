package http

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"github.com/zeptop-dev/captain/internal/auth"
	"github.com/zeptop-dev/captain/internal/backup"
	"github.com/zeptop-dev/captain/internal/captcha"
	"github.com/zeptop-dev/captain/internal/certs"
	"github.com/zeptop-dev/captain/internal/webhook"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"

	"github.com/zeptop-dev/captain/internal/config"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/http/admin"
	"github.com/zeptop-dev/captain/internal/jobs"
	"github.com/zeptop-dev/captain/internal/mail"
	"github.com/zeptop-dev/captain/internal/service"
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
	// Tags, region flags and drag order. The vip user sees both entries: order
	// follows sort, auto flags prefix 🇯🇵 from the name, an explicit region wins.
	_, b, _ = c.do("GET", "/api/admin/entries", nil, nil)
	ents := mustJSON[[]map[string]any](t, b)
	if len(ents) != 2 || ents[0]["Name"] != "JP mieru" {
		t.Fatalf("entries: %v", ents)
	}
	c.do("PUT", "/api/admin/entries/order", map[string]any{"IDs": []any{ents[1]["ID"], ents[0]["ID"]}}, nil)
	c.do("PATCH", "/api/admin/entries/"+itoa(int64(ents[1]["ID"].(float64))), map[string]any{"Name": "JP vip", "InboundID": 2, "DisplayHost": "entry.test", "DisplayPort": 443, "Tags": []string{"IPLC", "x2"}, "Region": "hk", "Enabled": true, "Rate": 1}, nil)
	_, b, _ = c.do("GET", "/api/admin/entries", nil, nil)
	ents = mustJSON[[]map[string]any](t, b)
	if ents[0]["Name"] != "JP vip" || ents[0]["Region"] != "HK" || fmt.Sprint(ents[0]["Tags"]) != "[IPLC x2]" {
		t.Fatalf("after reorder/patch: %v", ents)
	}
	_, b, _ = c.do("GET", "/api/admin/entries/tags", nil, nil)
	if strings.TrimSpace(string(b)) != `["IPLC","x2"]` {
		t.Fatalf("tags: %s", b)
	}
	c.do("PUT", "/api/admin/settings/subscription", map[string]any{"URLs": []string{}, "auto_flags": true}, nil)
	_, b, _ = anon.do("GET", "/sub/"+u2["sub_token"].(string)+"?client=clash", nil, nil)
	if i, j := strings.Index(string(b), "🇭🇰 JP vip"), strings.Index(string(b), "🇯🇵 JP mieru"); i < 0 || j < 0 || i > j {
		t.Fatalf("flags/order in clash doc:\n%s", b)
	}
	c.do("PUT", "/api/admin/settings/subscription", map[string]any{"URLs": []string{}}, nil)
	_, b, _ = anon.do("GET", "/sub/"+u2["sub_token"].(string)+"?client=clash", nil, nil)
	if !strings.Contains(string(b), "🇭🇰 JP vip") || strings.Contains(string(b), "🇯🇵") {
		t.Fatalf("explicit region must still flag, auto off:\n%s", b)
	}
	c.do("PUT", "/api/admin/entries/order", map[string]any{"IDs": []any{ents[1]["ID"], ents[0]["ID"]}}, nil)

	// Report traffic: charged to the subscription; quota exhaustion drops the user.
	uid := int64(u1["id"].(float64))
	_, b, _ = agent.do("POST", "/api/agent/report", agentproto.Report{
		Version: "0.1.0", Revision: st2.Revision,
		// One entry names its inbound (bosun v0.36+), one does not (older
		// agents): both are charged, the tagged one lands in that inbound's
		// daily bucket instead of the node's first inbound.
		Traffic: []spec.UserTraffic{{UserID: uid, Up: 100, Down: 150}, {UserID: uid, Up: 0, Down: 50, Inbound: "vip"}},
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
	var vipDown, firstDown int64
	_ = st.DB().QueryRow(`SELECT COALESCE(SUM(down_bytes),0) FROM traffic_daily WHERE user_id = ? AND inbound_id = 2`, uid).Scan(&vipDown)
	_ = st.DB().QueryRow(`SELECT COALESCE(SUM(down_bytes),0) FROM traffic_daily WHERE user_id = ? AND inbound_id = 1`, uid).Scan(&firstDown)
	if vipDown != 50 || firstDown != 150 {
		t.Fatalf("daily buckets: inbound 2 = %d (want 50), inbound 1 = %d (want 150)", vipDown, firstDown)
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
	// The hold window and state cache are what keep real nodes from
	// flapping; here the test ages devices out by hand, so drop both.
	service.DeviceHold, service.CacheTTL = 0, 0
	defer func() { service.DeviceHold, service.CacheTTL = 5*time.Minute, 10*time.Second }()
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

	// The ACME account only travels to nodes that obtain certificates
	// themselves: give this one an auto_cert inbound first.
	c.do("POST", "/api/admin/nodes/"+itoa(int64(node["id"].(float64)))+"/inbounds", map[string]any{"Tag": "tls", "Protocol": "vless", "Port": 443, "Settings": map[string]any{"tls": map[string]any{"mode": 1, "server_name": "jp1.example.com", "auto_cert": true}}}, nil)
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

	// Pushed certificates: only nodes with a covered TLS server name get
	// them; the webhook needs the token; deletion withdraws them.
	nid := itoa(int64(node["id"].(float64)))
	c.do("POST", "/api/admin/nodes/"+nid+"/inbounds", map[string]any{"Tag": "t", "Protocol": "trojan", "Port": 443, "Settings": map[string]any{"tls": map[string]any{"mode": 1, "server_name": "jp1.example.com", "auto_cert": true}}}, nil)
	certPEM, keyPEM := selfSignedPEM(t, "*.example.com", "example.com")
	if code, b, _ := c.do("POST", "/api/admin/certificates", map[string]string{"CertPEM": certPEM, "KeyPEM": "nope"}, nil); code != 400 {
		t.Fatalf("bad pair: %d %s", code, b)
	}
	code, b, _ := c.do("POST", "/api/admin/certificates", map[string]string{"CertPEM": certPEM, "KeyPEM": keyPEM}, nil)
	if code != 200 || !strings.Contains(string(b), `"domain":"*.example.com"`) || !strings.Contains(string(b), `"names":["*.example.com","example.com"]`) || strings.Contains(string(b), "PRIVATE") {
		t.Fatalf("upload: %d %s", code, b)
	}
	_, b, _ = agent.do("GET", "/api/agent/state", nil, nil)
	withCert := mustJSON[agentproto.State](t, b)
	if len(withCert.Node.Certificates) != 1 || withCert.Node.Certificates[0].Domain != "*.example.com" || withCert.Node.Certificates[0].KeyPEM != keyPEM+"\n" && withCert.Node.Certificates[0].KeyPEM != keyPEM {
		t.Fatalf("certificate should reach the node: %+v", withCert.Node.Certificates)
	}
	// A node without a covered name gets nothing.
	_, b, _ = c.do("POST", "/api/admin/nodes", map[string]string{"Name": "other"}, nil)
	other := mustJSON[map[string]any](t, b)
	oc := &client{t: t, srv: srv}
	_, b, _ = oc.do("POST", "/api/agent/pair", agentproto.PairRequest{Code: other["pair_code"].(string)}, nil)
	oc.token = mustJSON[agentproto.PairResponse](t, b).Token
	c.do("POST", "/api/admin/nodes/"+itoa(int64(other["id"].(float64)))+"/inbounds", map[string]any{"Tag": "t", "Protocol": "trojan", "Port": 443, "Settings": map[string]any{"tls": map[string]any{"mode": 1, "server_name": "a.b.example.com"}}}, nil)
	_, b, _ = oc.do("GET", "/api/agent/state", nil, nil)
	if st := mustJSON[agentproto.State](t, b); len(st.Node.Certificates) != 0 {
		t.Fatalf("wildcard must not cover two labels: %+v", st.Node.Certificates)
	}
	// Webhook: no token configured -> 403; rotate; wrong token 403; Certimate-style body upserts.
	anon := &client{t: t, srv: srv}
	if code, _, _ := anon.do("POST", "/api/hooks/certificate?token=x", map[string]string{}, nil); code != 403 {
		t.Fatalf("hook without configured token: %d", code)
	}
	_, b, _ = c.do("POST", "/api/admin/certificates/webhook-token", nil, nil)
	hookURL := mustJSON[map[string]string](t, b)["webhook_url"]
	if !strings.HasPrefix(hookURL, "http://test/api/hooks/certificate?token=") {
		t.Fatalf("hook url: %s", hookURL)
	}
	path := strings.TrimPrefix(hookURL, "http://test")
	if code, _, _ := anon.do("POST", path+"bad", map[string]string{"domain": "x"}, nil); code != 403 {
		t.Fatal("wrong token accepted")
	}
	cert2, key2 := selfSignedPEM(t, "jp1.example.com")
	code, b, _ = anon.do("POST", path, map[string]any{"domains": "jp1.example.com,www.example.com", "certificate": cert2, "privateKey": key2}, nil)
	if code != 200 || !strings.Contains(string(b), `"domain":"jp1.example.com"`) {
		t.Fatalf("hook: %d %s", code, b)
	}
	_, b, _ = c.do("GET", "/api/admin/certificates", nil, nil)
	if !strings.Contains(string(b), `"source":"webhook"`) || strings.Contains(string(b), "BEGIN") || !strings.Contains(string(b), hookURL) {
		t.Fatalf("list: %s", b)
	}
	_, b, _ = agent.do("GET", "/api/agent/state", nil, nil)
	if st := mustJSON[agentproto.State](t, b); len(st.Node.Certificates) != 2 {
		t.Fatalf("both the wildcard and the exact certificate cover jp1: %d", len(st.Node.Certificates))
	}
	list := mustJSON[map[string]any](t, func() []byte { _, b, _ := c.do("GET", "/api/admin/certificates", nil, nil); return b }())["certificates"].([]any)
	for _, it := range list {
		c.do("DELETE", "/api/admin/certificates/"+itoa(int64(it.(map[string]any)["id"].(float64))), nil, nil)
	}
	_, b, _ = agent.do("GET", "/api/agent/state", nil, nil)
	if st := mustJSON[agentproto.State](t, b); len(st.Node.Certificates) != 0 {
		t.Fatal("deleted certificates still pushed")
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
	// "Activate after the current plan" for a plan one already holds is a
	// renewal in disguise: refused, the portal offers "renew" for it.
	if code, body, _ := u.do("POST", "/api/portal/orders", map[string]any{"plan_id": planID, "gateway": "balance", "activation": "queue"}, nil); code != http.StatusBadRequest || !strings.Contains(string(body), "renew") {
		t.Fatalf("queueing a held plan: %d %s", code, body)
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
	// Only the single-plan mode replaces (and credits); stacking would keep basic.
	_ = st.SetSetting(context.Background(), service.SettingSubscription, service.SubscriptionSettings{SinglePlan: true})
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
	// Custom carrier targets replace the defaults on the node; bad ones are rejected.
	if code, _, _ := ac.do("PUT", "/api/admin/settings/probe", map[string]any{"enabled": true, "carrier_ping": true, "path": "status", "carriers": []map[string]string{{"name": "HK", "addr": "hkix"}}}, nil); code != 400 {
		t.Fatal("carrier without port accepted")
	}
	ac.do("PUT", "/api/admin/settings/probe", map[string]any{"enabled": true, "beat_seconds": 5, "carrier_ping": true, "path": "status", "hosts": []string{"status.example.com"}, "visibility": "public", "title": "Our Status",
		"carriers": []map[string]string{{"name": "HK", "addr": "www.hkix.net:443"}, {"name": "", "addr": ""}}, "alerts": map[string]any{"offline_seconds": 60, "cpu_pct": 80, "window_minutes": 1, "traffic": true}}, nil)
	_, b, _ = nc.do("GET", "/api/agent/state", nil, nil)
	if !strings.Contains(string(b), `"carriers":[{"name":"HK","addr":"www.hkix.net:443"}]`) {
		t.Fatalf("carriers missing from state: %s", b)
	}
	// Komari: a panel-wide setting reaches every node with the node's name.
	if code, _, _ := ac.do("PUT", "/api/admin/settings/komari", map[string]any{"enabled": true, "server": "https://komari.example.com/"}, nil); code != 400 {
		t.Fatal("komari without key accepted")
	}
	if code, b, _ := ac.do("PUT", "/api/admin/settings/komari", map[string]any{"enabled": true, "server": "https://komari.example.com/", "key": "adkey-1234567890", "interval": 5}, nil); code != 200 || !strings.Contains(string(b), `"has_key":true`) || strings.Contains(string(b), "adkey") {
		t.Fatalf("put komari: %d %s", code, b)
	}
	_, b, _ = nc.do("GET", "/api/agent/state", nil, nil)
	if !strings.Contains(string(b), `"komari":{"enabled":true,"server":"https://komari.example.com","key":"adkey-1234567890","name":"jp1","interval":5}`) {
		t.Fatalf("komari missing from state: %s", b)
	}
	// A line ingress adds a source-bound RTT task to the far end: the
	// reserved (SSH) port until an inbound uses the line, then that port.
	_, b, _ = ac.do("POST", "/api/admin/nodes/"+nodeID+"/ingresses", map[string]any{"Name": "IPLC", "BindIP": "10.10.0.2", "LineIP": "198.51.100.20", "PortFrom": 17700, "PortTo": 17799, "ReservedPorts": []int{17700}}, nil)
	gid := int64(mustJSON[map[string]any](t, b)["ingress"].(map[string]any)["id"].(float64))
	_, b, _ = nc.do("GET", "/api/agent/state", nil, nil)
	if !strings.Contains(string(b), `"target":"198.51.100.20:17700"`) {
		t.Fatalf("line task should use the reserved port before any inbound: %s", b)
	}
	ac.do("POST", "/api/admin/nodes/"+nodeID+"/inbounds", map[string]any{"Tag": "m", "Protocol": "mieru", "Port": 17710, "IngressID": gid, "Settings": map[string]any{"mieru_transport": "TCP"}}, nil)
	_, b, _ = nc.do("GET", "/api/agent/state", nil, nil)
	if !strings.Contains(string(b), `{"id":-`+itoa(gid)+`,"name":"IPLC","type":"tcp","target":"198.51.100.20:17710","interval_seconds":30,"source_ip":"10.10.0.2"}`) {
		t.Fatalf("line task missing from state: %s", b)
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

	// Port forwards: validated (inbound port collision, bad target), stripped
	// of panel-only fields in the state, statuses reported back by the node.
	ac.do("POST", "/api/admin/nodes/"+nodeID+"/inbounds", map[string]any{"Tag": "land", "Protocol": "mieru", "Port": 443, "Settings": map[string]any{"mieru_transport": "TCP"}}, nil)
	if code, b, _ := ac.do("PUT", "/api/admin/nodes/"+nodeID+"/forwards", map[string]any{"Forwards": []map[string]any{{"port": 443, "target": "1.2.3.4:443"}}}, nil); code != 400 || !strings.Contains(string(b), "already used") {
		t.Fatalf("forward on inbound port: %d %s", code, b)
	}
	if code, _, _ := ac.do("PUT", "/api/admin/nodes/"+nodeID+"/forwards", map[string]any{"Forwards": []map[string]any{{"port": 10443, "target": "nohost"}}}, nil); code != 400 {
		t.Fatal("bad target accepted")
	}
	code, b, _ = ac.do("PUT", "/api/admin/nodes/"+nodeID+"/forwards", map[string]any{"Forwards": []map[string]any{{"port": 10443, "protocol": "udp", "target": "land.test:443", "inbound_id": 7}, {"tag": "t2", "port": 10444, "target": "[2001:db8::1]:8443"}}}, nil)
	if code != 200 {
		t.Fatalf("forwards: %d %s", code, b)
	}
	_, b, _ = nc.do("GET", "/api/agent/state", nil, nil)
	if !strings.Contains(string(b), `"forwards":[{"tag":"fwd-10443","port":10443,"protocol":"udp","target":"land.test:443"},{"tag":"t2","port":10444,"protocol":"both","target":"[2001:db8::1]:8443"}]`) {
		t.Fatalf("state forwards:\n%s", b)
	}
	nc.do("POST", "/api/agent/report", agentproto.Report{Forwards: []agentproto.ForwardStatus{{Tag: "t2", Up: true, RTTMillis: 12, ActiveConn: 1, TotalConn: 5, BytesIn: 100, BytesOut: 200}}}, nil)
	_, b, _ = ac.do("GET", "/api/admin/nodes/"+nodeID+"/forwards", nil, nil)
	if !strings.Contains(string(b), `"inbound_id":7`) || !strings.Contains(string(b), `"t2":{"tag":"t2","up":true,"rtt_ms":12`) {
		t.Fatalf("forwards with status: %s", b)
	}
	// nft backend round-trips; preserve_source needs nft; unknown backends are refused.
	if code, _, _ := ac.do("PUT", "/api/admin/nodes/"+nodeID+"/forwards", map[string]any{"Forwards": []map[string]any{{"port": 10445, "target": "198.51.100.20:443", "backend": "gost"}}}, nil); code != 400 {
		t.Fatal("unknown backend accepted")
	}
	if code, _, _ := ac.do("PUT", "/api/admin/nodes/"+nodeID+"/forwards", map[string]any{"Forwards": []map[string]any{{"port": 10445, "target": "198.51.100.20:443", "preserve_source": true}}}, nil); code != 400 {
		t.Fatal("preserve_source without nft accepted")
	}
	ac.do("PUT", "/api/admin/nodes/"+nodeID+"/forwards", map[string]any{"Forwards": []map[string]any{{"port": 10445, "protocol": "tcp", "target": "198.51.100.20:443", "backend": "nft", "preserve_source": true}}}, nil)
	_, b, _ = nc.do("GET", "/api/agent/state", nil, nil)
	if !strings.Contains(string(b), `"target":"198.51.100.20:443","backend":"nft","preserve_source":true`) {
		t.Fatalf("nft forward in state:\n%s", b)
	}
	// A doctor report rides on the node report and shows on the node.
	nc.do("POST", "/api/agent/report", agentproto.Report{Doctor: &agentproto.DoctorReport{Checks: []agentproto.DoctorCheck{{ID: "inbound:t", Name: "inbound t", Status: "fail", Detail: "refused"}}, Summary: agentproto.DoctorSummary{Fail: 1}}}, nil)
	_, b, _ = ac.do("GET", "/api/admin/nodes/"+nodeID, nil, nil)
	if !strings.Contains(string(b), `"doctor":{`) || !strings.Contains(string(b), `"detail":"refused"`) {
		t.Fatalf("doctor in node detail: %s", b)
	}
	_, b, _ = ac.do("GET", "/api/admin/nodes", nil, nil)
	if !strings.Contains(string(b), `"doctor_fail":true`) {
		t.Fatalf("doctor_fail in node list: %s", b)
	}
	// Dropping a rule drops its status.
	ac.do("PUT", "/api/admin/nodes/"+nodeID+"/forwards", map[string]any{"Forwards": []map[string]any{}}, nil)
	_, b, _ = ac.do("GET", "/api/admin/nodes/"+nodeID+"/forwards", nil, nil)
	if strings.Contains(string(b), `"t2"`) {
		t.Fatalf("stale status kept: %s", b)
	}
}

func TestSubscriptionAdjustments(t *testing.T) {
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
	_, b, _ := ac.do("POST", "/api/admin/plans", map[string]any{"Name": "p", "PriceCents": 100, "PeriodDays": 30, "QuotaBytes": 10 << 30, "ResetMode": "monthly"}, nil)
	planID := int64(mustJSON[map[string]any](t, b)["ID"].(float64))
	uc := &client{t: t, srv: srv}
	uc.do("POST", "/api/portal/register", map[string]string{"Email": "u@test", "Password": "password123"}, nil)
	u, _ := st.UserByEmail(context.Background(), "u@test")
	uid := itoa(u.ID)
	if code, _, _ := ac.do("POST", "/api/admin/users/"+uid+"/subscription", map[string]any{"AddDays": 30}, nil); code != 409 {
		t.Fatalf("no plan should 409: %d", code)
	}
	ac.do("POST", "/api/admin/users/"+uid+"/grant", map[string]any{"PlanID": planID}, nil)
	sub0, _ := st.ActiveSubscription(context.Background(), u.ID)

	// +30 days, quota override 20 GB, reset day 15, then usage reset.
	code, b, _ := ac.do("POST", "/api/admin/users/"+uid+"/subscription", map[string]any{"AddDays": 30, "QuotaOverride": 20 << 30, "ResetDay": 15}, nil)
	if code != 200 {
		t.Fatalf("adjust: %d %s", code, b)
	}
	sub, _ := st.ActiveSubscription(context.Background(), u.ID)
	if !sub.ExpiresAt.Equal(sub0.ExpiresAt.AddDate(0, 0, 30)) || sub.QuotaBytes != 20<<30 || sub.ResetAt == nil || sub.ResetAt.Day() != 15 || !sub.ResetAt.After(time.Now()) {
		t.Fatalf("adjusted: exp %v quota %d reset %v", sub.ExpiresAt, sub.QuotaBytes, sub.ResetAt)
	}
	_ = st.AddTraffic(context.Background(), u.ID, 0, 1<<20, 2<<20, time.Now())
	ac.do("POST", "/api/admin/users/"+uid+"/subscription", map[string]any{"ResetUsage": true}, nil)
	sub, _ = st.ActiveSubscription(context.Background(), u.ID)
	if sub.UsedUpBytes+sub.UsedDownBytes != 0 {
		t.Fatal("usage not reset")
	}
	// Renewing the same plan keeps the override and the reset day.
	_ = st.AdjustBalance(context.Background(), u.ID, 100)
	if code, b, _ := uc.do("POST", "/api/portal/orders", map[string]any{"plan_id": planID, "gateway": "balance"}, nil); code != 200 {
		t.Fatalf("renew: %d %s", code, b)
	}
	sub, _ = st.ActiveSubscription(context.Background(), u.ID)
	if sub.QuotaBytes != 20<<30 || sub.ResetAt == nil || sub.ResetAt.Day() != 15 {
		t.Fatalf("override lost on renewal: quota %d reset %v", sub.QuotaBytes, sub.ResetAt)
	}
	// Unlimited override and back to plan default.
	ac.do("POST", "/api/admin/users/"+uid+"/subscription", map[string]any{"QuotaOverride": -1}, nil)
	if sub, _ = st.ActiveSubscription(context.Background(), u.ID); sub.QuotaBytes != 0 {
		t.Fatalf("unlimited override: %d", sub.QuotaBytes)
	}
	ac.do("POST", "/api/admin/users/"+uid+"/subscription", map[string]any{"QuotaOverride": 0}, nil)
	if sub, _ = st.ActiveSubscription(context.Background(), u.ID); sub.QuotaBytes != 10<<30 {
		t.Fatalf("plan default override: %d", sub.QuotaBytes)
	}
	if code, _, _ := ac.do("POST", "/api/admin/users/"+uid+"/subscription", map[string]any{"ResetDay": 31}, nil); code != 400 {
		t.Fatal("reset day 31 must be rejected")
	}
	// Renewal view lists the user with plan and expiry.
	_, b, _ = ac.do("GET", "/api/admin/renewals", nil, nil)
	if !strings.Contains(string(b), `"email":"u@test"`) || !strings.Contains(string(b), `"plan_name":"p"`) {
		t.Fatalf("renewals: %s", b)
	}
}

func TestSpeedtest(t *testing.T) {
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

	// A local listener stands in for an entry's public address.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	code, b, _ := ac.do("POST", "/api/admin/speedtest/tcping", map[string]any{"Host": "127.0.0.1", "Port": port}, nil)
	var res struct {
		MinMs float64 `json:"min_ms"`
		AvgMs float64 `json:"avg_ms"`
	}
	_ = json.Unmarshal(b, &res)
	if code != 200 || res.MinMs < 0 || res.AvgMs < res.MinMs {
		t.Fatalf("tcping: %d %s", code, b)
	}
	// A refused port still counts as reachable (host up); a blackholed one does not — use a closed port for refusal.
	closed, _ := net.Listen("tcp", "127.0.0.1:0")
	cport := closed.Addr().(*net.TCPAddr).Port
	closed.Close()
	_, b, _ = ac.do("POST", "/api/admin/speedtest/tcping", map[string]any{"Host": "127.0.0.1", "Port": cport}, nil)
	_ = json.Unmarshal(b, &res)
	if res.MinMs < 0 {
		t.Fatalf("refused port should measure: %s", b)
	}
	if code, _, _ := ac.do("POST", "/api/admin/speedtest/tcping", map[string]any{"Host": "", "Port": 1}, nil); code != 400 {
		t.Fatal("empty host accepted")
	}
	// The workbench lists entries with the cached result and nodes' probe pings incl. download Mbps.
	_, b, _ = ac.do("POST", "/api/admin/nodes", map[string]string{"Name": "n1", "PublicAddr": "127.0.0.1"}, nil)
	node := mustJSON[map[string]any](t, b)
	nodeID := int64(node["id"].(float64))
	_, b, _ = ac.do("POST", "/api/admin/nodes/"+itoa(nodeID)+"/inbounds", map[string]any{"Tag": "in", "Protocol": "shadowsocks", "Port": 1, "Settings": map[string]any{"cipher": "aes-128-gcm"}}, nil)
	ibID := int64(mustJSON[map[string]any](t, b)["ID"].(float64))
	ac.do("POST", "/api/admin/entries", map[string]any{"Name": "local", "InboundID": ibID, "DisplayHost": "127.0.0.1", "DisplayPort": port}, nil)
	ac.do("PUT", "/api/admin/settings/probe", map[string]any{"enabled": true, "path": "/status"}, nil)
	code, b, _ = ac.do("POST", "/api/admin/ping-tasks", map[string]any{"name": "dl", "type": "download", "target": "https://speed.test/file", "interval_seconds": 30, "enabled": true}, nil)
	if code != 200 || !strings.Contains(string(b), `"interval_seconds":600`) {
		t.Fatalf("download task floor: %d %s", code, b)
	}
	nc := &client{t: t, srv: srv}
	_, b, _ = nc.do("POST", "/api/agent/pair", map[string]string{"Code": node["pair_code"].(string), "Hostname": "n1", "Version": "v0.12.1", "Platform": "linux/amd64"}, nil)
	nc.token = mustJSON[map[string]any](t, b)["token"].(string)
	nc.do("POST", "/api/agent/beat", map[string]any{"version": "v0.12.1", "host": map[string]any{"cpu_percent": 1, "pings": []map[string]any{{"task_id": 1, "name": "dl", "latency_ms": 55, "mbps": 123.4}}}}, nil)
	_, b, _ = ac.do("GET", "/api/admin/speedtest", nil, nil)
	if !strings.Contains(string(b), `"name":"local"`) || !strings.Contains(string(b), `"min_ms"`) || !strings.Contains(string(b), `"mbps":123.4`) || !strings.Contains(string(b), `"probe_enabled":true`) {
		t.Fatalf("speedtest doc: %s", b)
	}
	_, b, _ = ac.do("GET", "/api/probe/nodes/"+itoa(nodeID)+"/pings?range=24h", nil, nil)
	if !strings.Contains(string(b), `"avg_mbps":123.4`) {
		t.Fatalf("ping stats mbps: %s", b)
	}
}

func TestAPITokensAndMCP(t *testing.T) {
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
	_, b, _ := ac.do("POST", "/api/admin/tokens", map[string]string{"Name": "cli"}, nil)
	token := mustJSON[map[string]any](t, b)["token"].(string)
	if !strings.HasPrefix(token, "cap_") {
		t.Fatalf("token %s", token)
	}
	// Bearer auth on the admin API, with the owner's role.
	tc := &client{t: t, srv: srv}
	if code, _, _ := tc.do("GET", "/api/admin/nodes", nil, map[string]string{"Authorization": "Bearer " + token}); code != 200 {
		t.Fatalf("bearer admin api: %d", code)
	}
	if code, _, _ := tc.do("GET", "/api/admin/nodes", nil, map[string]string{"Authorization": "Bearer cap_nope"}); code != 403 && code != 401 {
		t.Fatal("bad token accepted")
	}
	// MCP: unauthenticated 401; initialize; tools/list; read tool; write tool needs confirm.
	rpc := func(c *client, method string, params any, id int) (int, map[string]any) {
		code, body, _ := c.do("POST", "/mcp", map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}, map[string]string{"Authorization": "Bearer " + token})
		var out map[string]any
		_ = json.Unmarshal(body, &out)
		return code, out
	}
	if code, _, _ := (&client{t: t, srv: srv}).do("POST", "/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize"}, nil); code != 401 {
		t.Fatalf("mcp without token: %d", code)
	}
	code, out := rpc(tc, "initialize", map[string]any{"protocolVersion": "2025-06-18"}, 1)
	if code != 200 || out["result"].(map[string]any)["protocolVersion"] != "2025-06-18" {
		t.Fatalf("initialize: %d %v", code, out)
	}
	_, out = rpc(tc, "tools/list", nil, 2)
	toolsList := out["result"].(map[string]any)["tools"].([]any)
	if len(toolsList) < 15 {
		t.Fatalf("tools: %d", len(toolsList))
	}
	_, out = rpc(tc, "tools/call", map[string]any{"name": "dashboard"}, 3)
	if txt := out["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string); !strings.Contains(txt, `"users"`) {
		t.Fatalf("dashboard tool: %s", txt)
	}
	_, out = rpc(tc, "tools/call", map[string]any{"name": "user_create", "arguments": map[string]any{"email": "m@test", "password": "password123"}}, 4)
	if res := out["result"].(map[string]any); res["isError"] != true || !strings.Contains(res["content"].([]any)[0].(map[string]any)["text"].(string), "confirm") {
		t.Fatalf("write without confirm should error: %v", out)
	}
	_, out = rpc(tc, "tools/call", map[string]any{"name": "user_create", "arguments": map[string]any{"email": "m@test", "password": "password123", "confirm": true}}, 5)
	if res := out["result"].(map[string]any); res["isError"] == true {
		t.Fatalf("user_create: %v", out)
	}
	if _, err := st.UserByEmail(context.Background(), "m@test"); err != nil {
		t.Fatal("user not created via MCP")
	}
	_, out = rpc(tc, "tools/call", map[string]any{"name": "user_detail", "arguments": map[string]any{"email": "m@test"}}, 6)
	if txt := out["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string); !strings.Contains(txt, `"email": "m@test"`) {
		t.Fatalf("user_detail: %s", txt)
	}
	// A support token sees only its tools and cannot call write ones.
	ac.do("POST", "/api/admin/admins", map[string]string{"Email": "sup@test", "Password": "password123", "Role": "support"}, nil)
	sc := &client{t: t, srv: srv}
	sc.do("POST", "/api/admin/login", map[string]string{"Email": "sup@test", "Password": "password123"}, nil)
	_, b, _ = sc.do("POST", "/api/admin/tokens", map[string]string{"Name": "sup"}, nil)
	supTok := mustJSON[map[string]any](t, b)["token"].(string)
	code, body, _ := (&client{t: t, srv: srv}).do("POST", "/mcp", map[string]any{"jsonrpc": "2.0", "id": 7, "method": "tools/list"}, map[string]string{"Authorization": "Bearer " + supTok})
	if code != 200 || strings.Contains(string(body), `"user_create"`) || !strings.Contains(string(body), `"ticket_reply"`) {
		t.Fatalf("support tools: %s", body)
	}
	_, body, _ = (&client{t: t, srv: srv}).do("POST", "/mcp", map[string]any{"jsonrpc": "2.0", "id": 8, "method": "tools/call", "params": map[string]any{"name": "user_balance", "arguments": map[string]any{"user_id": 1, "delta_cents": 100, "confirm": true}}}, map[string]string{"Authorization": "Bearer " + supTok})
	if !strings.Contains(string(body), "not permitted") {
		t.Fatalf("support write tool: %s", body)
	}
	// Notifications get 202; unknown method is a JSON-RPC error.
	if code, _, _ := tc.do("POST", "/mcp", map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}, map[string]string{"Authorization": "Bearer " + token}); code != 202 {
		t.Fatalf("notification: %d", code)
	}
	_, out = rpc(tc, "nope", nil, 9)
	if out["error"] == nil {
		t.Fatal("unknown method should error")
	}
	// Token deletion revokes access.
	_, b, _ = ac.do("GET", "/api/admin/tokens", nil, nil)
	id := int64(mustJSON[[]map[string]any](t, b)[0]["id"].(float64))
	ac.do("DELETE", "/api/admin/tokens/"+itoa(id), nil, nil)
	if code, _, _ := tc.do("GET", "/api/admin/nodes", nil, map[string]string{"Authorization": "Bearer " + token}); code == 200 {
		t.Fatal("deleted token still works")
	}
}

func TestSubLinksAndTOTP(t *testing.T) {
	cfg := config.Default()
	cfg.BaseURL = "https://panel.test"
	conn, _ := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	_ = db.Migrate(context.Background(), conn, "sqlite")
	st := store.New(conn)
	adminUser, _ := admin.NewUser("admin@test", "password123", "admin")
	_ = st.CreateUser(context.Background(), adminUser)
	user, _ := admin.NewUser("u@test", "password123", "user")
	_ = st.CreateUser(context.Background(), user)
	srv := httptest.NewServer(New(cfg, st, slog.Default()).Handler())
	defer srv.Close()
	ac := &client{t: t, srv: srv}
	ac.do("POST", "/api/admin/login", map[string]string{"Email": "admin@test", "Password": "password123"}, nil)
	u, _ := st.UserByEmail(context.Background(), "u@test")

	// Long links by default; short links once the switch is on, and both keep working.
	_, b, _ := ac.do("GET", fmt.Sprintf("/api/admin/users/%d", u.ID), nil, nil)
	if !strings.Contains(string(b), "/sub/"+u.SubToken) {
		t.Fatalf("expected long link: %s", b)
	}
	ac.do("PUT", "/api/admin/settings/subscription", map[string]any{"URLs": []string{}, "short_links": true}, nil)
	_, b, _ = ac.do("GET", fmt.Sprintf("/api/admin/users/%d", u.ID), nil, nil)
	short := regexp.MustCompile(`https://panel\.test/s/[a-z0-9]{8}`).FindString(string(b))
	if short == "" {
		t.Fatalf("expected short link: %s", b)
	}
	anon := &client{t: t, srv: srv}
	if code, _, _ := anon.do("GET", strings.TrimPrefix(short, "https://panel.test"), nil, nil); code != 200 {
		t.Fatalf("short link: %d", code)
	}
	if code, _, _ := anon.do("GET", "/sub/"+u.SubToken, nil, nil); code != 200 {
		t.Fatal("long link must keep working")
	}
	if code, _, _ := anon.do("GET", "/s/nope1234", nil, nil); code != 410 {
		t.Fatalf("unknown code: %d", code)
	}

	// Temporary link: two uses, then gone; revocation removes it immediately.
	code, b, _ := ac.do("POST", fmt.Sprintf("/api/admin/users/%d/links", u.ID), map[string]any{"MaxUses": 2, "Hours": 1}, nil)
	if code != 200 {
		t.Fatalf("create temp: %d %s", code, b)
	}
	tmp := mustJSON[map[string]any](t, b)
	path := "/s/" + tmp["code"].(string)
	for i := 0; i < 2; i++ {
		if code, _, _ := anon.do("GET", path, nil, nil); code != 200 {
			t.Fatalf("temp use %d: %d", i, code)
		}
	}
	if code, _, _ := anon.do("GET", path, nil, nil); code != 410 {
		t.Fatalf("temp link should be used up: %d", code)
	}
	_, b, _ = ac.do("GET", fmt.Sprintf("/api/admin/users/%d/links", u.ID), nil, nil)
	if !strings.Contains(string(b), `"uses":2`) || !strings.Contains(string(b), `"kind":"temp"`) {
		t.Fatalf("links list: %s", b)
	}
	if code, _, _ := ac.do("POST", fmt.Sprintf("/api/admin/users/%d/links", u.ID), map[string]any{"MaxUses": 0, "Hours": 0}, nil); code != 400 {
		t.Fatal("unlimited temp link must be rejected")
	}
	_, b, _ = ac.do("POST", fmt.Sprintf("/api/admin/users/%d/links", u.ID), map[string]any{"MaxUses": 0, "Hours": 1}, nil)
	tmp = mustJSON[map[string]any](t, b)
	ac.do("DELETE", fmt.Sprintf("/api/admin/users/%d/links/%v", u.ID, tmp["id"]), nil, nil)
	if code, _, _ := anon.do("GET", "/s/"+tmp["code"].(string), nil, nil); code != 410 {
		t.Fatal("revoked link still served")
	}
	// Rotating the token also rotates the short code.
	ac.do("POST", fmt.Sprintf("/api/admin/users/%d/rotate-token", u.ID), nil, nil)
	if code, _, _ := anon.do("GET", strings.TrimPrefix(short, "https://panel.test"), nil, nil); code != 410 {
		t.Fatalf("old short code after rotation: %d", code)
	}

	// TOTP: setup, enable with a valid code, login needs the code (428), wrong code 401, then disable.
	_, b, _ = ac.do("POST", "/api/admin/2fa/setup", nil, nil)
	secret := mustJSON[map[string]any](t, b)["secret"].(string)
	if code, _, _ := ac.do("POST", "/api/admin/2fa/enable", map[string]string{"Code": "000000"}, nil); code != 400 {
		t.Fatal("wrong code enabled 2fa")
	}
	valid := auth.TOTPCode(secret, time.Now())
	if code, _, _ := ac.do("POST", "/api/admin/2fa/enable", map[string]string{"Code": valid}, nil); code != 200 {
		t.Fatal("enable failed")
	}
	_, b, _ = ac.do("GET", "/api/admin/me", nil, nil)
	if !strings.Contains(string(b), `"totp":true`) {
		t.Fatalf("me: %s", b)
	}
	lc := &client{t: t, srv: srv}
	if code, _, _ := lc.do("POST", "/api/admin/login", map[string]string{"Email": "admin@test", "Password": "password123"}, nil); code != 428 {
		t.Fatalf("login without code: %d", code)
	}
	if code, _, _ := lc.do("POST", "/api/admin/login", map[string]string{"Email": "admin@test", "Password": "password123", "Code": "111111"}, nil); code != 401 {
		t.Fatalf("login with bad code: %d", code)
	}
	if code, _, _ := lc.do("POST", "/api/admin/login", map[string]string{"Email": "admin@test", "Password": "password123", "Code": auth.TOTPCode(secret, time.Now())}, nil); code != 200 {
		t.Fatalf("login with code: %d", code)
	}
	if code, _, _ := lc.do("POST", "/api/admin/2fa/disable", map[string]string{"Code": "222222"}, nil); code != 400 {
		t.Fatal("disable without valid code")
	}
	if code, _, _ := lc.do("POST", "/api/admin/2fa/disable", map[string]string{"Code": auth.TOTPCode(secret, time.Now())}, nil); code != 200 {
		t.Fatal("disable failed")
	}
	if code, _, _ := (&client{t: t, srv: srv}).do("POST", "/api/admin/login", map[string]string{"Email": "admin@test", "Password": "password123"}, nil); code != 200 {
		t.Fatal("login after disable")
	}
}

// fakeObjectStore answers both the S3 (path-style) and WebDAV verbs the
// backup remotes use, recording what was stored.
type fakeObjectStore struct {
	mu   sync.Mutex
	objs map[string][]byte
	auth []string
}

func (f *fakeObjectStore) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.objs == nil {
		f.objs = map[string][]byte{}
	}
	f.auth = append(f.auth, r.Header.Get("Authorization"))
	key := strings.TrimPrefix(r.URL.Path, "/")
	switch r.Method {
	case "PUT":
		b, _ := io.ReadAll(r.Body)
		f.objs[key] = b
		w.WriteHeader(201)
	case "DELETE":
		delete(f.objs, key)
		w.WriteHeader(204)
	case "GET": // S3 ListObjectsV2
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, `<?xml version="1.0"?><ListBucketResult>`)
		for k := range f.objs {
			fmt.Fprintf(w, "<Contents><Key>%s</Key></Contents>", k)
		}
		fmt.Fprint(w, `</ListBucketResult>`)
	case "PROPFIND":
		w.WriteHeader(207)
		fmt.Fprint(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:">`)
		for k := range f.objs {
			fmt.Fprintf(w, "<d:response><d:href>/%s</d:href></d:response>", k)
		}
		fmt.Fprint(w, `</d:multistatus>`)
	default:
		w.WriteHeader(405)
	}
}

func TestBackups(t *testing.T) {
	cfg := config.Default()
	cfg.BaseURL = "https://panel.test"
	cfg.DataDir = t.TempDir()
	conn, _ := db.Open("sqlite", filepath.Join(cfg.DataDir, "c.db"))
	_ = db.Migrate(context.Background(), conn, "sqlite")
	st := store.New(conn)
	adminUser, _ := admin.NewUser("admin@test", "password123", "admin")
	_ = st.CreateUser(context.Background(), adminUser)
	srv := httptest.NewServer(New(cfg, st, slog.Default()).Handler())
	defer srv.Close()
	fake := &fakeObjectStore{}
	remote := httptest.NewServer(fake)
	defer remote.Close()
	ac := &client{t: t, srv: srv}
	ac.do("POST", "/api/admin/login", map[string]string{"Email": "admin@test", "Password": "password123"}, nil)

	// S3 (path-style against the fake), keep 2 remote copies.
	s3 := map[string]any{"remote": "s3", "keep": 7, "hour": 0, "remote_keep": 2, "s3": map[string]any{"endpoint": remote.URL, "bucket": "bk", "prefix": "cap", "access_key": "AK", "secret_key": "SK", "path_style": true}}
	if code, b, _ := ac.do("PUT", "/api/admin/settings/backup", s3, nil); code != 200 {
		t.Fatalf("put: %d %s", code, b)
	}
	if code, b, _ := ac.do("POST", "/api/admin/settings/backup/test", map[string]any{"remote": "s3", "s3": map[string]any{"endpoint": remote.URL, "bucket": "bk", "access_key": "AK", "path_style": true}}, nil); code != 200 {
		t.Fatalf("test remote: %d %s", code, b)
	}
	for i := 0; i < 3; i++ {
		if code, b, _ := ac.do("POST", "/api/admin/settings/backup/run", nil, nil); code != 200 {
			t.Fatalf("run %d: %d %s", i, code, b)
		}
		time.Sleep(1100 * time.Millisecond) // distinct time-suffixed names
	}
	fake.mu.Lock()
	n := len(fake.objs)
	var keys []string
	for k := range fake.objs {
		keys = append(keys, k)
	}
	signed := strings.HasPrefix(fake.auth[len(fake.auth)-1], "AWS4-HMAC-SHA256 Credential=AK/")
	fake.mu.Unlock()
	if n != 2 || !signed || !strings.HasPrefix(keys[0], "bk/cap/captain-") || !strings.HasSuffix(keys[0], ".db.gz") {
		t.Fatalf("remote objects: %v signed=%v", keys, signed)
	}
	_, b, _ := ac.do("GET", "/api/admin/settings/backup", nil, nil)
	view := mustJSON[map[string]any](t, b)
	if view["has_s3_secret"] != true || view["settings"].(map[string]any)["s3"].(map[string]any)["secret_key"] != "" {
		t.Fatalf("secret leaked or missing: %s", b)
	}
	files := view["files"].([]any)
	if len(files) != 3 || view["status"].(map[string]any)["remote_error"] != "" {
		t.Fatalf("files/status: %s", b)
	}
	name := files[0].(map[string]any)["name"].(string)
	code, b, h := ac.do("GET", "/api/admin/settings/backup/files/"+name, nil, nil)
	if code != 200 || !strings.HasPrefix(string(b), "SQLite format 3") || !strings.Contains(h.Get("Content-Disposition"), name) {
		t.Fatalf("download: %d %s", code, h.Get("Content-Disposition"))
	}
	if code, _, _ := ac.do("GET", "/api/admin/settings/backup/files/..%2Fc.db", nil, nil); code != 404 {
		t.Fatalf("traversal: %d", code)
	}
	// Blank secret keeps the stored one; switching to WebDAV uploads there.
	if code, _, _ := ac.do("PUT", "/api/admin/settings/backup", map[string]any{"remote": "webdav", "webdav": map[string]any{"url": remote.URL + "/dav/", "username": "u", "password": "p"}}, nil); code != 200 {
		t.Fatal("put webdav")
	}
	ac.do("POST", "/api/admin/settings/backup/run", nil, nil)
	fake.mu.Lock()
	var dav int
	for k := range fake.objs {
		if strings.HasPrefix(k, "dav/captain-") {
			dav++
		}
	}
	basic := strings.HasPrefix(fake.auth[len(fake.auth)-1], "Basic ")
	fake.mu.Unlock()
	if dav != 1 || !basic {
		t.Fatalf("webdav upload: %d basic=%v", dav, basic)
	}
	var s backup.Settings
	_ = st.GetSetting(context.Background(), backup.SettingKey, &s)
	if s.S3.SecretKey != "SK" {
		t.Fatal("stored S3 secret lost on resave")
	}
	// Operators cannot reach backups (settings prefix).
	ac.do("POST", "/api/admin/admins", map[string]string{"Email": "op@test", "Password": "password123", "Role": "operator"}, nil)
	oc := &client{t: t, srv: srv}
	oc.do("POST", "/api/admin/login", map[string]string{"Email": "op@test", "Password": "password123"}, nil)
	if code, _, _ := oc.do("GET", "/api/admin/settings/backup/files/"+name, nil, nil); code != 403 {
		t.Fatalf("operator download: %d", code)
	}
}

func selfSignedPEM(t *testing.T, names ...string) (string, string) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: names[0]}, DNSNames: names, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour)}
	der, _ := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	kb, _ := x509.MarshalECPrivateKey(key)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}))
}

// fakeIssuer signs whatever it is asked for and records the token used.
type fakeIssuer struct {
	mu     sync.Mutex
	tokens []string
	fail   error
	t      *testing.T
}

func (f *fakeIssuer) Issue(_ context.Context, req certs.IssueRequest) (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokens = append(f.tokens, req.CloudflareToken)
	if f.fail != nil {
		return "", "", f.fail
	}
	c, k := selfSignedPEM(f.t, req.Names...)
	return c, k, nil
}

func TestDomainsAndIssuedCertificates(t *testing.T) {
	cfg := config.Default()
	cfg.BaseURL = "https://panel.example.com"
	cfg.DataDir = t.TempDir()
	conn, _ := db.Open("sqlite", filepath.Join(cfg.DataDir, "c.db"))
	_ = db.Migrate(context.Background(), conn, "sqlite")
	st := store.New(conn)
	adminUser, _ := admin.NewUser("admin@test", "password123", "admin")
	_ = st.CreateUser(context.Background(), adminUser)
	issuer := &fakeIssuer{t: t}
	server := New(cfg, st, slog.Default(), Options{CertIssuer: issuer})
	srv := httptest.NewServer(server.Handler())
	defer srv.Close()
	ac := &client{t: t, srv: srv}
	ac.do("POST", "/api/admin/login", map[string]string{"Email": "admin@test", "Password": "password123"}, nil)

	// Domains: validation, registration, usage.
	if code, _, _ := ac.do("POST", "/api/admin/domains", map[string]string{"Name": "not a domain"}, nil); code != 400 {
		t.Fatal("bad domain accepted")
	}
	code, b, _ := ac.do("POST", "/api/admin/domains", map[string]string{"Name": "Example.COM"}, nil)
	if code != 200 || !strings.Contains(string(b), `"name":"example.com"`) {
		t.Fatalf("create domain: %d %s", code, b)
	}
	domID := int64(mustJSON[map[string]any](t, b)["id"].(float64))
	if code, _, _ := ac.do("POST", "/api/admin/domains", map[string]string{"Name": "example.com"}, nil); code != 400 {
		t.Fatal("duplicate domain accepted")
	}
	_, b, _ = ac.do("POST", "/api/admin/nodes", map[string]any{"Name": "jp", "PublicAddr": "203.0.113.5", "Domain": "JP1.example.com"}, nil)
	node := mustJSON[map[string]any](t, b)
	nid := itoa(int64(node["id"].(float64)))
	if node["domain"] != "jp1.example.com" {
		t.Fatalf("node domain: %v", node["domain"])
	}
	ac.do("POST", "/api/admin/nodes/"+nid+"/inbounds", map[string]any{"Tag": "t", "Protocol": "trojan", "Port": 443, "Settings": map[string]any{"tls": map[string]any{"mode": 1, "server_name": "jp1.example.com"}}}, nil)
	_, b, _ = ac.do("POST", "/api/admin/nodes/"+nid+"/inbounds", map[string]any{"Tag": "ss", "Protocol": "shadowsocks", "Port": 8388, "Settings": map[string]any{"cipher": "aes-128-gcm"}}, nil)
	ssIB := mustJSON[map[string]any](t, b)
	ac.do("PUT", "/api/admin/settings/subscription", map[string]any{"URLs": []string{"https://sub.example.com"}}, nil)
	_, b, _ = ac.do("GET", "/api/admin/domains", nil, nil)
	if !strings.Contains(string(b), `"nodes":["jp1.example.com"]`) || !strings.Contains(string(b), `"tls":["jp1.example.com"]`) || !strings.Contains(string(b), `"sub_hosts":["sub.example.com"]`) || !strings.Contains(string(b), `"panel":true`) || !strings.Contains(string(b), `"global_token":false`) {
		t.Fatalf("domain usage: %s", b)
	}
	// An entry on a plain inbound advertises the node domain, not the IP.
	_, b, _ = ac.do("POST", "/api/admin/entries", map[string]any{"Name": "ss", "InboundID": ssIB["ID"]}, nil)
	if !strings.Contains(string(b), `"DisplayHost":"jp1.example.com"`) {
		t.Fatalf("entry default host: %s", b)
	}

	// Issuance needs a token; the global one from ACME settings works, a
	// per-domain one wins, a manual domain refuses.
	if code, b, _ := ac.do("POST", "/api/admin/certificates/issue", map[string]any{"Names": []string{"jp1.example.com"}}, nil); code != 502 || !strings.Contains(string(b), "Cloudflare token") {
		t.Fatalf("issue without token: %d %s", code, b)
	}
	ac.do("PUT", "/api/admin/settings/acme", map[string]string{"Email": "ops@test", "CloudflareToken": "global-tok"}, nil)
	code, b, _ = ac.do("POST", "/api/admin/certificates/issue", map[string]any{"Name": "日本节点", "Names": []string{"jp1.example.com", "*.example.com"}}, nil)
	if code != 200 || !strings.Contains(string(b), `"name":"日本节点"`) || !strings.Contains(string(b), `"source":"acme"`) || !strings.Contains(string(b), `"names":["jp1.example.com","*.example.com"]`) || !strings.Contains(string(b), `"domain_id":`+itoa(domID)) {
		t.Fatalf("issue: %d %s", code, b)
	}
	certID := int64(mustJSON[map[string]any](t, b)["id"].(float64))
	if code, _, _ := ac.do("POST", "/api/admin/certificates/issue", map[string]any{"Names": []string{"bad name"}}, nil); code != 502 {
		t.Fatal("bad name accepted")
	}
	ac.do("PATCH", "/api/admin/domains/"+itoa(domID), map[string]string{"CFToken": "own-tok"}, nil)
	if code, b, _ := ac.do("POST", "/api/admin/certificates/"+itoa(certID)+"/renew", nil, nil); code != 200 || !strings.Contains(string(b), `"renewals":1`) {
		t.Fatalf("renew: %d %s", code, b)
	}
	issuer.mu.Lock()
	toks := append([]string{}, issuer.tokens...)
	issuer.mu.Unlock()
	if len(toks) != 2 || toks[0] != "global-tok" || toks[1] != "own-tok" {
		t.Fatalf("tokens used: %v", toks)
	}
	ac.do("PATCH", "/api/admin/domains/"+itoa(domID), map[string]string{"Provider": "manual"}, nil)
	if code, b, _ := ac.do("POST", "/api/admin/certificates/issue", map[string]any{"Names": []string{"x.example.com"}}, nil); code != 502 || !strings.Contains(string(b), "manual") {
		t.Fatalf("manual domain should refuse: %d %s", code, b)
	}
	ac.do("PATCH", "/api/admin/domains/"+itoa(domID), map[string]string{"Provider": "cloudflare"}, nil)

	// List: deployed nodes by coverage, no PEM; detail carries the chain but never the key.
	_, b, _ = ac.do("GET", "/api/admin/certificates", nil, nil)
	if !strings.Contains(string(b), `"nodes":["jp"]`) || strings.Contains(string(b), "BEGIN") || !strings.Contains(string(b), `"can_issue":true`) {
		t.Fatalf("list: %s", b)
	}
	_, b, _ = ac.do("GET", "/api/admin/certificates/"+itoa(certID), nil, nil)
	if !strings.Contains(string(b), "BEGIN CERTIFICATE") || strings.Contains(string(b), "PRIVATE KEY") || !strings.Contains(string(b), `"nodes":["jp"]`) {
		t.Fatalf("detail: %s", b)
	}
	ac.do("PATCH", "/api/admin/certificates/"+itoa(certID), map[string]any{"Name": "JP", "AutoRenew": false}, nil)
	_, b, _ = ac.do("GET", "/api/admin/certificates/"+itoa(certID), nil, nil)
	if !strings.Contains(string(b), `"name":"JP"`) || !strings.Contains(string(b), `"auto_renew":false`) {
		t.Fatalf("meta: %s", b)
	}
	// The node gets the certificate in its state.
	nc := &client{t: t, srv: srv}
	_, b, _ = nc.do("POST", "/api/agent/pair", agentproto.PairRequest{Code: node["pair_code"].(string)}, nil)
	nc.token = mustJSON[agentproto.PairResponse](t, b).Token
	_, b, _ = nc.do("GET", "/api/agent/state", nil, nil)
	if st := mustJSON[agentproto.State](t, b); len(st.Node.Certificates) != 1 || st.Node.Certificates[0].Domain != "jp1.example.com" {
		t.Fatalf("state certificates: %+v", mustJSON[agentproto.State](t, b).Node.Certificates)
	}

	// Scheduled renewal: only auto-renewing certificates within 30 days;
	// a failure lands in last_error.
	cs := server.Certs()
	cert, _ := st.CertificateByID(context.Background(), certID)
	cs.Now = func() time.Time { return cert.NotAfter.Add(-10 * 24 * time.Hour) }
	cs.RenewDue(context.Background())
	if c, _ := st.CertificateByID(context.Background(), certID); c.Renewals != 1 {
		t.Fatal("auto_renew=false must not renew")
	}
	ac.do("PATCH", "/api/admin/certificates/"+itoa(certID), map[string]any{"AutoRenew": true}, nil)
	cs.Now = func() time.Time { return cert.NotAfter.Add(-10*24*time.Hour + 2*time.Hour) } // past the hourly gate
	cs.RenewDue(context.Background())
	if c, _ := st.CertificateByID(context.Background(), certID); c.Renewals != 2 || c.LastError != "" {
		t.Fatalf("scheduled renewal: %+v", c)
	}
	issuer.mu.Lock()
	issuer.fail = errors.New("dns boom")
	issuer.mu.Unlock()
	cert, _ = st.CertificateByID(context.Background(), certID)
	cs.Now = func() time.Time { return cert.NotAfter.Add(-10*24*time.Hour + 4*time.Hour) }
	cs.RenewDue(context.Background())
	if c, _ := st.CertificateByID(context.Background(), certID); !strings.Contains(c.LastError, "dns boom") || c.Renewals != 2 {
		t.Fatalf("failed renewal: %+v", c)
	}
	// Uploads cannot be renewed; deleting the domain keeps the certificate.
	cp, kp := selfSignedPEM(t, "up.example.com")
	_, b, _ = ac.do("POST", "/api/admin/certificates", map[string]string{"CertPEM": cp, "KeyPEM": kp}, nil)
	upID := int64(mustJSON[map[string]any](t, b)["id"].(float64))
	if code, _, _ := ac.do("POST", "/api/admin/certificates/"+itoa(upID)+"/renew", nil, nil); code != 502 {
		t.Fatal("upload renewed")
	}
	ac.do("DELETE", "/api/admin/domains/"+itoa(domID), nil, nil)
	_, b, _ = ac.do("GET", "/api/admin/certificates", nil, nil)
	if !strings.Contains(string(b), `"name":"JP"`) {
		t.Fatalf("certificate lost with domain: %s", b)
	}
}

func TestLineIngresses(t *testing.T) {
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
	_, b, _ := ac.do("POST", "/api/admin/nodes", map[string]any{"Name": "jp", "PublicAddr": "192.0.2.10", "Domain": "jp1.example.com"}, nil)
	node := mustJSON[map[string]any](t, b)
	nid := itoa(int64(node["id"].(float64)))

	// Validation: something to reach the line by; a sane port range.
	if code, _, _ := ac.do("POST", "/api/admin/nodes/"+nid+"/ingresses", map[string]any{"Name": "IPLC"}, nil); code != 400 {
		t.Fatal("ingress without addresses accepted")
	}
	if code, _, _ := ac.do("POST", "/api/admin/nodes/"+nid+"/ingresses", map[string]any{"Name": "IPLC", "LineIP": "198.51.100.20", "PortFrom": 17799, "PortTo": 17701}, nil); code != 400 {
		t.Fatal("inverted range accepted")
	}
	code, b, _ := ac.do("POST", "/api/admin/nodes/"+nid+"/ingresses", map[string]any{"Name": "IPLC", "BindIP": "10.10.0.2", "LineIP": "198.51.100.20", "EntryHost": "203.0.113.30", "PortFrom": 17700, "PortTo": 17799, "ReservedPorts": []int{17700}}, nil)
	if code != 200 || !strings.Contains(string(b), `"reserved_ports":[17700]`) {
		t.Fatalf("create ingress: %d %s", code, b)
	}
	gid := int64(mustJSON[map[string]any](t, b)["ingress"].(map[string]any)["id"].(float64))
	if code, b, _ := ac.do("POST", "/api/admin/nodes/"+nid+"/inbounds", map[string]any{"Tag": "m", "Protocol": "mieru", "Port": 17700, "IngressID": gid, "Settings": map[string]any{"mieru_transport": "TCP"}}, nil); code != 400 || !strings.Contains(string(b), "reserved") {
		t.Fatalf("reserved port: %d %s", code, b)
	}

	// Inbounds: port must fit the line; the node state binds to the line NIC.
	if code, b, _ := ac.do("POST", "/api/admin/nodes/"+nid+"/inbounds", map[string]any{"Tag": "m", "Protocol": "mieru", "Port": 17800, "IngressID": gid, "Settings": map[string]any{"mieru_transport": "TCP"}}, nil); code != 400 || !strings.Contains(string(b), "range") {
		t.Fatalf("port outside range: %d %s", code, b)
	}
	_, b, _ = ac.do("POST", "/api/admin/nodes/"+nid+"/inbounds", map[string]any{"Tag": "m", "Protocol": "mieru", "Port": 17710, "IngressID": gid, "Settings": map[string]any{"mieru_transport": "TCP"}}, nil)
	mieru := mustJSON[map[string]any](t, b)
	_, b, _ = ac.do("POST", "/api/admin/nodes/"+nid+"/inbounds", map[string]any{"Tag": "hy2", "Protocol": "hysteria2", "Port": 8443, "Settings": map[string]any{"tls": map[string]any{"mode": 1, "server_name": "jp1.example.com"}}}, nil)
	hy2 := mustJSON[map[string]any](t, b)
	nc := &client{t: t, srv: srv}
	_, b, _ = nc.do("POST", "/api/agent/pair", agentproto.PairRequest{Code: node["pair_code"].(string)}, nil)
	nc.token = mustJSON[agentproto.PairResponse](t, b).Token
	_, b, _ = nc.do("GET", "/api/agent/state", nil, nil)
	state := mustJSON[agentproto.State](t, b)
	var mListen, hListen string
	for _, ib := range state.Node.Inbounds {
		if ib.Tag == "m" {
			mListen = ib.Listen
		}
		if ib.Tag == "hy2" {
			hListen = ib.Listen
		}
	}
	if mListen != "10.10.0.2" || hListen != "" {
		t.Fatalf("listen: mieru=%q hy2=%q", mListen, hListen)
	}
	// Node detail carries the ingresses.
	_, b, _ = ac.do("GET", "/api/admin/nodes/"+nid, nil, nil)
	if !strings.Contains(string(b), `"entry_host":"203.0.113.30"`) || !strings.Contains(string(b), `"IngressID":`+itoa(gid)) {
		t.Fatalf("node detail: %s", b)
	}

	// Entries: the line inbound advertises the carrier entry, the direct
	// inbound the node domain.
	_, b, _ = ac.do("POST", "/api/admin/entries", map[string]any{"Name": "沪日", "InboundID": mieru["ID"]}, nil)
	if !strings.Contains(string(b), `"DisplayHost":"203.0.113.30"`) || !strings.Contains(string(b), `"DisplayPort":17710`) {
		t.Fatalf("line entry: %s", b)
	}
	_, b, _ = ac.do("POST", "/api/admin/entries", map[string]any{"Name": "direct", "InboundID": hy2["ID"]}, nil)
	if !strings.Contains(string(b), `"DisplayHost":"jp1.example.com"`) {
		t.Fatalf("direct entry: %s", b)
	}
	// A port offset shifts the advertised port; no public entry means a
	// relay is needed and the entry says so.
	ac.do("PATCH", "/api/admin/ingresses/"+itoa(gid), map[string]any{"Name": "IPLC", "BindIP": "10.10.0.2", "LineIP": "198.51.100.20", "EntryHost": "203.0.113.30", "PortFrom": 17701, "PortTo": 17799, "PortOffset": 1000}, nil)
	_, b, _ = ac.do("POST", "/api/admin/entries", map[string]any{"Name": "沪日2", "InboundID": mieru["ID"]}, nil)
	if !strings.Contains(string(b), `"DisplayPort":18710`) {
		t.Fatalf("offset entry: %s", b)
	}
	ac.do("PATCH", "/api/admin/ingresses/"+itoa(gid), map[string]any{"Name": "IPLC", "BindIP": "10.10.0.2", "LineIP": "198.51.100.20", "PortFrom": 17701, "PortTo": 17799}, nil)
	if code, b, _ := ac.do("POST", "/api/admin/entries", map[string]any{"Name": "沪日3", "InboundID": mieru["ID"]}, nil); code != 400 || !strings.Contains(string(b), "relay") {
		t.Fatalf("entry without public entry: %d %s", code, b)
	}
	// Explicit addresses still win (a relay in front of the line).
	if code, b, _ := ac.do("POST", "/api/admin/entries", map[string]any{"Name": "via relay", "InboundID": mieru["ID"], "DisplayHost": "relay.example.com", "DisplayPort": 17710}, nil); code != 200 {
		t.Fatalf("explicit entry: %d %s", code, b)
	}
	// Deleting the ingress detaches inbounds instead of deleting them.
	ac.do("DELETE", "/api/admin/ingresses/"+itoa(gid), nil, nil)
	ib, err := st.InboundByID(context.Background(), int64(mieru["ID"].(float64)))
	if err != nil || ib.IngressID != nil {
		t.Fatalf("inbound after ingress delete: %+v %v", ib, err)
	}
}

// fakeCloudflare answers the zone lookup and record list/create/update
// calls the DNS service makes, recording records by name.
type fakeCloudflare struct {
	mu      sync.Mutex
	records map[string]map[string]string // name -> type -> content
	tokens  []string
	puts    int
}

func (f *fakeCloudflare) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.records == nil {
		f.records = map[string]map[string]string{}
	}
	f.tokens = append(f.tokens, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	w.Header().Set("Content-Type", "application/json")
	ok := func(v any) { _ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": v}) }
	switch {
	case r.URL.Path == "/zones":
		if r.URL.Query().Get("name") != "example.com" {
			ok([]any{})
			return
		}
		ok([]map[string]string{{"id": "zone1"}})
	case r.URL.Path == "/zones/zone1/dns_records" && r.Method == "GET":
		name, typ := r.URL.Query().Get("name"), r.URL.Query().Get("type")
		if c, has := f.records[name][typ]; has {
			ok([]map[string]any{{"id": name + "/" + typ, "type": typ, "name": name, "content": c, "proxied": false}})
			return
		}
		ok([]any{})
	case r.URL.Path == "/zones/zone1/dns_records" && r.Method == "POST":
		var rec struct{ Type, Name, Content string }
		_ = json.NewDecoder(r.Body).Decode(&rec)
		if f.records[rec.Name] == nil {
			f.records[rec.Name] = map[string]string{}
		}
		f.records[rec.Name][rec.Type] = rec.Content
		ok(map[string]string{"id": rec.Name + "/" + rec.Type})
	case strings.HasPrefix(r.URL.Path, "/zones/zone1/dns_records/") && r.Method == "PUT":
		var rec struct{ Type, Name, Content string }
		_ = json.NewDecoder(r.Body).Decode(&rec)
		f.records[rec.Name][rec.Type] = rec.Content
		f.puts++
		ok(map[string]string{"id": rec.Name + "/" + rec.Type})
	default:
		w.WriteHeader(404)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "errors": []map[string]string{{"message": "no route " + r.Method + " " + r.URL.Path}}})
	}
}

func (f *fakeCloudflare) get(name, typ string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.records[name][typ]
}

func TestAutoDNS(t *testing.T) {
	cfg := config.Default()
	cfg.BaseURL = "https://panel.example.com"
	conn, _ := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	_ = db.Migrate(context.Background(), conn, "sqlite")
	st := store.New(conn)
	adminUser, _ := admin.NewUser("admin@test", "password123", "admin")
	_ = st.CreateUser(context.Background(), adminUser)
	cf := &fakeCloudflare{}
	cfSrv := httptest.NewServer(cf)
	defer cfSrv.Close()
	srv := httptest.NewServer(New(cfg, st, slog.Default(), Options{DNSBase: cfSrv.URL}).Handler())
	defer srv.Close()
	ac := &client{t: t, srv: srv}
	ac.do("POST", "/api/admin/login", map[string]string{"Email": "admin@test", "Password": "password123"}, nil)

	// No registered domain: nothing happens, the node still saves.
	_, b, _ := ac.do("POST", "/api/admin/nodes", map[string]any{"Name": "jp", "PublicAddr": "192.0.2.10", "Domain": "jp1.example.com"}, nil)
	node := mustJSON[map[string]any](t, b)
	if node["pair_code"] == nil || node["dns"].([]any)[0].(map[string]any)["action"] != "skipped" {
		t.Fatalf("create without domain: %s", b)
	}
	nid := itoa(int64(node["id"].(float64)))
	// Registered domain with auto DNS but no token: an error, not a failure.
	ac.do("POST", "/api/admin/domains", map[string]string{"Name": "example.com"}, nil)
	_, b, _ = ac.do("PATCH", "/api/admin/nodes/"+nid, map[string]any{"Name": "jp", "PublicAddr": "192.0.2.10", "Domain": "jp1.example.com"}, nil)
	if !strings.Contains(string(b), `"ok":true`) || !strings.Contains(string(b), "no Cloudflare token") {
		t.Fatalf("update without token: %s", b)
	}
	ac.do("PUT", "/api/admin/settings/acme", map[string]string{"Email": "ops@test", "CloudflareToken": "cf-tok"}, nil)
	_, b, _ = ac.do("PATCH", "/api/admin/nodes/"+nid, map[string]any{"Name": "jp", "PublicAddr": "192.0.2.10", "V6Addr": "2001:db8::10", "Domain": "jp1.example.com"}, nil)
	if !strings.Contains(string(b), `"action":"created"`) || cf.get("jp1.example.com", "A") != "192.0.2.10" || cf.get("jp1.example.com", "AAAA") != "2001:db8::10" {
		t.Fatalf("records after update: %s A=%s AAAA=%s", b, cf.get("jp1.example.com", "A"), cf.get("jp1.example.com", "AAAA"))
	}
	// Same address again is unchanged; a new address updates in place.
	_, b, _ = ac.do("PATCH", "/api/admin/nodes/"+nid, map[string]any{"Name": "jp", "PublicAddr": "192.0.2.10", "V6Addr": "2001:db8::10", "Domain": "jp1.example.com"}, nil)
	if !strings.Contains(string(b), `"action":"unchanged"`) || strings.Contains(string(b), `"created"`) {
		t.Fatalf("unchanged: %s", b)
	}
	ac.do("PATCH", "/api/admin/nodes/"+nid, map[string]any{"Name": "jp", "PublicAddr": "192.0.2.11", "Domain": "jp1.example.com"}, nil)
	if cf.get("jp1.example.com", "A") != "192.0.2.11" || cf.puts != 1 {
		t.Fatalf("update in place: A=%s puts=%d", cf.get("jp1.example.com", "A"), cf.puts)
	}
	// A line ingress with an entry domain gets a record to the entry IP and
	// entries advertise the domain.
	code, b, _ := ac.do("POST", "/api/admin/nodes/"+nid+"/ingresses", map[string]any{"Name": "IPLC", "BindIP": "10.10.0.2", "LineIP": "198.51.100.20", "EntryHost": "203.0.113.30", "EntryDomain": "iplc.example.com", "PortFrom": 17701, "PortTo": 17799}, nil)
	if code != 200 || cf.get("iplc.example.com", "A") != "203.0.113.30" {
		t.Fatalf("ingress dns: %d %s", code, b)
	}
	gid := int64(mustJSON[map[string]any](t, b)["ingress"].(map[string]any)["id"].(float64))
	if code, _, _ := ac.do("POST", "/api/admin/nodes/"+nid+"/ingresses", map[string]any{"Name": "bad", "EntryHost": "entry.example.net", "EntryDomain": "x.example.com"}, nil); code != 400 {
		t.Fatal("entry domain over a host-name entry accepted")
	}
	_, b, _ = ac.do("POST", "/api/admin/nodes/"+nid+"/inbounds", map[string]any{"Tag": "m", "Protocol": "mieru", "Port": 17710, "IngressID": gid, "Settings": map[string]any{"mieru_transport": "TCP"}}, nil)
	mieru := mustJSON[map[string]any](t, b)
	_, b, _ = ac.do("POST", "/api/admin/entries", map[string]any{"Name": "line", "InboundID": mieru["ID"]}, nil)
	if !strings.Contains(string(b), `"DisplayHost":"iplc.example.com"`) {
		t.Fatalf("entry via domain: %s", b)
	}
	// Per-domain token wins; auto DNS off skips.
	domID := func() int64 {
		_, b, _ := ac.do("GET", "/api/admin/domains", nil, nil)
		return int64(mustJSON[map[string]any](t, b)["domains"].([]any)[0].(map[string]any)["id"].(float64))
	}()
	ac.do("PATCH", "/api/admin/domains/"+itoa(domID), map[string]any{"CFToken": "own-tok"}, nil)
	ac.do("PATCH", "/api/admin/nodes/"+nid, map[string]any{"Name": "jp", "PublicAddr": "192.0.2.12", "Domain": "jp1.example.com"}, nil)
	cf.mu.Lock()
	last := cf.tokens[len(cf.tokens)-1]
	cf.mu.Unlock()
	if last != "own-tok" {
		t.Fatalf("token used: %s", last)
	}
	ac.do("PATCH", "/api/admin/domains/"+itoa(domID), map[string]any{"AutoDNS": false}, nil)
	_, b, _ = ac.do("PATCH", "/api/admin/nodes/"+nid, map[string]any{"Name": "jp", "PublicAddr": "192.0.2.13", "Domain": "jp1.example.com"}, nil)
	if !strings.Contains(string(b), `"action":"skipped"`) || cf.get("jp1.example.com", "A") != "192.0.2.12" {
		t.Fatalf("auto dns off: %s", b)
	}
}

// Connections arriving from one of the panel's own nodes come through a
// forward that hides the client (no PROXY protocol): they count as one
// device altogether, not one per relay address.
func TestDeviceLimitRelay(t *testing.T) {
	service.DeviceHold, service.CacheTTL = 0, 0
	defer func() { service.DeviceHold, service.CacheTTL = 5*time.Minute, 10*time.Second }()
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
	_, b, _ := c.do("POST", "/api/admin/nodes", map[string]string{"Name": "exit", "PublicAddr": "203.0.113.30"}, nil)
	node := mustJSON[map[string]any](t, b)
	c.do("POST", "/api/admin/nodes", map[string]string{"Name": "relay", "PublicAddr": "198.51.100.20"}, nil)
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
	// One direct client plus connections from the relay: two devices, fine.
	agent.do("POST", "/api/agent/report", agentproto.Report{Online: map[string][]string{uuid: {"1.1.1.1", "198.51.100.20"}}}, nil)
	_, b, _ = agent.do("GET", "/api/agent/state", nil, nil)
	if stt := mustJSON[agentproto.State](t, b); len(stt.Users) != 1 {
		t.Fatalf("direct + relay should count as two: %+v", stt.Users)
	}
	_, b, _ = c.do("GET", "/api/admin/users/"+itoa(int64(u["id"].(float64))), nil, nil)
	if !strings.Contains(string(b), `"via_relay":true`) {
		t.Fatalf("relay address should be marked: %s", b)
	}
	// A second direct client makes three.
	_, b, _ = agent.do("POST", "/api/agent/report", agentproto.Report{Online: map[string][]string{uuid: {"2.2.2.2"}}}, nil)
	if rr := mustJSON[agentproto.ReportResponse](t, b); !rr.StateChanged {
		t.Fatal("third device should change state")
	}
	_, b, _ = agent.do("GET", "/api/agent/state", nil, nil)
	if stt := mustJSON[agentproto.State](t, b); len(stt.Users) != 0 {
		t.Fatalf("over-limit user still provisioned: %+v", stt.Users)
	}
}
