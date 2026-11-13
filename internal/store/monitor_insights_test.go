package store

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

func TestRouteWatchReportReplayAndRules(t *testing.T) {
	s, node, _ := monitorRig(t)
	ctx := context.Background()
	base := time.Now().Add(-time.Minute).Unix()
	seq := int64(0)
	policy := RouteWatchPolicy{Expected: map[string]string{"CT": "cn2"}, Confirmations: 2, Notify: true}
	if e := s.SetRouteWatch(ctx, node, policy); e != nil {
		t.Fatal(e)
	}
	s.db.Exec(`UPDATE node_route_watch SET updated_at=? WHERE node_id=?`, base-1, node)
	complete := func(class, rules string, good bool) {
		t.Helper()
		seq++
		id := fmt.Sprintf("route-%d", seq)
		params, _ := json.Marshal(spec.DiagnosticRequest{Type: "return_route", Route: &spec.ReturnRouteRequest{City: "beijing", Family: "ipv4"}})
		if e := s.QueueNetworkDiagnostic(ctx, id, node, spec.DiagnosticRequest{Type: "return_route", Route: &spec.ReturnRouteRequest{City: "beijing", Family: "ipv4"}}); e != nil {
			t.Fatal(e)
		}
		s.db.Exec(`UPDATE node_jobs SET created_at=?,params_json=? WHERE id=?`, base+seq, string(params), id)
		r := spec.DiagnosticResult{Outcome: "ok", Route: &spec.ReturnRouteResult{City: "beijing", Family: "ipv4", RulesVersion: rules, Paths: []spec.RoutePath{{Carrier: "CT", Target: "198.51.100.20", Outcome: "ok", Reached: good, Classification: class}}}}
		b, _ := json.Marshal(r)
		if e := s.CompleteReportedNodeJob(ctx, node, id, b, ""); e != nil {
			t.Fatal(e)
		}
		// Same report must not increase confirmations or add a second event.
		if e := s.CompleteReportedNodeJob(ctx, node, id, b, ""); e != nil {
			t.Fatal(e)
		}
	}
	complete("cn2", "r1", true)
	ev, _ := s.RouteEvents(ctx, node)
	if len(ev) != 0 {
		t.Fatal("first sample confirmed", ev)
	}
	complete("cn2", "r1", true)
	complete("163", "r1", true)
	complete("163", "r1", false)
	complete("163", "r1", true)
	ev, _ = s.RouteEvents(ctx, node)
	if len(ev) != 1 {
		t.Fatal("unknown did not break confirmation", ev)
	}
	complete("163", "r1", true)
	ev, _ = s.RouteEvents(ctx, node)
	if len(ev) != 2 || ev[0].Kind != "changed" || ev[0].Before != "cn2" {
		t.Fatal(ev)
	}
	checks, e := s.RouteAlertChecks(ctx, node, time.Now())
	if e != nil || len(checks) == 0 || !checks[0].Active {
		t.Fatal(checks, e)
	}
	complete("cn2", "r2", true)
	ev, _ = s.RouteEvents(ctx, node)
	if ev[0].Kind != "rules_changed" {
		t.Fatal(ev)
	}
	checks, _ = s.RouteAlertChecks(ctx, node, time.Now())
	for _, c := range checks {
		if c.Active {
			t.Fatal("rules change retained old conclusion", checks)
		}
	}
	complete("cn2", "r2", true)
	ev, _ = s.RouteEvents(ctx, node)
	if ev[0].Kind != "baseline" {
		t.Fatal(ev)
	}
	// New policy fences old pending jobs and clears active confirmation states.
	if e := s.SetRouteWatch(ctx, node, RouteWatchPolicy{Expected: map[string]string{}, Confirmations: 3}); e != nil {
		t.Fatal(e)
	}
	var states int
	s.db.QueryRow(`SELECT COUNT(*) FROM node_route_states WHERE node_id=?`, node).Scan(&states)
	if states != 0 {
		t.Fatal(states)
	}
}
func TestNetworkBaselineFreezeRecoveryIdentity(t *testing.T) {
	s, node, _ := monitorRig(t)
	ctx := context.Background()
	policy := DefaultNetworkAlertPolicy()
	policy.Mode = "adaptive"
	policy.MinSamples = 5
	policy.WindowMinutes = 2
	if e := s.SetNetworkAlertPolicy(ctx, node, policy); e != nil {
		t.Fatal(e)
	}
	base := time.Now().Add(-20 * time.Minute)
	s.db.Exec(`UPDATE node_network_alerts SET updated_at=? WHERE node_id=?`, base.Unix()-1, node)
	task := PingTask{Name: "WAN", Type: "icmp", Target: "198.51.100.20", SourceInterface: "eth0", Enabled: true, IntervalSeconds: 10, NodeIDs: []int64{node}}
	if e := s.SavePingTask(ctx, &task); e != nil {
		t.Fatal(e)
	}
	a := task.AgentTask()
	seq := uint64(0)
	sample := func(ms float64) {
		t.Helper()
		seq++
		at := base.Add(time.Duration(seq) * 10 * time.Second)
		m := spec.ProbeMeasurement{Sequence: seq, At: at.Unix(), LatencyMs: ms, Outcome: "ok"}
		if ms < 0 {
			m.Outcome = "timeout"
		}
		p := spec.PingResult{TaskID: task.ID, Name: task.Name, Series: a.Series, Family: a.Family, At: at.Unix(), Quality: &spec.PingQuality{ProbeMeasurement: m, Epoch: "probe-1", Type: "icmp", IntervalSeconds: 10, Recent: []spec.ProbeMeasurement{m}}}
		h := spec.SystemStatus{Pings: []spec.PingResult{p}, ProbeInterfaceVersion: 1}
		if e := s.RecordBeat(ctx, node, h, at); e != nil {
			t.Fatal(e)
		}
		if e := s.RecordBeat(ctx, node, h, at); e != nil {
			t.Fatal(e)
		}
	}
	for i := 0; i < 14; i++ {
		sample(100)
	}
	states, e := s.NetworkAlertStates(ctx, node)
	if e != nil || len(states) != 1 || !states[0].Ready || states[0].Baseline != 100 {
		t.Fatal(states, e)
	}
	learned := states[0].Learned
	for i := 0; i < 14; i++ {
		sample(500)
	}
	states, _ = s.NetworkAlertStates(ctx, node)
	if !states[0].Frozen || !states[0].LatencyAlert || states[0].Baseline > 101 || states[0].Learned > learned+1 {
		t.Fatal("incident poisoned baseline", states)
	}
	for i := 0; i < 14; i++ {
		sample(100)
	}
	states, _ = s.NetworkAlertStates(ctx, node)
	if states[0].Frozen || states[0].HoldUntil <= states[0].Last {
		t.Fatal("no recovery hold", states)
	}
	for i := 0; i < 14; i++ {
		sample(-1)
	}
	states, _ = s.NetworkAlertStates(ctx, node)
	if !states[0].LossAlert {
		t.Fatal("total loss not detected", states)
	}
	task.Target = "203.0.113.30"
	if e = s.SavePingTask(ctx, &task); e != nil {
		t.Fatal(e)
	}
	if CurrentTaskSample([]PingTask{task}, node, task.ID, task.Name, a.Series, a.Family) {
		t.Fatal("old path survived target edit")
	}
	policy.Mode = "off"
	if e = s.SetNetworkAlertPolicy(ctx, node, policy); e != nil {
		t.Fatal(e)
	}
	states, _ = s.NetworkAlertStates(ctx, node)
	if len(states) != 0 {
		t.Fatal("policy change retained baseline")
	}
}
func TestKindSilenceDoesNotHideOtherIncidents(t *testing.T) {
	s, node, _ := monitorRig(t)
	ctx := context.Background()
	at := time.Now()
	notice, e := s.CheckIncident(ctx, node, IncidentCheck{Kind: "cpu", Active: true, Value: 90}, at)
	if e != nil || notice == nil {
		t.Fatal(notice, e)
	}
	if e = s.SilenceIncident(ctx, notice.ID, 86400, at); e != nil {
		t.Fatal(e)
	}
	if v, e := s.CheckIncident(ctx, node, IncidentCheck{Kind: "cpu"}, at.Add(time.Second)); e != nil || v != nil {
		t.Fatal("recovery escaped silence", v, e)
	}
	if v, e := s.CheckIncident(ctx, node, IncidentCheck{Kind: "disk", Active: true}, at.Add(time.Second)); e != nil || v == nil {
		t.Fatal("other type suppressed", v, e)
	}
	if v, e := s.CheckIncident(ctx, node, IncidentCheck{Kind: "cpu", Active: true}, at.Add(2*time.Second)); e != nil || v != nil {
		t.Fatal(v, e)
	}
	if v, e := s.CheckIncident(ctx, node, IncidentCheck{Kind: "cpu", Active: true}, at.Add(25*time.Hour)); e != nil || v == nil {
		t.Fatal("expiry did not notify continuing event", v, e)
	}
}
func TestMonitorStorageAndMaintenance(t *testing.T) {
	s, _, _ := monitorRig(t)
	ctx := context.Background()
	v, e := s.MonitorStorage(ctx)
	if e != nil || v.DatabaseBytes <= 0 {
		t.Fatal(v, e)
	}
	if _, e = s.MaintainMonitoring(ctx); e != nil {
		t.Fatal(e)
	}
	s.maintenance.Lock()
	defer s.maintenance.Unlock()
	if _, e = s.MaintainMonitoring(ctx); e != ErrMaintenanceBusy {
		t.Fatal(e)
	}
}

