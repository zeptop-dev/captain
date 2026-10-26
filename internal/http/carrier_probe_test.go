package http

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestCarrierProbeSettingsCompatibilityAndValidation(t *testing.T) {
	r := newRig(t)
	path := "/api/admin/settings/probe"
	put := func(value any, want int) store.ProbeSettings {
		t.Helper()
		code, body, _ := r.c.do("PUT", path, value, nil)
		if code != want {
			t.Fatalf("settings: %d %s", code, body)
		}
		if code != 200 {
			return store.ProbeSettings{}
		}
		return mustJSON[store.ProbeSettings](t, body)
	}
	// An installation with custom TCP targets retains them on its first read.
	legacy := []spec.Carrier{{Name: "Custom", Addr: "private.example.com:443"}}
	for _, replacement := range [][]spec.Carrier{legacy, {}} {
		if err := r.st.SetSetting(context.Background(), store.SettingProbe, store.ProbeSettings{Carriers: []spec.Carrier{{Name: "Custom", Addr: "[2001:db8::1]:80"}}}); err != nil {
			t.Fatal(err)
		}
		got := put(map[string]any{"enabled": true, "carrier_ping": true, "carriers": replacement}, 200)
		if got.CarrierProbe.Family != "ipv4" || len(got.CarrierProbe.TCPIPv6) != 0 || len(got.CarrierProbe.TCPIPv4) != len(replacement) {
			t.Fatal("first old-client replacement retained obsolete IPv6 targets", got.CarrierProbe)
		}
	}
	if err := r.st.SetSetting(context.Background(), store.SettingProbe, store.ProbeSettings{Carriers: legacy}); err != nil {
		t.Fatal(err)
	}
	_, body, _ := r.c.do("GET", path, nil, nil)
	initial := mustJSON[store.ProbeSettings](t, body)
	if initial.CarrierProbe == nil || initial.CarrierProbe.Protocol != "tcp" || initial.CarrierProbe.Family != "ipv4" || !reflect.DeepEqual(initial.CarrierProbe.TCPIPv4, legacy) {
		t.Fatalf("legacy migration: %s", body)
	}
	config := spec.CarrierProbeConfig{
		Protocol: "icmp", Family: "dual",
		TCPIPv4: legacy, TCPIPv6: []spec.Carrier{{Name: "Custom", Addr: "[2001:db8::1]:443"}},
		ICMPIPv4: []spec.Carrier{{Name: "Custom", Addr: "198.51.100.20"}}, ICMPIPv6: []spec.Carrier{{Name: "Custom", Addr: "2001:db8::2"}},
	}
	got := put(map[string]any{"enabled": true, "carrier_ping": true, "carrier_probe": config}, 200)
	if !reflect.DeepEqual(*got.CarrierProbe, config) {
		t.Fatal("four lists lost", got.CarrierProbe)
	}
	for _, oldRequest := range []map[string]any{
		{"enabled": true, "carrier_ping": true, "title": "Updated"},
		{"enabled": true, "carrier_ping": true, "carrier_probe": nil, "carriers": nil},
	} {
		got = put(oldRequest, 200)
		if !reflect.DeepEqual(*got.CarrierProbe, config) {
			t.Fatal("omission/null overwrote carrier settings", got.CarrierProbe)
		}
	}
	got = put(map[string]any{"enabled": true, "carrier_ping": true, "carriers": []spec.Carrier{}}, 200)
	if len(got.CarrierProbe.TCPIPv4) != 0 || !reflect.DeepEqual(got.CarrierProbe.ICMPIPv6, config.ICMPIPv6) || got.CarrierProbe.Protocol != "icmp" || got.CarrierProbe.Family != "dual" {
		t.Fatal("legacy explicit clearing affected other lists or modes", got.CarrierProbe)
	}
	for _, invalid := range []spec.CarrierProbeConfig{
		{Protocol: "udp"}, {Family: "any"},
		{TCPIPv4: []spec.Carrier{{Name: "CT", Addr: "host.example.com"}}},
		{ICMPIPv4: []spec.Carrier{{Name: "CT", Addr: "host.example.com:80"}}},
		{ICMPIPv6: []spec.Carrier{{Name: "CT", Addr: "198.51.100.20"}}},
		{TCPIPv4: []spec.Carrier{{Name: "<img>", Addr: "host.example.com:80"}}},
	} {
		put(map[string]any{"enabled": true, "carrier_probe": invalid}, 400)
	}
	// The legacy boolean must be off even though the new object enables probes:
	// an old binary ignores the object rather than measuring the wrong protocol.
	_, body, _ = r.agent.do("GET", "/api/agent/state", nil, nil)
	state := mustJSON[agentproto.State](t, body)
	if state.Probe == nil || state.Probe.CarrierPing || state.Probe.CarrierProbe == nil || state.Probe.CarrierProbe.Protocol != "icmp" || len(state.Probe.CarrierProbe.Tasks()) != 2 {
		t.Fatalf("agent contract: %s", body)
	}
	put(map[string]any{"enabled": true, "carrier_ping": false}, 200)
	_, body, _ = r.agent.do("GET", "/api/agent/state", nil, nil)
	state = mustJSON[agentproto.State](t, body)
	if state.Probe.CarrierProbe != nil || state.Probe.CarrierPing {
		t.Fatal("disabled carrier task still sent")
	}
}

