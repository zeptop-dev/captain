package http

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/bosun/pkg/subscription"
	"github.com/zeptop-dev/captain/internal/domain"
	"strings"
	"testing"
)

func TestNaiveSubscriptionHTTPMatchesAgentAndAuthorization(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	post := func(path string, body any) []byte {
		t.Helper()
		return accessAuditCall(t, r.c, "POST", path, body, 200)
	}
	caps := spec.CapabilitiesForCore("singbox")
	accessAuditCall(t, r.agent, "POST", "/api/agent/report", agentproto.Report{
		Cores: map[string]agentproto.CoreStatus{"singbox": {Running: true, Capabilities: &caps}},
	}, 200)
	ib := mustJSON[domain.Inbound](t, post("/api/admin/nodes/"+itoa(r.nodeID)+"/inbounds", map[string]any{
		"Tag": "naive-subscriber", "Protocol": "naive", "Core": "singbox", "Port": 8445, "Settings": map[string]any{"tls": map[string]any{"mode": 1, "server_name": "node.example.com", "auto_cert": true, "acme": "http"}},
	}))
	entry := mustJSON[domain.Entry](t, post("/api/admin/entries", map[string]any{
		"Name": "Naive subscriber", "InboundID": ib.ID, "DisplayHost": "naive.example.com", "DisplayPort": 8445,
	}))
	group := mustJSON[struct{ ID int64 }](t, post("/api/admin/groups", map[string]string{"Name": "Naive access"}))
	plan := mustJSON[domain.Plan](t, post("/api/admin/plans", map[string]any{
		"Name": "Naive plan", "PriceCents": 1, "PeriodDays": 30, "GroupID": group.ID,
	}))
	user := mustJSON[struct {
		ID       int64  `json:"id"`
		UUID     string `json:"uuid"`
		SubToken string `json:"sub_token"`
	}](t, post("/api/admin/users", map[string]string{"Email": "naive-subscriber@example.com", "Password": "password123"}))
	post("/api/admin/users/"+itoa(user.ID)+"/grant", map[string]int64{"PlanID": plan.ID})
	accessAuditCall(t, r.c, "PUT", "/api/admin/node-access/inbounds/"+itoa(ib.ID),
		accessAuditPolicy(ib.ID, accessAuditGet(t, r).Revision, "groups", []int64{group.ID}, true), 200)
	deniedID, _ := r.user("naive-denied@example.com")
	denied, err := r.st.UserByID(ctx, deniedID)
	if err != nil {
		t.Fatal(err)
	}
	// Subscription credentials must match the users sent to the actual listener.
	state := mustJSON[agentproto.State](t, accessAuditCall(t, r.agent, "GET", "/api/agent/state", nil, 200))
	found := false
	for _, inbound := range state.Node.Inbounds {
		if inbound.Tag != ib.Tag {
			continue
		}
		found = true
		if !inbound.ScopedUsers || len(inbound.Users) != 1 || inbound.Users[0].UUID != user.UUID || inbound.Users[0].Password != user.UUID {
			t.Fatal("Naive agent credentials do not match the authorized subscription user")
		}
	}
	if !found {
		t.Fatal("Naive inbound missing from the actual agent state")
	}

	reader := &client{t: t, srv: r.srv}
	check := func(token string, want bool) {
		t.Helper()
		code, body, _ := reader.do("GET", "/sub/"+token+"?client=singbox", nil, nil)
		if code != 200 {
			t.Fatalf("subscription status %d", code)
		}
		var doc struct {
			Outbounds []map[string]any `json:"outbounds"`
		}
		if err := json.Unmarshal(body, &doc); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, o := range doc.Outbounds {
			if o["tag"] == entry.Name {
				found = true
				if o["type"] != "naive" || o["username"] != user.UUID || o["password"] != user.UUID {
					t.Fatal("subscriber identity mismatch")
				}
			}
		}
		if found != want {
			t.Fatalf("visible=%v wanted=%v", found, want)
		}
		code, body, _ = reader.do("GET", "/sub/"+token+"?client=v2rayn", nil, nil)
		if code != 200 {
			t.Fatalf("URI status %d", code)
		}
		plain, err := base64.StdEncoding.DecodeString(string(body))
		if err != nil {
			t.Fatal(err)
		}
		found = false
		for _, line := range strings.Split(string(plain), "\n") {
			if !strings.HasPrefix(line, "naive+https://") {
				continue
			}
			found = true
			l, err := subscription.ParseURI(line)
			if err != nil || l.UUID != user.UUID || l.Password != user.UUID || l.Port != entry.DisplayPort || l.Inbound.TLS.ServerName != "node.example.com" {
				t.Fatal("URI credentials/endpoint mismatch", err)
			}
		}
		if found != want {
			t.Fatal("URI authorization mismatch")
		}
	}
	check(user.SubToken, true)
	check(denied.SubToken, false)
	accessAuditCall(t, r.c, "PUT", "/api/admin/node-access/inbounds/"+itoa(ib.ID), accessAuditPolicy(ib.ID, accessAuditGet(t, r).Revision, "none", nil, true), 200)
	check(user.SubToken, false)
}
