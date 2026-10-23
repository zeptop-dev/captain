package store

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
)

func TestNodeHostTrafficObservationAndReset(t *testing.T) {
	s, id, _ := monitorRig(t)
	ctx := context.Background()
	base := time.Date(2026, 10, 10, 12, 0, 0, 0, time.Local)
	view := func() *NodeHostTraffic {
		t.Helper()
		all, err := s.NodeHostTrafficViews(ctx, 0)
		if err != nil {
			t.Fatal(err)
		}
		one, err := s.NodeHostTrafficViews(ctx, id)
		if err != nil || len(one) != 1 || !reflect.DeepEqual(one[id], all[id]) {
			t.Fatalf("single/batch: %+v %+v %v", one, all, err)
		}
		return one[id]
	}
	if v := view(); v.Valid || v.CycleStart != nil || v.UpdatedAt != nil {
		t.Fatalf("unsampled: %+v", v)
	}
	net := []spec.NetworkResource{{Name: "eth0", ID: "boot:1", Included: true, Up: 1000, Down: 2000}}
	epoch := "one"
	beat := func(seq uint64, at time.Time, nets []spec.NetworkResource) {
		t.Helper()
		if err := s.RecordBeat(ctx, id, spec.SystemStatus{Resources: &spec.Resources{Epoch: epoch, Sequence: seq, Networks: nets}}, at); err != nil {
			t.Fatal(err)
		}
	}
	beat(1, base, net)
	if v := view(); v.Valid || v.CycleStart == nil || v.CycleEnd == nil || v.UpdatedAt != nil {
		t.Fatalf("baseline is not a measured zero: %+v", v)
	}
	beat(2, base.Add(time.Second), net)
	if v := view(); !v.Valid || v.UsedUp != 0 || v.UsedDown != 0 || v.UpdatedAt == nil || *v.UpdatedAt != base.Add(time.Second).Unix() {
		t.Fatalf("measured zero: %+v", v)
	}
	beat(3, base.Add(2*time.Second), nil)
	if v := view(); !v.Valid || v.UpdatedAt == nil || *v.UpdatedAt != base.Add(time.Second).Unix() {
		t.Fatalf("missing network refreshed time: %+v", v)
	}
	beat(4, base.Add(3*time.Second), net)
	if v := view(); v.UpdatedAt == nil || *v.UpdatedAt != base.Add(time.Second).Unix() {
		t.Fatalf("new baseline refreshed time: %+v", v)
	}
	net[0].Up += 500
	net[0].Down += 700
	beat(5, base.Add(4*time.Second), net)
	if v := view(); !v.Valid || v.UsedUp != 500 || v.UsedDown != 700 {
		t.Fatalf("measured usage: %+v", v)
	}
	epoch = "restarted"
	beat(1, base.Add(5*time.Second), net)
	if v := view(); !v.Valid || v.UsedUp != 500 || v.UsedDown != 700 || v.UpdatedAt == nil || *v.UpdatedAt != base.Add(4*time.Second).Unix() {
		t.Fatalf("new sampler baseline refreshed observation: %+v", v)
	}
	reset := base.Add(6 * time.Second)
	if err := s.ResetNodeTraffic(ctx, id, reset); err != nil {
		t.Fatal(err)
	}
	if v := view(); v.Valid || v.UsedUp != 0 || v.UsedDown != 0 || v.UpdatedAt != nil || v.CycleStart == nil || *v.CycleStart != reset.Unix() {
		t.Fatalf("manual reset claimed a measurement: %+v", v)
	}
	beat(2, base.Add(7*time.Second), net)
	if v := view(); !v.Valid || v.UpdatedAt == nil || *v.UpdatedAt != base.Add(7*time.Second).Unix() || v.CycleStart == nil || *v.CycleStart != reset.Unix() {
		t.Fatalf("fresh measured zero after reset: %+v", v)
	}
	nextMonth := time.Date(2026, 11, 1, 0, 0, 0, 0, time.Local)
	beat(3, nextMonth, nil)
	if v := view(); v.Valid || v.UpdatedAt != nil || v.CycleStart == nil || *v.CycleStart != nextMonth.Unix() {
		t.Fatalf("empty new period claimed old observation: %+v", v)
	}
	missing, err := s.NodeHostTrafficViews(ctx, id+100)
	if err != nil || len(missing) != 0 {
		t.Fatalf("unknown server: %+v %v", missing, err)
	}
}

