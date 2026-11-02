package http

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestInterfaceProbeAPICompatibilityAndDelivery(t *testing.T) {
	r := newRig(t)
	if code, b, _ := r.c.do("PUT", "/api/admin/settings/probe", map[string]any{"enabled": true, "carrier_ping": false}, nil); code != 200 {
		t.Fatalf("settings %d %s", code, b)
	}
	save := func(method, path string, v any, want int) store.PingTask {
		t.Helper()
		code, b, _ := r.c.do(method, path, v, nil)
		if code != want {
			t.Fatalf("save %d %s", code, b)
		}
		if code != 200 {
			return store.PingTask{}
		}
		return mustJSON[store.PingTask](t, b)
	}
	value := func() map[string]any {
		return map[string]any{"name": "WAN RTT", "type": "tcp", "target": "198.51.100.20:443", "source_interface": "pppoe-wan", "interval_seconds": 30, "node_ids": []int64{r.nodeID}, "enabled": true}
	}
	for _, bad := range []map[string]any{{"node_ids": []int64{}}, {"node_ids": []int64{999999}}, {"target": "probe.example.com:443"}, {"source_interface": "wan/other"}, {"source_interface": "a-interface-too-long"}, {"type": "http", "target": "https://198.51.100.20/"}} {
		v := value()
		for k, x := range bad {
			v[k] = x
		}
		save("POST", "/api/admin/ping-tasks", v, 400)
	}
	task := save("POST", "/api/admin/ping-tasks", value(), 200)
	path := "/api/admin/ping-tasks/" + itoa(task.ID)
	// Old editors omit new fields. Their toggle must never turn binding off.
	for _, null := range []bool{false, true} {
		v := value()
		delete(v, "source_interface")
		if null {
			v["source_interface"] = nil
		}
		v["enabled"] = false
		got := save("PATCH", path, v, 200)
		if got.SourceInterface != "pppoe-wan" || got.Enabled {
			t.Fatal("old editor changed path", got)
		}
	}
	task = save("PATCH", path, value(), 200)
	legacy := save("POST", "/api/admin/ping-tasks", map[string]any{"name": "default", "type": "tcp", "target": "probe.example.com:443", "enabled": true}, 200)
	state := func() (agentproto.State, []byte) {
		t.Helper()
		code, b, _ := r.agent.do("GET", "/api/agent/state", nil, nil)
		if code != 200 {
			t.Fatalf("state %d %s", code, b)
		}
		return mustJSON[agentproto.State](t, b), b
	}
	s, _ := state()
	if s.Probe == nil || len(s.Probe.InterfaceTasks) != 0 || len(s.Probe.Tasks) != 1 || s.Probe.Tasks[0].ID != legacy.ID {
		t.Fatal("unsupported agent received bound task", s.Probe)
	}
	beat := func(cap int) {
		t.Helper()
		code, b, _ := r.agent.do("POST", "/api/agent/beat", agentproto.Beat{Host: spec.SystemStatus{ProbeInterfaceVersion: cap}}, nil)
		if code != 204 {
			t.Fatalf("beat %d %s", code, b)
		}
	}
	beat(1)
	s, wire := state()
	if len(s.Probe.InterfaceTasks) != 1 || s.Probe.InterfaceTasks[0] != task.AgentTask() || s.Probe.InterfaceTasks[0].Family != "ipv4" {
		t.Fatalf("missing exact path %+v", s.Probe)
	}
	// A downgraded agent with a stale persisted capability sees only Tasks.
	var old struct {
		Probe struct {
			Tasks []struct {
				ID int64 `json:"id"`
			} `json:"tasks"`
		} `json:"probe"`
	}
	if err := json.Unmarshal(wire, &old); err != nil {
		t.Fatal(err)
	}
	if len(old.Probe.Tasks) != 1 || old.Probe.Tasks[0].ID != legacy.ID {
		t.Fatal("legacy decoder executes bound task", old)
	}
	beat(0)
	s, _ = state()
	if len(s.Probe.InterfaceTasks) != 0 {
		t.Fatal("capability downgrade not observed")
	}
	v := value()
	v["target"] = "[2001:db8::1]:443"
	task = save("PATCH", path, v, 200)
	if task.AgentTask().Family != "ipv6" {
		t.Fatal("IPv6 family", task)
	}
	v["source_interface"] = ""
	task = save("PATCH", path, v, 200)
	if task.SourceInterface != "" {
		t.Fatal("explicit clear ignored")
	}
	save("PATCH", "/api/admin/ping-tasks/999999", value(), 404)
}