func TestMonitorReadBudgetAndCancellation(t *testing.T) {
	s, _, _ := monitorRig(t)
	ctx := context.Background()
	_, release, e := s.AcquireMonitorRead(ctx)
	if e != nil {
		t.Fatal(e)
	}
	c, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, e = s.AcquireMonitorRead(c); e == nil {
		t.Fatal("canceled reader admitted")
	}
	release()
	_, release, e = s.AcquireMonitorRead(ctx)
	if e != nil {
		t.Fatal(e)
	}
	release()
}

func TestStaleEvaluationCannotReopenDisabledInsight(t *testing.T) {
	s, node, _ := monitorRig(t)
	ctx := context.Background()
	at := time.Now()
	// A worker captured an earlier source, then the administrator removed the policy/state.
	check := IncidentCheck{Kind: "network_latency:WAN:ipv4:old", Active: true, Value: 500, Threshold: 150, SourceKind: "network", SourceKey: "old", SourceAt: at.Unix()}
	if n, e := s.CheckIncident(ctx, node, check, at); e != nil || n != nil {
		t.Fatal("absent source reopened incident", n, e)
	}
	rows, e := s.MonitorIncidents(ctx, node, 0, "open")
	if e != nil || len(rows) != 0 {
		t.Fatal(rows, e)
	}
}

