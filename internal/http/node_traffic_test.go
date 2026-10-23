package http

import (
	"context"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

type serverTrafficView struct {
	ID    int64 `json:"id"`
	Proxy int64 `json:"traffic_today_bytes"`
	Host  *struct {
		Valid bool   `json:"valid"`
		Up    int64  `json:"used_up"`
		Down  int64  `json:"used_down"`
		Start *int64 `json:"cycle_start"`
		End   *int64 `json:"cycle_end"`
		At    *int64 `json:"updated_at"`
	} `json:"host_traffic"`
}

func nodeTrafficViews(t *testing.T, r *rig, id int64) []serverTrafficView {
	t.Helper()
	code, body, _ := r.c.do("GET", "/api/admin/nodes", nil, nil)
	if code != 200 {
		t.Fatalf("list: %d %s", code, body)
	}
	var listed serverTrafficView
	for _, n := range mustJSON[[]serverTrafficView](t, body) {
		if n.ID == id {
			listed = n
		}
	}
	code, body, _ = r.c.do("GET", "/api/admin/nodes/"+itoa(id), nil, nil)
	if code != 200 {
		t.Fatalf("detail: %d %s", code, body)
	}
	detail := mustJSON[struct {
		Node serverTrafficView `json:"node"`
	}](t, body)
	return []serverTrafficView{listed, detail.Node}
}

func TestNodeTrafficListDetailSeparatesProxyAndHost(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	uid, _ := r.user("traffic@example.com")
	ibs, err := r.st.InboundsByNode(ctx, r.nodeID)
	if err != nil || len(ibs) != 1 {
		t.Fatal(ibs, err)
	}
	now := time.Now()
	if err := r.st.AddTraffic(ctx, uid, ibs[0].ID, 100, 200, now); err != nil {
		t.Fatal(err)
	}
	views := nodeTrafficViews(t, r, r.nodeID)
	for _, v := range views {
		if v.Proxy != 300 {
			t.Fatalf("list/detail proxy traffic differs: %+v", v)
		}
	}
	for _, v := range views {
		if v.Host == nil || v.Host.Valid || v.Host.At != nil {
			t.Fatalf("unknown host usage presented as measured: %+v", v.Host)
		}
	}
	// Counting mode limits the quota only; the server view retains both directions.
	p, err := r.st.NodeProbe(ctx, r.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.st.UpdateNodeProbe(ctx, r.nodeID, p.Hidden, p.Info, 10000, 1, "up"); err != nil {
		t.Fatal(err)
	}
	for i := uint64(0); i < 2; i++ {
		h := spec.SystemStatus{Resources: &spec.Resources{Epoch: "test", Sequence: i + 1, Networks: []spec.NetworkResource{{Name: "eth0", ID: "boot:1", Included: true, Up: 1000 + 500*i, Down: 2000 + 700*i}}}}
		if err := r.st.RecordBeat(ctx, r.nodeID, h, now.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range nodeTrafficViews(t, r, r.nodeID) {
		if v.Proxy != 300 || v.Host == nil || !v.Host.Valid || v.Host.Up != 500 || v.Host.Down != 700 || v.Host.At == nil || *v.Host.At != now.Add(time.Second).Unix() || v.Host.Start == nil || v.Host.End == nil || *v.Host.Start >= *v.Host.End {
			t.Fatalf("server and subscriber traffic mixed: %+v host=%+v", v, v.Host)
		}
	}
}
