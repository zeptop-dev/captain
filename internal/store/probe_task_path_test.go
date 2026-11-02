package store

import (
	"context"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
)

func TestProbePathHistoryRejectsUnboundStaleAndUnsupported(t *testing.T) {
	s := subDesignStore(t)
	ctx := context.Background()
	n := &domain.Node{Name: "Router"}
	if err := s.CreateNode(ctx, n, "PAIR", time.Hour); err != nil {
		t.Fatal(err)
	}
	task := PingTask{Name: "WAN", Type: "icmp", Target: "198.51.100.20", NodeIDs: []int64{n.ID}, Enabled: true, IntervalSeconds: 30}
	if err := s.SavePingTask(ctx, &task); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Truncate(time.Minute).Add(30 * time.Second)
	write := func(cap int, series, family string, seq uint64) {
		t.Helper()
		m := spec.ProbeMeasurement{Sequence: seq, At: at.Unix(), LatencyMs: 8, Outcome: "ok"}
		h := spec.SystemStatus{ProbeInterfaceVersion: cap, Pings: []spec.PingResult{{TaskID: task.ID, Name: task.Name, Series: series, Family: family, At: at.Unix(), LatencyMs: 8, Quality: &spec.PingQuality{ProbeMeasurement: m, Epoch: "router", Type: "icmp"}}}}
		if err := s.RecordBeat(ctx, n.ID, h, at); err != nil {
			t.Fatal(err)
		}
	}
	count := func(want int) {
		t.Helper()
		p, err := s.NodePingStats(ctx, n.ID, "m", at.Add(-time.Minute), at.Add(time.Minute))
		if err != nil || len(p) != want {
			t.Fatalf("history count %d want%d: %+v %v", len(p), want, p, err)
		}
	}
	write(0, "", "", 1)
	count(1)
	task.SourceInterface = "pppoe-wan"
	if err := s.SavePingTask(ctx, &task); err != nil {
		t.Fatal(err)
	}
	count(0)
	wire := task.AgentTask()
	write(0, wire.Series, wire.Family, 2)
	count(0)
	write(1, "", "", 3)
	count(0)
	write(1, wire.Series, "ipv6", 4)
	count(0)
	write(1, wire.Series, wire.Family, 5)
	count(1)
	before, err := s.ProbePublicPolicyFingerprint(ctx)
	if err != nil {
		t.Fatal(err)
	}
	task.SourceInterface = "eth1"
	if err := s.SavePingTask(ctx, &task); err != nil {
		t.Fatal(err)
	}
	after, err := s.ProbePublicPolicyFingerprint(ctx)
	if err != nil || before == after {
		t.Fatal("path change did not invalidate public cache", err)
	}
	count(0)
	write(1, wire.Series, wire.Family, 6)
	count(0)
	wire = task.AgentTask()
	write(1, wire.Series, wire.Family, 7)
	count(1)
	task.NodeIDs = []int64{n.ID + 1}
	if err := s.SavePingTask(ctx, &task); err != nil {
		t.Fatal(err)
	}
	count(0)
	// Existing raw rows remain for normal retention; they are never relabeled.
	var rows int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM node_ping_series_stats WHERE node_id=? AND res='m'`, n.ID).Scan(&rows); err != nil || rows != 2 {
		t.Fatalf("old paths lost %d %v", rows, err)
	}
	task.SourceInterface = ""
	task.NodeIDs = []int64{n.ID}
	if err := s.SavePingTask(ctx, &task); err != nil {
		t.Fatal(err)
	}
	if CurrentTaskSample([]PingTask{task}, n.ID, task.ID, task.Name, wire.Series, wire.Family) {
		t.Fatal("bound sample accepted after clear")
	}
}