func TestAdaptiveLossNeedsNoSuccessfulLatencyBaseline(t *testing.T) {
	s, node, _ := monitorRig(t)
	ctx := context.Background()
	policy := DefaultNetworkAlertPolicy()
	policy.Mode = "adaptive"
	policy.WindowMinutes = 2
	policy.MinSamples = 5
	if e := s.SetNetworkAlertPolicy(ctx, node, policy); e != nil {
		t.Fatal(e)
	}
	base := time.Now().Add(-5 * time.Minute)
	s.db.Exec(`UPDATE node_network_alerts SET updated_at=? WHERE node_id=?`, base.Unix()-1, node)
	task := PingTask{Name: "WAN", Type: "icmp", Target: "198.51.100.20", SourceInterface: "eth0", Enabled: true, IntervalSeconds: 10}
	if e := s.SavePingTask(ctx, &task); e != nil {
		t.Fatal(e)
	}
	a := task.AgentTask()
	for i := 1; i <= 14; i++ {
		at := base.Add(time.Duration(i) * 10 * time.Second)
		m := spec.ProbeMeasurement{Sequence: uint64(i), At: at.Unix(), LatencyMs: -1, Outcome: "timeout"}
		p := spec.PingResult{TaskID: task.ID, Name: task.Name, Series: a.Series, Family: a.Family, At: at.Unix(), Quality: &spec.PingQuality{ProbeMeasurement: m, Epoch: "loss", Type: "icmp", IntervalSeconds: 10, Recent: []spec.ProbeMeasurement{m}}}
		if e := s.RecordBeat(ctx, node, spec.SystemStatus{ProbeInterfaceVersion: 1, Pings: []spec.PingResult{p}}, at); e != nil {
			t.Fatal(e)
		}
	}
	states, e := s.NetworkAlertStates(ctx, node)
	if e != nil || len(states) != 1 || states[0].Ready || !states[0].LossReady || !states[0].LossAlert || states[0].Baseline != 0 {
		t.Fatal(states, e)
	}
	v := states[0]
	kind := "network_loss:WAN:ipv4:test"
	notice, e := s.CheckIncident(ctx, node, IncidentCheck{Kind: kind, Active: true, SourceKind: "network", SourceKey: v.Key, SourceAt: v.Last, Value: 100, Threshold: 20}, time.Now())
	if e != nil || notice == nil {
		t.Fatal("loss suppressed waiting for latency baseline", notice, e)
	}
}
