package http

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/zeptop-dev/captain/internal/service"
	"github.com/zeptop-dev/captain/internal/store"
	"gopkg.in/yaml.v3"
)

func TestEntryIPPolicyReachesSubscriptionAndShareLinks(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	uid, path, _ := profileSetup(t, r)
	entries, _ := r.st.ListEntries(ctx)
	base := entries[0]
	for _, mode := range []string{"ipv4", "ipv6", "dual"} {
		code, body, _ := r.c.do("POST", "/api/admin/entries", map[string]any{
			"Name": mode, "InboundID": base.InboundID, "DisplayHost": "dual.example.com", "DisplayPort": 443, "IPVersion": mode,
		}, nil)
		if code != 200 {
			t.Fatal(code, string(body))
		}
	}
	for _, format := range []string{"clash", "singbox"} {
		code, body, _ := r.c.do("GET", path+"?client="+format, nil, nil)
		if code != 200 {
			t.Fatal(code, string(body))
		}
		var doc map[string]any
		var proxies []any
		if format == "clash" {
			if err := yaml.Unmarshal(body, &doc); err != nil {
				t.Fatal(err)
			}
			if doc["ipv6"] != true {
				t.Fatal("required IPv6 not enabled", doc)
			}
			proxies = doc["proxies"].([]any)
		} else {
			if err := json.Unmarshal(body, &doc); err != nil {
				t.Fatal(err)
			}
			proxies = doc["outbounds"].([]any)
		}
		seen := map[string]bool{}
		for _, raw := range proxies {
			p := raw.(map[string]any)
			name, _ := p["name"].(string)
			if format == "singbox" {
				name, _ = p["tag"].(string)
			}
			if !slices.Contains([]string{"ipv4", "ipv6", "dual"}, name) {
				continue
			}
			seen[name] = true
			if p["server"] != "dual.example.com" {
				t.Fatal("hostname was replaced", p)
			}
			if format == "clash" && p["ip-version"] != name {
				t.Fatal("lost family", p)
			}
			if format == "singbox" {
				resolver, ok := p["domain_resolver"].(map[string]any)
				if !ok || resolver["server"] != "local" {
					t.Fatal("missing resolver", p)
				}
				if name != "dual" && resolver["strategy"] != name+"_only" {
					t.Fatal("lost family", p)
				}
				if _, exists := p["ip-version"]; exists {
					t.Fatal("Mihomo field leaked", p)
				}
			}
		}
		if len(seen) != 3 {
			t.Fatal("missing policy entries", format, seen)
		}
	}
	code, body, _ := r.c.do("GET", path+"?client=uri", nil, nil)
	if code != 200 {
		t.Fatal(code, string(body))
	}
	decoded, err := base64.StdEncoding.DecodeString(string(body))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(decoded), "#ipv4") || strings.Contains(string(decoded), "#ipv6") || !strings.Contains(string(decoded), "#dual") {
		t.Fatal("URI changed forced policy into automatic", string(decoded))
	}
	u, _ := r.st.UserByID(ctx, uid)
	links, err := (&service.Subscription{Store: r.st}).EntryLinks(ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	for _, link := range links {
		if (link.IPVersion == "ipv4" || link.IPVersion == "ipv6") && link.URI != "" {
			t.Fatal("share link lost family", link)
		}
	}
	// A saved global template may deny IPv6; do not quietly rewrite it or
	// return a document that falsely labels these entries IPv6-capable.
	if err := r.st.SetSetting(ctx, store.SettingSubTemplates, store.SubTemplates{"clash": "ipv6: false\nproxies: []\n"}); err != nil {
		t.Fatal(err)
	}
	code, body, _ = r.c.do("GET", path+"?client=clash", nil, nil)
	if code != 422 || !strings.Contains(string(body), "disables IPv6") {
		t.Fatal(code, string(body))
	}
	// A family-only node can still use that same template.
	entries, _ = r.st.ListEntries(ctx)
	for _, e := range entries {
		if e.IPVersion == "ipv6" || e.IPVersion == "dual" {
			e.IPVersion = "ipv4"
			if err := r.st.UpdateEntry(ctx, e); err != nil {
				t.Fatal(err)
			}
		}
	}
	if code, body, _ := r.c.do("GET", path+"?client=clash", nil, nil); code != 200 {
		t.Fatal(code, string(body))
	}
}

func TestEntryIPSupportCannotWidenTemplateReadPermission(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	if err := r.st.SetSetting(ctx, store.SettingSubTemplates, store.SubTemplates{"clash": "ipv6: false\nproxies: []\n"}); err != nil {
		t.Fatal(err)
	}
	path := "/api/admin/entries/ip-version-support?host=node.example.com&protocol=vless&ip_version=ipv6"
	visitor := &client{t: t, srv: r.srv}
	if code, _, _ := visitor.do("GET", path, nil, nil); code != 401 {
		t.Fatal("anonymous", code)
	}
	var got struct {
		Formats []struct {
			Format    string
			Supported bool
			Reason    string
		} `json:"formats"`
		Conflicts []string `json:"template_conflicts"`
		Checked   bool     `json:"templates_checked"`
	}
	code, body, _ := r.c.do("GET", path, nil, nil)
	if code != 200 {
		t.Fatal(code, string(body))
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if !got.Checked || !slices.Contains(got.Conflicts, "clash") {
		t.Fatal(string(body))
	}
	for _, grants := range [][]string{{"entries:read"}, {"users:read"}, {"entries:read", "settings:read"}} {
		token, _, err := r.st.CreateAPIToken(ctx, 1, "capability", "read", nil, grants)
		if err != nil {
			t.Fatal(err)
		}
		code, body, _ := visitor.do("GET", path, nil, map[string]string{"Authorization": "Bearer " + token})
		if !slices.Contains(grants, "entries:read") {
			if code != 403 {
				t.Fatal("scope bypass", code)
			}
			continue
		}
		if code != 200 {
			t.Fatal(code, string(body))
		}
		got.Conflicts = nil
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatal(err)
		}
		want := slices.Contains(grants, "settings:read")
		if got.Checked != want || !want && len(got.Conflicts) > 0 {
			t.Fatal("private template summary leaked", string(body))
		}
	}
	if code, _, _ := r.c.do("GET", "/api/admin/entries/ip-version-support?host=192.0.2.10&ip_version=ipv6", nil, nil); code != 400 {
		t.Fatal("mismatch accepted", code)
	}
}
