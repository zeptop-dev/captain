package service

import (
	"context"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
	"github.com/zeptop-dev/captain/internal/testdb"
	"strings"
	"testing"
	"time"
)

func TestNetworkAnomalyFlowsThroughExistingIncidentLifecycle(t *testing.T) {
	ctx := context.Background()
	s := store.New(testdb.Open(t))
	n := &domain.Node{Name: "Example node"}
	if e := s.CreateNode(ctx, n, "code", time.Hour); e != nil {
		t.Fatal(e)
	}
	settings := store.ProbeSettings{Enabled: true, BeatSeconds: 10}
	settings.Normalize()
	if e := s.SetSetting(ctx, store.SettingProbe, settings); e != nil {
		t.Fatal(e)
	}
	policy := store.DefaultNetworkAlertPolicy()
	policy.Mode = "fixed"
	policy.FixedMS = 100
	policy.MinSamples = 5
	policy.WindowMinutes = 2
	if e := s.SetNetworkAlertPolicy(ctx, n.ID, policy); e != nil {
		t.Fatal(e)
	}
	base := time.Now().Add(-5 * time.Minute)
	s.DB().Exec(`UPDATE node_network_alerts SET updated_at=? WHERE node_id=?`, base.Unix()-1, n.ID)
	task := store.PingTask{Name: "WAN", Type: "icmp", Target: "198.51.100.20", SourceInterface: "eth0", Enabled: true, IntervalSeconds: 10, NodeIDs: []int64{n.ID}}
	if e := s.SavePingTask(ctx, &task); e != nil {
		t.Fatal(e)
	}
	a := task.AgentTask()
	p := &Probe{Store: s}
	for i := 1; i <= 14; i++ {
		at := base.Add(time.Duration(i) * 10 * time.Second)
		m := spec.ProbeMeasurement{Sequence: uint64(i), At: at.Unix(), LatencyMs: 300, Outcome: "ok"}
		host := spec.SystemStatus{ProbeInterfaceVersion: 1, Pings: []spec.PingResult{{TaskID: task.ID, Name: task.Name, Series: a.Series, Family: a.Family, At: at.Unix(), Quality: &spec.PingQuality{ProbeMeasurement: m, Epoch: "one", Type: "icmp", IntervalSeconds: 10, Recent: []spec.ProbeMeasurement{m}}}}}
		if e := p.Record(ctx, n, "v0.95.0", host, at); e != nil {
			t.Fatal(e)
		}
	}
	incidents, e := s.MonitorIncidents(ctx, n.ID, 0, "open")
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, i := range incidents {
		if strings.HasPrefix(i.Kind, "network_latency:") {
			found = true
			if i.Value != 300 || i.Threshold != 100 {
				t.Fatal(i)
			}
		}
	}
	if !found {
		t.Fatal("measurements never reached incident lifecycle", incidents)
	}
	policy.Mode = "off"
	if e = s.SetNetworkAlertPolicy(ctx, n.ID, policy); e != nil {
		t.Fatal(e)
	}
	incidents, _ = s.MonitorIncidents(ctx, n.ID, 0, "resolved")
	if len(incidents) != 1 || incidents[0].Resolution != "disabled" {
		t.Fatal("policy change invented recovery", incidents)
	}
}
