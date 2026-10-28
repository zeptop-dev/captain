package http

import (
	"context"
	"strings"
	"testing"

	"github.com/zeptop-dev/bosun/pkg/subdesign"
	"github.com/zeptop-dev/captain/internal/store"
	"gopkg.in/yaml.v3"
)

type subDesignDeliveryDocument struct {
	MixedPort     int   `yaml:"mixed-port"`
	IPv6          *bool `yaml:"ipv6"`
	TCPConcurrent *bool `yaml:"tcp-concurrent"`
	Proxies       []struct {
		Name   string `yaml:"name"`
		Server string `yaml:"server"`
		Dialer string `yaml:"dialer-proxy"`
	} `yaml:"proxies"`
	Groups []struct {
		Name    string   `yaml:"name"`
		Type    string   `yaml:"type"`
		Proxies []string `yaml:"proxies"`
	} `yaml:"proxy-groups"`
	Rules []string `yaml:"rules"`
	DNS   struct {
		Enable                bool                `yaml:"enable"`
		RespectRules          bool                `yaml:"respect-rules"`
		Nameserver            []string            `yaml:"nameserver"`
		ProxyServerNameserver []string            `yaml:"proxy-server-nameserver"`
		NameserverPolicy      map[string][]string `yaml:"nameserver-policy"`
	} `yaml:"dns"`
}

func decodeSubDesignDelivery(t *testing.T, b []byte) subDesignDeliveryDocument {
	t.Helper()
	var doc subDesignDeliveryDocument
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatalf("subscription is not valid typed YAML: %v", err)
	}
	return doc
}

func applySubDesignDelivery(t *testing.T, c *client) {
	t.Helper()
	status, b, _ := c.do("GET", "/api/admin/settings/sub-design/presets/basic", nil, nil)
	if status != 200 {
		t.Fatalf("load basic: %d %s", status, b)
	}
	d := mustJSON[subdesign.Design](t, b)
	d.Enhancements = subdesign.Enhancements{Regions: true, DNSProtection: true, Claude: true, WebRTC: true, QUIC: true, NodeDualStack: true}
	if status, b, _ = c.do("POST", "/api/admin/settings/sub-design/apply", map[string]any{"design": d, "formats": []string{"clash"}}, nil); status != 200 {
		t.Fatalf("apply enhancements: %d %s", status, b)
	}
}