func TestCarrierProbePublicSeriesAndUpgradeState(t *testing.T) {
	r := newRig(t)
	anon := &client{t: t, srv: r.srv}
	config := spec.CarrierProbeConfig{Protocol: "tcp", Family: "dual"}
	set := func() {
		t.Helper()
		code, body, _ := r.c.do("PUT", "/api/admin/settings/probe", map[string]any{"enabled": true, "carrier_ping": true, "carrier_probe": config, "public_sections": []string{"latency", "history"}}, nil)
		if code != 200 {
			t.Fatalf("settings: %d %s", code, body)
		}
	}
	set()
	type taskView struct {
		ID                                        int64 `json:"id"`
		Name, Series, Family, Type, State, Reason string
	}
	type nodeView struct {
		Host  spec.SystemStatus `json:"host"`
		Tasks []taskView        `json:"ping_tasks"`
	}
	read := func() nodeView {
		t.Helper()
		// Staff requests bypass the short shared response cache. Privacy is
		// otherwise identical for latency, and anonymous is checked below.
		code, body, _ := r.c.do("GET", "/api/probe", nil, nil)
		if code != 200 || strings.Contains(string(body), "endpoint.nxtrace.org") || strings.Contains(string(body), "private.example.com") {
			t.Fatalf("public targets leaked: %d %s", code, body)
		}
		doc := mustJSON[struct {
			Nodes []nodeView `json:"nodes"`
		}](t, body)
		if len(doc.Nodes) != 1 {
			t.Fatalf("missing node: %s", body)
		}
		return doc.Nodes[0]
	}
	beat := func(host spec.SystemStatus) {
		t.Helper()
		if code, body, _ := r.agent.do("POST", "/api/agent/beat", agentproto.Beat{Host: host}, nil); code != 204 {
			t.Fatalf("beat: %d %s", code, body)
		}
	}
	at := time.Now()
	old := spec.SystemStatus{Pings: []spec.PingResult{{Name: "CT", LatencyMs: 7, At: at.Unix()}}}
	beat(old)
	view := read()
	if len(view.Tasks) != 6 || len(view.Host.Pings) != 0 {
		t.Fatal("legacy results used for new tasks", view)
	}
	for _, task := range view.Tasks {
		if task.State != "unsupported" || task.Reason != "agent_upgrade_required" || !spec.ValidCarrierSeries(task.Series) || task.Type != "tcp" {
			t.Fatal("missing upgrade indication or task metadata", task)
		}
	}
	host := spec.SystemStatus{CarrierProbeVersion: 1, Resources: &spec.Resources{Epoch: "run", Sequence: 1, At: at.UnixMilli()}}
	makeResult := func(task spec.PingTask) spec.PingResult {
		latency, outcome := 11.0, "ok"
		if task.Family == "ipv6" {
			latency, outcome = -1, "timeout"
		}
		m := spec.ProbeMeasurement{Sequence: 1, At: at.Unix(), LatencyMs: latency, Outcome: outcome, DurationMs: 20}
		return spec.PingResult{Name: task.Name, Family: task.Family, Series: task.Series, At: at.Unix(), LatencyMs: latency, Quality: &spec.PingQuality{ProbeMeasurement: m, Epoch: "run", Type: "tcp_reachability", Recent: []spec.ProbeMeasurement{m}}}
	}
	for _, task := range config.Tasks() {
		host.Pings = append(host.Pings, makeResult(task))
	}
	stale := config.Tasks()[0]
	stale.Series = spec.CarrierSeries("tcp", "ipv4", "CT", "private.example.com:80")
	host.Pings = append(host.Pings, makeResult(stale), old.Pings[0])
	beat(host)
	view = read()
	if len(view.Host.Pings) != 6 || view.Host.CarrierProbeVersion != 1 {
		t.Fatal("current v4/v6 data missing or stale series included", view.Host)
	}
	for _, task := range view.Tasks {
		if task.State != "" || task.Reason != "" {
			t.Fatal("received sample still marked waiting", task)
		}
	}
	path := "/api/probe/nodes/" + itoa(r.nodeID) + "/pings?range=1h"
	_, body, _ := anon.do("GET", path, nil, nil)
	points := mustJSON[struct {
		Points []store.PingPoint `json:"points"`
	}](t, body).Points
	if len(points) != 6 {
		t.Fatalf("history series isolation: %s", body)
	}
	for _, point := range points {
		if point.Family == "ipv6" && (point.AvgMs != -1 || point.Lost != 1) {
			t.Fatal("IPv6 timeout became zero or IPv4 success", point)
		}
	}
	config.Protocol = "icmp"
	set()
	view = read()
	if len(view.Host.Pings) != 0 || len(view.Tasks) != 6 || view.Tasks[0].Reason != "waiting" {
		t.Fatal("TCP data reused after changing to ICMP", view)
	}
	_, body, _ = anon.do("GET", path, nil, nil)
	points = mustJSON[struct {
		Points []store.PingPoint `json:"points"`
	}](t, body).Points
	if len(points) != 0 {
		t.Fatalf("old protocol history reused: %s", body)
	}
	_, body, _ = anon.do("GET", "/api/probe", nil, nil)
	if strings.Contains(string(body), "endpoint.nxtrace.org") || strings.Contains(string(body), "private.example.com") {
		t.Fatal("anonymous response disclosed targets")
	}
}
