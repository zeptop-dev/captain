package store

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
)

func proxyTrafficWG(t *testing.T, s *Store, a, b int64, name string) WireGuardWrite {
	t.Helper()
	w := WireGuardWrite{
		Link:  WireGuardLink{ID: name, TransitID: a, ExitID: b, Endpoint: "192.0.2.10:51820", ListenPort: 51820, MTU: 1380},
		User:  domain.Inbound{NodeID: a, Tag: name, Protocol: spec.VLESS, Port: 18443, Core: "xray", Enabled: true},
		Entry: domain.Entry{Name: name, DisplayHost: "example.com", DisplayPort: 18443, Enabled: true},
	}
	if err := s.SaveWireGuardLink(context.Background(), &w); err != nil {
		t.Fatal(err)
	}
	return w
}

func proxyTrafficInbound(t *testing.T, s *Store, node int64, tag string) *domain.Inbound {
	t.Helper()
	ib := &domain.Inbound{NodeID: node, Tag: tag, Protocol: spec.VLESS, Port: 443, Enabled: true}
	if err := s.CreateInbound(context.Background(), ib); err != nil {
		t.Fatal(err)
	}
	return ib
}

func proxyTrafficNode(t *testing.T, s *Store, name string) int64 {
	t.Helper()
	n := &domain.Node{Name: name}
	if err := s.CreateNode(context.Background(), n, name, time.Hour); err != nil {
		t.Fatal(err)
	}
	return n.ID
}

