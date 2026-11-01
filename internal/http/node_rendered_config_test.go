package http

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/http/admin"
	"github.com/zeptop-dev/captain/internal/store"
)

type renderedView struct {
	Status  string                           `json:"status"`
	Current *store.NodeRenderedConfigRecord  `json:"current"`
	History []store.NodeRenderedConfigRecord `json:"history"`
}

func TestNodeRenderedConfigReceiptAndAccess(t *testing.T) {
	r := newRig(t)
	path := "/api/admin/nodes/" + itoa(r.nodeID) + "/rendered-config"
	view := func() renderedView {
		t.Helper()
		code, body, h := r.c.do("GET", path, nil, nil)
		if code != 200 || h.Get("Cache-Control") != "no-store" {
			t.Fatal(code, string(body))
		}
		return mustJSON[renderedView](t, body)
	}
	if v := view(); v.Status != "unknown" || len(v.History) != 0 {
		t.Fatal(v)
	}
	for _, tc := range []struct {
		scopes []string
		want   int
	}{
		{[]string{"nodes:read"}, 403}, {[]string{"rendered-config:read"}, 403},
		{[]string{"nodes:read", "rendered-config:read"}, 200},
		{[]string{"GET /api/admin/nodes/{id}", "GET /api/admin/nodes/{id}/rendered-config"}, 200},
	} {
		c := accessAuditToken(t, r, 1, "read", tc.scopes)
		accessAuditCall(t, c, "GET", path, nil, tc.want)
	}
	for _, role := range []string{"operator", "support"} {
		staff, err := admin.NewUser(role+"-rendered@example.com", "password123", role)
		if err != nil {
			t.Fatal(err)
		}
		if err = r.st.CreateStaff(context.Background(), staff); err != nil {
			t.Fatal(err)
		}
		c := accessAuditToken(t, r, staff.ID, "read", []string{"*"})
		accessAuditCall(t, c, "GET", path, nil, 403)
	}
	session := strings.Repeat("c", 32)
	code, b, _ := r.agent.do("GET", "/api/agent/state", nil, map[string]string{agentproto.ConfigSessionHeader: session})
	if code != 200 {
		t.Fatal(code)
	}
	state := mustJSON[agentproto.State](t, b)
	receipt := agentproto.ConfigReceipt{DeliveryID: state.ConfigDelivery.ID, Session: session, ConfigRevision: state.ConfigRevision, StateRevision: state.Revision, Sequence: 1, ObservedAt: time.Now(), Applied: true}
	send := func(meta any) {
		t.Helper()
		rep := map[string]any{"config_receipt": receipt, "applied_revision": state.Revision, "rendered_config": meta}
		accessAuditCall(t, r.agent, "POST", "/api/agent/report", rep, 200)
	}
	good := map[string]any{"version": 1, "status": "observed", "artifacts": []any{}, "secret_unexpected": "must-not-be-stored"}
	send(good)
	v := view()
	if v.Status != "observed" || v.Current.Evidence == nil {
		t.Fatal(v)
	}
	_, body, _ := r.c.do("GET", path, nil, nil)
	if strings.Contains(string(body), "must-not-be-stored") {
		t.Fatal("unrecognized fields leaked")
	}
	receipt.Sequence++
	receipt.ObservedAt = time.Now()
	receipt.Applied = false
	send(good)
	if v = view(); v.Status != "pending" || v.Current.Evidence.Status != "unavailable" {
		t.Fatal(v)
	}
	receipt.Sequence++
	receipt.ObservedAt = time.Now()
	receipt.Applied = true
	send("malformed")
	if v = view(); v.Status != "unavailable" || v.Current.MetadataStatus != "invalid" {
		t.Fatal(v)
	}
	receipt.Sequence++
	receipt.ObservedAt = time.Now()
	send(good)
	r.user("rendered-change@example.com")
	if v = view(); v.Status != "pending" {
		t.Fatal("old evidence confirmed new dynamic state", v)
	}
}

func TestNodeRenderedConfigMalformedMetadataStillBills(t *testing.T) {
	r := newRig(t)
	uid, _ := r.user("rendered-billing@example.com")
	bad := []any{"wrong", 42, []any{}, map[string]any{"version": "wrong"}, map[string]any{"version": 1, "status": "observed", "artifacts": strings.Repeat("secret", 1000)}}
	for i, metadata := range bad {
		rep := map[string]any{"traffic_epoch": strings.Repeat("d", 32), "traffic_seq": i + 1, "traffic": []spec.UserTraffic{{UserID: uid, Up: 100, Down: 20}}, "rendered_config": metadata}
		for range 2 {
			accessAuditCall(t, r.agent, "POST", "/api/agent/report", rep, 200)
		}
	}
	sub, err := r.st.ActiveSubscription(context.Background(), uid)
	if err != nil || sub.UsedUpBytes != int64(len(bad)*100) || sub.UsedDownBytes != int64(len(bad)*20) {
		t.Fatal(sub, err)
	}
	v := mustJSON[renderedView](t, accessAuditCall(t, r.c, "GET", "/api/admin/nodes/"+itoa(r.nodeID)+"/rendered-config", nil, 200))
	if v.Current != nil || len(v.History) != 0 {
		t.Fatal("metadata without receipt manufactured evidence", v)
	}
}
