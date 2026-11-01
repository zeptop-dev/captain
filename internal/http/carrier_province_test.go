package http

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/probedata"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestCarrierProvincePersistsForOlderEditorsAndExpandsForOlderAgents(t *testing.T) {
	r := newRig(t)
	const path = "/api/admin/settings/probe"
	put := func(cfg any) store.ProbeSettings {
		t.Helper()
		code, body, _ := r.c.do("PUT", path, map[string]any{"enabled": true, "carrier_ping": true, "carrier_probe": cfg}, nil)
		if code != 200 {
			t.Fatalf("settings: %d %s", code, body)
		}
		return mustJSON[store.ProbeSettings](t, body)
	}
	custom := []spec.Carrier{{Name: "Private", Addr: "private.example.com:443"}}
	cfg := spec.CarrierProbeConfig{Protocol: "tcp", Family: "dual", TCPProvince: "gd", TCPIPv4: custom}
	put(cfg)
	for _, province := range []any{nil, ""} {
		got := put(map[string]any{"protocol": "tcp", "family": "dual", "tcp_ipv4": custom, "tcp_province": province})
		if got.CarrierProbe.TCPProvince != "gd" || !reflect.DeepEqual(got.CarrierProbe.TCPIPv4, custom) {
			t.Fatal("old editor erased province or custom targets", got.CarrierProbe)
		}
	}
	got := put(map[string]any{"protocol": "tcp", "family": "dual", "tcp_ipv4": custom})
	if got.CarrierProbe.TCPProvince != "gd" {
		t.Fatal("omission reset province", got.CarrierProbe)
	}
	code, body, _ := r.agent.do("GET", "/api/agent/state", nil, nil)
	if code != 200 {
		t.Fatalf("state: %d %s", code, body)
	}
	state := mustJSON[agentproto.State](t, body)
	if state.Probe == nil || state.Probe.CarrierProbe == nil || state.Probe.CarrierPing {
		t.Fatal("missing explicit carrier configuration")
	}
	// Decode exactly the pre-province wire fields. Such agents must receive
	// usable addresses rather than fall back to their bundled Beijing defaults.
	var legacy struct {
		Protocol string         `json:"protocol"`
		Family   string         `json:"family"`
		TCPIPv4  []spec.Carrier `json:"tcp_ipv4"`
		TCPIPv6  []spec.Carrier `json:"tcp_ipv6"`
	}
	raw, _ := json.Marshal(state.Probe.CarrierProbe)
	if err := json.Unmarshal(raw, &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.Protocol != "tcp" || legacy.Family != "dual" || !reflect.DeepEqual(legacy.TCPIPv4, custom) || len(legacy.TCPIPv6) != 3 || legacy.TCPIPv6[0].Addr != "gd-ct-v6.ip.zstaticcdn.com:80" {
		t.Fatalf("old agent would probe the wrong targets: %s", raw)
	}
	_, body, _ = r.c.do("GET", path, nil, nil)
	got = mustJSON[store.ProbeSettings](t, body)
	if len(got.CarrierProbe.TCPIPv6) != 0 || len(got.CarrierProbe.ICMPIPv4) != 0 {
		t.Fatal("runtime expansion was persisted as a custom draft")
	}
	cfg.TCPProvince = "bj"
	if got = put(cfg); got.CarrierProbe.TCPProvince != "bj" {
		t.Fatal("explicit reset did not work")
	}
	cfg.TCPProvince = "unknown"
	if code, _, _ = r.c.do("PUT", path, map[string]any{"enabled": true, "carrier_probe": cfg}, nil); code != 400 {
		t.Fatal("unknown province accepted", code)
	}
}

