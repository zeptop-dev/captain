package http

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/http/probeaccess"
	"github.com/zeptop-dev/captain/internal/probedata"
	"github.com/zeptop-dev/captain/internal/probetheme"
	"github.com/zeptop-dev/captain/internal/store"
	"golang.org/x/net/websocket"
)

func probeRequest(t *testing.T, r *rig, method, path, host string, body io.Reader, cookie *http.Cookie, headers map[string]string) (int, []byte, http.Header) {
	t.Helper()
	req, err := http.NewRequest(method, r.srv.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = host
	if cookie != nil {
		req.AddCookie(cookie)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	c := *r.srv.Client()
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, b, resp.Header
}

func configureKomariProbe(t *testing.T, r *rig, visibility string) {
	t.Helper()
	if code, b, _ := r.c.do("PUT", "/api/admin/settings/probe", map[string]any{"enabled": true, "visibility": visibility, "hosts": []string{"probe.example.com"}, "path": "/status", "title": "Example monitor"}, nil); code != 200 {
		t.Fatalf("settings %d %s", code, b)
	}
}

func TestKomariProbeRoutingNativeDataAndAuthIsolation(t *testing.T) {
	r := newRig(t)
	configureKomariProbe(t, r, "public")
	ctx := context.Background()
	if err := r.st.RecordBeat(ctx, r.nodeID, spec.SystemStatus{CPUPercent: 17, MemTotal: 1024, MemUsed: 256, DiskTotal: 4096, DiskUsed: 512, TCP: 3, UDP: 2}, time.Now()); err != nil {
		t.Fatal(err)
	}
	code, b, _ := probeRequest(t, r, "GET", "/api/probe/v2", "test", nil, nil, nil)
	if code != 200 {
		t.Fatalf("native snapshot: %d %s", code, b)
	}
	native := mustJSON[probedata.Snapshot](t, b)
	if len(native.Nodes) != 1 || native.Nodes[0].UUID == "" {
		t.Fatalf("native IDs: %s", b)
	}
	code, b, _ = probeRequest(t, r, "GET", "/api/nodes", "probe.example.com", nil, r.c.cookie, nil)
	wire := mustJSON[struct {
		Data []map[string]any `json:"data"`
	}](t, b)
	if code != 200 || len(wire.Data) != 1 || wire.Data[0]["uuid"] != native.Nodes[0].UUID {
		t.Fatalf("inconsistent public identity: %d %s", code, b)
	}
	for _, path := range []string{"/api/nodes", "/api/public", "/api/rpc2", "/api/clients"} {
		if code, _, _ := probeRequest(t, r, "GET", path, "test", nil, r.c.cookie, nil); code != 404 {
			t.Errorf("compat route on main %s: %d", path, code)
		}
	}
	for _, path := range []string{"/api/admin/me", "/api/portal/me", "/api/portal/me/lang", "/api/portal/me/hwid-devices/device", "/api/probe/access", "/api/probex", "/api/agent/state", "/admin/", "/portal/"} {
		if code, _, _ := probeRequest(t, r, "GET", path, "probe.example.com", nil, r.c.cookie, nil); code != 404 {
			t.Errorf("panel path exposed %s: %d", path, code)
		}
	}
	code, b, _ = probeRequest(t, r, "GET", "/api/me", "probe.example.com", nil, r.c.cookie, nil)
	if code != 200 || strings.Contains(string(b), "admin@test") || strings.Contains(string(b), `"logged_in":true`) {
		t.Fatalf("normal session crossed theme boundary: %d %s", code, b)
	}
	// Staff cookies cannot widen the legacy projection on the isolated host.
	if code, b, _ := r.c.do("PUT", "/api/admin/nodes/"+itoa(r.nodeID)+"/probe", map[string]any{"Hidden": true}, nil); code != 200 {
		t.Fatalf("hide: %d %s", code, b)
	}
	code, b, _ = probeRequest(t, r, "GET", "/api/probe", "probe.example.com", nil, r.c.cookie, nil)
	if code != 200 || len(mustJSON[struct {
		Nodes []any `json:"nodes"`
	}](t, b).Nodes) != 0 {
		t.Fatalf("legacy staff projection leaked: %d %s", code, b)
	}
	code, b, _ = probeRequest(t, r, "GET", "/api/nodes", "probe.example.com", nil, r.c.cookie, nil)
	if code != 200 || len(mustJSON[struct {
		Data []any `json:"data"`
	}](t, b).Data) != 0 {
		t.Fatalf("compat hidden node leaked: %d %s", code, b)
	}
}

func TestKomariProbePrivateReadSessionLifecycle(t *testing.T) {
	r := newRig(t)
	configureKomariProbe(t, r, "admins")
	if code, _, _ := probeRequest(t, r, "GET", "/api/nodes", "probe.example.com", nil, r.c.cookie, nil); code != 403 {
		t.Fatalf("panel cookie accepted on private probe: %d", code)
	}
	if code, b, _ := probeRequest(t, r, "GET", "/", "probe.example.com", nil, nil, nil); code != 200 || !strings.Contains(string(b), probeaccess.LoginPath) {
		t.Fatalf("no trusted login screen: %d %s", code, b)
	}
	code, b, _ := probeRequest(t, r, "POST", probeaccess.MainPath, "test", strings.NewReader("host=probe.example.com"), r.c.cookie, map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Origin": "http://test"})
	if code != 200 {
		t.Fatalf("grant: %d %s", code, b)
	}
	match := regexp.MustCompile(`name="ticket" value="([^"]+)"`).FindSubmatch(b)
	if len(match) != 2 {
		t.Fatalf("no grant ticket: %s", b)
	}
	code, b, h := probeRequest(t, r, "POST", probeaccess.ExchangePath, "probe.example.com", strings.NewReader("ticket="+string(match[1])), nil, map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Origin": "http://test"})
	if code != 303 {
		t.Fatalf("exchange: %d %s", code, b)
	}
	resp := http.Response{Header: h}
	cookies := resp.Cookies()
	if len(cookies) != 1 {
		t.Fatal("no read session")
	}
	cookie := cookies[0]
	for _, path := range []string{"/api/probe/v2", "/api/nodes", "/api/public"} {
		if code, b, _ := probeRequest(t, r, "GET", path, "probe.example.com", nil, cookie, nil); code != 200 {
			t.Fatalf("private read %s: %d %s", path, code, b)
		}
	}
	if code, _, _ := probeRequest(t, r, "GET", "/api/admin/me", "test", nil, cookie, nil); code != 401 {
		t.Fatalf("probe cookie became panel session: %d", code)
	}
	if err := r.st.DeleteSession(context.Background(), r.c.cookie.Value); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := probeRequest(t, r, "GET", "/api/nodes", "probe.example.com", nil, cookie, nil); code != 403 {
		t.Fatalf("parent logout did not revoke child: %d", code)
	}
}

func TestKomariThemeServingAndFilteredConfiguration(t *testing.T) {
	r := newRig(t)
	configureKomariProbe(t, r, "public")
	ctx := context.Background()
	data := probedata.Engine{Store: r.st, SiteName: "Captain"}
	s, err := data.Snapshot(ctx, probedata.Scope{})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	files := map[string]string{
		"komari-theme.json":    `{"name":"Fixture","short":"fixture","version":"1","configuration":{"data":[{"key":"featured","type":"nodes","default":"[]"}]}}`,
		"dist/index.html":      `<title>Komari Monitor</title><main>original-theme-fixture</main><script src="/assets/theme.js"></script>`,
		"dist/assets/theme.js": `window.fixture=true;`,
	}
	for path, body := range files {
		f, e := z.Create(path)
		if e != nil {
			t.Fatal(e)
		}
		_, _ = f.Write([]byte(body))
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	m := probetheme.New(r.st)
	p, err := m.Install(ctx, buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := m.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Activate(ctx, p.Digest, catalog.State.Revision); err != nil {
		t.Fatal(err)
	}
	catalog, err = m.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.SaveSettings(ctx, p.Digest, catalog.Packages[0].Revision, map[string]any{"featured": []any{s.Nodes[0].UUID, "hidden-unknown"}}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/instance/" + s.Nodes[0].UUID, "/assets/theme.js", "/themes/fixture/dist/assets/theme.js"} {
		if code, b, _ := probeRequest(t, r, "GET", path, "probe.example.com", nil, nil, nil); code != 200 || (!strings.Contains(string(b), "fixture") && !strings.Contains(string(b), "Fixture")) {
			t.Fatalf("theme %s: %d %s", path, code, b)
		}
	}
	code, b, _ := probeRequest(t, r, "GET", "/status/", "test", nil, nil, nil)
	if code != 200 || strings.Contains(string(b), "original-theme-fixture") {
		t.Fatalf("third party theme on panel origin: %d %s", code, b)
	}
	code, b, _ = probeRequest(t, r, "GET", "/api/public", "probe.example.com", nil, nil, nil)
	if code != 200 || !strings.Contains(string(b), s.Nodes[0].UUID) || strings.Contains(string(b), "hidden-unknown") {
		t.Fatalf("selector filtering: %d %s", code, b)
	}
	if err = r.st.UpdateNodeProbe(ctx, r.nodeID, true, store.NodeProbeInfo{}, 0, 1, "sum"); err != nil {
		t.Fatal(err)
	}
	code, b, _ = probeRequest(t, r, "GET", "/api/public", "probe.example.com", nil, nil, nil)
	if code != 200 || strings.Contains(string(b), s.Nodes[0].UUID) {
		t.Fatalf("hidden selected identity leaked: %d %s", code, b)
	}
	var out any
	if err = json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
}

func TestKomariOriginIsolationSurvivesRemovalAndInvalidSettings(t *testing.T) {
	r := newRig(t)
	configureKomariProbe(t, r, "public")
	if code, _, _ := probeRequest(t, r, "GET", "/api/admin/me", "probe.example.com", nil, r.c.cookie, nil); code != 404 {
		t.Fatalf("initial isolation: %d", code)
	}
	// Even malformed storage cannot make an old theme origin a management site.
	if err := r.st.SetSetting(context.Background(), store.SettingProbe, map[string]any{"hosts": "invalid"}); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := probeRequest(t, r, "GET", "/api/admin/me", "probe.example.com", nil, r.c.cookie, nil); code != 503 {
		t.Fatalf("invalid settings fail open: %d", code)
	}
	if err := r.st.SetSetting(context.Background(), store.SettingProbe, store.ProbeSettings{Enabled: true, Visibility: "public", Path: "/status"}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/admin/me", "/api/portal/me", "/api/site", "/admin/", "/"} {
		if code, _, _ := probeRequest(t, r, "GET", path, "probe.example.com", nil, r.c.cookie, nil); code != 404 {
			t.Errorf("retired origin %s: %d", path, code)
		}
	}
	if code, _, _ := probeRequest(t, r, "GET", "/api/admin/me", "test", nil, r.c.cookie, nil); code != 200 {
		t.Fatalf("main site locked: %d", code)
	}
}

func TestKomariSocketDoesNotHoldAccountLock(t *testing.T) {
	r := newRig(t)
	if code, b, _ := r.c.do("PUT", "/api/admin/settings/probe", map[string]any{"enabled": true, "hosts": []string{"127.0.0.1"}, "visibility": "public"}, nil); code != 200 {
		t.Fatalf("settings %d %s", code, b)
	}
	wsURL := "ws" + strings.TrimPrefix(r.srv.URL, "http") + "/api/rpc2"
	ws, err := websocket.Dial(wsURL, "", r.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if err = websocket.Message.Send(ws, `{"jsonrpc":"2.0","method":"rpc.ping","id":1}`); err != nil {
		t.Fatal(err)
	}
	var msg string
	if err = websocket.Message.Receive(ws, &msg); err != nil || !strings.Contains(msg, "pong") {
		t.Fatalf("health %s %v", msg, err)
	}
	locked := make(chan struct{})
	go func() { r.st.Accounts.Lock(); r.st.Accounts.Unlock(); close(locked) }()
	select {
	case <-locked:
	case <-time.After(2 * time.Second):
		t.Fatal("socket held account lock")
	}
	if err = r.st.SetSetting(context.Background(), store.SettingProbe, store.ProbeSettings{Enabled: true, Hosts: []string{"127.0.0.1"}, Visibility: "admins"}); err != nil {
		t.Fatal(err)
	}
	_ = ws.SetReadDeadline(time.Now().Add(8 * time.Second))
	if err = websocket.Message.Receive(ws, &msg); err == nil {
		t.Fatalf("socket remained readable after revocation: %s", msg)
	}
}
