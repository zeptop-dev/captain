package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"

	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
)

func TestPeriodStart(t *testing.T) {
	loc := time.UTC
	if got := periodStart(time.Date(2026, 3, 10, 5, 0, 0, 0, loc), 15); !got.Equal(time.Date(2026, 2, 15, 0, 0, 0, 0, loc)) {
		t.Fatalf("before reset day: %v", got)
	}
	if got := periodStart(time.Date(2026, 3, 20, 5, 0, 0, 0, loc), 15); !got.Equal(time.Date(2026, 3, 15, 0, 0, 0, 0, loc)) {
		t.Fatalf("after reset day: %v", got)
	}
	if got := periodStart(time.Date(2026, 3, 1, 0, 0, 0, 0, loc), 1); !got.Equal(time.Date(2026, 3, 1, 0, 0, 0, 0, loc)) {
		t.Fatalf("first: %v", got)
	}
}

func TestRecordBeatAggregatesAndTraffic(t *testing.T) {
	conn, _ := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	_ = db.Migrate(context.Background(), conn, "sqlite")
	s := New(conn)
	ctx := context.Background()
	n := &domain.Node{Name: "jp"}
	_ = s.CreateNode(ctx, n, "CODE", time.Hour)
	_ = s.UpdateNodeProbe(ctx, n.ID, false, NodeProbeInfo{Region: "JP"}, 10<<30, 1, "sum")

	base := time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)
	beat := func(at time.Time, cpu float64, up, down uint64) {
		h := spec.SystemStatus{CPUPercent: cpu, MemTotal: 1000, MemUsed: 500, DiskTotal: 100, DiskUsed: 10, NetTotalUp: up, NetTotalDown: down, NetUp: 100, NetDown: 200,
			Pings: []spec.PingResult{{Name: "CT", LatencyMs: 40}, {Name: "CU", LatencyMs: -1}}}
		if err := s.RecordBeat(ctx, n.ID, h, at); err != nil {
			t.Fatal(err)
		}
	}
	beat(base, 10, 1000, 2000) // first beat only sets the counters
	beat(base.Add(10*time.Second), 30, 1500, 2600)
	beat(base.Add(20*time.Second), 50, 1400, 2700) // up counter went backwards: ignored, down counted

	pts, err := s.NodeStats(ctx, n.ID, "m", base, base.Add(time.Minute))
	if err != nil || len(pts) != 1 {
		t.Fatalf("minute buckets: %v %v", pts, err)
	}
	if pts[0].Samples != 3 || pts[0].CPU != 30 || pts[0].MemTotal != 1000 || pts[0].MemUsed != 500 {
		t.Fatalf("bucket: %+v", pts[0])
	}
	if d, _ := s.NodeStats(ctx, n.ID, "d", base.Add(-24*time.Hour), base); len(d) != 1 || d[0].Samples != 3 {
		t.Fatalf("day bucket: %+v", d)
	}
	pp, _ := s.NodePingStats(ctx, n.ID, "m", base, base.Add(time.Minute))
	if len(pp) != 2 || pp[0].Name != "CT" || pp[0].AvgMs != 40 || pp[1].Name != "CU" || pp[1].Lost != 3 || pp[1].AvgMs != -1 {
		t.Fatalf("ping buckets: %+v", pp)
	}
	np, _ := s.NodeProbe(ctx, n.ID)
	if np.UsedUp != 500 || np.UsedDown != 700 || np.Billed() != 1200 || !np.PeriodStart.Equal(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("traffic: %+v", np)
	}
	// New period: usage rolls into prev and restarts from the current counters.
	beat(base.AddDate(0, 1, 0), 5, 5000, 9000)
	beat(base.AddDate(0, 1, 0).Add(10*time.Second), 5, 5100, 9050)
	np, _ = s.NodeProbe(ctx, n.ID)
	// The delta straddling the boundary (1400→5000, 2700→9000) is charged to the new period.
	if np.PrevUsed != 1200 || np.UsedUp != 3700 || np.UsedDown != 6350 || !np.PeriodStart.Equal(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("rollover: %+v", np)
	}
	if avg, ok := s.SustainedAverage(ctx, n.ID, "mem", time.Hour, base.AddDate(0, 1, 0).Add(10*time.Second)); !ok || avg != 50 {
		t.Fatalf("sustained mem %v %v", avg, ok)
	}
	if fire, _ := s.AlertOnce(ctx, n.ID, "cpu", time.Hour, base); !fire {
		t.Fatal("first alert should fire")
	}
	if fire, _ := s.AlertOnce(ctx, n.ID, "cpu", time.Hour, base.Add(time.Minute)); fire {
		t.Fatal("cooldown ignored")
	}
	if fire, _ := s.AlertOnce(ctx, n.ID, "cpu", time.Hour, base.Add(2*time.Hour)); !fire {
		t.Fatal("after cooldown should fire")
	}
	if err := s.PruneStats(ctx, base.AddDate(1, 0, 0)); err != nil {
		t.Fatal(err)
	}
	if pts, _ := s.NodeStats(ctx, n.ID, "m", base, base.Add(time.Minute)); len(pts) != 0 {
		t.Fatal("minute buckets should be pruned")
	}
}
