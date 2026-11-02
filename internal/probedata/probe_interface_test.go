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

func TestInterfaceTaskProjectionAndHistory(t *testing.T) {
	e := fixture(t)
	ctx := context.Background()
	task := store.PingTask{Name: "WAN RTT", Type: "tcp", Target: "198.51.100.20:443", SourceInterface: "pppoe-wan", NodeIDs: []int64{1}, Enabled: true, IntervalSeconds: 30}
	if err := e.Store.SavePingTask(ctx, &task); err != nil {
		t.Fatal(err)
	}
	at := e.clock()
	wire := task.AgentTask()
	m := spec.ProbeMeasurement{Sequence: 1, At: at.Unix(), LatencyMs: 11, Outcome: "ok"}
	host := spec.SystemStatus{ProbeInterfaceVersion: 1, Pings: []spec.PingResult{{TaskID: task.ID, Name: task.Name, Series: wire.Series, Family: wire.Family, At: at.Unix(), LatencyMs: 11, Quality: &spec.PingQuality{ProbeMeasurement: m, Epoch: "router", Type: "tcp"}}}}
	record(t, e, host, at)
	snap, err := e.Snapshot(ctx, Scope{})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(snap)
	if strings.Contains(string(raw), "pppoe-wan") || strings.Contains(string(raw), "198.51.100.20") {
		t.Fatal("private path leaked", string(raw))
	}
	if len(snap.PingTasks) != 1 || snap.PingTasks[0].Series != wire.Series || len(snap.Nodes[0].Pings) != 1 {
		t.Fatalf("missing current bound task %+v", snap)
	}
	q := PingQuery{Start: at.Add(-time.Minute), End: at}
	got, err := e.PingStats(ctx, Scope{}, q)
	if err != nil || len(got) != 1 || got[0].Total != 1 || got[0].Avg == nil || *got[0].Avg != 11 {
		t.Fatalf("bound path history %+v %v", got, err)
	}
	task.SourceInterface = "eth1"
	if err := e.Store.SavePingTask(ctx, &task); err != nil {
		t.Fatal(err)
	}
	snap, err = e.Snapshot(ctx, Scope{})
	if err != nil || len(snap.Nodes[0].Pings) != 0 {
		t.Fatal("old path still current", snap, err)
	}
	got, err = e.PingStats(ctx, Scope{}, q)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range got {
		if p.Total != 0 {
			t.Fatal("old path history relabeled", got)
		}
	}
}
