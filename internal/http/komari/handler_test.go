package komari

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/probedata"
	"github.com/zeptop-dev/captain/internal/store"
	"golang.org/x/net/websocket"
)

const fixtureUUID = "5ad81cc0-c397-4ec4-b349-76cc0ce77de8"

var fixtureTime = time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)

func number(v float64) *float64   { return &v }
func uintNumber(v uint64) *uint64 { return &v }

type fixtureReader struct {
	mu          sync.Mutex
	queries     []probedata.Query
	pingQueries []probedata.PingQuery
	error       error
}

func (f *fixtureReader) Snapshot(context.Context, probedata.Scope) (probedata.Snapshot, error) {
	return probedata.Snapshot{Now: fixtureTime, Schema: "captain.probe.v2", Settings: probedata.Settings{Title: "Example probe", Visibility: "public", BeatSeconds: 5}, Nodes: []probedata.Node{{
		UUID: fixtureUUID, Name: "Test server", Group: "Example", Inventory: probedata.Inventory{CPUModel: "Test CPU", CPUCores: 2, MemTotal: uintNumber(1024), IPv4: "192.0.2.10"},
		CreatedAt: fixtureTime.Add(-72 * time.Hour), UpdatedAt: fixtureTime.Add(-3 * time.Hour),
		Status:  probedata.Status{Online: true, Fresh: true, LastSeen: &fixtureTime, ReceivedAt: &fixtureTime, Values: map[string]*float64{"cpu.usage": number(0), "memory.used": number(512), "net.out.rate": number(123), "net.in.rate": number(456), "net.total.up": number(9000), "net.total.down": number(8000), "connections.tcp": number(3), "connections.udp": number(2)}},
		Traffic: &probedata.Traffic{Valid: true, UsedUp: 100, UsedDown: 200, Used: 300, Limit: 10000, Mode: "sum"},
		Info:    &probedata.Info{Region: "US", Cost: &probedata.Cost{Amount: "3.95", Currency: "USD", Cycle: "monthly"}},
		Recent:  []probedata.Recent{{Time: fixtureTime, Values: map[string]*float64{"cpu.usage": number(0), "net.out.rate": number(123)}}},
	}}, PingTasks: []probedata.PingTask{{ID: "carrier:abc", LegacyID: 9000001, Name: "Carrier", Type: "tcp", Interval: 60, EntityIDs: []string{fixtureUUID}}}}, nil
}
func (f *fixtureReader) Definitions(context.Context, probedata.Scope) ([]probedata.Definition, error) {
	return []probedata.Definition{{Key: "cpu.usage", Type: "gauge", Unit: "%", History: true, Aggregations: []string{"avg"}}, {Key: "net.total.up", Type: "counter", Unit: "bytes", History: false}, {Key: "ping.latency_ms", Type: "gauge", Unit: "ms", History: true}, {Key: "ping.loss", Type: "gauge", Unit: "ratio", History: true}}, nil
}
func (f *fixtureReader) Query(_ context.Context, _ probedata.Scope, q probedata.Query) ([]probedata.Series, error) {
	f.mu.Lock()
	f.queries = append(f.queries, q)
	f.mu.Unlock()
	if f.error != nil {
		return nil, f.error
	}
	out := []probedata.Series{}
	for _, key := range q.MetricKeys {
		v := number(12)
		tags := map[string]string{}
		if key == "ping.latency_ms" {
			v = number(25)
			tags["task_id"] = "carrier:abc"
		}
		if key == "ping.loss" {
			v = number(.1)
			tags["task_id"] = "carrier:abc"
		}
		out = append(out, probedata.Series{MetricKey: key, EntityID: fixtureUUID, Tags: tags, Count: 1, Points: []probedata.Point{{Time: &fixtureTime, Value: v, Count: 10}}})
	}
	return out, nil
}
func (f *fixtureReader) PingStats(_ context.Context, _ probedata.Scope, q probedata.PingQuery) ([]probedata.PingStat, error) {
	f.mu.Lock()
	f.pingQueries = append(f.pingQueries, q)
	f.mu.Unlock()
	return []probedata.PingStat{{EntityID: fixtureUUID, TaskID: "carrier:abc", Name: "Carrier", Total: 10, Valid: 9, Loss: number(.1), Min: number(20), Max: number(30), Avg: number(25), P50: number(20), P99: number(30), P99P50Ratio: number(1.5)}}, nil
}
func fixtureHandler(f *fixtureReader) *Handler {
	return New(Deps{Reader: f, Gate: func(*http.Request) bool { return true }, Now: func() time.Time { return fixtureTime }, Version: "1.48.0", Hash: "test"})
}
func jsonObject(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("invalid JSON %s: %v", raw, err)
	}
	return out
}
func doHTTP(t *testing.T, h *Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func rpc(t *testing.T, h *Handler, method string, params any) map[string]any {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	w := doHTTP(t, h, "POST", "/api/rpc2", string(body))
	if w.Code != 200 {
		t.Fatalf("HTTP %d %s", w.Code, w.Body.String())
	}
	return jsonObject(t, w.Body.Bytes())
}

func TestRESTAndCommonWireShapes(t *testing.T) {
	h := fixtureHandler(&fixtureReader{})
	w := doHTTP(t, h, "GET", "/api/nodes", "")
	body := jsonObject(t, w.Body.Bytes())
	nodes := body["data"].([]any)
	n := nodes[0].(map[string]any)
	if body["status"] != "success" || n["uuid"] != fixtureUUID || n["cpu_name"] != "Test CPU" || n["price"] != 3.95 || n["billing_cycle"] != float64(30) {
		t.Fatalf("node projection: %s", w.Body.String())
	}
	if _, ok := n["ipv4"]; ok {
		t.Fatal("legacy public nodes exposed IP")
	}
	if strings.Contains(w.Body.String(), "192.0.2.10") {
		t.Fatal("legacy node extension bypassed address projection")
	}
	common := rpc(t, h, "common:getNodes", map[string]any{"uuid": fixtureUUID})["result"].(map[string]any)
	if common["ipv4"] != "192.0.2.10" {
		t.Fatal("common nodes should preserve explicitly public address")
	}
	latest := rpc(t, h, "common:getNodesLatestStatus", map[string]any{"uuid": fixtureUUID})["result"].(map[string]any)
	if latest["net_out"] != float64(123) || latest["net_in"] != float64(456) || latest["net_total_up"] != float64(9000) || latest["cpu"] != float64(0) {
		t.Fatalf("runtime projection: %#v", latest)
	}
	if latest["connections"] != float64(5) || latest["captain"].(map[string]any)["connections_tcp"] != float64(3) {
		t.Fatal("Komari combined count and exact TCP extension must both be retained")
	}
	if latest["temp"] != nil || latest["swap"] != nil {
		t.Fatal("unobserved data must stay null")
	}
	traffic := latest["captain"].(map[string]any)["traffic"].(map[string]any)
	if traffic["used_up"] != float64(100) {
		t.Fatal("period traffic missing")
	}
	me := doHTTP(t, h, "GET", "/api/me", "")
	identity := jsonObject(t, me.Body.Bytes())
	if identity["logged_in"] != false || identity["username"] != "Guest" {
		t.Fatalf("identity must be raw: %s", me.Body.String())
	}
	if _, ok := identity["data"]; ok {
		t.Fatal("/api/me must not be wrapped")
	}
}

func TestNodeMetadataTimestampsStayDistinctFromMeasurements(t *testing.T) {
	h := fixtureHandler(&fixtureReader{})
	legacy := doHTTP(t, h, "GET", "/api/nodes", "")
	nodes := jsonObject(t, legacy.Body.Bytes())["data"].([]any)
	common := rpc(t, h, "common:getNodes", map[string]any{"uuid": fixtureUUID})["result"].(map[string]any)
	for _, n := range []map[string]any{nodes[0].(map[string]any), common} {
		if n["created_at"] != fixtureTime.Add(-72*time.Hour).Format(time.RFC3339) || n["updated_at"] != fixtureTime.Add(-3*time.Hour).Format(time.RFC3339) {
			t.Fatalf("node metadata must preserve stored row times: %#v", n)
		}
	}
	latest := rpc(t, h, "common:getNodesLatestStatus", map[string]any{"uuid": fixtureUUID})["result"].(map[string]any)
	if latest["time"] != fixtureTime.Format(time.RFC3339) {
		t.Fatalf("runtime timestamp must remain the accepted measurement time: %#v", latest)
	}
	recent := doHTTP(t, h, "GET", "/api/recent/"+fixtureUUID, "")
	report := jsonObject(t, recent.Body.Bytes())["data"].([]any)[0].(map[string]any)
	if report["updated_at"] != fixtureTime.Format(time.RFC3339) {
		t.Fatalf("report timestamp borrowed node modification time: %#v", report)
	}
}

func TestRecentAndTaskWireShapes(t *testing.T) {
	h := fixtureHandler(&fixtureReader{})
	w := doHTTP(t, h, "GET", "/api/recent/"+fixtureUUID, "")
	recent := jsonObject(t, w.Body.Bytes())["data"].([]any)[0].(map[string]any)
	if recent["cpu"].(map[string]any)["usage"] != float64(0) || recent["network"].(map[string]any)["up"] != float64(123) {
		t.Fatal("nested recent shape changed")
	}
	if recent["ram"].(map[string]any)["total"] != nil || recent["captain"].(map[string]any)["historical"] != true {
		t.Fatal("nested history borrowed current inventory or freshness")
	}
	flat := rpc(t, h, "common:getNodeRecentStatus", []any{fixtureUUID})["result"].(map[string]any)
	if flat["count"] != float64(1) || flat["records"].([]any)[0].(map[string]any)["ram_total"] != nil {
		t.Fatal("historical total must not be guessed from current hardware")
	}
	tasks := doHTTP(t, h, "GET", "/api/task/ping", "")
	task := jsonObject(t, tasks.Body.Bytes())["data"].([]any)[0].(map[string]any)
	if task["id"] != float64(9000001) || task["clients"].([]any)[0] != fixtureUUID {
		t.Fatalf("task shape %s", tasks.Body.String())
	}
	if _, ok := task["target"]; ok {
		t.Fatal("task target leaked")
	}
}

func TestOnlyPublicThemeSettingsCrossBoundary(t *testing.T) {
	h := fixtureHandler(&fixtureReader{})
	h.d.PublicSettings = func(*http.Request) (map[string]any, error) {
		return map[string]any{"theme": "example", "theme_settings": map[string]any{"color": "green"}, "custom_head": "secret script", "private_site": false, "token": "secret", "login_url": "/__captain/login"}, nil
	}
	w := doHTTP(t, h, "GET", "/api/public", "")
	body := jsonObject(t, w.Body.Bytes())["data"].(map[string]any)
	if body["theme"] != "example" || body["custom_head"] != "" || body["login_url"] != "/__captain/login" || strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("unsafe public settings %s", w.Body.String())
	}
	for _, target := range []string{"https://example.com/login", "//example.com/login", "/admin/login"} {
		h.d.PublicSettings = func(*http.Request) (map[string]any, error) { return map[string]any{"login_url": target}, nil }
		w = doHTTP(t, h, "GET", "/api/public", "")
		if _, ok := jsonObject(t, w.Body.Bytes())["data"].(map[string]any)["login_url"]; ok {
			t.Fatalf("untrusted login target exposed: %s", target)
		}
	}
}

