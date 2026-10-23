package store

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
)

func TestPublicStorageHistoryMeasuredBytesPrivacyAndGaps(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err = db.Migrate(ctx, conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	s := New(conn)
	node := &domain.Node{Name: "history"}
	if err = s.CreateNode(ctx, node, "PAIR", time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RedeemPairCode(ctx, "PAIR", "test-token-hash", "", "0.76.0", "linux"); err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC().Truncate(time.Minute).Add(-5 * time.Minute)
	host := spec.SystemStatus{DiskTotal: 500, DiskUsed: 5, Valid: &spec.MetricValidity{Disk: true}}
	if err = s.RecordBeat(ctx, node.ID, host, base.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	fs := spec.FilesystemResource{ID: "private-identity", Mount: "/private/data", Device: "/dev/example", Type: "fuse.mergerfs", Pool: true, Total: storageUint(1000), Used: storageUint(100), SampledAt: base.UnixMilli()}
	host.Resources = &spec.Resources{Epoch: "run", At: base.UnixMilli(), Sequence: 1, Filesystems: []spec.FilesystemResource{fs}}
	record := func(at time.Time) {
		t.Helper()
		host.Resources.At = at.UnixMilli()
		if err := s.RecordBeat(ctx, node.ID, host, at); err != nil {
			t.Fatal(err)
		}
		host.Resources.Sequence++
	}
	record(base)
	// The same asynchronous success sent in another minute must stay a gap.
	record(base.Add(time.Minute))
	host.Resources.Filesystems[0].SampledAt = base.Add(2 * time.Minute).UnixMilli()
	host.Resources.Filesystems[0].Total = storageUint(2000)
	host.Resources.Filesystems[0].Used = storageUint(400)
	record(base.Add(2 * time.Minute))
	// Persistent history is sufficient after reconstructing the service/store.
	s = New(conn)
	now := base.Add(3 * time.Minute)
	query := func() PublicStorageHistory {
		t.Helper()
		h, err := s.StorageHistory(ctx, node.ID, base.Add(-time.Minute), now, now, false)
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	h := query()
	if h.Res != "m" || h.Step != 60 || len(h.Volumes) != 2 || h.Volumes[0].Kind != "system" || h.Volumes[1].Kind != "pool" {
		t.Fatalf("metadata: %+v", h)
	}
	root, data := h.Volumes[0].Points, h.Volumes[1].Points
	if root[0].Used == nil || *root[0].Used != 5 || *root[0].Total != 500 {
		t.Fatalf("legacy measured root lost: %+v", root[0])
	}
	if data[0].Used != nil || data[2].Used != nil || *data[1].Used != 100 || *data[1].Total != 1000 || *data[3].Used != 400 || *data[3].Total != 2000 {
		t.Fatalf("resize/cached gap: %+v", data)
	}
	for _, metric := range []string{"used", "total", "disk"} {
		history, err := s.ResourceHistory(ctx, node.ID, "1h", "filesystem:"+url.QueryEscape(fs.Mount)+":"+metric, now)
		if err != nil {
			t.Fatal(err)
		}
		samples := 0
		for _, p := range history.Points {
			samples += p.Samples
		}
		if samples != 2 {
			t.Fatalf("%s cached sample counted: %d", metric, samples)
		}
	}
	// Model the previous release's percent-only bucket. The historical 10%
	// must not become 200 bytes by multiplying today's 2000-byte capacity.
	var oldRaw []byte
	if err := conn.QueryRowContext(ctx, `SELECT data FROM node_resource_stats WHERE node_id=? AND res='m' AND ts=?`, node.ID, base.Unix()).Scan(&oldRaw); err != nil {
		t.Fatal(err)
	}
	oldBucket, err := decodeResourceBucket(oldRaw)
	if err != nil {
		t.Fatal(err)
	}
	delete(oldBucket.Values, "filesystem:"+url.QueryEscape(fs.Mount)+":used")
	delete(oldBucket.Values, "filesystem:"+url.QueryEscape(fs.Mount)+":total")
	oldBucket.Truncated = true
	oldRaw, err = encodeResourceBucket(oldBucket)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `UPDATE node_resource_stats SET data=? WHERE node_id=? AND res='m' AND ts=?`, oldRaw, node.ID, base.Unix()); err != nil {
		t.Fatal(err)
	}
	oldHistory := query()
	if !oldHistory.Truncated {
		t.Fatal("historical truncation was hidden from the public projection")
	}
	if p := oldHistory.Volumes[1].Points[1]; p.Used != nil || p.Total != nil {
		t.Fatalf("invented historical capacity: %+v", p)
	}
	set := func(items ...NodeStorageItem) {
		t.Helper()
		policy := NodeStorage{Mode: "manual", Items: items}
		if err := s.UpdateNodeProbeWithRegion(ctx, node.ID, false, NodeProbeInfo{}, 0, 0, "sum", nil, nil, &policy); err != nil {
			t.Fatal(err)
		}
	}
	set(NodeStorageItem{Mount: fs.Mount, Name: "Archive"}, NodeStorageItem{Mount: "/missing", Name: "Backup"})
	h = query()
	if len(h.Volumes) != 2 || h.Volumes[0].Name != "Archive" || h.Volumes[0].ID != "storage-1" || h.Volumes[1].Name != "Backup" {
		t.Fatalf("manual order: %+v", h)
	}
	for _, p := range h.Volumes[1].Points {
		if p.Total != nil || p.Used != nil {
			t.Fatal("missing volume borrowed root history")
		}
	}
	raw, _ := json.Marshal(h)
	for _, private := range []string{fs.Mount, fs.Device, fs.ID, "mount", "device", "sampled_at", "system"} {
		if strings.Contains(string(raw), private) {
			t.Fatalf("private data leaked: %s", raw)
		}
	}
	set()
	if empty := query(); empty.Volumes == nil || len(empty.Volumes) != 0 || empty.Truncated {
		t.Fatalf("manual empty fell back: %+v", empty)
	}
}

func TestStorageHistoryRangeBounds(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		age, width time.Duration
		res        string
	}{
		{0, time.Hour, "m"}, {0, 24 * time.Hour, "m"},
		{0, 7 * 24 * time.Hour, "h"}, {0, 30 * 24 * time.Hour, "d"},
		{2 * 24 * time.Hour, time.Hour, "h"}, {30 * 24 * time.Hour, time.Hour, "d"},
	} {
		to := now.Add(-tc.age)
		res, step, err := StorageHistoryRange(to.Add(-tc.width), to, now)
		if err != nil || res != tc.res || tc.width/step > 1440 {
			t.Fatalf("range %+v: %s %s %v", tc, res, step, err)
		}
	}
	for _, bounds := range [][2]time.Time{{now, now}, {now, now.Add(time.Second)}, {now.Add(-91 * 24 * time.Hour), now}, {time.Time{}, now}} {
		if _, _, err := StorageHistoryRange(bounds[0], bounds[1], now); !errors.Is(err, ErrStorageHistoryRange) {
			t.Fatalf("accepted invalid window: %+v", bounds)
		}
	}
}
