package store

import (
	"context"
	"encoding/json"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
)

func storageUint(v uint64) *uint64 { return &v }

func TestStorageCandidatesAndProjection(t *testing.T) {
	at := time.Now()
	f := func(m string, n uint64) spec.FilesystemResource {
		return spec.FilesystemResource{ID: m, Mount: m, Device: "/dev/example", Type: "ext4", Total: storageUint(n), Used: storageUint(10), SampledAt: at.UnixMilli()}
	}
	pool := f("/data", 4000)
	pool.Type = "fuse.mergerfs"
	pool.Pool = true
	pool.MemberMounts = []string{"/mnt/disk1", "/mnt/disk2"}
	h := &spec.SystemStatus{DiskTotal: 500, DiskUsed: 10, Resources: &spec.Resources{At: at.UnixMilli(), Filesystems: []spec.FilesystemResource{f("/boot", 100), f("/mnt/disk1", 2000), f("/", 500), pool, f("/mnt/disk2", 2000)}}}
	candidates := StorageCandidates(h, at, at)
	got := (NodeStorage{}).Public(candidates)
	if len(got) != 2 || got[0].Kind != "system" || got[1].Kind != "pool" || *got[1].Total != 4000 {
		t.Fatalf("default: %+v", got)
	}
	if *h.Resources.Filesystems[3].Total != 4000 {
		t.Fatal("source mutated")
	}
	manual := NodeStorage{Mode: "manual", Items: []NodeStorageItem{{Mount: "/data", Name: "Archive"}, {Mount: "/", Name: "System"}, {Mount: "/missing", Name: "Backup"}}}
	got = manual.Public(candidates)
	if len(got) != 3 || got[0].Name != "Archive" || got[2].Total != nil || got[2].Used != nil {
		t.Fatalf("manual ordering/missing: %+v", got)
	}
	raw, _ := json.Marshal(got)
	for _, private := range []string{"/data", "/dev/", "/mnt/", "mount", "device", "member", "sampled_at"} {
		if strings.Contains(string(raw), private) {
			t.Fatalf("private field leaked: %s", raw)
		}
	}
	if got := (NodeStorage{Mode: "manual"}).Public(candidates); got == nil || len(got) != 0 {
		t.Fatal("explicit empty must stay empty")
	}
	if (NodeStorage{Mode: "manual", Items: []NodeStorageItem{{Mount: "/data"}}}).RootVisible() {
		t.Fatal("hidden root remains visible")
	}
	// Mount identity, not equal capacities, drives deduplication. Two independent
	// disks with identical capacities remain separately selectable.
	h.Resources.Filesystems = append(h.Resources.Filesystems, f("/mnt/disk3", 2000), f("/mnt/disk3", 2000))
	if c := StorageCandidates(h, at, at); len(c) != 6 {
		t.Fatalf("dedup: %+v", c)
	}
	// Shared-space datasets remain separate views and are never summed.
	h.Resources.Filesystems = []spec.FilesystemResource{f("/", 500), f("/home", 500)}
	h.Resources.Filesystems[0].Type = "btrfs"
	h.Resources.Filesystems[1].Type = "btrfs"
	if c := StorageCandidates(h, at, at); len(c) != 2 || *c[0].Total != 500 || *c[1].Total != 500 {
		t.Fatalf("shared pool: %+v", c)
	}
}

func TestStorageFreshnessAndLegacy(t *testing.T) {
	received := time.Now()
	agentAt := received.Add(24 * time.Hour)
	f := spec.FilesystemResource{ID: "volume", Mount: "/data", SampledAt: agentAt.Add(-20 * time.Second).UnixMilli(), Total: storageUint(100), Used: storageUint(0)}
	h := &spec.SystemStatus{Resources: &spec.Resources{At: agentAt.UnixMilli(), Filesystems: []spec.FilesystemResource{f}}}
	data := func(now time.Time) StorageCandidate {
		for _, c := range StorageCandidates(h, received, now) {
			if c.Mount == "/data" {
				return c
			}
		}
		t.Fatal("missing data")
		return StorageCandidate{}
	}
	if c := data(received); c.Used == nil || *c.Used != 0 {
		t.Fatal("clock skew or measured zero rejected")
	}
	if c := data(received.Add(71 * time.Second)); c.Used != nil {
		t.Fatal("cached reading made fresh")
	}
	h.Resources.Filesystems[0].SampledAt = agentAt.Add(time.Minute).UnixMilli()
	if c := data(received); c.Total != nil {
		t.Fatal("future reading accepted")
	}
	h.Resources.Filesystems[0].SampledAt = 0
	if c := data(received); c.Total != nil {
		t.Fatal("new collector pending reading accepted")
	}
	h.Resources.Filesystems[0].ID = ""
	if c := data(received); c.Total == nil {
		t.Fatal("legacy synchronous reading lost")
	}
	h = &spec.SystemStatus{DiskTotal: 100, DiskUsed: 25}
	c := StorageCandidates(h, received, received)
	if len(c) != 1 || c[0].Total == nil || *c[0].Used != 25 {
		t.Fatalf("legacy root: %+v", c)
	}
	if c := StorageCandidates(h, received, received.Add(91*time.Second)); c[0].Total != nil {
		t.Fatal("offline root made fresh")
	}
}

