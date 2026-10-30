package http

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/http/admin"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestTrafficHistoryMalformedMetadataStillAccounts(t *testing.T) {
	r := newRig(t)
	uid, _ := r.user("sample-history@example.com")
	path := fmt.Sprintf("/api/admin/nodes/%d/traffic-history", r.nodeID)
	bad := []any{"wrong", 42, []any{}, map[string]any{"version": "wrong"}, map[string]any{"version": 1, "collection_ended_at": false}, map[string]any{"reasons": []string{strings.Repeat("private-invalid", 500)}}}
	for i, metadata := range bad {
		rep := map[string]any{"traffic_epoch": strings.Repeat("a", 32), "traffic_seq": i + 1, "traffic": []spec.UserTraffic{{UserID: uid, Up: 100, Down: 20}}, "traffic_sample": metadata, "traffic_delivery": metadata}
		for range 2 {
			if code, body, _ := r.agent.do("POST", "/api/agent/report", rep, nil); code != 200 {
				t.Fatalf("optional metadata blocked accounting: %d %s", code, body)
			}
		}
	}
	code, body, headers := r.c.do("GET", path, nil, nil)
	if code != 200 || headers.Get("Cache-Control") != "no-store" {
		t.Fatal(code, string(body))
	}
	page := mustJSON[store.TrafficHistoryPage](t, body)
	if len(page.Items) != len(bad) || page.Delivery != nil {
		t.Fatal(page)
	}
	for _, item := range page.Items {
		if item.MetadataStatus != "invalid" || item.Sample != nil || item.UpBytes != 100 || item.DownBytes != 20 || !item.TotalsKnown {
			t.Fatal(item)
		}
	}
	if strings.Contains(string(body), "private-invalid") {
		t.Fatal("raw invalid metadata leaked")
	}
	sub, err := r.st.ActiveSubscription(context.Background(), uid)
	if err != nil || sub.UsedUpBytes != int64(len(bad)*100) || sub.UsedDownBytes != int64(len(bad)*20) {
		t.Fatal(sub, err)
	}
	// A pre-feature report remains valid and its unknown provenance is explicit.
	rep := agentproto.Report{TrafficSeq: 99, Traffic: []spec.UserTraffic{{UserID: uid, Up: 3}}}
	if code, body, _ := r.agent.do("POST", "/api/agent/report", rep, nil); code != 200 {
		t.Fatal(code, string(body))
	}
	_, body, _ = r.c.do("GET", path+"?limit=1", nil, nil)
	page = mustJSON[store.TrafficHistoryPage](t, body)
	if page.Items[0].MetadataStatus != "legacy" || page.Items[0].Sample != nil {
		t.Fatal(page)
	}
}

func TestTrafficHistoryScopesAndReplayFreshness(t *testing.T) {
	r := newRig(t)
	path := fmt.Sprintf("/api/admin/nodes/%d/traffic-history", r.nodeID)
	c := &client{t: t, srv: r.srv}
	for _, role := range []string{"operator", "support"} {
		staff, err := admin.NewUser(role+"-history@example.com", "password123", role)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.st.CreateStaff(context.Background(), staff); err != nil {
			t.Fatal(err)
		}
		token, _, err := r.st.CreateAPIToken(context.Background(), staff.ID, "history role", "read", nil, []string{"*"})
		if err != nil {
			t.Fatal(err)
		}
		if code, _, _ := c.do("GET", path, nil, map[string]string{"Authorization": "Bearer " + token}); code != 403 {
			t.Fatal("scope expanded role", role, code)
		}
	}
	for _, tc := range []struct {
		scopes []string
		want   int
	}{{[]string{"nodes:read"}, 403}, {[]string{"traffic-history:read"}, 403}, {[]string{"traffic-history:read", "nodes:read"}, 200}, {[]string{"GET /api/admin/nodes/{id}/traffic-history", "GET /api/admin/nodes/{id}"}, 200}} {
		token, _, err := r.st.CreateAPIToken(context.Background(), 1, "history", "read", nil, tc.scopes)
		if err != nil {
			t.Fatal(err)
		}
		if code, body, _ := c.do("GET", path, nil, map[string]string{"Authorization": "Bearer " + token}); code != tc.want {
			t.Fatal(tc.scopes, code, string(body))
		}
	}
	for _, query := range []string{"?before=-1", "?limit=101", "?limit=wrong"} {
		if code, _, _ := r.c.do("GET", path+query, nil, nil); code != 400 {
			t.Fatal(query, code)
		}
	}
	at := time.Now().UTC().Add(-4 * time.Minute)
	rep := agentproto.Report{TrafficEpoch: strings.Repeat("b", 32), TrafficSeq: 1, TrafficDelivery: &agentproto.TrafficDelivery{SampledAt: at, PendingBytes: 10, JournalBytes: 300}}
	for range 2 {
		if code, body, _ := r.agent.do("POST", "/api/agent/report", rep, nil); code != 200 {
			t.Fatal(code, string(body))
		}
	}
	_, body, _ := r.c.do("GET", path, nil, nil)
	page := mustJSON[store.TrafficHistoryPage](t, body)
	if page.Delivery == nil || page.Delivery.FreshForMS != 0 || len(page.Items) != 1 {
		t.Fatal("old snapshot became current", page)
	}
	rep.TrafficDelivery.SampledAt = time.Now().UTC()
	if code, body, _ := r.agent.do("POST", "/api/agent/report", rep, nil); code != 200 {
		t.Fatal(code, string(body))
	}
	_, body, _ = r.c.do("GET", path, nil, nil)
	page = mustJSON[store.TrafficHistoryPage](t, body)
	if page.Delivery == nil || page.Delivery.FreshForMS <= 0 || page.Delivery.ReceivedAt.IsZero() || len(page.Items) != 1 {
		t.Fatal("current queue observation lost or recounted", page)
	}
}