func TestSubDesignDeliveryEnhancementsAndClientChain(t *testing.T) {
	r := newRig(t)
	uid, path, _ := profileSetup(t, r)
	entries, err := r.st.ListEntries(context.Background())
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries: %v %v", entries, err)
	}
	front := entries[0]
	status, b, _ := r.c.do("POST", "/api/admin/entries", map[string]any{
		"Name": "JP Chained", "InboundID": front.InboundID,
		"DisplayHost": "exit.example.com", "DisplayPort": 443,
		"ClientDialerEntryID": front.ID,
	}, nil)
	if status != 200 {
		t.Fatalf("create client chain: %d %s", status, b)
	}
	applySubDesignDelivery(t, r.c)
	// Subscription tokens do not require an administrator or portal session.
	visitor := &client{t: t, srv: r.srv}
	status, b, _ = visitor.do("GET", path+"?client=clash", nil, nil)
	if status != 200 {
		t.Fatalf("subscription: %d %s", status, b)
	}
	doc := decodeSubDesignDelivery(t, b)
	if doc.IPv6 == nil || !*doc.IPv6 || doc.TCPConcurrent == nil || !*doc.TCPConcurrent {
		t.Fatal("dual-stack enhancement missing from the actual subscription")
	}
	if len(doc.Proxies) != 2 {
		t.Fatalf("expected front and chain, got %d proxies", len(doc.Proxies))
	}
	chainFound, frontFound := false, false
	for _, p := range doc.Proxies {
		if p.Name == "JP Chained" {
			chainFound = p.Dialer == front.Name && p.Server == "exit.example.com"
		}
		if p.Name == front.Name {
			frontFound = p.Dialer == "" && p.Server == "node.example.com"
		}
	}
	if !chainFound || !frontFound {
		t.Fatal("applying the generated template lost the authorized client chain")
	}
	if !doc.DNS.Enable || !doc.DNS.RespectRules || len(doc.DNS.Nameserver) == 0 || len(doc.DNS.ProxyServerNameserver) == 0 {
		t.Fatal("DNS enhancement missing from the actual subscription")
	}
	for _, resolver := range doc.DNS.Nameserver {
		if !strings.HasPrefix(resolver, "https://") || !strings.HasSuffix(resolver, "#🤖 Claude") {
			t.Fatalf("unbound application DNS resolver: %q", resolver)
		}
	}
	for _, resolver := range doc.DNS.ProxyServerNameserver {
		if !strings.HasSuffix(resolver, "#DIRECT") {
			t.Fatal("proxy hostname bootstrap does not explicitly bypass proxy groups")
		}
	}
	if len(doc.DNS.NameserverPolicy["+.anthropic.com"]) == 0 {
		t.Fatal("Claude DNS policy missing")
	}
	claudeAt, chinaAt, matchAt := -1, -1, -1
	webRTC, quic := false, false
	for i, rule := range doc.Rules {
		switch rule {
		case "DOMAIN-SUFFIX,anthropic.com,🤖 Claude":
			claudeAt = i
		case "GEOIP,CN,DIRECT":
			chinaAt = i
		case "MATCH,🚀 节点选择":
			matchAt = i
		case "DOMAIN,stun.l.google.com,REJECT":
			webRTC = true
		case "AND,((NETWORK,UDP),(DST-PORT,443)),REJECT":
			quic = true
		}
	}
	if claudeAt < 0 || chinaAt <= claudeAt || matchAt != len(doc.Rules)-1 || !webRTC || !quic {
		t.Fatalf("enhancement priority lost: Claude=%d China=%d Match=%d WebRTC=%t QUIC=%t", claudeAt, chinaAt, matchAt, webRTC, quic)
	}
	jp, jpAuto, hk, claude := false, false, false, false
	for _, g := range doc.Groups {
		switch g.Name {
		case "🇯🇵 日本":
			jp = g.Type == "select" && len(g.Proxies) == 2 && g.Proxies[0] == "🇯🇵 日本 · 自动" && g.Proxies[1] == "JP Chained"
		case "🇯🇵 日本 · 自动":
			jpAuto = g.Type == "url-test" && len(g.Proxies) == 1 && g.Proxies[0] == "JP Chained"
		case "🇭🇰 香港":
			hk = true
		case "🤖 Claude":
			claude = len(g.Proxies) == 2
			for _, p := range g.Proxies {
				if p == "DIRECT" {
					t.Fatal("generated Claude group permits an implicit direct fallback")
				}
			}
		}
	}
	if !jp || !jpAuto || hk || !claude {
		t.Fatalf("regional/Claude group expansion failed: Japan=%t autoJapan=%t emptyHK=%t Claude=%t", jp, jpAuto, hk, claude)
	}
	// Removing access to the front must still remove its dependent chain. The
	// generated DNS/AI selectors must remain loadable without becoming DIRECT.
	if err := r.st.SetUserEntryBlocks(context.Background(), uid, []int64{front.ID}); err != nil {
		t.Fatal(err)
	}
	status, b, _ = visitor.do("GET", path+"?client=clash", nil, nil)
	if status != 200 {
		t.Fatalf("blocked subscription: %d %s", status, b)
	}
	doc = decodeSubDesignDelivery(t, b)
	if len(doc.Proxies) != 0 {
		t.Fatal("generated template bypassed the blocked client-chain front")
	}
	for _, g := range doc.Groups {
		if g.Name == "🤖 Claude" && (g.Type != "select" || len(g.Proxies) != 1 || g.Proxies[0] != "REJECT") {
			t.Fatal("empty generated Claude policy did not fail closed")
		}
	}
}

func TestSubDesignDeliveryPortalCannotManagePresets(t *testing.T) {
	r := newRig(t)
	r.user("preset-customer@example.com")
	portal := &client{t: t, srv: r.srv}
	if status, b, _ := portal.do("POST", "/api/portal/login", map[string]string{"Email": "preset-customer@example.com", "Password": "password123"}, nil); status != 200 {
		t.Fatalf("portal login: %d %s", status, b)
	}
	if status, _, _ := portal.do("GET", "/api/portal/me", nil, nil); status != 200 {
		t.Fatal("test requires a valid ordinary customer session", status)
	}
	d, _ := subdesign.Preset("basic")
	status, b, _ := r.c.do("POST", "/api/admin/settings/sub-design/presets/custom", map[string]any{"name": "Private administrator preset", "design": d}, nil)
	if status != 200 {
		t.Fatalf("create admin preset: %d %s", status, b)
	}
	p := mustJSON[store.SubDesignPreset](t, b)
	for _, req := range []struct {
		method, path string
		body         any
	}{
		{"GET", "/api/admin/settings/sub-design", nil},
		{"GET", "/api/admin/settings/sub-design/claude-rules", nil},
		{"POST", "/api/admin/settings/sub-design/claude-rules/sync", nil},
		{"GET", "/api/admin/settings/sub-design/presets/basic", nil},
		{"GET", "/api/admin/settings/sub-design/presets/" + p.Key, nil},
		{"POST", "/api/admin/settings/sub-design/presets/sync", nil},
		{"POST", "/api/admin/settings/sub-design/presets/custom", map[string]any{"name": "Unauthorized", "design": d}},
		{"DELETE", "/api/admin/settings/sub-design/presets/" + p.Key, nil},
		{"PUT", "/api/admin/settings/sub-design", map[string]any{"design": d}},
		{"POST", "/api/admin/settings/sub-design/inspect", map[string]any{"design": d}},
		{"POST", "/api/admin/settings/sub-design/preview", map[string]any{"design": d, "format": "clash"}},
		{"POST", "/api/admin/settings/sub-design/apply", map[string]any{"design": d, "formats": []string{"clash"}}},
	} {
		if status, body, _ := portal.do(req.method, req.path, req.body, nil); status != 401 && status != 403 {
			t.Fatalf("customer managed presets: %s %s: %d %s", req.method, req.path, status, body)
		}
	}
	if status, _, _ := r.c.do("GET", "/api/admin/settings/sub-design/presets/"+p.Key, nil, nil); status != 200 {
		t.Fatal("denied customer operation removed the administrator preset", status)
	}
}