func TestFrameworkRPCContractAndDiscovery(t *testing.T) {
	h := fixtureHandler(&fixtureReader{})
	for method, want := range map[string]string{"rpc.ping": "pong", "rpc.version": "2.0"} {
		if got := rpc(t, h, method, nil)["result"]; got != want {
			t.Fatalf("%s = %v, want %s", method, got, want)
		}
	}
	for _, params := range []any{nil, map[string]any{"internal": true}, []any{true}} {
		methods := rpc(t, h, "rpc.methods", params)["result"].([]any)
		wantInternal := params != nil
		wantCount := len(callableMethods)
		if !wantInternal {
			wantCount -= 4
		}
		if len(methods) != wantCount {
			t.Fatalf("method count %d != %d", len(methods), wantCount)
		}
		previous := ""
		for _, raw := range methods {
			name := raw.(string)
			if name <= previous || strings.HasPrefix(name, "admin:") || strings.HasPrefix(name, "agent:") || (!wantInternal && strings.HasPrefix(name, "rpc.")) {
				t.Fatalf("unsafe or unordered method %s", name)
			}
			previous = name
			// A listed method may reject omitted parameters, but it must exist.
			if e, ok := rpc(t, h, name, nil)["error"].(map[string]any); ok && e["code"] == float64(-32601) {
				t.Fatalf("advertised unsupported method %s", name)
			}
		}
	}
	if got := rpc(t, h, "rpc.help", map[string]any{"method": "rpc.ping"})["result"].(map[string]any); got["name"] != "rpc.ping" || got["returns"] != "string" {
		t.Fatalf("help contract: %#v", got)
	}
	if got := rpc(t, h, "rpc.help", nil)["result"].([]any); len(got) != len(callableMethods) {
		t.Fatal("help overview omitted supported methods")
	}
	for _, method := range []string{"admin:exec", "agent:report", "rpc.discover"} {
		if e := rpc(t, h, "rpc.help", []any{method})["error"].(map[string]any); e["code"] != float64(-32600) {
			t.Fatalf("help advertised %s", method)
		}
	}
	server := httptest.NewServer(h)
	defer server.Close()
	ws, err := websocket.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/api/rpc2", "", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if err = websocket.Message.Send(ws, `{"jsonrpc":"2.0","id":1,"method":"rpc.ping"}`); err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if err = websocket.JSON.Receive(ws, &response); err != nil {
		t.Fatal(err)
	}
	if response["result"] != "pong" {
		t.Fatalf("websocket health check: %#v", response)
	}
}