func TestCarrierProvincePublicMetadataAndHistoryIsolation(t *testing.T) {
	r := newRig(t)
	cfg := spec.CarrierProbeConfig{Protocol: "tcp", Family: "dual", TCPProvince: "bj"}
	set := func(sections []string) {
		t.Helper()
		code, body, _ := r.c.do("PUT", "/api/admin/settings/probe", map[string]any{"enabled": true, "carrier_ping": true, "carrier_probe": cfg, "public_sections": sections}, nil)
		if code != 200 {
			t.Fatalf("settings: %d %s", code, body)
		}
	}
	type taskView struct{ Name, Family, Province, Series string }
	read := func() []taskView {
		t.Helper()
		code, body, _ := r.c.do("GET", "/api/probe", nil, nil)
		if code != 200 || strings.Contains(string(body), "ip.zstaticcdn.com") || strings.Contains(string(body), "private.example.com") {
			t.Fatalf("public target leak or failed read: %d %s", code, body)
		}
		doc := mustJSON[struct {
			Nodes []struct {
				Tasks []taskView `json:"ping_tasks"`
			} `json:"nodes"`
		}](t, body)
		if len(doc.Nodes) != 1 {
			t.Fatal("missing node")
		}
		code, body, _ = r.c.do("GET", "/api/probe/v2", nil, nil)
		if code != 200 || strings.Contains(string(body), "ip.zstaticcdn.com") || strings.Contains(string(body), "private.example.com") {
			t.Fatalf("v2 target leak or failed read: %d %s", code, body)
		}
		v2 := mustJSON[probedata.Snapshot](t, body)
		if len(v2.PingTasks) != len(doc.Nodes[0].Tasks) {
			t.Fatal("transport task count differs")
		}
		for i, task := range v2.PingTasks {
			if task.Province != doc.Nodes[0].Tasks[i].Province {
				t.Fatal("transport province differs")
			}
		}
		return doc.Nodes[0].Tasks
	}
	sections := []string{"latency", "history"}
	set(sections)
	for _, task := range read() {
		if task.Province != "bj" {
			t.Fatal("missing preset province", task)
		}
	}
	at := time.Now()
	host := spec.SystemStatus{CarrierProbeVersion: 1, Resources: &spec.Resources{Epoch: "province", Sequence: 1, At: at.UnixMilli()}}
	for _, task := range cfg.Tasks() {
		m := spec.ProbeMeasurement{Sequence: 1, At: at.Unix(), LatencyMs: 12, Outcome: "ok", DurationMs: 12}
		host.Pings = append(host.Pings, spec.PingResult{Name: task.Name, Series: task.Series, Family: task.Family, At: at.Unix(), LatencyMs: 12, Quality: &spec.PingQuality{ProbeMeasurement: m, Epoch: "province", Type: "tcp_reachability", Recent: []spec.ProbeMeasurement{m}}})
	}
	if code, body, _ := r.agent.do("POST", "/api/agent/beat", agentproto.Beat{Host: host}, nil); code != 204 {
		t.Fatalf("beat: %d %s", code, body)
	}
	history := func() []store.PingPoint {
		t.Helper()
		code, body, _ := r.c.do("GET", "/api/probe/nodes/"+itoa(r.nodeID)+"/pings?range=1h", nil, nil)
		if code != 200 {
			t.Fatalf("history: %d %s", code, body)
		}
		return mustJSON[struct {
			Points []store.PingPoint `json:"points"`
		}](t, body).Points
	}
	if len(history()) != 6 {
		t.Fatal("baseline missing")
	}
	cfg.TCPProvince = "gd"
	set(sections)
	for _, task := range read() {
		if task.Province != "gd" {
			t.Fatal("province unchanged", task)
		}
	}
	if len(history()) != 0 {
		t.Fatal("Beijing history reused for Guangdong")
	}
	cfg.TCPProvince = "bj"
	set(sections)
	if len(history()) != 6 {
		t.Fatal("old target history was deleted")
	}
	cfg.TCPIPv4 = []spec.Carrier{{Name: "CT", Addr: "private.example.com:80"}}
	set(sections)
	for _, task := range read() {
		if task.Family == "ipv4" && task.Province != "" {
			t.Fatal("custom target mislabelled as a province")
		}
		if task.Family == "ipv6" && task.Province != "bj" {
			t.Fatal("IPv6 preset province lost")
		}
	}
	cfg.Protocol = "icmp"
	set(sections)
	for _, task := range read() {
		if task.Province != "" {
			t.Fatal("TCP province exposed on ICMP")
		}
	}
	set([]string{"history"})
	if len(read()) != 0 {
		t.Fatal("hidden latency metadata exposed")
	}
}
