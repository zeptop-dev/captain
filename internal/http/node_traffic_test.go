package http

import (
	"context"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

type serverTrafficView struct {
	ID         int64                   `json:"id"`
	Proxy      int64                   `json:"traffic_today_bytes"`
	Attributed *store.NodeProxyTraffic `json:"proxy_traffic"`
	Host       *struct {
		Valid bool   `json:"valid"`
		Up    int64  `json:"used_up"`
		Down  int64  `json:"used_down"`
		Start *int64 `json:"cycle_start"`
		End   *int64 `json:"cycle_end"`
		At    *int64 `json:"updated_at"`
	} `json:"host_traffic"`
}

func TestNodeProxyTrafficReverseExitAndLegacyReport(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	uid, _ := r.user("exit-traffic@example.com")
	ibs, err := r.st.InboundsByNode(ctx, r.nodeID)
	if err != nil || len(ibs) != 1 {
		t.Fatal(ibs, err)
	}
	b := &domain.Node{Name: "exit"}
	if err := r.st.CreateNode(ctx, b, "exit-pair", time.Hour); err != nil {
		t.Fatal(err)
	}
	w := store.WireGuardWrite{
		Link:  store.WireGuardLink{ID: "attribution", TransitID: r.nodeID, ExitID: b.ID, Endpoint: "192.0.2.10:51820", ListenPort: 51820, MTU: 1380},
		User:  domain.Inbound{NodeID: r.nodeID, Tag: "attribution-user", Protocol: spec.VLESS, Port: 18443, Core: "xray", Enabled: true},
		Entry: domain.Entry{Name: "reverse", DisplayHost: "example.com", DisplayPort: 18443, Enabled: true},
	}
	if err := r.st.SaveWireGuardLink(ctx, &w); err != nil {
		t.Fatal(err)
	}
	user, err := r.st.UserByID(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	report := agentproto.Report{TrafficSeq: 41, Traffic: []spec.UserTraffic{
		{UserID: user.AgentID, Inbound: w.User.Tag, Up: 100, Down: 200},
		{UserID: user.AgentID, Inbound: ibs[0].Tag, Up: 8, Down: 9},
		{UserID: user.AgentID, Down: 500},
		{UserID: user.AgentID, Inbound: "removed-inbound", Down: 60},
	}}
	for range 2 {
		if code, body, _ := r.agent.do("POST", "/api/agent/report", report, nil); code != 200 {
			t.Fatalf("report: %d %s", code, body)
		}
	}
	for _, v := range nodeTrafficViews(t, r, r.nodeID) {
		if v.Proxy != 877 || v.Attributed == nil || v.Attributed.TodayBytes != 577 || v.Attributed.CollectedTodayBytes != 877 || v.Attributed.ForwardedTodayBytes != 300 || v.Attributed.UnattributedTodayBytes != 560 {
			t.Fatalf("A API: %+v attribution=%+v", v, v.Attributed)
		}
	}
	for _, v := range nodeTrafficViews(t, r, b.ID) {
		if v.Proxy != 0 || v.Attributed == nil || v.Attributed.TodayBytes != 300 || v.Attributed.CollectedTodayBytes != 0 || v.Attributed.ReceivedTodayBytes != 300 {
			t.Fatalf("B API: %+v attribution=%+v", v, v.Attributed)
		}
	}
	sub, err := r.st.ActiveSubscription(ctx, uid)
	if err != nil || sub.UsedUpBytes+sub.UsedDownBytes != 877 {
		t.Fatal("projection charged the user again", sub, err)
	}
	if err := r.st.DeleteWireGuardLink(ctx, b.ID, w.Link.ID, w.Link.Version); err != nil {
		t.Fatal(err)
	}
	for _, v := range nodeTrafficViews(t, r, b.ID) {
		if v.Attributed == nil || v.Attributed.TodayBytes != 300 {
			t.Fatalf("deleting connection removed exit history: %+v", v.Attributed)
		}
	}
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
		if v.Attributed == nil || v.Attributed.TodayBytes != 300 || v.Attributed.CollectedTodayBytes != 300 || v.Attributed.ReceivedTodayBytes != 0 || v.Attributed.ForwardedTodayBytes != 0 {
			t.Fatalf("ordinary inbound attribution differs: %+v", v.Attributed)
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
