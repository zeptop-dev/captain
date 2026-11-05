package http

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestNodeProbeOverrideReachesNativeAndPrivateReaders(t *testing.T) {
	r := newRig(t)
	anon := &client{t: t, srv: r.srv}
	ctx := context.Background()
	if code, b, _ := r.c.do("PUT", "/api/admin/settings/probe", map[string]any{"enabled": true, "page_enabled": true, "carrier_ping": false, "path": "/status", "public_sections": []string{"latency", "history"}}, nil); code != 200 {
		t.Fatalf("settings %d %s", code, b)
	}
	p, err := r.st.NodeProbePlan(ctx, r.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &spec.CarrierProbeConfig{Protocol: "icmp", Family: "ipv4"}
	view, err := r.st.UpdateNodeProbePlan(ctx, r.nodeID, store.NodeProbePlanChange{Revision: p.Revision, Mode: "custom", Custom: cfg}, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	task := cfg.Tasks()[0]
	at := time.Now().Unix()
	m := spec.ProbeMeasurement{At: at, Sequence: 1, LatencyMs: 28, Outcome: "ok", DurationMs: 28}
	host := spec.SystemStatus{CarrierProbeVersion: 1, Resources: &spec.Resources{At: at * 1000, Epoch: "plan", Sequence: 1}, Pings: []spec.PingResult{{At: at, Name: task.Name, Family: task.Family, Series: task.Series, LatencyMs: 28, Quality: &spec.PingQuality{Epoch: "plan", Type: "icmp", ProbeMeasurement: m, Recent: []spec.ProbeMeasurement{m}}}}}
	if code, b, _ := r.agent.do("POST", "/api/agent/beat", agentproto.Beat{Host: host}, nil); code != 204 {
		t.Fatalf("beat %d %s", code, b)
	}
	code, b, _ := anon.do("GET", "/api/probe", nil, nil)
	public := mustJSON[struct {
		Nodes []struct {
			PingTasks []struct {
				Series string `json:"series"`
			} `json:"ping_tasks"`
			Host spec.SystemStatus `json:"host"`
		} `json:"nodes"`
	}](t, b)
	if code != 200 || len(public.Nodes) != 1 || len(public.Nodes[0].PingTasks) != 3 || len(public.Nodes[0].Host.Pings) != 1 || public.Nodes[0].Host.Pings[0].Series != task.Series {
		t.Fatalf("custom plan lost when global default off %d %s", code, b)
	}
	path := "/api/admin/nodes/" + itoa(r.nodeID) + "/network-quality?range=1h"
	code, b, _ = r.c.do("GET", path, nil, nil)
	private := mustJSON[struct {
		Current []spec.PingResult        `json:"current"`
		Plan    store.EffectiveProbePlan `json:"probe_plan"`
	}](t, b)
	if code != 200 || len(private.Current) != 1 || private.Plan.Source != "custom" || !private.Plan.Enabled {
		t.Fatalf("private effective plan %d %s", code, b)
	}
	// Future-boundary edit immediately hides the prior sample in native and admin.
	custom := &spec.CarrierProbeConfig{Protocol: "tcp", Family: "ipv4"}
	code, b, _ = r.c.do("PUT", "/api/admin/settings/nodes/"+itoa(r.nodeID)+"/probe-plan", store.NodeProbePlanChange{Revision: view.Revision, Mode: "custom", Custom: custom}, nil)
	if code != 200 {
		t.Fatalf("edit %d %s", code, b)
	}
	_, b, _ = anon.do("GET", "/api/probe", nil, nil)
	public = mustJSON[struct {
		Nodes []struct {
			PingTasks []struct {
				Series string `json:"series"`
			} `json:"ping_tasks"`
			Host spec.SystemStatus `json:"host"`
		} `json:"nodes"`
	}](t, b)
	if len(public.Nodes[0].Host.Pings) != 0 || len(public.Nodes[0].PingTasks) != 3 || !strings.Contains(string(b), `"reason":"waiting"`) {
		t.Fatalf("old samples relabeled %s", b)
	}
	_, b, _ = r.c.do("GET", path, nil, nil)
	private = mustJSON[struct {
		Current []spec.PingResult        `json:"current"`
		Plan    store.EffectiveProbePlan `json:"probe_plan"`
	}](t, b)
	if len(private.Current) != 0 {
		t.Fatal("admin kept old series", string(b))
	}
}

func TestAddressAdvisoryReportIsPrivateAndNeverAutoFollows(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	if err := r.st.SetAddressPolicy(ctx, r.nodeID, true, true); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Unix()
	f := &agentproto.PublicAddressFallback{Source: agentproto.PublicAddressFallbackSource, Origin: "https://panel.example.com:2096", Address: "203.0.113.30", SampledAt: at}
	report := agentproto.Report{PublicAddresses: &agentproto.PublicAddresses{SampledAt: at, FallbackIPv4: f}}
	if code, b, _ := r.agent.do("POST", "/api/agent/report", report, nil); code != 200 {
		t.Fatalf("report %d %s", code, b)
	}
	code, b, _ := r.c.do("GET", "/api/admin/nodes/"+itoa(r.nodeID)+"/addresses", nil, nil)
	got := mustJSON[store.NodeAddresses](t, b)
	if code != 200 || got.Observed.FallbackIPv4 == nil || got.Observed.FallbackIPv4.Address != f.Address || got.Observed.IPv4.Address != "" || got.DNSPending {
		t.Fatalf("advisory API %d %s", code, b)
	}
	// Additive wire remains harmless to readers that know only legacy fields.
	raw, _ := json.Marshal(report.PublicAddresses)
	var legacy struct {
		IPv4 string `json:"ipv4"`
		IPv6 string `json:"ipv6"`
	}
	if err := json.Unmarshal(raw, &legacy); err != nil || legacy.IPv4 != "" || legacy.IPv6 != "" {
		t.Fatal("legacy consensus fields populated", legacy, err)
	}
}

func TestNetworkQualitySummaryDoesNotDisclosePrivateProbeTargets(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	accessAuditCall(t, r.c, "PUT", "/api/admin/settings/probe", map[string]any{"enabled": true}, 200)
	plan, err := r.st.NodeProbePlan(ctx, r.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &spec.CarrierProbeConfig{Protocol: "tcp", Family: "ipv4", TCPIPv4: []spec.Carrier{{Name: "private-label", Addr: "10.10.0.2:2096"}}}
	_, err = r.st.UpdateNodeProbePlan(ctx, r.nodeID, store.NodeProbePlanChange{Revision: plan.Revision, Mode: "custom", Custom: cfg}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	_, operator := accessAuditStaff(t, r, domain.RoleOperator)
	narrow := accessAuditToken(t, r, 1, "read", []string{"nodes:*"})
	for _, c := range []*client{operator, narrow} {
		accessAuditCall(t, c, "GET", "/api/admin/nodes/"+itoa(r.nodeID)+"/probe-plan", nil, 403)
		b := accessAuditCall(t, c, "GET", "/api/admin/nodes/"+itoa(r.nodeID)+"/network-quality?range=1h", nil, 200)
		if strings.Contains(string(b), "10.10.0.2") || strings.Contains(string(b), "private-label") || strings.Contains(string(b), "tcp_ipv4") {
			t.Fatal("private target leaked", string(b))
		}
		if !strings.Contains(string(b), `"custom_targets":true`) || !strings.Contains(string(b), `"protocol":"tcp"`) {
			t.Fatal("safe summary missing", string(b))
		}
	}
}
