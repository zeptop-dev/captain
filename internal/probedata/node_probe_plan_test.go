package probedata

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestNodePlansProjectDistinctTasksAndHistory(t *testing.T) {
	e := fixture(t)
	ctx := context.Background()
	settings, err := e.settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	settings.CarrierPing = true
	if err = e.Store.SetSetting(ctx, store.SettingProbe, settings); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Store.DB().Exec(`UPDATE nodes SET probe_hidden=0 WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Add(5 * time.Minute).Truncate(time.Minute)
	e.Now = func() time.Time { return now }
	plan, err := e.Store.NodeProbePlan(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	custom := &spec.CarrierProbeConfig{Protocol: "icmp", Family: "ipv4"}
	custom.Normalize()
	view, err := e.Store.UpdateNodeProbePlan(ctx, 2, store.NodeProbePlanChange{Revision: plan.Revision, Mode: "custom", Custom: custom}, now.Add(-2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	for id, cfg := range map[int64]*spec.CarrierProbeConfig{1: settings.CarrierProbe, 2: custom} {
		task := cfg.Tasks()[0]
		m := spec.ProbeMeasurement{At: now.Add(-30 * time.Second).Unix(), Sequence: 1, LatencyMs: 24, Outcome: "ok", DurationMs: 25}
		kind := task.Type
		if task.TCPReachability {
			kind = "tcp_reachability"
		}
		host := spec.SystemStatus{CarrierProbeVersion: 1, Resources: &spec.Resources{At: m.At * 1000, Epoch: "instance", Sequence: 1}, Pings: []spec.PingResult{{Name: task.Name, Family: task.Family, Series: task.Series, At: m.At, LatencyMs: 24, Quality: &spec.PingQuality{Type: kind, Epoch: "run", ProbeMeasurement: m, Recent: []spec.ProbeMeasurement{m}}}}}
		if err = e.Store.RecordBeat(ctx, id, host, time.Unix(m.At, 0)); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(host)
		if _, err = e.Store.DB().Exec(`UPDATE nodes SET monitor_at=?,monitor_host_json=? WHERE id=?`, m.At, string(raw), id); err != nil {
			t.Fatal(err)
		}
	}
	check := func(wantTasks, wantNodesWithPings int) Snapshot {
		t.Helper()
		snap, err := e.Snapshot(ctx, Scope{})
		if err != nil {
			t.Fatal(err)
		}
		found := 0
		for _, n := range snap.Nodes {
			if len(n.Pings) > 0 {
				found++
			}
		}
		if len(snap.PingTasks) != wantTasks || found != wantNodesWithPings {
			t.Fatalf("task/member projection: tasks=%d nodeswithpings=%d", len(snap.PingTasks), found)
		}
		for _, task := range snap.PingTasks {
			if len(task.EntityIDs) != 1 {
				t.Fatalf("plan projected onto wrong nodes %+v", task)
			}
		}
		raw, _ := json.Marshal(snap)
		if strings.Contains(string(raw), "tcp_ipv4") || strings.Contains(string(raw), "changed_at") {
			t.Fatal("private plan config leaked")
		}
		return snap
	}
	snap := check(6, 2)
	hist, err := e.PingStats(ctx, Scope{}, PingQuery{Start: now.Add(-time.Hour), End: now})
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, p := range hist {
		total += p.Total
	}
	if total != 2 {
		t.Fatalf("current plans lost history: %+v", hist)
	}
	// Turning off global defaults still leaves the independent server's plan.
	settings.CarrierPing = false
	if err = e.Store.SetSetting(ctx, store.SettingProbe, settings); err != nil {
		t.Fatal(err)
	}
	snap = check(3, 1)
	if len(snap.Nodes) != 2 {
		t.Fatal("disabling carriers removed resource nodes")
	}
	// Disabling the custom plan hides both old live values and historical buckets.
	_, err = e.Store.UpdateNodeProbePlan(ctx, 2, store.NodeProbePlanChange{Revision: view.Revision, Mode: "disabled"}, now)
	if err != nil {
		t.Fatal(err)
	}
	snap = check(0, 0)
	hist, err = e.PingStats(ctx, Scope{}, PingQuery{Start: now.Add(-time.Hour), End: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 0 {
		t.Fatal("disabled plan exposed history", hist)
	}
}