type retentionReader struct {
	fixtureReader
	definitions []probedata.Definition
}

func (f *retentionReader) Definitions(context.Context, probedata.Scope) ([]probedata.Definition, error) {
	return f.definitions, nil
}

func TestPublicHistoryCapabilitiesUseHoursAndVisibleDefinitions(t *testing.T) {
	r := &retentionReader{definitions: []probedata.Definition{
		{Key: "cpu.usage", History: true, RetentionDays: 14},
		{Key: "ping.latency_ms", History: true, RetentionDays: 7},
		{Key: "unavailable", History: false, RetentionDays: 999},
	}}
	h := New(Deps{Reader: r, Gate: func(*http.Request) bool { return true }})
	w := doHTTP(t, h, "GET", "/api/public", "")
	settings := jsonObject(t, w.Body.Bytes())["data"].(map[string]any)
	if settings["record_enabled"] != true || settings["record_preserve_time"] != float64(336) || settings["ping_record_preserve_time"] != float64(168) {
		t.Fatalf("history disabled or wrong retention units: %#v", settings)
	}
	r.definitions = nil
	settings = rpc(t, h, "common:getPublicInfo", nil)["result"].(map[string]any)
	if settings["record_enabled"] != false || settings["record_preserve_time"] != float64(0) || settings["ping_record_preserve_time"] != float64(0) {
		t.Fatalf("hidden history advertised: %#v", settings)
	}
}

