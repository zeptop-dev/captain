package probedata

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

func fixture(t *testing.T) *Engine {
	t.Helper()
	ctx := context.Background()
	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "probe.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err = db.Migrate(ctx, conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	st := store.New(conn)
	s := store.ProbeSettings{Enabled: true, Title: "Public monitor", Visibility: "public", ShowIP: false, CarrierPing: false}
	s.Normalize()
	if err = st.SetSetting(ctx, store.SettingProbe, s); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(`INSERT INTO nodes(id,name,token_hash,public_addr,created_at,updated_at) VALUES(1,'Public','paired','203.0.113.30',0,0),(2,'Private','paired-2','198.51.100.20',0,0),(3,'Unpaired',NULL,'192.0.2.10',0,0)`); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(`UPDATE nodes SET probe_hidden=1 WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	return &Engine{Store: st, SiteName: "Captain", Now: func() time.Time { return now }}
}
func record(t *testing.T, e *Engine, h spec.SystemStatus, at time.Time) {
	t.Helper()
	ctx := context.Background()
	if err := e.Store.RecordBeat(ctx, 1, h, at); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(h)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.Store.DB().Exec(`UPDATE nodes SET monitor_at=?,monitor_host_json=? WHERE id=1`, at.Unix(), string(raw)); err != nil {
		t.Fatal(err)
	}
}
func publicID(t *testing.T, e *Engine) string {
	t.Helper()
	snap, err := e.Snapshot(context.Background(), Scope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Nodes) != 1 {
		t.Fatalf("nodes=%d", len(snap.Nodes))
	}
	return snap.Nodes[0].UUID
}
func setSections(t *testing.T, e *Engine, sections ...string) {
	t.Helper()
	ctx := context.Background()
	s, err := e.settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	s.PublicSections = &sections
	if err = e.Store.SetSetting(ctx, store.SettingProbe, s); err != nil {
		t.Fatal(err)
	}
}
func TestPublicProjectionPrivacyValidityAndFreshness(t *testing.T) {
	e := fixture(t)
	ctx := context.Background()
	at := e.clock().Add(-10 * time.Minute)
	valid := spec.MetricValidity{CPU: true, Memory: false, Network: true, Disk: true, Load: true, Uptime: true}
	h := spec.SystemStatus{Valid: &valid, CPUPercent: 0, MemUsed: 123, MemTotal: 456, DiskUsed: 20, DiskTotal: 100, NetUp: 5, Info: &spec.HostInfo{CPUModel: "test cpu", OS: "test OS"}, Resources: &spec.Resources{Epoch: "private epoch", Sequence: 1, Processes: []spec.ProcessResource{{PID: 123}}}}
	record(t, e, h, at)
	// A current status poll can confirm presence but cannot freshen old metrics.
	if _, err := e.Store.DB().Exec(`UPDATE nodes SET last_seen_at=? WHERE id=1`, e.clock().Unix()); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []Scope{{}, {Staff: true}} {
		snap, err := e.Snapshot(ctx, scope)
		if err != nil {
			t.Fatal(err)
		}
		if len(snap.Nodes) != 1 {
			t.Fatalf("hidden/unpaired nodes leaked: %+v", snap.Nodes)
		}
		n := snap.Nodes[0]
		if !n.Status.Online || n.Status.Fresh || n.Status.ReceivedAt == nil || !n.Status.ReceivedAt.Equal(at) {
			t.Fatalf("presence conflated with sampling: %+v", n.Status)
		}
		if n.Status.Values["cpu.usage"] == nil || *n.Status.Values["cpu.usage"] != 0 || n.Status.Values["memory.used"] != nil || n.Inventory.MemTotal != nil {
			t.Fatalf("zero and unknown conflated: %+v", n.Status)
		}
		raw, _ := json.Marshal(snap)
		for _, secret := range []string{"203.0.113.30", "198.51.100.20", "private epoch", "\"pid\"", "\"resources\"", "Unpaired", "Private"} {
			if strings.Contains(string(raw), secret) {
				t.Fatalf("private value %q leaked", secret)
			}
		}
	}
	setSections(t, e, "cpu", "history")
	snap, err := e.Snapshot(ctx, Scope{Staff: true})
	if err != nil {
		t.Fatal(err)
	}
	n := snap.Nodes[0]
	if n.Info != nil || n.Traffic != nil || n.Inventory.OS != "" || n.Inventory.DiskTotal != nil {
		t.Fatalf("category leak: %+v", n)
	}
	for _, r := range n.Recent {
		for k := range r.Values {
			if k != "cpu.usage" {
				t.Fatalf("recent leak: %s", k)
			}
		}
	}
	defs, err := e.Definitions(ctx, Scope{})
	if err != nil || len(defs) != 1 || defs[0].Key != "cpu.usage" {
		t.Fatalf("defs=%+v err=%v", defs, err)
	}
}
func TestNodeRecordTimestampsRemainSeparateFromMetricReceipt(t *testing.T) {
	e := fixture(t)
	created, updated, received := e.clock().Add(-24*time.Hour), e.clock().Add(-time.Hour), e.clock().Add(-time.Minute)
	if _, err := e.Store.DB().Exec(`UPDATE nodes SET created_at=?,updated_at=? WHERE id=1`, created.Unix(), updated.Unix()); err != nil {
		t.Fatal(err)
	}
	record(t, e, spec.SystemStatus{Valid: &spec.MetricValidity{CPU: true}, CPUPercent: 12}, received)
	snap, err := e.Snapshot(context.Background(), Scope{})
	if err != nil {
		t.Fatal(err)
	}
	n := snap.Nodes[0]
	if !n.CreatedAt.Equal(created) || !n.UpdatedAt.Equal(updated) || n.Status.ReceivedAt == nil || !n.Status.ReceivedAt.Equal(received) {
		t.Fatalf("node row timestamps conflated with metrics: %+v", n)
	}
	updated = e.clock()
	if _, err = e.Store.DB().Exec(`UPDATE nodes SET updated_at=? WHERE id=1`, updated.Unix()); err != nil {
		t.Fatal(err)
	}
	snap, err = e.Snapshot(context.Background(), Scope{})
	if err != nil {
		t.Fatal(err)
	}
	n = snap.Nodes[0]
	if !n.CreatedAt.Equal(created) || !n.UpdatedAt.Equal(updated) || !n.Status.ReceivedAt.Equal(received) {
		t.Fatalf("row update changed metric evidence: %+v", n)
	}
}

func TestStableIdentitySurvivesRenameButNotReuse(t *testing.T) {
	e := fixture(t)
	id := publicID(t, e)
	if len(id) != 36 || id[14] != '4' {
		t.Fatalf("bad public UUID %q", id)
	}
	if _, err := e.Store.DB().Exec(`UPDATE nodes SET name='Renamed' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if got := publicID(t, e); got != id {
		t.Fatalf("rename changed UUID %q -> %q", id, got)
	}
	if _, err := e.Store.DB().Exec(`DELETE FROM nodes WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Store.DB().Exec(`INSERT INTO nodes(id,name,token_hash,created_at,updated_at) VALUES(1,'Replacement','paired',0,0)`); err != nil {
		t.Fatal(err)
	}
	if got := publicID(t, e); got == id {
		t.Fatal("reused SQLite ID inherited public UUID")
	}
	got, err := e.Query(context.Background(), Scope{}, Query{MetricKeys: []string{"cpu.usage"}, EntityIDs: []string{id}, Start: e.clock().Add(-time.Hour), End: e.clock()})
	if err != nil || len(got) != 0 {
		t.Fatalf("old UUID selected replacement: %+v %v", got, err)
	}
}
func TestFreshRollupsWeightEachMetricAndPreserveExtrema(t *testing.T) {
	e := fixture(t)
	ctx := context.Background()
	base := e.clock().Add(-30 * time.Minute)
	record(t, e, spec.SystemStatus{Valid: &spec.MetricValidity{CPU: true, Memory: true}, CPUPercent: 10, MemUsed: 10, MemTotal: 100}, base)
	record(t, e, spec.SystemStatus{Valid: &spec.MetricValidity{CPU: false, Memory: true}, CPUPercent: 99, MemUsed: 90, MemTotal: 100}, base.Add(10*time.Second))
	record(t, e, spec.SystemStatus{Valid: &spec.MetricValidity{CPU: true, Memory: false}, CPUPercent: 40, MemUsed: 9000, MemTotal: 100}, base.Add(time.Minute))
	id := publicID(t, e)
	for algorithm, want := range map[string]float64{"avg": 25, "sum": 50, "count": 2, "min": 10, "max": 40, "first": 10, "last": 40} {
		got, err := e.Query(ctx, Scope{}, Query{MetricKeys: []string{"cpu.usage"}, EntityIDs: []string{id}, Start: base, End: base.Add(2 * time.Minute), MaxPoints: 1, Aggregation: algorithm})
		if err != nil {
			t.Fatalf("%s: %v", algorithm, err)
		}
		if len(got) != 1 || len(got[0].Points) != 1 || got[0].Points[0].Value == nil || *got[0].Points[0].Value != want || got[0].Points[0].Count != 2 {
			t.Fatalf("%s got=%+v", algorithm, got)
		}
	}
	got, err := e.Query(ctx, Scope{}, Query{MetricKeys: []string{"memory.used"}, Start: base, End: base.Add(2 * time.Minute), MaxPoints: 1})
	if err != nil {
		t.Fatal(err)
	}
	if *got[0].Points[0].Value != 50 || got[0].Points[0].Count != 2 {
		t.Fatalf("memory weighted using CPU/sample count: %+v", got[0])
	}
	// Unknown entities are indistinguishable from hidden ones.
	ids, err := e.Store.ProbePublicIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got, err = e.Query(ctx, Scope{Staff: true}, Query{MetricKeys: []string{"cpu.usage"}, EntityIDs: []string{ids[2]}, Start: base, End: base.Add(2 * time.Minute)})
	if err != nil || len(got) != 0 {
		t.Fatalf("hidden history leaked: %+v %v", got, err)
	}
}
func TestLegacyAveragesUsePerMetricCountsAndRejectInventedExtrema(t *testing.T) {
	e := fixture(t)
	base := e.clock().Add(-time.Hour).Truncate(time.Minute)
	// 9 CPU observations of 10 and 1 observation of 100 -> 19, not 55.
	for i, row := range []struct {
		n   int
		sum float64
	}{{9, 90}, {1, 100}} {
		if _, err := e.Store.DB().Exec(`INSERT INTO node_stats(node_id,res,ts,samples,cpu,cpu_n) VALUES(1,'m',?,?,?,?)`, base.Add(time.Duration(i)*time.Minute).Unix(), row.n+5, row.sum, row.n); err != nil {
			t.Fatal(err)
		}
	}
	q := Query{MetricKeys: []string{"cpu.usage"}, Start: base, End: base.Add(2 * time.Minute), MaxPoints: 1}
	got, err := e.Query(context.Background(), Scope{}, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || *got[0].Points[0].Value != 19 || got[0].Points[0].Count != 10 {
		t.Fatalf("incorrect weighting %+v", got)
	}
	q.Aggregation = "min"
	if _, err = e.Query(context.Background(), Scope{}, q); !errors.Is(err, ErrUnsupportedAggregation) {
		t.Fatalf("invented historical minimum: %v", err)
	}
}
func TestMixedUpgradeBucketKeepsPerMetricEvidence(t *testing.T) {
	e := fixture(t)
	ctx := context.Background()
	base := e.clock().Add(-3 * time.Hour).Truncate(time.Minute)
	// The first CPU sample predates sufficient statistics. Memory and counters
	// are first observed after the upgrade, in the same minute bucket.
	if _, err := e.Store.DB().Exec(`INSERT INTO node_stats(node_id,res,ts,samples,cpu,cpu_n) VALUES(1,'m',?,1,10,1)`, base.Unix()); err != nil {
		t.Fatal(err)
	}
	record(t, e, spec.SystemStatus{Valid: &spec.MetricValidity{CPU: true, Memory: true, Network: true}, CPUPercent: 20, MemUsed: 12, MemTotal: 100, NetTotalUp: 900, NetTotalDown: 1000}, base.Add(10*time.Second))
	q := Query{MetricKeys: []string{"cpu.usage", "memory.used", "net.total.up"}, Start: base, End: base.Add(time.Minute), MaxPoints: 1, Aggregation: "avg", AggregationByMetric: map[string]string{"memory.used": "min", "net.total.up": "last"}}
	got, err := e.Query(ctx, Scope{}, q)
	if err != nil {
		t.Fatalf("unrelated legacy CPU evidence rejected fresh observations: %v", err)
	}
	want := map[string]struct {
		value  float64
		count  int
		source string
	}{"cpu.usage": {15, 2, "legacy_average"}, "memory.used": {12, 1, "rollup"}, "net.total.up": {900, 1, "rollup"}}
	if len(got) != len(want) {
		t.Fatalf("series=%+v", got)
	}
	for _, series := range got {
		w, ok := want[series.MetricKey]
		if !ok || series.Source != w.source || len(series.Points) != 1 || series.Points[0].Value == nil || *series.Points[0].Value != w.value || series.Points[0].Count != w.count {
			t.Fatalf("incorrect per-metric evidence: %+v", series)
		}
	}
	q.MetricKeys = []string{"cpu.usage"}
	q.Aggregation = "last"
	q.AggregationByMetric = nil
	if _, err = e.Query(ctx, Scope{}, q); !errors.Is(err, ErrUnsupportedAggregation) {
		t.Fatalf("legacy CPU last must remain unknown: %v", err)
	}
}

func TestPingStatsWeightSuccessesAndKeepUnknownDistribution(t *testing.T) {
	e := fixture(t)
	ctx := context.Background()
	task := store.PingTask{Name: "Public latency", Type: "tcp", Target: "example.com:443", IntervalSeconds: 30, Enabled: true}
	if err := e.Store.SavePingTask(ctx, &task); err != nil {
		t.Fatal(err)
	}
	base := e.clock().Add(-time.Hour).Truncate(time.Minute)
	for i, row := range []struct {
		n, lost int
		sum     float64
	}{{10, 9, 100}, {10, 0, 100}} {
		if _, err := e.Store.DB().Exec(`INSERT INTO node_ping_stats(node_id,res,ts,task_id,name,samples,lost,sum_ms) VALUES(1,'m',?,?,?,?,?,?)`, base.Add(time.Duration(i)*time.Minute).Unix(), task.ID, task.Name, row.n, row.lost, row.sum); err != nil {
			t.Fatal(err)
		}
	}
	got, err := e.PingStats(ctx, Scope{}, PingQuery{Start: base, End: base.Add(2 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("stats=%+v", got)
	}
	s := got[0]
	if s.Total != 20 || s.Valid != 11 || s.Avg == nil || math.Abs(*s.Avg-200.0/11) > 1e-9 || s.Loss == nil || *s.Loss != .45 {
		t.Fatalf("bad ping weighted stats %+v", s)
	}
	if s.P50 != nil || s.P99 != nil || s.Min != nil || s.Max != nil || s.Stddev != nil || s.Latest != nil {
		t.Fatalf("fabricated distribution/latest %+v", s)
	}
	snap, err := e.Snapshot(ctx, Scope{})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(snap)
	if strings.Contains(string(raw), "example.com") || strings.Contains(string(raw), "target") {
		t.Fatal("ping target leaked")
	}
	task.Enabled = false
	if err = e.Store.SavePingTask(ctx, &task); err != nil {
		t.Fatal(err)
	}
	got, err = e.PingStats(ctx, Scope{}, PingQuery{Start: base, End: base.Add(2 * time.Minute)})
	if err != nil || len(got) > 0 {
		t.Fatalf("disabled task history exposed %v %v", got, err)
	}
}
func TestHistoryFillAndQueryBounds(t *testing.T) {
	e := fixture(t)
	ctx := context.Background()
	base := e.clock().Add(-time.Hour)
	q := Query{MetricKeys: []string{"cpu.usage"}, Start: base, End: e.clock(), MaxPoints: 5, FillEmpty: true}
	got, err := e.Query(ctx, Scope{}, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0].Points) != 5 {
		t.Fatalf("empty series %+v", got)
	}
	for _, p := range got[0].Points {
		if p.Value != nil || p.Count != 0 {
			t.Fatal("missing observation reported zero")
		}
	}
	q.MaxPointsByMetric = map[string]int{"cpu.usage": 5001}
	if _, err = e.Query(ctx, Scope{}, q); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("point override bypassed bound: %v", err)
	}
	setSections(t, e, "cpu")
	defs, err := e.Definitions(ctx, Scope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 || defs[0].History || slices.Contains(defs[0].Aggregations, "avg") {
		t.Fatal("disabled history advertised")
	}
}

func TestAcceptedSequenceAndNICDeltasShareTransaction(t *testing.T) {
	e := fixture(t)
	ctx := context.Background()
	at := e.clock().Add(-time.Minute)
	h := spec.SystemStatus{Valid: &spec.MetricValidity{CPU: true, Network: true}, CPUPercent: 25, Resources: &spec.Resources{Epoch: "test-epoch", Sequence: 1, At: at.UnixMilli(), Networks: []spec.NetworkResource{{Name: "eth0", ID: "boot-interface-1", Included: true, Up: 100, Down: 200}}}}
	if _, err := e.Store.RecordNodeBeat(ctx, 1, "paired", h, at); err != nil {
		t.Fatal(err)
	}
	h.Resources.Sequence = 2
	h.Resources.At = at.Add(10 * time.Second).UnixMilli()
	h.Resources.Networks[0].Up = 150
	h.Resources.Networks[0].Down = 280
	if _, err := e.Store.RecordNodeBeat(ctx, 1, "paired", h, at.Add(10*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Store.RecordNodeBeat(ctx, 1, "paired", h, at.Add(20*time.Second)); !errors.Is(err, store.ErrStaleBeat) {
		t.Fatalf("replay accepted: %v", err)
	}
	points, err := e.Store.ProbeMetricHistory(ctx, 1, "m", at, at.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 1 || points[0].Values["cpu.usage"].Count != 2 || points[0].Values["traffic.up"].Sum != 50 || points[0].Values["traffic.down"].Sum != 80 {
		t.Fatalf("metric receipt/deltas mismatch: %+v", points)
	}
	h.Resources.Sequence = 3
	if _, err = e.Store.RecordNodeBeat(ctx, 1, "revoked", h, at.Add(30*time.Second)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("old token accepted: %v", err)
	}
	again, err := e.Store.ProbeMetricHistory(ctx, 1, "m", at, at.Add(time.Minute))
	if err != nil || again[0].Values["cpu.usage"].Count != 2 {
		t.Fatalf("rejected token wrote metrics: %+v %v", again, err)
	}
}
func TestSiteResetClearsPublicIdentityAndMetricRows(t *testing.T) {
	e := fixture(t)
	ctx := context.Background()
	old := publicID(t, e)
	record(t, e, spec.SystemStatus{CPUPercent: 12}, e.clock())
	admin := &domain.User{Email: "admin@example.com", PasswordHash: "test-hash", Role: domain.RoleAdmin, Status: "active"}
	if err := e.Store.CreateStaff(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Store.ResetSite(ctx, admin.ID); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"probe_public_ids", "node_probe_metric_stats"} {
		var count int
		if err := e.Store.DB().QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("reset %s count=%d err=%v", table, count, err)
		}
	}
	replacement := &domain.Node{Name: "New install"}
	if err := e.Store.CreateNode(ctx, replacement, "test-pair-code", time.Hour); err != nil {
		t.Fatal(err)
	}
	ids, err := e.Store.ProbePublicIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ids[replacement.ID] == old || ids[replacement.ID] == "" {
		t.Fatalf("reset reused public identity %+v", ids)
	}
}
func TestMetricRetentionAndWriteCost(t *testing.T) {
	e := fixture(t)
	ctx := context.Background()
	for id := 4; id <= 10; id++ {
		if _, err := e.Store.DB().Exec(`INSERT INTO nodes(id,name,created_at,updated_at) VALUES(?,'Load test',0,0)`, id); err != nil {
			t.Fatal(err)
		}
	}
	at := e.clock().Add(-time.Hour)
	h := spec.SystemStatus{CPUPercent: 12, MemUsed: 1 << 29, MemTotal: 1 << 30, DiskUsed: 1 << 30, DiskTotal: 1 << 32, NetUp: 100, NetDown: 200, Load1: .5, TCP: 10, UDP: 2, Processes: 50, Uptime: 3600}
	started := time.Now()
	for i := 0; i < 100; i++ {
		if err := e.Store.RecordBeat(ctx, int64(i%10+1), h, at.Add(time.Duration(i)*5*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	elapsed := time.Since(started)
	var count, bytes int
	if err := e.Store.DB().QueryRow(`SELECT count(*),sum(length(values_json)) FROM node_probe_metric_stats`).Scan(&count, &bytes); err != nil {
		t.Fatal(err)
	}
	t.Logf("100 accepted beats / 10 nodes: %s, %d rollup rows, %d JSON bytes", elapsed, count, bytes)
	// Ensure pruning is wired to the existing scheduled PruneStats operation.
	for _, r := range []struct {
		res string
		age time.Duration
	}{{"r", 3 * time.Hour}, {"m", 49 * time.Hour}, {"h", 61 * 24 * time.Hour}, {"d", 731 * 24 * time.Hour}} {
		if _, err := e.Store.DB().Exec(`INSERT INTO node_probe_metric_stats(node_id,res,ts,values_json) VALUES(1,?,?,'{}')`, r.res, e.clock().Add(-r.age).Unix()); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.Store.PruneStats(ctx, e.clock()); err != nil {
		t.Fatal(err)
	}
	var after int
	if err := e.Store.DB().QueryRow(`SELECT count(*) FROM node_probe_metric_stats`).Scan(&after); err != nil || after != count {
		t.Fatalf("retention removed valid data or kept expired rows: before=%d after=%d err=%v", count, after, err)
	}
}
func TestMergedPingHistogramDoesNotAveragePercentiles(t *testing.T) {
	e := fixture(t)
	ctx := context.Background()
	task := store.PingTask{Name: "Latency", Type: "tcp", Target: "example.com:443", Enabled: true, IntervalSeconds: 10}
	if err := e.Store.SavePingTask(ctx, &task); err != nil {
		t.Fatal(err)
	}
	base := e.clock().Add(-time.Hour)
	for i, row := range []struct {
		count int
		ms    float64
	}{{90, 10}, {10, 100}} {
		bin := 1 + max(0, int(math.Ceil(math.Log(row.ms/.1)/math.Log(1.05))))
		quality, _ := json.Marshal(map[string]any{"n": row.count, "histogram": map[int]int{bin: row.count}, "min": row.ms, "max": row.ms, "outcomes": map[string]int{"ok": row.count}})
		if _, err := e.Store.DB().Exec(`INSERT INTO node_ping_stats(node_id,res,ts,task_id,name,samples,lost,sum_ms,quality_json) VALUES(1,'m',?,?,?,?,0,?,?)`, base.Add(time.Duration(i)*time.Minute).Unix(), task.ID, task.Name, row.count, float64(row.count)*row.ms, string(quality)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := e.PingStats(ctx, Scope{}, PingQuery{Start: base, End: base.Add(2 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].P50 == nil || *got[0].P50 < 10 || *got[0].P50 > 10.5 || got[0].P99 == nil || *got[0].P99 != 100 || *got[0].Avg != 19 {
		t.Fatalf("bad merged quantiles %+v", got)
	}
}

func TestRootDiskSelectionAppliesToCurrentRecentAndHistory(t *testing.T) {
	e := fixture(t)
	ctx := context.Background()
	record(t, e, spec.SystemStatus{DiskUsed: 10, DiskTotal: 100}, e.clock().Add(-time.Minute))
	if _, err := e.Store.DB().Exec(`UPDATE nodes SET probe_storage_json='{"mode":"manual","items":[{"mount":"/mnt/private","name":"Data"}]}' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	snap, err := e.Snapshot(ctx, Scope{Staff: true})
	if err != nil {
		t.Fatal(err)
	}
	n := snap.Nodes[0]
	if n.RootDiskPublic || n.Inventory.DiskTotal != nil || n.Status.Values["disk.used"] != nil {
		t.Fatalf("root current leaked: %+v", n)
	}
	for _, r := range n.Recent {
		if r.Values["disk.used"] != nil || r.Values["captain.disk.total"] != nil {
			t.Fatal("root recent leaked")
		}
	}
	got, err := e.Query(ctx, Scope{Staff: true}, Query{MetricKeys: []string{"disk.used", "captain.disk.total"}, Start: e.clock().Add(-time.Hour), End: e.clock()})
	if err != nil || len(got) > 0 {
		t.Fatalf("root history leaked: %+v %v", got, err)
	}
	raw, _ := json.Marshal(snap)
	if strings.Contains(string(raw), "/mnt/private") {
		t.Fatal("storage path leaked")
	}
}
func TestMetricPointLimitIncludesEndBoundary(t *testing.T) {
	e := fixture(t)
	ctx := context.Background()
	base := e.clock().Add(-time.Minute)
	record(t, e, spec.SystemStatus{CPUPercent: 10}, base)
	record(t, e, spec.SystemStatus{CPUPercent: 20}, e.clock())
	got, err := e.Query(ctx, Scope{}, Query{MetricKeys: []string{"cpu.usage"}, Start: base, End: e.clock(), MaxPoints: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0].Points) != 1 || got[0].Points[0].Count != 2 || *got[0].Points[0].Value != 15 {
		t.Fatalf("inclusive end exceeded maxpoints or dropped measurement: %+v", got)
	}
}

func TestPublicMetadataKeepsPrecisionAndPrivateBandwidthHidden(t *testing.T) {
	e := fixture(t)
	ctx := context.Background()
	info := `{"billing":{"amount":"12.3456","currency":"USD","cycle":"monthly"},"bandwidth":{"inbound_mbps":500,"outbound_mbps":200,"public":false},"auto_renewal":{"enabled":true,"anchor_date":"2026-01-31","anchor_day":31},"location_settings":{"mode":"manual","manual":{"country":"US","city":"Example city","lat":0,"lng":0,"precision":"city"}}}`
	if _, err := e.Store.DB().Exec(`UPDATE nodes SET probe_info_json=? WHERE id=1`, info); err != nil {
		t.Fatal(err)
	}
	snap, err := e.Snapshot(ctx, Scope{})
	if err != nil {
		t.Fatal(err)
	}
	n := snap.Nodes[0]
	if n.Info == nil || n.Info.Cost == nil || n.Info.Cost.Amount != "12.3456" || n.Info.Bandwidth != nil || n.Info.Location == nil || n.Info.Location.Precision != "city" || n.Info.Latitude == nil || *n.Info.Latitude != 0 {
		t.Fatalf("metadata lost precision or privacy: %+v", n.Info)
	}
	raw, _ := json.Marshal(n.Info)
	for _, field := range []string{"anchor_date", "anchor_day", "location_settings"} {
		if strings.Contains(string(raw), field) {
			t.Fatalf("private metadata %s leaked", field)
		}
	}
}

func TestNonUTCDailyRollupDoesNotDuplicateLegacyFallback(t *testing.T) {
	e := fixture(t)
	ctx := context.Background()
	zone := time.FixedZone("test-zone", 8*3600)
	at := e.clock().Add(-70 * 24 * time.Hour).In(zone)
	record(t, e, spec.SystemStatus{CPUPercent: 17}, at)
	year, month, day := at.Date()
	start := time.Date(year, month, day, 0, 0, 0, 0, zone)
	got, err := e.Query(ctx, Scope{}, Query{MetricKeys: []string{"cpu.usage"}, Start: start, End: start.Add(24 * time.Hour), MaxPoints: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0].Points) != 1 || got[0].Points[0].Count != 1 || *got[0].Points[0].Value != 17 || got[0].Source != "rollup" {
		t.Fatalf("daily bucket duplicated across timezone: %+v", got)
	}
}

func TestPublicPolicyFingerprintIgnoresBeatsButDetectsVisibility(t *testing.T) {
	e := fixture(t)
	ctx := context.Background()
	before, err := e.Store.ProbePublicPolicyFingerprint(ctx)
	if err != nil {
		t.Fatal(err)
	}
	record(t, e, spec.SystemStatus{CPUPercent: 17}, e.clock())
	after, err := e.Store.ProbePublicPolicyFingerprint(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("ordinary heartbeat changed public policy fingerprint")
	}
	if _, err := e.Store.DB().Exec(`UPDATE nodes SET probe_hidden=1 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	after, err = e.Store.ProbePublicPolicyFingerprint(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("hiding a server did not change public policy fingerprint")
	}
}
func TestSnapshotBoundsPolicyRetriesWithoutServiceGeneration(t *testing.T) {
	e := fixture(t)
	ctx := context.Background()
	at := e.clock()
	calls := 0
	e.Now = func() time.Time {
		calls++
		s, err := e.settings(ctx)
		if err != nil {
			t.Fatal(err)
		}
		s.ShowIP = !s.ShowIP
		if err = e.Store.SetSetting(ctx, store.SettingProbe, s); err != nil {
			t.Fatal(err)
		}
		return at
	}
	got, err := e.Snapshot(ctx, Scope{Staff: true})
	if !errors.Is(err, ErrPolicyChanged) || len(got.Nodes) > 0 || calls != 3 {
		t.Fatalf("changing privacy did not fail closed in bounded retries: calls=%d nodes=%d err=%v", calls, len(got.Nodes), err)
	}
}
func TestSnapshotDoesNotRetryForContinuousPresenceUpdates(t *testing.T) {
	e := fixture(t)
	ctx := context.Background()
	at := e.clock()
	calls := 0
	e.Now = func() time.Time {
		calls++
		if _, err := e.Store.DB().Exec(`UPDATE nodes SET last_seen_at=?,monitor_at=?,monitor_host_json='{"cpu_percent":17}' WHERE id=1`, at.Unix(), at.Unix()); err != nil {
			t.Fatal(err)
		}
		return at
	}
	got, err := e.Snapshot(ctx, Scope{})
	if err != nil || calls != 1 || len(got.Nodes) != 1 {
		t.Fatalf("heartbeat caused policy retry: calls=%d err=%v", calls, err)
	}
}