func TestProxyTrafficAttributionSnapshotsAndReceipts(t *testing.T) {
	s, a, _ := monitorRig(t)
	ctx := context.Background()
	b, a2 := proxyTrafficNode(t, s, "B"), proxyTrafficNode(t, s, "A2")
	w1, w2 := proxyTrafficWG(t, s, a, b, "wg-a"), proxyTrafficWG(t, s, a2, b, "wg-a2")
	ordinaryA, ordinaryB := proxyTrafficInbound(t, s, a, "direct-a"), proxyTrafficInbound(t, s, b, "direct-b")
	u := &domain.User{Email: "traffic@example.com", UUID: "traffic-user", Status: "active"}
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO plans(id,name,price_cents,period_days,quota_bytes,created_at,updated_at) VALUES(1,'usage',0,0,0,1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO subscriptions(user_id,plan_id,starts_at,quota_bytes,created_at,updated_at) VALUES(?,1,1,0,1,1)`, u.ID); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	if _, err := s.db.Exec(`UPDATE node_proxy_identities SET attribution_started_at='2026-11-03 12:00:00'`); err != nil {
		t.Fatal(err)
	}
	epoch := strings.Repeat("a", 32)
	accept := func(node int64, seq uint64, sample ...TrafficSample) {
		t.Helper()
		_, seen, err := s.AcceptNodeTraffic(ctx, node, epoch, seq, sample, nil, nil, at)
		if err != nil || seen {
			t.Fatalf("accept: seen=%v err=%v", seen, err)
		}
	}
	accept(a, 1,
		TrafficSample{UserID: u.ID, InboundID: w1.User.ID, Up: 100, Down: 200},
		TrafficSample{UserID: u.ID, InboundID: ordinaryA.ID, Up: 9, Down: 11},
		TrafficSample{UserID: u.ID, InboundID: w1.User.ID, Up: 2, Down: 3, UnattributedInbound: true},
		TrafficSample{UserID: u.ID, InboundID: w1.User.ID, Up: -1, Down: 100})
	accept(a2, 1, TrafficSample{UserID: u.ID, InboundID: w2.User.ID, Up: 10, Down: 20})
	accept(b, 1, TrafficSample{UserID: u.ID, InboundID: ordinaryB.ID, Up: 5, Down: 7})
	check := func() map[int64]*NodeProxyTraffic {
		t.Helper()
		views, err := s.NodeProxyTrafficViews(ctx, 0, at)
		if err != nil {
			t.Fatal(err)
		}
		if v := views[a]; v.TodayBytes != 25 || v.CollectedTodayBytes != 325 || v.ForwardedTodayBytes != 300 || v.UnattributedTodayBytes != 5 {
			t.Fatalf("A: %+v", v)
		}
		if v := views[a2]; v.TodayBytes != 0 || v.CollectedTodayBytes != 30 || v.ForwardedTodayBytes != 30 {
			t.Fatalf("A2: %+v", v)
		}
		if v := views[b]; v.TodayBytes != 342 || v.CollectedTodayBytes != 12 || v.ReceivedTodayBytes != 330 || !v.HistoryComplete || v.DayStart != at.Truncate(24*time.Hour).Unix() || v.DayEnd-v.DayStart != 86400 {
			t.Fatalf("B: %+v", v)
		}
		var charged, daily int64
		if err := s.db.QueryRow(`SELECT SUM(used_up_bytes+used_down_bytes) FROM subscriptions WHERE user_id=?`, u.ID).Scan(&charged); err != nil {
			t.Fatal(err)
		}
		if err := s.db.QueryRow(`SELECT SUM(up_bytes+down_bytes) FROM traffic_daily WHERE user_id=?`, u.ID).Scan(&daily); err != nil {
			t.Fatal(err)
		}
		if charged != 367 || daily != charged || views[a].TodayBytes+views[a2].TodayBytes+views[b].TodayBytes != charged {
			t.Fatalf("double charge or count: charge=%d daily=%d views=%+v", charged, daily, views)
		}
		return views
	}
	before := check()
	_, seen, err := s.AcceptNodeTraffic(ctx, a, epoch, 1, []TrafficSample{{UserID: u.ID, InboundID: w1.User.ID, Down: 999}}, nil, nil, at)
	if err != nil || !seen {
		t.Fatalf("retry: %v %v", seen, err)
	}
	check()
	if err := s.DeleteWireGuardLink(ctx, b, w1.Link.ID, w1.Link.Version); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteWireGuardLink(ctx, b, w2.Link.ID, w2.Link.Version); err != nil {
		t.Fatal(err)
	}
	if after := check(); !reflect.DeepEqual(before, after) {
		t.Fatalf("deleting the topology rewrote history: before=%+v after=%+v", before, after)
	}
	// Reusing a deleted node's numeric ID must not inherit either its collection
	// history or the other server's already-attributed traffic.
	if _, err := s.db.Exec(`DELETE FROM nodes WHERE id=?`, a); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO nodes(id,name,created_at,updated_at) VALUES(?,'replacement',1,1)`, a); err != nil {
		t.Fatal(err)
	}
	views, err := s.NodeProxyTrafficViews(ctx, 0, at)
	if err != nil || views[a].CollectedTodayBytes != 0 || views[a].TodayBytes != 0 || views[b].TodayBytes != 342 || views[b].ReceivedTodayBytes != 330 {
		t.Fatalf("node-ID reuse: %+v %v", views, err)
	}
	one, err := s.NodeProxyTrafficViews(ctx, b, at)
	if err != nil || len(one) != 1 || !reflect.DeepEqual(one[b], views[b]) {
		t.Fatalf("single/all differ: %+v %+v %v", one, views, err)
	}
	if _, err := s.db.Exec(`DELETE FROM nodes WHERE id=?`, b); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO nodes(id,name,created_at,updated_at) VALUES(?,'replacement-exit',1,1)`, b); err != nil {
		t.Fatal(err)
	}
	views, err = s.NodeProxyTrafficViews(ctx, 0, at)
	if err != nil || views[b].TodayBytes != 0 || views[b].ReceivedTodayBytes != 0 || views[a2].ForwardedTodayBytes != 30 {
		t.Fatalf("exit-ID reuse moved accepted attribution: %+v %v", views, err)
	}
}

func TestProxyTrafficAttributionRollbackAndUTC(t *testing.T) {
	s, a, _ := monitorRig(t)
	ctx := context.Background()
	b := proxyTrafficNode(t, s, "B")
	w := proxyTrafficWG(t, s, a, b, "wg")
	u := &domain.User{Email: "rollback@example.com", UUID: "rollback", Status: "active"}
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	epoch := strings.Repeat("b", 32)
	before := time.Date(2026, 10, 23, 7, 59, 0, 0, time.FixedZone("UTC+8", 8*3600))
	after := before.Add(2 * time.Minute)
	samples := []TrafficSample{{UserID: u.ID, InboundID: w.User.ID, Up: 10, Down: 20}}
	if _, err := s.db.Exec(`CREATE TRIGGER fail_proxy_traffic BEFORE INSERT ON node_proxy_traffic_daily BEGIN SELECT RAISE(ABORT,'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AcceptNodeTraffic(ctx, a, epoch, 1, samples, nil, nil, before); err == nil {
		t.Fatal("projection write failure was acknowledged")
	}
	var receipts, buckets int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM node_traffic_receipts`).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM traffic_daily`).Scan(&buckets); err != nil {
		t.Fatal(err)
	}
	if receipts != 0 || buckets != 0 {
		t.Fatal("receipt/customer traffic survived rollback", receipts, buckets)
	}
	if _, err := s.db.Exec(`DROP TRIGGER fail_proxy_traffic`); err != nil {
		t.Fatal(err)
	}
	for i, at := range []time.Time{before, after} {
		if _, seen, err := s.AcceptNodeTraffic(ctx, a, epoch, uint64(i+1), samples, nil, nil, at); err != nil || seen {
			t.Fatal(seen, err)
		}
	}
	for _, at := range []time.Time{before, after} {
		views, err := s.NodeProxyTrafficViews(ctx, b, at)
		if err != nil || views[b].TodayBytes != 30 || views[b].DayStart != at.UTC().Truncate(24*time.Hour).Unix() {
			t.Fatal(views, err)
		}
	}
	// An old report with no inbound tag must stay on its collector even if the
	// billing fallback happens to be a reverse inbound.
	samples[0].UnattributedInbound = true
	if _, seen, err := s.AcceptNodeTraffic(ctx, a, "", 0, samples, nil, nil, after); err != nil || seen {
		t.Fatal(seen, err)
	}
	views, err := s.NodeProxyTrafficViews(ctx, 0, after)
	if err != nil || views[a].TodayBytes != 30 || views[a].UnattributedTodayBytes != 30 || views[b].TodayBytes != 30 {
		t.Fatal(views, err)
	}
}