type expandedPingReader struct {
	fixtureReader
	tasks []probedata.PingTask
}

func (f *expandedPingReader) Snapshot(ctx context.Context, scope probedata.Scope) (probedata.Snapshot, error) {
	s, err := f.fixtureReader.Snapshot(ctx, scope)
	s.PingTasks = f.tasks
	return s, err
}

func TestMetricBudgetIncludesSelectedPingTaskExpansion(t *testing.T) {
	f := &expandedPingReader{}
	for i := int64(1); i <= 9; i++ {
		f.tasks = append(f.tasks, probedata.PingTask{ID: "task:" + strconv.FormatInt(i, 10), LegacyID: i, EntityIDs: []string{fixtureUUID}})
	}
	// Unrelated assignments must not be counted against this node's query.
	for i := int64(10); i <= 100; i++ {
		f.tasks = append(f.tasks, probedata.PingTask{ID: "task:" + strconv.FormatInt(i, 10), LegacyID: i, EntityIDs: []string{"other-node"}})
	}
	h := New(Deps{Reader: f, Gate: func(*http.Request) bool { return true }, Now: func() time.Time { return fixtureTime }})
	for _, params := range []map[string]any{
		{"metric_keys": []string{"ping.latency_ms"}, "max_points": 4000},
		{"metric_keys": []string{"cpu.usage", "ping.loss"}, "max_points": 4000},
		{"metric_keys": []string{"ping.latency_ms", "ping.loss"}, "max_points_by_metric": map[string]int{"ping.latency_ms": 2000, "ping.loss": 2000}},
	} {
		result := rpc(t, h, "public:queryMetrics", params)
		if result["error"] == nil || len(f.queries) != 0 {
			t.Fatalf("expanded query reached history reader: %#v", result)
		}
	}
	result := rpc(t, h, "public:queryMetrics", map[string]any{"metric_keys": []string{"ping.latency_ms", "ping.loss"}, "max_points": 4000, "tags": map[string]string{"task_id": "1"}})
	if result["error"] != nil || len(f.queries) != 1 || f.queries[0].Tags["task_id"] != "task:1" {
		t.Fatalf("single numeric task filter not applied before budgeting: %#v", result)
	}
	result = rpc(t, h, "public:queryMetrics", map[string]any{"metric_keys": []string{"ping.latency_ms"}, "max_points": 3000})
	if result["error"] != nil || len(f.queries) != 2 {
		t.Fatalf("unrelated tasks incorrectly counted: %#v", result)
	}
	if got := selectedPingSeries([]probedata.PingTask{{ID: "one", EntityIDs: []string{"a"}}, {ID: "two", EntityIDs: []string{"b"}}}, []string{"a", "b"}, ""); got != 2 {
		t.Fatalf("sparse assignments expanded to a Cartesian product: %d", got)
	}
	result = rpc(t, h, "public:queryMetrics", map[string]any{"metric_key": "ping.latency_ms", "max_points": 6000})
	if result["error"] == nil || len(f.queries) != 2 {
		t.Fatalf("clamping bypassed the expanded aggregate budget: %#v", result)
	}
}

