package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/migrations"
)

func carrierHistoryDB(t *testing.T, legacy bool) *sql.DB {
	t.Helper()
	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "carrier.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if legacy {
		goose.SetBaseFS(migrations.FS)
		goose.SetLogger(goose.NopLogger())
		if err := goose.SetDialect("sqlite"); err != nil {
			t.Fatal(err)
		}
		err = goose.UpToContext(context.Background(), conn, ".", 88)
	} else {
		err = db.Migrate(context.Background(), conn, "sqlite")
	}
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO nodes(id,name,created_at,updated_at) VALUES(1,'Carrier test',0,0)`); err != nil {
		t.Fatal(err)
	}
	return conn
}

func writeCarrierHistory(t *testing.T, conn *sql.DB, at time.Time, pings ...spec.PingResult) {
	t.Helper()
	tx, err := conn.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := recordPingHistory(context.Background(), tx, 1, spec.SystemStatus{Pings: pings}, at); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func carrierHistoryResult(protocol, family, target string, seq uint64, at time.Time, ms float64) spec.PingResult {
	kind := protocol
	if protocol == "tcp" {
		kind = "tcp_reachability"
	}
	outcome := "ok"
	if ms < 0 {
		outcome = "timeout"
	}
	m := spec.ProbeMeasurement{Sequence: seq, At: at.Unix(), LatencyMs: ms, Outcome: outcome}
	return spec.PingResult{Name: "CT", Family: family, Series: spec.CarrierSeries(protocol, family, "CT", target), At: at.Unix(), LatencyMs: ms,
		Quality: &spec.PingQuality{ProbeMeasurement: m, Epoch: "same-run", Type: kind, IntervalSeconds: 10}}
}

func TestCarrierHistorySeparatesFamiliesProtocolsTargetsAndCursors(t *testing.T) {
	conn := carrierHistoryDB(t, false)
	at := time.Date(2026, 10, 25, 10, 1, 10, 0, time.UTC)
	v4 := carrierHistoryResult("tcp", "ipv4", "192.0.2.10:80", 1, at, 10)
	v6 := carrierHistoryResult("tcp", "ipv6", "[2001:db8::1]:80", 1, at, 20)
	icmp := carrierHistoryResult("icmp", "ipv4", "192.0.2.10", 1, at, 30)
	target := carrierHistoryResult("tcp", "ipv4", "198.51.100.20:80", 1, at, -1)
	legacy := spec.PingResult{Name: "CT", At: at.Unix(), LatencyMs: 50}
	// Identical names, epoch, sequence and bucket must still be five series.
	writeCarrierHistory(t, conn, at, v4, v6, icmp, target, legacy)
	writeCarrierHistory(t, conn, at, v4, v6, icmp, target, legacy)
	v4 = carrierHistoryResult("tcp", "ipv4", "192.0.2.10:80", 2, at.Add(10*time.Second), 14)
	writeCarrierHistory(t, conn, at.Add(10*time.Second), v4)
	s := New(conn)
	for _, res := range []string{"m", "h", "d"} {
		points, err := s.NodePingStats(context.Background(), 1, res, at.Truncate(24*time.Hour), at.Add(time.Minute))
		if err != nil || len(points) != 5 {
			t.Fatalf("%s points: %+v, %v", res, points, err)
		}
		bySeries := map[string]PingPoint{}
		for _, p := range points {
			if p.Name != "CT" {
				t.Fatalf("display name became storage identity: %+v", p)
			}
			bySeries[p.Series] = p
		}
		p := bySeries[v4.Series]
		if p.Family != "ipv4" || p.Samples != 2 || p.AvgMs != 12 || p.Quality == nil || p.Quality.Type != "tcp_reachability" || p.Quality.Jitter == nil || *p.Quality.Jitter != 4 || p.Quality.Skipped != 0 {
			t.Fatalf("%s IPv4 cursor/aggregate %+v %+v", res, p, p.Quality)
		}
		for _, expected := range []spec.PingResult{v6, icmp, target} {
			p := bySeries[expected.Series]
			if p.Family != expected.Family || p.Samples != 1 || p.AvgMs != expected.LatencyMs || p.Quality == nil || p.Quality.Type != expected.Quality.Type || p.Quality.Jitter != nil || p.Quality.Skipped != 0 {
				t.Fatalf("%s independent series %+v %+v", res, p, p.Quality)
			}
		}
		if p := bySeries[""]; p.Samples != 1 || p.AvgMs != 50 || p.Family != "" || p.Quality != nil {
			t.Fatalf("legacy data contaminated: %+v", p)
		}
		if p := bySeries[target.Series]; p.Lost != 1 || p.Quality.P50 != nil {
			t.Fatalf("failed target acquired a successful latency: %+v", p)
		}
		encoded, err := json.Marshal(points)
		if err != nil || strings.Contains(string(encoded), "192.0.2.10") || strings.Contains(string(encoded), "2001:db8") {
			t.Fatalf("public history exposed a target: %s, %v", encoded, err)
		}
	}
	var cursors int
	if err := conn.QueryRow(`SELECT (SELECT COUNT(*) FROM node_ping_cursors) + (SELECT COUNT(*) FROM node_ping_series_cursors)`).Scan(&cursors); err != nil || cursors != 5 {
		t.Fatalf("cursor count %d, %v", cursors, err)
	}
}

func TestCarrierHistoryRejectsIncompleteIdentityWithoutLegacyFallback(t *testing.T) {
	conn := carrierHistoryDB(t, false)
	at := time.Date(2026, 10, 25, 10, 1, 10, 0, time.UTC)
	base := func() spec.PingResult { return carrierHistoryResult("tcp", "ipv4", "192.0.2.10:80", 1, at, 10) }
	for _, mutate := range []func(*spec.PingResult){
		func(p *spec.PingResult) { p.Series = "carrier:bad" },
		func(p *spec.PingResult) { p.Series = strings.ToUpper(p.Series) },
		func(p *spec.PingResult) { p.Series = "" },
		func(p *spec.PingResult) { p.Family = "" },
		func(p *spec.PingResult) { p.Family = "dual" },
		func(p *spec.PingResult) { p.Quality = nil },
		func(p *spec.PingResult) { p.Quality.Epoch = "" },
		func(p *spec.PingResult) { p.Quality.Type = "http" },
		func(p *spec.PingResult) { p.Quality.Outcome = "timeout" },
		func(p *spec.PingResult) { p.TaskID = 1 },
	} {
		p := base()
		mutate(&p)
		writeCarrierHistory(t, conn, at, p)
	}
	points, err := New(conn).NodePingStats(context.Background(), 1, "m", at.Add(-time.Minute), at)
	if err != nil || len(points) != 0 {
		t.Fatalf("invalid metadata persisted as a carrier or legacy result: %+v, %v", points, err)
	}
	// A custom task can explicitly constrain a family without a carrier hash.
	custom := base()
	custom.TaskID, custom.Series, custom.Name, custom.Quality.Type = 42, "", "Custom", "tcp"
	writeCarrierHistory(t, conn, at, custom)
	custom.Family = "ipv6"
	writeCarrierHistory(t, conn, at, custom)
	points, err = New(conn).NodePingStats(context.Background(), 1, "m", at.Add(-time.Minute), at)
	if err != nil || len(points) != 2 || points[0].Family != "ipv4" || points[1].Family != "ipv6" || points[0].Samples != 1 || points[1].Samples != 1 {
		t.Fatalf("custom family histories were lost or mixed: %+v, %v", points, err)
	}
}

func TestCarrierHistoryMigrationPreservesLegacyDataAndCursor(t *testing.T) {
	conn := carrierHistoryDB(t, true)
	ctx := context.Background()
	at := time.Date(2026, 10, 25, 10, 1, 10, 0, time.UTC)
	series := spec.CarrierSeries("tcp", "ipv4", "CT", "192.0.2.10:80")
	// Even a historical label that resembles an opaque series stays a label.
	for _, name := range []string{"CT", series} {
		if _, err := conn.Exec(`INSERT INTO node_ping_stats(node_id,task_id,name,res,ts,samples,lost,sum_ms,sum_mbps,quality_json) VALUES(1,0,?,'m',?,2,1,16,3,'{}')`, name, at.Truncate(time.Minute).Unix()); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(`INSERT INTO node_ping_cursors(node_id,task_id,name,epoch,sequence,last_ms,seen_at) VALUES(1,0,?,'legacy',?,16,?)`, name, at.Unix(), at.Unix()); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Migrate(ctx, conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	s := New(conn)
	points, err := s.NodePingStats(ctx, 1, "m", at.Add(-time.Minute), at)
	if err != nil || len(points) != 2 {
		t.Fatalf("migrated points %+v, %v", points, err)
	}
	for _, p := range points {
		if p.Series != "" || p.Family != "" || p.Samples != 2 || p.Lost != 1 || p.AvgMs != 16 || p.AvgMbps != 3 || p.Quality != nil {
			t.Fatalf("migration changed legacy meaning: %+v", p)
		}
	}
	writeCarrierHistory(t, conn, at, spec.PingResult{Name: "CT", At: at.Unix(), LatencyMs: 16})
	writeCarrierHistory(t, conn, at, carrierHistoryResult("tcp", "ipv4", "192.0.2.10:80", 1, at, 32))
	points, err = s.NodePingStats(ctx, 1, "m", at.Add(-time.Minute), at)
	if err != nil || len(points) != 3 || points[0].Samples != 2 {
		t.Fatalf("legacy dedup cursor lost after migration: %+v, %v", points, err)
	}
	if err := s.PruneStats(ctx, at.Add(8*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{`SELECT COUNT(*) FROM node_ping_stats WHERE res='m'`, `SELECT COUNT(*) FROM node_ping_series_stats WHERE res='m'`, `SELECT COUNT(*) FROM node_ping_cursors`, `SELECT COUNT(*) FROM node_ping_series_cursors`} {
		var count int
		if err := conn.QueryRow(query).Scan(&count); err != nil || count != 0 {
			t.Fatalf("prune left %d rows, %v", count, err)
		}
	}
	// Recreate rows to exercise foreign-key cascades for the new tables too.
	writeCarrierHistory(t, conn, at, carrierHistoryResult("tcp", "ipv4", "192.0.2.10:80", 1, at, 32))
	if _, err := conn.Exec(`DELETE FROM nodes WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{`SELECT COUNT(*) FROM node_ping_stats`, `SELECT COUNT(*) FROM node_ping_series_stats`, `SELECT COUNT(*) FROM node_ping_cursors`, `SELECT COUNT(*) FROM node_ping_series_cursors`} {
		var count int
		if err := conn.QueryRow(query).Scan(&count); err != nil || count != 0 {
			t.Fatalf("cascade left %d rows, %v", count, err)
		}
	}
}

