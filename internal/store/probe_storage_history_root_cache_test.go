package store

import (
	"context"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
)

// The root-summary compatibility fallback must preserve genuinely measured
// legacy history without filling gaps left by the asynchronous sampler.
func TestPublicStorageHistoryRootCacheKeepsGapsAndLegacyBytes(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "root-cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := db.Migrate(ctx, conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	s := New(conn)
	node := &domain.Node{Name: "storage history"}
	if err := s.CreateNode(ctx, node, "PAIR", time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RedeemPairCode(ctx, "PAIR", "test-token-hash", "", "0.76.0", "linux"); err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC().Truncate(time.Minute).Add(-4 * time.Minute)
	legacyAt := base.Add(-2 * time.Minute)
	host := spec.SystemStatus{DiskTotal: 500, DiskUsed: 5, Valid: &spec.MetricValidity{Disk: true}}
	if err := s.RecordBeat(ctx, node.ID, host, legacyAt); err != nil {
		t.Fatal(err)
	}

	// Before byte-pair history existed, a data volume had only a percentage.
	// Its old bytes remain unknown even though today's capacity is available.
	dataMount := "/example/data"
	var oldRaw []byte
	if err := conn.QueryRowContext(ctx, `SELECT data FROM node_resource_stats WHERE node_id=? AND res='m' AND ts=?`, node.ID, legacyAt.Unix()).Scan(&oldRaw); err != nil {
		t.Fatal(err)
	}
	oldBucket, err := decodeResourceBucket(oldRaw)
	if err != nil {
		t.Fatal(err)
	}
	oldBucket.Values["filesystem:"+url.QueryEscape(dataMount)+":disk"] = resourceAggregate{N: 1, Sum: 10, Peak: 10}
	oldRaw, err = encodeResourceBucket(oldBucket)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `UPDATE node_resource_stats SET data=? WHERE node_id=? AND res='m' AND ts=?`, oldRaw, node.ID, legacyAt.Unix()); err != nil {
		t.Fatal(err)
	}

	host.DiskUsed, host.DiskTotal = 100, 1000
	host.Resources = &spec.Resources{Epoch: "run", At: base.UnixMilli(), Sequence: 1, Filesystems: []spec.FilesystemResource{
		{ID: "root-clock", Mount: "/", Total: storageUint(1000), Used: storageUint(100), SampledAt: base.UnixMilli()},
		{ID: "data-clock", Mount: dataMount, Total: storageUint(10000), Used: storageUint(1000), SampledAt: base.UnixMilli()},
	}}
	if err := s.RecordBeat(ctx, node.ID, host, base); err != nil {
		t.Fatal(err)
	}
	// A different heartbeat with the same successful filesystem sample is
	// still cached. Its legacy root summary must not resurrect a new point.
	cachedAt := base.Add(time.Minute)
	host.Resources.At = cachedAt.UnixMilli()
	host.Resources.Sequence++
	if err := s.RecordBeat(ctx, node.ID, host, cachedAt); err != nil {
		t.Fatal(err)
	}
	now := cachedAt.Add(30 * time.Second)
	private, err := s.ResourceHistory(ctx, node.ID, "1h", "filesystem:"+url.QueryEscape("/")+":used", now)
	if err != nil {
		t.Fatal(err)
	}
	for _, point := range private.Points {
		if point.TS == cachedAt.Unix() && (point.Samples != 0 || point.Average != nil) {
			t.Fatalf("cached root was already counted in detailed history: %+v", point)
		}
	}
	// Reconstruct the store as well: visibility and compatibility cannot depend
	// on remembering the upgrade boundary only in process-local state.
	s = New(conn)
	history, err := s.StorageHistory(ctx, node.ID, legacyAt, now, now, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(history.Volumes) != 2 || history.Volumes[0].Kind != "system" {
		t.Fatalf("unexpected selected volumes: %+v", history.Volumes)
	}
	pointAt := func(volume int, at time.Time) PublicStorageHistoryPoint {
		t.Helper()
		for _, point := range history.Volumes[volume].Points {
			if point.TS == at.Unix() {
				return point
			}
		}
		t.Fatalf("missing timestamp %v for volume %d", at, volume)
		return PublicStorageHistoryPoint{}
	}
	legacyRoot := pointAt(0, legacyAt)
	if legacyRoot.Used == nil || legacyRoot.Total == nil || *legacyRoot.Used != 5 || *legacyRoot.Total != 500 {
		t.Fatalf("real legacy root bytes lost after a new collector report: %+v", legacyRoot)
	}
	legacyData := pointAt(1, legacyAt)
	if legacyData.Used != nil || legacyData.Total != nil {
		t.Fatalf("old data-volume percentage invented historical bytes: %+v", legacyData)
	}
	for i, expected := range []float64{100, 1000} {
		fresh := pointAt(i, base)
		if fresh.Used == nil || fresh.Total == nil || *fresh.Used != expected || *fresh.Total != expected*10 {
			t.Fatalf("fresh volume %d bytes lost: %+v", i, fresh)
		}
		cached := pointAt(i, cachedAt)
		if cached.Used != nil || cached.Total != nil {
			t.Errorf("cached volume %d was restored by compatibility fallback: %+v", i, cached)
		}
	}
}
