package http

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/store"
)

func TestLocationIPAPIPrivateSettingsAndLegacyOmission(t *testing.T) {
	r := newRig(t)
	path := "/api/admin/nodes/" + itoa(r.nodeID) + "/probe"
	ctx := context.Background()
	call := func(c *client, method, p string, in any, want int) []byte {
		t.Helper()
		status, raw, _ := c.do(method, p, in, nil)
		if status != want {
			t.Fatalf("%s %s: %d %s", method, p, status, raw)
		}
		return raw
	}
	read := func() *store.NodeProbe {
		t.Helper()
		return mustJSON[struct {
			Probe *store.NodeProbe `json:"probe"`
		}](t, call(r.c, "GET", path, nil, 200)).Probe
	}
	if p := read(); p.LocationIPSettings.Mode != "auto" || p.LocationIPSettings.AllowEntryFallback {
		t.Fatal(p)
	}
	settings := store.LocationIPSettings{Mode: "manual", ManualIP: "203.0.113.30"}
	call(r.c, "PUT", path, map[string]any{"RegionMode": "auto", "LocationIP": settings}, 200)
	state, err := r.st.NodeRegionState(ctx, r.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	d := store.RegionDetection{IPv4: store.RegionResult{IP: settings.ManualIP, Country: "JP", Status: "ok", CheckedAt: time.Now().Unix()}}
	if err = r.st.SaveRegionDetection(ctx, state, d); err != nil {
		t.Fatal(err)
	}
	p := read()
	if p.Info.Region != "JP" || p.RegionDetection.IPSource != "manual" || p.LocationIPSettings != settings {
		t.Fatal(p)
	}
	// Old clients omit the added field; they cannot erase it by echoing Info.
	call(r.c, "PUT", path, map[string]any{"Info": p.Info}, 200)
	if read().LocationIPSettings != settings {
		t.Fatal("legacy omission erased selection")
	}
	for _, bad := range []map[string]any{{"mode": "unknown"}, {"mode": "manual"}, {"mode": "manual", "manual_ip": "10.10.0.2"}, {"mode": "manual", "manual_ip": "https://example.com"}} {
		call(r.c, "PUT", path, map[string]any{"LocationIP": bad}, 400)
	}
	if read().LocationIPSettings != settings {
		t.Fatal("invalid update mutated profile")
	}
	anon := &client{t: t, srv: r.srv}
	call(anon, "PUT", path, map[string]any{"LocationIP": settings}, 401)
	call(r.c, "PUT", "/api/admin/settings/probe", map[string]any{"enabled": true, "path": "/status", "show_ip": false}, 200)
	raw := call(anon, "GET", "/api/probe", nil, 200)
	for _, private := range []string{settings.ManualIP, "location_ip_settings", "manual_ip", "allow_entry_fallback", "ip_source", "selection_reason", "country.is"} {
		if strings.Contains(string(raw), private) {
			t.Fatalf("private geolocation settings leaked: %s", private)
		}
	}
	out := mustJSON[struct {
		Nodes []struct {
			Info store.NodeProbeInfo `json:"info"`
		} `json:"nodes"`
	}](t, raw)
	if len(out.Nodes) != 1 || out.Nodes[0].Info.Region != "JP" || out.Nodes[0].Info.Location == nil {
		t.Fatalf("public location lost: %s", raw)
	}
	// Choosing an existing entry is deliberate; saving the choice never edits
	// the underlying machine address, DNS, or its address-following switches.
	before, err := r.st.NodeAddresses(ctx, r.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	call(r.c, "PUT", path, map[string]any{"RegionMode": "auto", "LocationIP": map[string]any{"mode": "entry"}}, 200)
	after, err := r.st.NodeAddresses(ctx, r.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	if after.EntryIPv4 != before.EntryIPv4 || after.EntryIPv6 != before.EntryIPv6 || after.AutoIPv4 != before.AutoIPv4 || after.DNSPending != before.DNSPending {
		t.Fatal("location settings changed network", before, after)
	}
}
