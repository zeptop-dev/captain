package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
)

func TestResourceValidityAndReplay(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open("sqlite", t.TempDir()+"/metrics.db")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := db.Migrate(ctx, conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	s := New(conn)
	n := &domain.Node{Name: "node"}
	if err := s.CreateNode(ctx, n, "PAIR", time.Hour); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	h := spec.SystemStatus{CPUPercent: 40, MemUsed: 50, MemTotal: 100, Valid: &spec.MetricValidity{CPU: true, Memory: true}, Resources: &spec.Resources{Epoch: "first", Sequence: 1}}
	if err := s.RecordBeat(ctx, n.ID, h, base); err != nil {
		t.Fatal(err)
	}
	h.Resources.Sequence = 2
	h.CPUPercent = 0 // measured zero participates
	if err := s.RecordBeat(ctx, n.ID, h, base.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordBeat(ctx, n.ID, h, base.Add(2*time.Second)); !errors.Is(err, ErrStaleBeat) {
		t.Fatalf("replay: %v", err)
	}
	h.Resources.Sequence = 3
	h.Valid = &spec.MetricValidity{}
	h.CPUPercent = 100
	h.MemUsed = 999 // invalid values must not enter sums
	if err := s.RecordBeat(ctx, n.ID, h, base.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	for _, res := range []string{"m", "h", "d"} {
		points, err := s.NodeStats(ctx, n.ID, res, base.Add(-24*time.Hour), base.Add(time.Minute))
		if err != nil || len(points) != 1 {
			t.Fatalf("%s: %v %v", res, points, err)
		}
		p := points[0]
		if p.Samples != 3 || p.CPU != 20 || p.MemUsed != 50 || p.MemTotal != 100 || !p.Valid.CPU || p.Valid.Network {
			t.Fatalf("bad validity: %+v", p)
		}
	}
	if avg, ok := s.SustainedAverage(ctx, n.ID, "cpu", time.Minute, base.Add(5*time.Second)); !ok || avg != 20 {
		t.Fatalf("alert average %v %v", avg, ok)
	}
	if _, ok := s.SustainedAverage(ctx, n.ID, "disk", time.Minute, base.Add(5*time.Second)); ok {
		t.Fatal("missing disk triggered a valid average")
	}
}

func TestMonthlyCountersFollowDeviceIdentity(t *testing.T) {
	nic := func(name, id string, included bool, n uint64) spec.NetworkResource {
		return spec.NetworkResource{Name: name, ID: id, Included: included, Up: n, Down: n}
	}
	old := networkCounters{Epoch: "agent", Sequence: 1, Networks: []spec.NetworkResource{nic("eth0", "boot:1", true, 100), nic("docker0", "boot:2", false, 100)}}
	cases := []struct {
		name, epoch string
		nets        []spec.NetworkResource
		want        int64
	}{
		{"normal", "agent", []spec.NetworkResource{nic("eth0", "boot:1", true, 150)}, 50},
		{"add NIC", "agent", []spec.NetworkResource{nic("eth0", "boot:1", true, 150), nic("eth1", "boot:3", true, 1<<40)}, 50},
		{"counter reset", "agent", []spec.NetworkResource{nic("eth0", "boot:1", true, 1)}, 0},
		{"interface recreated", "agent", []spec.NetworkResource{nic("eth0", "boot:3", true, 1000)}, 0},
		{"host reboot", "agent", []spec.NetworkResource{nic("eth0", "next:1", true, 1000)}, 0},
		{"agent restart", "next", []spec.NetworkResource{nic("eth0", "boot:1", true, 1000)}, 0},
		{"selection changes", "agent", []spec.NetworkResource{nic("eth0", "boot:1", false, 1000), nic("docker0", "boot:2", true, 1000)}, 0},
		{"missing", "agent", nil, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			up, down, next, _ := nextNetworkCounters(old, &spec.Resources{Epoch: tc.epoch, Sequence: 2, Networks: tc.nets})
			if up != tc.want || down != tc.want || next.Sequence != 2 {
				t.Fatalf("delta %d/%d next=%+v", up, down, next)
			}
		})
	}
}

func TestMonthlyResourceCountersPersistAcrossStoreReopen(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/counters.db"
	conn, err := db.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	s := New(conn)
	n := &domain.Node{Name: "node"}
	if err := s.CreateNode(ctx, n, "PAIR", time.Hour); err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	sample := spec.SystemStatus{Resources: &spec.Resources{Epoch: "run", Sequence: 1, Networks: []spec.NetworkResource{{Name: "eth0", ID: "boot:1", Included: true, Up: 1000, Down: 2000}}}}
	if err := s.RecordBeat(ctx, n.ID, sample, at); err != nil {
		t.Fatal(err)
	}
	conn.Close()
	conn, err = db.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	s = New(conn)
	sample.Resources.Sequence = 2
	sample.Resources.Networks[0].Up += 500
	sample.Resources.Networks[0].Down += 600
	if err := s.RecordBeat(ctx, n.ID, sample, at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	p, err := s.NodeProbe(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.UsedUp != 500 || p.UsedDown != 600 {
		t.Fatalf("lost persisted NIC baseline: %+v", p)
	}
	if err := s.RecordBeat(ctx, n.ID, sample, at.Add(2*time.Second)); !errors.Is(err, ErrStaleBeat) {
		t.Fatal("replayed persisted sample counted")
	}
}