func TestInterfaceProbeCurrentHistoryAndPublicPrivacy(t *testing.T) {
	r := newRig(t)
	r.c.do("PUT", "/api/admin/settings/probe", map[string]any{"enabled": true, "carrier_ping": false, "public_sections": []string{"latency", "history"}}, nil)
	v := map[string]any{"name": "WAN probe", "type": "icmp", "target": "198.51.100.20", "source_interface": "pppoe-wan", "interval_seconds": 30, "node_ids": []int64{r.nodeID}, "enabled": true}
	code, b, _ := r.c.do("POST", "/api/admin/ping-tasks", v, nil)
	if code != 200 {
		t.Fatalf("task %d %s", code, b)
	}
	task := mustJSON[store.PingTask](t, b)
	wire := task.AgentTask()
	at := time.Now()
	m := spec.ProbeMeasurement{Sequence: 1, At: at.Unix(), LatencyMs: 12, Outcome: "ok"}
	result := spec.PingResult{TaskID: task.ID, Name: task.Name, Series: wire.Series, Family: wire.Family, At: at.Unix(), LatencyMs: 12, Quality: &spec.PingQuality{ProbeMeasurement: m, Epoch: "path-one", Type: "icmp"}}
	beat := func(p spec.PingResult) {
		t.Helper()
		code, b, _ := r.agent.do("POST", "/api/agent/beat", agentproto.Beat{Host: spec.SystemStatus{ProbeInterfaceVersion: 1, Pings: []spec.PingResult{p}}}, nil)
		if code != 204 {
			t.Fatalf("beat %d %s", code, b)
		}
	}
	beat(result)
	history := func(want int) {
		t.Helper()
		code, b, _ := r.c.do("GET", "/api/admin/nodes/"+itoa(r.nodeID)+"/network-quality?range=1h", nil, nil)
		if code != 200 {
			t.Fatalf("history %d %s", code, b)
		}
		doc := mustJSON[struct {
			Current []spec.PingResult `json:"current"`
			Points  []store.PingPoint `json:"points"`
			Paths   map[string]string `json:"task_paths"`
		}](t, b)
		if len(doc.Points) != want || len(doc.Current) != want || doc.Paths[itoa(task.ID)] != v["source_interface"] {
			t.Fatalf("history %+v", doc)
		}
	}
	history(1)
	code, b, _ = r.c.do("GET", "/api/probe", nil, nil)
	if code != 200 || strings.Contains(string(b), "pppoe-wan") || strings.Contains(string(b), "198.51.100.20") {
		t.Fatalf("path leaked %d %s", code, b)
	}
	pub := mustJSON[struct {
		Nodes []struct {
			Host  spec.SystemStatus `json:"host"`
			Tasks []struct {
				Series string `json:"series"`
			} `json:"ping_tasks"`
		} `json:"nodes"`
	}](t, b)
	if len(pub.Nodes) != 1 || len(pub.Nodes[0].Host.Pings) != 1 || len(pub.Nodes[0].Tasks) != 1 || pub.Nodes[0].Tasks[0].Series != wire.Series {
		t.Fatalf("public custom path missing %s", b)
	}
	v["source_interface"] = "eth1"
	code, b, _ = r.c.do("PATCH", "/api/admin/ping-tasks/"+itoa(task.ID), v, nil)
	if code != 200 {
		t.Fatalf("edit %d %s", code, b)
	}
	history(0)
	// Delayed samples from the previous binding cannot populate the new path.
	result.Quality.Sequence++
	result.Quality.At++
	result.At++
	beat(result)
	history(0)
	task = mustJSON[store.PingTask](t, b)
	wire = task.AgentTask()
	result.Series = wire.Series
	result.Quality.Epoch = "path-two"
	result.Quality.Sequence = 1
	beat(result)
	history(1)
}