func TestStorageSnapshotClockNormalization(t *testing.T) {
	received := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	for _, skew := range []time.Duration{-24 * time.Hour, 24 * time.Hour} {
		clock := received.Add(skew).UnixMilli()
		f := spec.FilesystemResource{ID: "data", Mount: "/data", SampledAt: clock - 17000, Total: storageUint(100), Used: storageUint(10)}
		h := spec.SystemStatus{Resources: &spec.Resources{At: clock, Filesystems: []spec.FilesystemResource{f}}}
		out := MonitorSnapshot(h, received)
		if out.Resources.Filesystems[0].SampledAt != received.Add(-17*time.Second).UnixMilli() {
			t.Fatalf("skew %s: filesystem clock was not normalized: %+v", skew, out.Resources.Filesystems[0])
		}
		if h.Resources.At != clock || h.Resources.Filesystems[0].SampledAt != f.SampledAt {
			t.Fatal("normalization changed the original report used for history")
		}
		if !filesystemFresh(out.Resources.Filesystems[0], out.Resources.At, received, received) || filesystemFresh(out.Resources.Filesystems[0], out.Resources.At, received, received.Add(74*time.Second)) {
			t.Fatal("normalization changed measurement age")
		}
		for _, sample := range []int64{0, -1, clock + 60000, clock - 91000} {
			h.Resources.Filesystems[0].SampledAt = sample
			out = MonitorSnapshot(h, received)
			if filesystemFresh(out.Resources.Filesystems[0], out.Resources.At, received, received) {
				t.Fatalf("invalid or stale sample became fresh: %d", sample)
			}
		}
	}
}

func TestStorageConfigValidation(t *testing.T) {
	for _, bad := range []NodeStorage{
		{Mode: "bogus"}, {Mode: "manual", Items: []NodeStorageItem{{Mount: "relative"}}},
		{Mode: "manual", Items: []NodeStorageItem{{Mount: "/data/../etc"}}},
		{Mode: "manual", Items: []NodeStorageItem{{Mount: "/data"}, {Mount: "/data"}}},
		{Mode: "manual", Items: []NodeStorageItem{{Mount: "/data", Name: "<b>bad</b>"}}},
		{Mode: "manual", Items: []NodeStorageItem{{Mount: "/data", Name: strings.Repeat("界", 33)}}},
		{Mode: "manual", Items: make([]NodeStorageItem, 33)},
	} {
		if bad.Validate() == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	good := NodeStorage{Mode: "manual", Items: []NodeStorageItem{{Mount: "/my data", Name: " 数据池 "}}}
	if good.Validate() != nil || good.Items[0].Name != "数据池" {
		t.Fatalf("valid unicode/path: %+v", good)
	}
}

func TestStorageHistoryDeduplicatesAsyncSamples(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "storage.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := db.Migrate(ctx, conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	s := New(conn)
	node := &domain.Node{Name: "storage"}
	if err := s.CreateNode(ctx, node, "PAIR", time.Hour); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	f := spec.FilesystemResource{ID: "data", Mount: "/data", SampledAt: at.UnixMilli(), Total: storageUint(100), Used: storageUint(10)}
	h := spec.SystemStatus{Resources: &spec.Resources{Epoch: "run", At: at.UnixMilli(), Sequence: 1, Filesystems: []spec.FilesystemResource{f}}}
	record := func(offset time.Duration) {
		t.Helper()
		if err := s.RecordBeat(ctx, node.ID, h, at.Add(offset)); err != nil {
			t.Fatal(err)
		}
		h.Resources.Sequence++
	}
	record(0)
	h.Resources.At = at.Add(10 * time.Second).UnixMilli()
	h.Resources.Filesystems = nil
	record(10 * time.Second)
	// Recreate Store to prove the cursor is in the transactionally persisted JSON.
	s = New(s.DB())
	h.Resources.Filesystems = []spec.FilesystemResource{f}
	h.Resources.At = at.Add(20 * time.Second).UnixMilli()
	record(20 * time.Second)
	h.Resources.Filesystems[0].SampledAt = at.Add(30 * time.Second).UnixMilli()
	h.Resources.Filesystems[0].Used = storageUint(30)
	h.Resources.At = at.Add(30 * time.Second).UnixMilli()
	record(30 * time.Second)
	hist, err := s.ResourceHistory(ctx, node.ID, "1h", "filesystem:"+url.QueryEscape("/data")+":disk", at.Add(40*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	point := hist.Points[len(hist.Points)-1]
	if point.Samples != 2 || point.Average == nil || *point.Average != 20 {
		t.Fatalf("cached history counted: %+v", point)
	}
	np, err := s.NodeProbe(ctx, node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(np.counters.Filesystems, map[string]int64{"data": at.Add(30 * time.Second).UnixMilli()}) {
		t.Fatalf("cursor: %+v", np.counters)
	}
}
