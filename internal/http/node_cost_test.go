package http

import (
	"context"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestNodeCostAPIAndScheduleEdit(t *testing.T) {
	for _, mode := range []string{"expiry_next", "expiry_previous"} {
		t.Run(mode, func(t *testing.T) { testNodeCostAPIAndScheduleEdit(t, mode) })
	}
}

func testNodeCostAPIAndScheduleEdit(t *testing.T, mode string) {
	r := newRig(t)
	ctx := context.Background()
	path := "/api/admin/nodes/" + itoa(r.nodeID) + "/probe"
	call := func(info any, day int, want int) {
		t.Helper()
		status, b, _ := r.c.do("PUT", path, map[string]any{"Info": info, "ResetDay": day, "Mode": "sum"}, nil)
		if status != want {
			t.Fatalf("%d %s", status, b)
		}
	}
	call(map[string]any{"price": "legacy text", "traffic_reset": map[string]string{"mode": "manual"}}, 1, 200)
	at := time.Now()
	for i := 0; i < 2; i++ {
		if err := r.st.RecordBeat(ctx, r.nodeID, spec.SystemStatus{NetTotalUp: uint64(100 + i*100), NetTotalDown: uint64(100 + i*100)}, at.Add(time.Duration(i)*time.Millisecond)); err != nil {
			t.Fatal(err)
		}
	}
	before, _ := r.st.NodeProbe(ctx, r.nodeID)
	info := map[string]any{"expires_at": "2027-06-09", "billing": map[string]string{"amount": "36.36", "currency": "USD", "cycle": "custom", "years": "1.5"}, "traffic_reset": map[string]string{"mode": mode}}
	call(info, 1, 200)
	p, err := r.st.NodeProbe(ctx, r.nodeID)
	if err != nil || p.Info.Price != "USD 36.36 / 1.5 years" || p.Info.TrafficReset.Mode != mode {
		t.Fatal(p, err)
	}
	if err := r.st.RecordBeat(ctx, r.nodeID, spec.SystemStatus{NetTotalUp: 300, NetTotalDown: 300}, time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	after, _ := r.st.NodeProbe(ctx, r.nodeID)
	if after.Billed() != before.Billed()+200 {
		t.Fatalf("schedule edit lost traffic: before %d after %d", before.Billed(), after.Billed())
	}
	// An old client echo must preserve the newly added fields.
	call(map[string]any{"price": p.Info.Price, "expires_at": p.Info.ExpiresAt}, 1, 200)
	p, _ = r.st.NodeProbe(ctx, r.nodeID)
	if p.Info.Billing == nil || p.Info.TrafficReset.Mode != mode {
		t.Fatal("legacy client cleared new settings")
	}
	info["expires_at"] = ""
	call(info, 1, 400)
	info["expires_at"] = "2027-02-30"
	call(info, 1, 400)
	info["expires_at"] = "2027-06-09"
	info["billing"] = &store.NodeCost{Amount: "0", Currency: "USD", Cycle: "annual"}
	call(info, 31, 200)
	p, _ = r.st.NodeProbe(ctx, r.nodeID)
	if p.Info.Billing.Amount != "0" {
		t.Fatal("zero price lost")
	}
}
