package http

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/config"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/http/admin"
	"github.com/zeptop-dev/captain/internal/store"
)

// A portal session for a staff account must not open the admin console:
// only the admin login (password + authenticator) mints admin sessions.
func TestPortalSessionCannotReachAdmin(t *testing.T) {
	cfg := config.Default()
	cfg.BaseURL = "http://test"
	conn, _ := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	_ = db.Migrate(context.Background(), conn, "sqlite")
	st := store.New(conn)
	adminUser, _ := admin.NewUser("admin@test", "password123", "admin")
	_ = st.CreateUser(context.Background(), adminUser)
	srv := httptest.NewServer(New(cfg, st, slog.Default()).Handler())
	defer srv.Close()

	viaPortal := &client{t: t, srv: srv}
	if code, b, _ := viaPortal.do("POST", "/api/portal/login", map[string]string{"email": "admin@test", "password": "password123"}, nil); code != 200 {
		t.Fatalf("portal login: %d %s", code, b)
	}
	if code, _, _ := viaPortal.do("GET", "/api/admin/nodes", nil, nil); code != 401 {
		t.Fatalf("portal session reached the admin API: %d", code)
	}
	viaAdmin := &client{t: t, srv: srv}
	viaAdmin.do("POST", "/api/admin/login", map[string]string{"Email": "admin@test", "Password": "password123"}, nil)
	if code, _, _ := viaAdmin.do("GET", "/api/admin/nodes", nil, nil); code != 200 {
		t.Fatalf("admin session refused: %d", code)
	}
	// Staff passwords are not reset through the mailbox flow, and the
	// refusal looks like any wrong code, so it does not reveal staff
	// addresses either.
	if code, _, _ := viaPortal.do("POST", "/api/portal/password/reset", map[string]string{"Email": "admin@test", "Code": "000000", "Password": "newpassword1"}, nil); code != 400 {
		t.Fatalf("staff reset should be refused: %d", code)
	}
	again := &client{t: t, srv: srv}
	if code, _, _ := again.do("POST", "/api/admin/login", map[string]string{"Email": "admin@test", "Password": "password123"}, nil); code != 200 {
		t.Fatalf("the staff password changed: %d", code)
	}
}

// A node may only account traffic for users it serves; samples for anyone
// else (a rogue box guessing ids) are dropped, and hostile tags/listen
// addresses never reach the node.
func TestNodeReportScopedToItsUsers(t *testing.T) {
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
	_, b, _ := c.do("POST", "/api/admin/groups", map[string]string{"Name": "vip"}, nil)
	group := mustJSON[map[string]any](t, b)
	_, b, _ = c.do("POST", "/api/admin/nodes", map[string]string{"Name": "n"}, nil)
	node := mustJSON[map[string]any](t, b)
	nodeID := itoa(int64(node["id"].(float64)))
	// Hostile inbound fields are refused at the door.
	if code, _, _ := c.do("POST", "/api/admin/nodes/"+nodeID+"/inbounds", map[string]any{"Tag": "x\"\n}", "Protocol": "vless", "Port": 1}, nil); code != 400 {
		t.Fatalf("hostile tag accepted: %d", code)
	}
	if code, _, _ := c.do("POST", "/api/admin/nodes/"+nodeID+"/inbounds", map[string]any{"Tag": "ok", "Protocol": "vless", "Port": 1, "Listen": "1.2.3.4 tcp dport 22 drop"}, nil); code != 400 {
		t.Fatalf("hostile listen accepted: %d", code)
	}
	if code, _, _ := c.do("PUT", "/api/admin/nodes/"+nodeID+"/forwards", map[string]any{"Forwards": []map[string]any{{"tag": "a\"b", "port": 10445, "target": "198.51.100.20:443"}}}, nil); code != 400 {
		t.Fatalf("hostile forward tag accepted: %d", code)
	}
	// The node only carries a vip-group inbound.
	c.do("POST", "/api/admin/nodes/"+nodeID+"/inbounds", map[string]any{"Tag": "vip", "Protocol": "vless", "Port": 443, "GroupID": group["id"]}, nil)
	_, b, _ = c.do("POST", "/api/admin/plans", map[string]any{"Name": "vip", "PriceCents": 1, "PeriodDays": 30, "GroupID": group["id"]}, nil)
	vipPlan := mustJSON[map[string]any](t, b)
	_, b, _ = c.do("POST", "/api/admin/plans", map[string]any{"Name": "basic", "PriceCents": 1, "PeriodDays": 30}, nil)
	basicPlan := mustJSON[map[string]any](t, b)
	_, b, _ = c.do("POST", "/api/admin/users", map[string]string{"Email": "vip@test", "Password": "password123"}, nil)
	vip := mustJSON[map[string]any](t, b)
	_, b, _ = c.do("POST", "/api/admin/users", map[string]string{"Email": "basic@test", "Password": "password123"}, nil)
	basic := mustJSON[map[string]any](t, b)
	c.do("POST", "/api/admin/users/"+itoa(int64(vip["id"].(float64)))+"/grant", map[string]any{"PlanID": vipPlan["ID"]}, nil)
	c.do("POST", "/api/admin/users/"+itoa(int64(basic["id"].(float64)))+"/grant", map[string]any{"PlanID": basicPlan["ID"]}, nil)

	agent := &client{t: t, srv: srv}
	_, b, _ = agent.do("POST", "/api/agent/pair", agentproto.PairRequest{Code: node["pair_code"].(string)}, nil)
	agent.token = mustJSON[agentproto.PairResponse](t, b).Token
	vipID, basicID := int64(vip["id"].(float64)), int64(basic["id"].(float64))
	agent.do("POST", "/api/agent/report", agentproto.Report{Traffic: []spec.UserTraffic{{UserID: vipID, Up: 100, Down: 200}, {UserID: basicID, Up: 5000, Down: 5000}, {UserID: 9999, Up: 1, Down: 1}}}, nil)
	vs, _ := st.ActiveSubscription(context.Background(), vipID)
	bs, _ := st.ActiveSubscription(context.Background(), basicID)
	if vs.UsedUpBytes+vs.UsedDownBytes != 300 {
		t.Fatalf("vip traffic not charged: %+v", vs)
	}
	if bs.UsedUpBytes+bs.UsedDownBytes != 0 {
		t.Fatalf("basic user charged by a node that does not serve them: %+v", bs)
	}
	_ = strings.TrimSpace
}