func TestLargeThemePointRequestsClampWithoutUnboundedQueries(t *testing.T) {
	f := &fixtureReader{}
	h := fixtureHandler(f)
	for _, params := range []map[string]any{
		{"metric_key": "ping.latency_ms", "entity_id": fixtureUUID, "max_points": 6000},
		{"metric_key": "cpu.usage", "entity_id": fixtureUUID, "max_points_by_metric": map[string]int{"cpu.usage": 1000000000}},
	} {
		out := rpc(t, h, "public:queryMetrics", params)
		if out["error"] != nil {
			t.Fatalf("original theme request rejected: %#v", out)
		}
		result := out["result"].(map[string]any)
		if result["captain"].(map[string]any)["request_limited"] != true {
			t.Fatal("clamping metadata missing")
		}
		q := f.queries[len(f.queries)-1]
		if q.MaxPoints > 4000 || q.MaxPointsByMetric[q.MetricKeys[0]] != 4000 {
			t.Fatalf("unbounded backend query: %#v", q)
		}
	}
	for _, kind := range []string{"load", "ping"} {
		out := rpc(t, h, "common:getRecords", map[string]any{"type": kind, "uuid": fixtureUUID, "load_type": "cpu", "maxCount": 6000, "max_count": 6000})
		if out["error"] != nil {
			t.Fatalf("legacy theme request rejected: %#v", out)
		}
		metadata := out["result"].(map[string]any)["captain"].(map[string]any)
		if metadata["request_limited"] != true || metadata["effective_max_count"] != float64(4000) {
			t.Fatalf("legacy clamp metadata: %#v", metadata)
		}
		if q := f.queries[len(f.queries)-1]; q.MaxPoints > 4000 {
			t.Fatalf("unbounded legacy query: %#v", q)
		}
	}
	for _, value := range []any{-2, 1.5} {
		if rpc(t, h, "public:queryMetrics", map[string]any{"metric_key": "cpu.usage", "max_points": value})["error"] == nil {
			t.Fatal("invalid point count accepted")
		}
		if rpc(t, h, "common:getRecords", map[string]any{"uuid": fixtureUUID, "maxCount": value})["error"] == nil {
			t.Fatal("invalid record count accepted")
		}
	}
}

func TestJSONRPCValidationAndReadOnlyAllowlist(t *testing.T) {
	h := fixtureHandler(&fixtureReader{})
	for _, method := range []string{"admin:getNodes", "admin:exec", "agent:report", "common:execute", "rpc.discover", "getNodes"} {
		out := rpc(t, h, method, nil)
		if out["error"].(map[string]any)["code"] != float64(-32601) {
			t.Fatalf("method accepted %s", method)
		}
	}
	for _, body := range []string{`null`, `[]`, `{"jsonrpc":"1.0","id":1,"method":"public:getMe"}`, `{"jsonrpc":"2.0","id":{},"method":"public:getMe"}`} {
		w := doHTTP(t, h, "POST", "/api/rpc2", body)
		if jsonObject(t, w.Body.Bytes())["error"] == nil {
			t.Fatalf("invalid accepted %s", body)
		}
	}
	w := doHTTP(t, h, "POST", "/api/rpc2", `{"jsonrpc":"2.0","method":"public:getVersion"}`)
	if w.Code != 204 || w.Body.Len() != 0 {
		t.Fatalf("notification wrote response %d %s", w.Code, w.Body.String())
	}
	w = doHTTP(t, h, "POST", "/api/rpc2", `[{"jsonrpc":"2.0","method":"public:getVersion"},{"jsonrpc":"2.0","id":"kept","method":"public:getVersion"},null]`)
	var batch []map[string]any
	if json.Unmarshal(w.Body.Bytes(), &batch) != nil || len(batch) != 2 || batch[0]["id"] != "kept" || batch[1]["error"] == nil {
		t.Fatalf("batch response %s", w.Body.String())
	}
	w = doHTTP(t, h, "POST", "/api/rpc2", `{"jsonrpc":"2.0","id":null,"method":"public:getVersion"}`)
	if jsonObject(t, w.Body.Bytes())["result"] == nil {
		t.Fatal("explicit null id must get a response")
	}
}

func TestRequestBoundsAndGate(t *testing.T) {
	h := fixtureHandler(&fixtureReader{})
	w := doHTTP(t, h, "POST", "/api/rpc2", strings.Repeat(" ", maxMessage+1))
	if w.Code != 413 {
		t.Fatalf("oversize status %d", w.Code)
	}
	r := httptest.NewRequest("POST", "http://probe.example.com/api/rpc2", strings.NewReader(`{}`))
	r.Header.Set("Origin", "https://evil.example.com")
	r.Header.Set("X-Forwarded-Host", "evil.example.com")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("forwarded host bypassed origin")
	}
	h.d.Gate = nil
	w = doHTTP(t, h, "GET", "/api/nodes", "")
	if w.Code != 403 {
		t.Fatal("missing gate must deny")
	}
	h.d.Gate = func(*http.Request) bool { return false }
	w = doHTTP(t, h, "GET", "/api/public", "")
	if w.Code != 403 {
		t.Fatal("closed site exposed settings")
	}
	for _, path := range []string{"/api/admin", "/api/clients/register", "/api/recent/abc/file", "/api/recent/"} {
		if Handles(path) {
			t.Fatalf("accepted non-public path %s", path)
		}
	}
}