func TestCarrierHistoryMigrationRetainsLegacyUpsertKeys(t *testing.T) {
	conn := carrierHistoryDB(t, true)
	if err := db.Migrate(context.Background(), conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 25, 10, 1, 10, 0, time.UTC)
	// These are the exact old writer's conflict keys, not the new writer's
	// selected path. An older binary must still collect its custom probes.
	for seq := 1; seq <= 2; seq++ {
		if _, err := conn.Exec(`INSERT INTO node_ping_stats(node_id,task_id,name,res,ts,samples,lost,sum_ms,sum_mbps,quality_json) VALUES(?,?,?,?,?,1,?,?,?,?) ON CONFLICT(node_id,task_id,name,res,ts) DO UPDATE SET samples=samples+1,lost=lost+excluded.lost,sum_ms=sum_ms+excluded.sum_ms,sum_mbps=sum_mbps+excluded.sum_mbps,quality_json=CASE WHEN ? THEN excluded.quality_json ELSE quality_json END`, 1, 7, "Old custom task", "m", at.Truncate(time.Minute).Unix(), 0, 12, 0, "{}", false); err != nil {
			t.Fatalf("old stats writer failed after migration: %v", err)
		}
		if _, err := conn.Exec(`INSERT INTO node_ping_cursors(node_id,task_id,name,epoch,sequence,last_ms,seen_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(node_id,task_id,name,epoch) DO UPDATE SET sequence=excluded.sequence,last_ms=excluded.last_ms,seen_at=excluded.seen_at`, 1, 7, "Old custom task", "old-run", seq, 12, at.Unix()); err != nil {
			t.Fatalf("old cursor writer failed after migration: %v", err)
		}
	}
	points, err := New(conn).NodePingStats(context.Background(), 1, "m", at.Add(-time.Minute), at)
	if err != nil || len(points) != 1 || points[0].Samples != 2 || points[0].AvgMs != 12 || points[0].Series != "" || points[0].Family != "" {
		t.Fatalf("old writer data was lost or given invented metadata: %+v, %v", points, err)
	}
	var seq int
	if err := conn.QueryRow(`SELECT sequence FROM node_ping_cursors WHERE node_id=1 AND task_id=7 AND name='Old custom task' AND epoch='old-run'`).Scan(&seq); err != nil || seq != 2 {
		t.Fatalf("old cursor update was lost: %d, %v", seq, err)
	}
}