func TestProxyTrafficVLESSReverseAndHistoryCoverage(t *testing.T) {
	s, a, _ := monitorRig(t)
	ctx := context.Background()
	b := proxyTrafficNode(t, s, "B")
	write := ReverseWrite{
		Link:     ReverseLink{ID: "reverse", TransitID: a},
		User:     domain.Inbound{NodeID: a, Tag: "user", Protocol: spec.VLESS, Port: 443, Enabled: true},
		Receiver: domain.Inbound{NodeID: a, Tag: "receiver", Protocol: spec.VLESS, Port: 444, Enabled: true},
		Entry:    domain.Entry{Name: "reverse", DisplayHost: "example.com", DisplayPort: 443, Enabled: true},
	}
	if err := s.SaveReverseLinks(ctx, b, nil, []ReverseWrite{write}); err != nil {
		t.Fatal(err)
	}
	links, err := s.ReverseLinks(ctx, b)
	if err != nil || len(links) != 1 {
		t.Fatal(links, err)
	}
	at := time.Date(2026, 10, 23, 12, 30, 0, 0, time.UTC)
	if _, err := s.db.Exec(`UPDATE node_proxy_identities SET attribution_started_at = '2026-10-23 12:00:00'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddTrafficSamples(ctx, []TrafficSample{{UserID: 100, InboundID: links[0].UserInboundID, Up: 100, Down: 200}}, at); err != nil {
		t.Fatal(err)
	}
	v, err := s.NodeProxyTrafficViews(ctx, b, at)
	if err != nil || v[b].TodayBytes != 300 || v[b].HistoryComplete || v[b].AttributionStartedAt != at.Add(-30*time.Minute).Unix() {
		t.Fatal(v, err)
	}
	v, err = s.NodeProxyTrafficViews(ctx, b, at.Add(24*time.Hour))
	if err != nil || v[b].TodayBytes != 0 || !v[b].HistoryComplete {
		t.Fatal(v, err)
	}
}