func TestMetricAliasesBudgetsAndUnknowns(t *testing.T) {
	f := &fixtureReader{}
	h := fixtureHandler(f)
	out := rpc(t, h, "public:queryMetrics", map[string]any{"metrics": []string{"cpu.usage"}, "entity_id": fixtureUUID, "hours": "2", "max_points": 100, "algorithm": "avg", "fill_empty": false})
	if out["error"] != nil {
		t.Fatalf("query error %#v", out)
	}
	if len(f.queries) != 1 || f.queries[0].Aggregation != "avg" || f.queries[0].FillEmpty || f.queries[0].End.Sub(f.queries[0].Start) != 2*time.Hour {
		t.Fatalf("query not normalized %#v", f.queries)
	}
	for _, p := range []map[string]any{{"metric_key": "cpu.usage", "max_points": -1}, {"metric_key": "cpu.usage", "entity_id": "hidden"}, {"metric_key": "cpu.usage", "hours": 20000}, {"metric_key": "cpu.usage", "start": "2026-10-29T00:00:00Z"}} {
		if rpc(t, h, "public:queryMetrics", p)["error"] == nil {
			t.Fatalf("invalid query accepted %#v", p)
		}
	}
	f.error = probedata.ErrUnsupportedAggregation
	err := rpc(t, h, "public:queryMetrics", map[string]any{"metric_key": "cpu.usage", "aggregation": "p99"})["error"].(map[string]any)
	if err["code"] != float64(-32602) || err["data"].(map[string]any)["reason"] != "unsupported_aggregation" {
		t.Fatal("unsupported aggregate must be explicit")
	}
}

func TestLegacyRecordsAndPingUseCanonicalQueries(t *testing.T) {
	f := &fixtureReader{}
	h := fixtureHandler(f)
	w := doHTTP(t, h, "GET", "/api/records/load?uuid="+fixtureUUID+"&hours=4&load_type=cpu", "")
	data := jsonObject(t, w.Body.Bytes())["data"].(map[string]any)
	row := data["records"].([]any)[0].(map[string]any)
	if row["cpu"] != float64(12) || row["client"] != fixtureUUID || data["load_type"] != "cpu" {
		t.Fatalf("load wire %s", w.Body.String())
	}
	grouped := rpc(t, h, "common:getRecords", map[string]any{"uuid": fixtureUUID, "load_type": "cpu", "maxCount": 10})["result"].(map[string]any)
	if grouped["records"].(map[string]any)[fixtureUUID] == nil || grouped["from"] == nil || grouped["to"] == nil {
		t.Fatal("common records not grouped")
	}
	w = doHTTP(t, h, "GET", "/api/records/ping?uuid="+fixtureUUID+"&task_id=9000001", "")
	data = jsonObject(t, w.Body.Bytes())["data"].(map[string]any)
	row = data["records"].([]any)[0].(map[string]any)
	if row["task_id"] != float64(9000001) || row["value"] != float64(25) {
		t.Fatalf("ping projection %s", w.Body.String())
	}
	if f.queries[len(f.queries)-1].Tags["task_id"] != "carrier:abc" || f.pingQueries[len(f.pingQueries)-1].TaskIDs[0] != "carrier:abc" {
		t.Fatal("legacy ID did not resolve to canonical series")
	}
	if data["tasks"].([]any)[0].(map[string]any)["loss"] != float64(10) {
		t.Fatal("weighted attempt loss not preserved")
	}
}