// A node job must wake the node's long-poll: the revision changes when a
// job is queued, so a GET with the previous ETag returns the state with
// the job instead of 304 forever.
func TestNodeJobChangesRevision(t *testing.T) {
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
	_, b, _ = agent.do("POST", "/api/agent/pair", agentproto.PairRequest{Code: node["pair_code"].(string)}, nil)
	agent.token = mustJSON[agentproto.PairResponse](t, b).Token
	_, b, hdr := agent.do("GET", "/api/agent/state", nil, nil)
	etag := hdr.Get("ETag")
	if code, _, _ := agent.do("GET", "/api/agent/state", nil, map[string]string{"If-None-Match": etag}); code != 304 {
		t.Fatalf("unchanged state should be 304, got %d", code)
	}
	if code, b, _ := c.do("POST", "/api/admin/nodes/"+nodeID+"/jobs", map[string]any{"kind": "reality_scan", "params": map[string]any{}}, nil); code != 200 {
		t.Fatalf("queue job: %d %s", code, b)
	}
	code, b, _ := agent.do("GET", "/api/agent/state", nil, map[string]string{"If-None-Match": etag})
	if code != 200 || !strings.Contains(string(b), `"reality_scan"`) {
		t.Fatalf("job did not change the revision: %d %s", code, b)
	}
}

func TestMetricsEndpoint(t *testing.T) {
	cfg := config.Default()
	cfg.BaseURL = "http://test"
	conn, _ := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	_ = db.Migrate(context.Background(), conn, "sqlite")
	st := store.New(conn)
	adminUser, _ := admin.NewUser("admin@test", "password123", "admin")
	_ = st.CreateUser(context.Background(), adminUser)
	srv := httptest.NewServer(New(cfg, st, slog.Default()).Handler())
	defer srv.Close()
	anon := &client{t: t, srv: srv}
	if code, _, _ := anon.do("GET", "/api/admin/metrics", nil, nil); code != 401 {
		t.Fatalf("metrics without login: %d", code)
	}
	c := &client{t: t, srv: srv}
	c.do("POST", "/api/admin/login", map[string]string{"Email": "admin@test", "Password": "password123"}, nil)
	code, b, hdr := c.do("GET", "/api/admin/metrics", nil, nil)
	if code != 200 || !strings.Contains(string(b), "captain_nodes 0") || !strings.Contains(string(b), "# TYPE captain_job_errors_total counter") {
		t.Fatalf("metrics: %d %s", code, b)
	}
	if hdr.Get("X-Request-ID") == "" {
		t.Fatal("request id header missing")
	}
}

// A cookie session is only honoured for writes that come from the panel's
// own origin; a page on another site cannot drive the admin or portal API
// with the victim's cookie.
func TestCrossSiteWritesRefused(t *testing.T) {
	cfg := config.Default()
	cfg.BaseURL = "http://test"
	conn, _ := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	_ = db.Migrate(context.Background(), conn, "sqlite")
	st := store.New(conn)
	adminUser, _ := admin.NewUser("admin@test", "password123", "admin")
	_ = st.CreateUser(context.Background(), adminUser)
	srv := httptest.NewServer(New(cfg, st, slog.Default()).Handler())
	defer srv.Close()

	evil := map[string]string{"Origin": "https://evil.example"}
	c := &client{t: t, srv: srv}
	if code, _, _ := c.do("POST", "/api/admin/login", map[string]string{"Email": "admin@test", "Password": "password123"}, evil); code != 403 {
		t.Fatalf("cross-site login accepted: %d", code)
	}
	if code, _, _ := c.do("POST", "/api/admin/login", map[string]string{"Email": "admin@test", "Password": "password123"}, map[string]string{"Origin": "http://test"}); code != 200 {
		t.Fatalf("same-origin login refused: %d", code)
	}
	if code, _, _ := c.do("POST", "/api/admin/groups", map[string]string{"Name": "g"}, evil); code != 403 {
		t.Fatalf("cross-site write accepted: %d", code)
	}
	if code, _, _ := c.do("GET", "/api/admin/nodes", nil, evil); code != 200 {
		t.Fatalf("cross-site read (harmless) refused: %d", code)
	}
	if code, _, _ := c.do("POST", "/api/admin/groups", map[string]string{"Name": "g"}, map[string]string{"Origin": srv.URL}); code != 200 {
		t.Fatalf("same-origin write refused: %d", code)
	}
	if code, _, _ := c.do("POST", "/api/portal/login", map[string]string{"email": "admin@test", "password": "password123"}, evil); code != 403 {
		t.Fatalf("cross-site portal login accepted: %d", code)
	}
}