func TestSubDesignDeliveryRespectsProfileAndResponseRulePrecedence(t *testing.T) {
	for _, action := range []string{"profile", "serve", "block"} {
		t.Run(action, func(t *testing.T) {
			r := newRig(t)
			uid, path, profile := profileSetup(t, r)
			applySubDesignDelivery(t, r.c)
			if status, b, _ := r.c.do("PUT", "/api/admin/users/"+itoa(uid)+"/subscription-profile", map[string]int64{"profile_id": profile.ID}, nil); status != 200 {
				t.Fatalf("assign profile: %d %s", status, b)
			}
			if action != "profile" {
				rule := map[string]any{"name": "Priority", "enabled": true, "action": action, "format": "clash", "template": "mixed-port: 27891\nproxies: []\n", "headers": map[string]string{"Profile-Update-Interval": "6"}}
				if status, b, _ := r.c.do("PUT", "/api/admin/settings/response-rules", []any{rule}, nil); status != 200 {
					t.Fatalf("set response rule: %d %s", status, b)
				}
			}
			visitor := &client{t: t, srv: r.srv}
			status, b, headers := visitor.do("GET", path+"?client=clash", nil, nil)
			if action == "block" {
				if status != 403 {
					t.Fatal("generated template bypassed the blocking response rule", status)
				}
				return
			}
			if status != 200 {
				t.Fatalf("subscription: %d %s", status, b)
			}
			doc := decodeSubDesignDelivery(t, b)
			port, interval := 17891, "3"
			if action == "serve" {
				port, interval = 27891, "6"
			}
			if doc.MixedPort != port || headers.Get("Profile-Update-Interval") != interval || doc.DNS.Enable || doc.IPv6 != nil || doc.TCPConcurrent != nil || strings.Contains(string(b), "🤖 Claude") {
				t.Fatal("global generator bypassed the higher-priority template or headers")
			}
			if len(doc.Proxies) != 1 || doc.Proxies[0].Name != "VIP Example" {
				t.Fatal("profile remark rules or authorized entries were lost")
			}
		})
	}
}

func TestSubDesignDeliveryWireGuardDeviceIgnoresEnhancements(t *testing.T) {
	r, u, _, _ := nativeWGFixture(t)
	_, path := createNativeDevice(t, r, u.ID, "Example device")
	applySubDesignDelivery(t, r.c)
	visitor := &client{t: t, srv: r.srv}
	status, b, _ := visitor.do("GET", path+"?client=clash", nil, nil)
	if status != 200 {
		t.Fatalf("WG device subscription: %d %s", status, b)
	}
	doc := decodeSubDesignDelivery(t, b)
	if doc.DNS.Enable || doc.IPv6 != nil || doc.TCPConcurrent != nil || strings.Contains(string(b), "🤖 Claude") || strings.Contains(string(b), "🇯🇵 日本") {
		t.Fatal("WG device inherited the ordinary subscription template")
	}
	if len(doc.Proxies) != 1 || doc.Proxies[0].Name != "Native WG" || !strings.Contains(string(b), "type: wireguard") {
		t.Fatal("WG device lost its scoped proxy")
	}
	// The same account's ordinary token still receives the administrator's
	// generated template; this exemption belongs to the device credential.
	status, b, _ = visitor.do("GET", "/sub/"+u.SubToken+"?client=clash", nil, nil)
	if status != 200 || !decodeSubDesignDelivery(t, b).DNS.Enable {
		t.Fatal("device isolation disabled the user's ordinary enhancements", status)
	}
}
