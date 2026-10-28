package http

import (
	"context"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/subdesign"
	"github.com/zeptop-dev/captain/internal/store"
	"gopkg.in/yaml.v3"
)

func TestSubDesignDualStackDraftPublishAndDisable(t *testing.T) {
	r := newRig(t)
	_, path, _ := profileSetup(t, r)
	d, _ := subdesign.Preset("basic")
	d.Enhancements.NodeDualStack = true
	// Keep another format's existing template throughout selection and rejection.
	original := store.SubTemplates{"stash": "mixed-port: 17890\nproxies: []\n"}
	if err := r.st.SetSetting(context.Background(), store.SettingSubTemplates, original); err != nil {
		t.Fatal(err)
	}
	if status, b, _ := r.c.do("PUT", "/api/admin/settings/sub-design", map[string]any{"design": d}, nil); status != 200 {
		t.Fatalf("save draft: %d %s", status, b)
	}
	status, b, _ := r.c.do("GET", "/api/admin/settings/sub-design", nil, nil)
	if status != 200 || !mustJSON[struct {
		Design subdesign.Design `json:"design"`
	}](t, b).Design.Enhancements.NodeDualStack {
		t.Fatal("saved draft lost dual-stack option", status)
	}
	status, b, _ = r.c.do("POST", "/api/admin/settings/sub-design/presets/custom", map[string]any{"name": "Dual stack", "design": d}, nil)
	if status != 200 {
		t.Fatalf("save preset: %d %s", status, b)
	}
	preset := mustJSON[store.SubDesignPreset](t, b)
	status, b, _ = r.c.do("GET", "/api/admin/settings/sub-design/presets/"+preset.Key, nil, nil)
	if status != 200 || !mustJSON[subdesign.Design](t, b).Enhancements.NodeDualStack {
		t.Fatal("custom preset lost dual-stack option", status)
	}
	visitor := &client{t: t, srv: r.srv}
	read := func() subDesignDeliveryDocument {
		t.Helper()
		status, b, _ := visitor.do("GET", path+"?client=clash", nil, nil)
		if status != 200 {
			t.Fatalf("subscription: %d %s", status, b)
		}
		return decodeSubDesignDelivery(t, b)
	}
	if doc := read(); doc.IPv6 != nil || doc.TCPConcurrent != nil {
		t.Fatal("saving draft or preset changed a published subscription")
	}
	for _, formats := range []any{[]string{"clash", "stash"}, nil} {
		body := map[string]any{"design": d}
		if formats != nil {
			body["formats"] = formats
		}
		if status, _, _ := r.c.do("POST", "/api/admin/settings/sub-design/apply", body, nil); status != 400 {
			t.Fatal("unsupported multi-format application succeeded", status)
		}
		if doc := read(); doc.IPv6 != nil {
			t.Fatal("rejected application partially published clash")
		}
	}
	apply := func() {
		t.Helper()
		if status, b, _ := r.c.do("POST", "/api/admin/settings/sub-design/apply", map[string]any{"design": d, "formats": []string{"clash"}}, nil); status != 200 {
			t.Fatalf("publish: %d %s", status, b)
		}
	}
	apply()
	// Published subscriptions retain the existing 15-second template cache.
	// Exercise that real path without bypassing it through a test-only hook.
	time.Sleep(15 * time.Second)
	if doc := read(); doc.IPv6 == nil || !*doc.IPv6 || doc.TCPConcurrent == nil || !*doc.TCPConcurrent {
		t.Fatal("published subscription missing dual-stack options")
	}
	var templates store.SubTemplates
	if err := r.st.GetSetting(context.Background(), store.SettingSubTemplates, &templates); err != nil || templates["stash"] != original["stash"] {
		t.Fatal("publishing clash modified the unselected Stash template", err)
	}
	d.Enhancements.NodeDualStack = false
	apply()
	time.Sleep(15 * time.Second)
	if doc := read(); doc.IPv6 != nil || doc.TCPConcurrent != nil {
		t.Fatal("turning off the overlay left its global settings in the template")
	}
}

func TestSubDesignDualStackPreservesExplicitEndpointAndFamily(t *testing.T) {
	r := newRig(t)
	_, path, _ := profileSetup(t, r)
	entries, err := r.st.ListEntries(context.Background())
	if err != nil || len(entries) != 1 {
		t.Fatal(entries, err)
	}
	entry := entries[0]
	for _, host := range []string{"dual.example.com", "198.51.100.20", "2001:db8::20"} {
		status, b, _ := r.c.do("POST", "/api/admin/entries", map[string]any{
			"Name": host, "InboundID": entry.InboundID, "DisplayHost": host,
			"DisplayPort": 443, "ClientExtra": map[string]any{"ip-version": "ipv4"},
		}, nil)
		if status != 200 {
			t.Fatalf("create explicit endpoint: %d %s", status, b)
		}
	}
	applySubDesignDelivery(t, r.c)
	visitor := &client{t: t, srv: r.srv}
	status, b, _ := visitor.do("GET", path+"?client=clash", nil, nil)
	if status != 200 {
		t.Fatalf("subscription: %d %s", status, b)
	}
	var doc struct {
		Proxies []struct {
			Name   string `yaml:"name"`
			Server string `yaml:"server"`
			Family string `yaml:"ip-version"`
		} `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Proxies) != 4 {
		t.Fatal("dual-stack changed the authorized node set", len(doc.Proxies))
	}
	for _, proxy := range doc.Proxies {
		if proxy.Name == entry.Name {
			continue
		}
		if proxy.Server != proxy.Name || proxy.Family != "ipv4" {
			t.Fatal("dual-stack silently rewrote an endpoint or explicit family", proxy)
		}
	}
}
