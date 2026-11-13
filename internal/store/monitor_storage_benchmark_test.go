package store

import (
	"context"
	"fmt"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/testdb"
	"testing"
	"time"
)

// One iteration is 50 nodes, 20 minutes, 6 legacy series per node (6,000 samples).
// This is a local storage/write baseline, not a production throughput guarantee.
func BenchmarkMonitoringFiftyNodes(b *testing.B) {
	s := New(testdb.Open(b))
	ctx := context.Background()
	nodes := make([]int64, 50)
	for i := range nodes {
		n := &domain.Node{Name: fmt.Sprintf("example-%d", i)}
		if e := s.CreateNode(ctx, n, fmt.Sprintf("pair-%d", i), time.Hour); e != nil {
			b.Fatal(e)
		}
		nodes[i] = n.ID
	}
	before, e := s.MonitorStorage(ctx)
	if e != nil {
		b.Fatal(e)
	}
	at := time.Now().Add(-time.Hour)
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		for tick := 0; tick < 20; tick++ {
			stamp := at.Add(time.Duration(n*20+tick) * time.Minute)
			for _, node := range nodes {
				h := spec.SystemStatus{CPUPercent: 10, MemTotal: 1 << 30, MemUsed: 1 << 28, DiskTotal: 10 << 30, DiskUsed: 1 << 30}
				for k := 0; k < 6; k++ {
					h.Pings = append(h.Pings, spec.PingResult{Name: fmt.Sprintf("example-series-%d", k), TaskID: int64(k + 1000), At: stamp.Unix(), LatencyMs: float64(100 + k)})
				}
				if e := s.RecordBeat(ctx, node, h, stamp); e != nil {
					b.Fatal(e)
				}
			}
		}
	}
	b.StopTimer()
	after, e := s.MonitorStorage(ctx)
	if e != nil {
		b.Fatal(e)
	}
	b.ReportMetric(float64(after.DatabaseBytes-before.DatabaseBytes)/float64(b.N), "db-bytes/batch")
	b.ReportMetric(float64(after.WALBytes), "wal-bytes")
	b.ReportMetric(6000, "samples/batch")
}