func TestNodeHostTrafficLegacyAndCycleEnd(t *testing.T) {
	for _, tc := range []struct {
		start, end, expiry, mode string
		day                      int
	}{
		{"2027-02-28", "2027-03-31", "", "manual", 31},
		{"2028-02-29", "2028-03-31", "2027-01-31", "expiry", 1},
		{"2027-02-01", "2027-03-01", "2027-01-31", "expiry_next", 1},
		{"2027-06-15", "2027-07-10", "2027-06-09", "expiry_next", 1},
	} {
		t.Run(tc.start+tc.mode, func(t *testing.T) {
			start, err := time.ParseInLocation("2006-01-02", tc.start, time.Local)
			if err != nil {
				t.Fatal(err)
			}
			p := NodeProbe{PeriodStart: start, ResetDay: tc.day, UsedUp: 500, UsedDown: 700, Info: NodeProbeInfo{ExpiresAt: tc.expiry, TrafficReset: &NodeTrafficReset{Mode: tc.mode}}}
			v := hostTrafficView(&p)
			if !v.Valid || v.UpdatedAt != nil || v.CycleEnd == nil || time.Unix(*v.CycleEnd, 0).In(time.Local).Format("2006-01-02") != tc.end {
				t.Fatalf("legacy known usage/cycle: %+v", v)
			}
			p.UsedUp, p.UsedDown = 0, 0
			if hostTrafficView(&p).Valid {
				t.Fatal("legacy zero without measurement became known")
			}
		})
	}
}

func TestNodeHostTrafficExcludingTunnelKeepsPhysicalDelta(t *testing.T) {
	s, id, _ := monitorRig(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 10, 12, 0, 0, 0, time.Local)
	nets := []spec.NetworkResource{{Name: "eth0", ID: "boot:1", Included: true, Up: 100, Down: 200}, {Name: "bwg2", ID: "boot:2", Included: true, Up: 100, Down: 200}}
	for seq := uint64(1); seq <= 3; seq++ {
		if seq > 1 {
			for i := range nets {
				nets[i].Up += 10
				nets[i].Down += 20
			}
		}
		if seq == 3 {
			nets[1].Included = false
		}
		if err := s.RecordBeat(ctx, id, spec.SystemStatus{Resources: &spec.Resources{Epoch: "one", Sequence: seq, Networks: nets}}, at.Add(time.Duration(seq)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	v, err := s.NodeHostTrafficViews(ctx, id)
	if err != nil || !v[id].Valid || v[id].UsedUp != 30 || v[id].UsedDown != 60 {
		t.Fatalf("selection change removed eth0 delta or changed history: %+v %v", v[id], err)
	}
}

func TestNodeTrafficTodayUsesUTCAndInboundOwner(t *testing.T) {
	s, a, _ := monitorRig(t)
	ctx := context.Background()
	b := &domain.Node{Name: "exit"}
	if err := s.CreateNode(ctx, b, "exit", time.Hour); err != nil {
		t.Fatal(err)
	}
	ib := &domain.Inbound{NodeID: a, Tag: "customer", Protocol: spec.VLESS, Port: 443, Enabled: true}
	if err := s.CreateInbound(ctx, ib); err != nil {
		t.Fatal(err)
	}
	u := &domain.User{Email: "customer@example.com", UUID: "traffic-user", Status: "active"}
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	local := time.FixedZone("UTC+8", 8*3600)
	before := time.Date(2026, 10, 22, 0, 5, 0, 0, local)
	after := time.Date(2026, 10, 22, 8, 5, 0, 0, local)
	if err := s.AddTraffic(ctx, u.ID, ib.ID, 10, 20, before); err != nil {
		t.Fatal(err)
	}
	if err := s.AddTraffic(ctx, u.ID, ib.ID, 40, 50, after); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		at   time.Time
		want int64
	}{{before, 30}, {after, 90}} {
		v, err := s.NodeTrafficToday(ctx, tc.at)
		if err != nil || v[a] != tc.want || v[b.ID] != 0 {
			t.Fatalf("UTC/ownership: %+v %v", v, err)
		}
	}
}