func TestLegacyWebSocketAndRevocation(t *testing.T) {
	var enabled atomic.Bool
	enabled.Store(true)
	h := fixtureHandler(&fixtureReader{})
	h.d.Gate = func(*http.Request) bool { return enabled.Load() }
	s := httptest.NewServer(h)
	defer s.Close()
	ws, err := websocket.Dial("ws"+strings.TrimPrefix(s.URL, "http")+"/api/clients", "", s.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if err = websocket.Message.Send(ws, "get"); err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err = websocket.JSON.Receive(ws, &out); err != nil {
		t.Fatal(err)
	}
	data := out["data"].(map[string]any)
	if data["online"].([]any)[0] != fixtureUUID {
		t.Fatal("online list absent")
	}
	rep := data["data"].(map[string]any)[fixtureUUID].(map[string]any)
	if rep["network"].(map[string]any)["totalUp"] != float64(9000) || rep["cpu"].(map[string]any)["usage"] != float64(0) {
		t.Fatal("legacy report semantics changed")
	}
	enabled.Store(false)
	_ = websocket.Message.Send(ws, "get")
	_ = ws.SetReadDeadline(time.Now().Add(time.Second))
	if err = websocket.JSON.Receive(ws, &out); err == nil {
		t.Fatal("revoked socket continued receiving data")
	}
}

func TestRPCWebSocketAndCrossOrigin(t *testing.T) {
	h := fixtureHandler(&fixtureReader{})
	s := httptest.NewServer(h)
	defer s.Close()
	url := "ws" + strings.TrimPrefix(s.URL, "http") + "/api/rpc2"
	if ws, err := websocket.Dial(url, "", "https://evil.example.com"); err == nil {
		ws.Close()
		t.Fatal("cross-origin socket accepted")
	}
	ws, err := websocket.Dial(url, "", s.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if err = websocket.Message.Send(ws, `{"jsonrpc":"2.0","id":"ws","method":"common:getNodesLatestStatus","params":{"uuid":"`+fixtureUUID+`"}}`); err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err = websocket.JSON.Receive(ws, &out); err != nil {
		t.Fatal(err)
	}
	if out["id"] != "ws" || out["result"].(map[string]any)["net_total_up"] != float64(9000) {
		t.Fatalf("rpc websocket output %#v", out)
	}
	if err = websocket.Message.Send(ws, `{"jsonrpc":"2.0","id":2,"method":"admin:exec"}`); err != nil {
		t.Fatal(err)
	}
	if err = websocket.JSON.Receive(ws, &out); err != nil {
		t.Fatal(err)
	}
	if out["error"].(map[string]any)["code"] != float64(-32601) {
		t.Fatal("websocket admitted admin method")
	}
}

func TestIdleSocketRevokesWithoutAnotherMessage(t *testing.T) {
	var enabled atomic.Bool
	enabled.Store(true)
	h := fixtureHandler(&fixtureReader{})
	h.d.Gate = func(*http.Request) bool { return enabled.Load() }
	s := httptest.NewServer(h)
	defer s.Close()
	ws, err := websocket.Dial("ws"+strings.TrimPrefix(s.URL, "http")+"/api/clients", "", s.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	enabled.Store(false)
	start := time.Now()
	_ = ws.SetReadDeadline(start.Add(8 * time.Second))
	var out []byte
	if err = websocket.Message.Receive(ws, &out); err == nil {
		t.Fatal("idle revoked connection was not closed")
	}
	if time.Since(start) > 7*time.Second {
		t.Fatal("idle connection only closed at the client deadline")
	}
}

func TestThemeSettingsReadFailureDoesNotReturnDefaults(t *testing.T) {
	h := fixtureHandler(&fixtureReader{})
	h.d.PublicSettings = func(*http.Request) (map[string]any, error) { return nil, io.ErrUnexpectedEOF }
	w := doHTTP(t, h, "GET", "/api/public", "")
	if w.Code != 500 || jsonObject(t, w.Body.Bytes())["status"] != "error" {
		t.Fatalf("settings failure rendered fake defaults: %d %s", w.Code, w.Body.String())
	}
}

func TestConcurrentPolicyChangeFailsClosed(t *testing.T) {
	h := fixtureHandler(&fixtureReader{})
	h.d.PublicSettings = func(*http.Request) (map[string]any, error) { return nil, probedata.ErrPolicyChanged }
	w := doHTTP(t, h, "GET", "/api/public", "")
	if w.Code != http.StatusServiceUnavailable || jsonObject(t, w.Body.Bytes())["data"] != nil {
		t.Fatalf("policy change returned a stale success: %d %s", w.Code, w.Body.String())
	}
	result := rpc(t, h, "public:getPublicSettings", nil)
	e := result["error"].(map[string]any)
	if result["result"] != nil || e["code"] != float64(-32004) || e["data"].(map[string]any)["reason"] != "policy_changed" {
		t.Fatalf("policy change must be a typed retryable error: %#v", result)
	}
}

func TestHTTPResponseIsNeverCacheable(t *testing.T) {
	h := fixtureHandler(&fixtureReader{})
	s := httptest.NewServer(h)
	defer s.Close()
	resp, err := http.Get(s.URL + "/api/nodes")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.Header.Get("Cache-Control") != "no-store" || !bytes.Contains(raw, []byte(fixtureUUID)) {
		t.Fatal("read response missing no-store")
	}
}

func TestModernPingPercentilesAndMetricDefaults(t *testing.T) {
	f := &fixtureReader{}
	h := fixtureHandler(f)
	stats := rpc(t, h, "public:getPingMetricStats", map[string]any{"uuid": fixtureUUID, "task_id": 9000001})["result"].(map[string]any)["stats"].([]any)[0].(map[string]any)
	if stats["task_id"] != "9000001" || stats["loss"] != float64(10) || stats["p99_p50_ratio"] != float64(.5) {
		t.Fatalf("stats units/IDs changed: %#v", stats)
	}
	if stats["captain"].(map[string]any)["p99_p50_ratio"] != float64(1.5) {
		t.Fatal("native quotient must remain separately available")
	}
	out := rpc(t, h, "public:queryMetrics", map[string]any{"metric_key": "ping.latency_ms", "entity_id": fixtureUUID, "tags": map[string]string{"task_id": "9000001"}, "algorithm": " Mean "})
	ser := out["result"].(map[string]any)["series"].([]any)[0].(map[string]any)
	if ser["tags"].(map[string]any)["task_id"] != "9000001" {
		t.Fatal("metric task does not match public catalog")
	}
	q := f.queries[len(f.queries)-1]
	if q.FillEmpty || q.End.Sub(q.Start) != 4*time.Hour || q.Aggregation != "avg" {
		t.Fatalf("Komari query defaults changed: %#v", q)
	}
}

func TestCanonicalReaderThroughRealRESTAndRPC(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "probe.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err = db.Migrate(ctx, conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	st := store.New(conn)
	settings := store.ProbeSettings{Enabled: true, Title: "Example", Visibility: "public"}
	settings.Normalize()
	if err = st.SetSetting(ctx, store.SettingProbe, settings); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(`INSERT INTO nodes(id,name,token_hash,public_addr,created_at,updated_at) VALUES(1,'Public','paired','192.0.2.10',0,0),(2,'Hidden','paired2','192.0.2.11',0,0)`); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(`UPDATE nodes SET probe_hidden=1 WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	at := fixtureTime.Add(-2 * time.Minute)
	status := spec.SystemStatus{Valid: &spec.MetricValidity{CPU: true, Memory: true, Network: true}, CPUPercent: 0, MemTotal: 2048, MemUsed: 1024, NetUp: 50, NetDown: 60, NetTotalUp: 9000, NetTotalDown: 8000}
	if err = st.RecordBeat(ctx, 1, status, at); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(status)
	if _, err = conn.Exec(`UPDATE nodes SET monitor_at=?,monitor_host_json=?,last_seen_at=? WHERE id=1`, at.Unix(), string(raw), fixtureTime.Unix()); err != nil {
		t.Fatal(err)
	}
	engine := &probedata.Engine{Store: st, SiteName: "Example", Now: func() time.Time { return fixtureTime }}
	h := New(Deps{Reader: engine, Gate: func(*http.Request) bool { return true }, Now: func() time.Time { return fixtureTime }})
	w := doHTTP(t, h, "GET", "/api/nodes", "")
	body := jsonObject(t, w.Body.Bytes())
	nodes := body["data"].([]any)
	if len(nodes) != 1 || strings.Contains(w.Body.String(), "192.0.2.") || strings.Contains(w.Body.String(), "paired") {
		t.Fatal("projection leaked private inventory")
	}
	id := nodes[0].(map[string]any)["uuid"].(string)
	latest := rpc(t, h, "common:getNodesLatestStatus", map[string]any{"uuid": id})["result"].(map[string]any)
	if latest["cpu"] != float64(0) || latest["net_total_up"] != float64(9000) || latest["ram_total"] != float64(2048) {
		t.Fatalf("actual current metrics %#v", latest)
	}
	w = doHTTP(t, h, "GET", "/api/recent/"+id, "")
	recent := jsonObject(t, w.Body.Bytes())["data"].([]any)
	if len(recent) != 1 || recent[0].(map[string]any)["ram"].(map[string]any)["total"] != float64(2048) {
		t.Fatalf("recorded historical totals lost %s", w.Body.String())
	}
	query := rpc(t, h, "public:queryMetrics", map[string]any{"metric_key": "cpu.usage", "entity_id": id, "hours": 1, "max_points": 20})
	if query["error"] != nil {
		t.Fatalf("real metric query failed %#v", query)
	}
	series := query["result"].(map[string]any)["series"].([]any)
	if len(series) != 1 || series[0].(map[string]any)["points"].([]any)[0].(map[string]any)["value"] != float64(0) {
		t.Fatal("recorded zero lost in RPC")
	}
	w = doHTTP(t, h, "GET", "/api/records/load?uuid="+id+"&load_type=ram", "")
	data := jsonObject(t, w.Body.Bytes())["data"].(map[string]any)
	rows := data["records"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["ram_total"] != float64(2048) {
		t.Fatalf("stored historical capacity not projected %s", w.Body.String())
	}
	// Record two more observations in the same minute, forcing real stored
	// aggregation before projection: interval traffic sums, counters keep last,
	// and rates average. A shared average would produce incorrect accounting.
	status.NetTotalUp, status.NetTotalDown, status.NetUp = 9010, 8020, 60
	if err = st.RecordBeat(ctx, 1, status, at.Add(10*time.Second)); err != nil {
		t.Fatal(err)
	}
	status.NetTotalUp, status.NetTotalDown, status.NetUp = 9030, 8060, 70
	if err = st.RecordBeat(ctx, 1, status, at.Add(20*time.Second)); err != nil {
		t.Fatal(err)
	}
	w = doHTTP(t, h, "GET", "/api/records/load?uuid="+id+"&load_type=network", "")
	data = jsonObject(t, w.Body.Bytes())["data"].(map[string]any)
	rows = data["records"].([]any)
	if len(rows) != 1 {
		t.Fatalf("expected one aggregated minute: %s", w.Body.String())
	}
	row := rows[0].(map[string]any)
	if row["traffic_up"] != float64(30) || row["traffic_down"] != float64(60) || row["net_total_up"] != float64(9030) || row["net_total_down"] != float64(8060) || row["net_out"] != float64(60) {
		t.Fatalf("legacy network aggregation changed counter semantics: %#v", row)
	}
}
