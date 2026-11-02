package http

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
	"gopkg.in/yaml.v3"
)

// Exercise the real admin, agent and public subscription handlers together:
// a running SSH inbound must be published only to its authorized subscribers.
func TestSSHSubscriptionHTTPPreservesAccessCredentialsAndHostPin(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	post := func(path string, body any) []byte {
		t.Helper()
		return accessAuditCall(t, r.c, "POST", path, body, 200)
	}
	caps := spec.CapabilitiesForCore("singbox-extended")
	accessAuditCall(t, r.agent, "POST", "/api/agent/report", agentproto.Report{
		Cores: map[string]agentproto.CoreStatus{"singbox-extended": {Running: true, Capabilities: &caps}},
	}, 200)
	ib := mustJSON[domain.Inbound](t, post("/api/admin/nodes/"+itoa(r.nodeID)+"/inbounds", map[string]any{
		"Tag": "ssh-subscriber", "Protocol": "ssh", "Core": "singbox-extended", "Port": 2222,
	}))
	if ib.Settings.SSH == nil || ib.Settings.SSH.PublicKey == "" || ib.Settings.SSH.PrivateKey == "" {
		t.Fatal("SSH save did not generate a host identity")
	}
	entry := mustJSON[domain.Entry](t, post("/api/admin/entries", map[string]any{
		"Name": "SSH subscriber", "InboundID": ib.ID, "DisplayHost": "ssh.example.com", "DisplayPort": 2222,
	}))
	group := mustJSON[struct{ ID int64 }](t, post("/api/admin/groups", map[string]string{"Name": "SSH access"}))
	plan := mustJSON[domain.Plan](t, post("/api/admin/plans", map[string]any{
		"Name": "SSH plan", "PriceCents": 1, "PeriodDays": 30, "GroupID": group.ID,
	}))
	user := mustJSON[struct {
		ID       int64  `json:"id"`
		UUID     string `json:"uuid"`
		SubToken string `json:"sub_token"`
	}](t, post("/api/admin/users", map[string]string{"Email": "ssh-subscriber@example.com", "Password": "password123"}))
	post("/api/admin/users/"+itoa(user.ID)+"/grant", map[string]int64{"PlanID": plan.ID})
	accessAuditCall(t, r.c, "PUT", "/api/admin/node-access/inbounds/"+itoa(ib.ID),
		accessAuditPolicy(ib.ID, accessAuditGet(t, r).Revision, "groups", []int64{group.ID}, true), 200)
	deniedID, _ := r.user("ssh-denied@example.com")
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
			t.Fatal("SSH agent credentials do not match the authorized subscription user")
		}
	}
	if !found {
		t.Fatal("SSH inbound missing from the actual agent state")
	}
	reader := &client{t: t, srv: r.srv} // No administrator/portal session.
	formats := []struct {
		name, query, ua, collection, nameKey, userKey, pinKey string
		json                                                  bool
	}{
		{"clash-verge-ua", "", "Clash-Verge/v2.4.0", "proxies", "name", "username", "host-key", false},
		{"explicit-clash", "?client=clash", "", "proxies", "name", "username", "host-key", false},
		{"explicit-singbox-over-clash-ua", "?client=singbox", "Clash-Verge/v2.4.0", "outbounds", "tag", "user", "host_key", true},
		{"nekobox-android-default-stays-clash", "", "NekoBox/Android/1.4.2 (Prefer ClashMeta Format)", "proxies", "name", "username", "host-key", false},
		{"nekobox-pc-default-stays-clash", "", "NekoBox/PC/4.0.1 (Prefer ClashMeta Format)", "proxies", "name", "username", "host-key", false},
		{"explicit-nb4a", "?client=nb4a", "NekoBox/Android/1.4.2 (Prefer ClashMeta Format)", "outbounds", "tag", "user", "host_key", true},
		{"explicit-nekobox-android", "?client=nekobox-android", "NekoBox/Android/1.4.2 (Prefer ClashMeta Format)", "outbounds", "tag", "user", "host_key", true},
	}
	read := func(t *testing.T, token string, format int) map[string]any {
		t.Helper()
		f := formats[format]
		code, body, _ := reader.do("GET", "/sub/"+token+f.query, nil, map[string]string{"User-Agent": f.ua})
		if code != 200 {
			t.Fatalf("subscription returned %d", code)
		}
		if strings.Contains(string(body), ib.Settings.SSH.PrivateKey) || strings.Contains(string(body), "PRIVATE KEY") || strings.Contains(string(body), "private_key") || strings.Contains(string(body), "private-key") {
			t.Fatal("SSH host private key leaked into a subscription")
		}
		var doc map[string]any
		var err error
		if f.json {
			err = json.Unmarshal(body, &doc)
		} else {
			err = yaml.Unmarshal(body, &doc)
		}
		if err != nil {
			t.Fatal("invalid subscription document", err)
		}
		items, ok := doc[f.collection].([]any)
		if !ok {
			t.Fatalf("subscription is missing its %s collection", f.collection)
		}
		for _, item := range items {
			proxy, ok := item.(map[string]any)
			if !ok {
				t.Fatal("subscription contains an invalid proxy object")
			}
			if proxy[f.nameKey] == entry.Name {
				return proxy
			}
		}
		return nil
	}
	for i, f := range formats {
		t.Run(f.name, func(t *testing.T) {
			proxy := read(t, user.SubToken, i)
			if proxy == nil {
				t.Fatal("authorized SSH entry omitted from the subscription")
			}
			if proxy["type"] != "ssh" || proxy["server"] != entry.DisplayHost || proxy[f.userKey] != spec.ProxyUsername(user.UUID, ib.Tag) || proxy["password"] != user.UUID {
				t.Fatal("SSH type, endpoint or tagged subscriber credentials changed")
			}
			if f.json && proxy["server_port"] != float64(entry.DisplayPort) || !f.json && proxy["port"] != entry.DisplayPort {
				t.Fatal("SSH subscription changed the published connection port")
			}
			pins, ok := proxy[f.pinKey].([]any)
			if !ok || len(pins) != 1 || pins[0] != ib.Settings.SSH.PublicKey {
				t.Fatal("SSH subscription lost its generated host public-key pin")
			}
			if proxy["udp"] == true {
				t.Fatal("SSH subscription falsely advertised UDP")
			}
			if proxy := read(t, denied.SubToken, i); proxy != nil {
				t.Fatal("SSH subscription bypassed inbound group authorization")
			}
		})
	}
	// User-specific blocking remains effective even with a compatible renderer.
	if err := r.st.SetUserEntryBlocks(ctx, user.ID, []int64{entry.ID}); err != nil {
		t.Fatal(err)
	}
	for i := range formats {
		if proxy := read(t, user.SubToken, i); proxy != nil {
			t.Fatal("blocked SSH entry leaked into a subscription")
		}
	}
}
