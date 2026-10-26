package http

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestNodeLocationAPIPublicProjectionAndLegacyWrites(t *testing.T) {
	r := newRig(t)
	path := "/api/admin/nodes/" + itoa(r.nodeID) + "/probe"
	call := func(c *client, method, path string, in any, want int) []byte {
		t.Helper()
		status, b, _ := c.do(method, path, in, nil)
		if status != want {
			t.Fatalf("%s %s: %d %s", method, path, status, b)
		}
		return b
	}
	anon := &client{t: t, srv: r.srv}
	lat, lng := 35.68, 139.76
	loc := &domain.GeoLocation{Country: "JP", City: "Tokyo", Subdivision: "13", Lat: &lat, Lng: &lng, Precision: "city"}
	settings := &domain.LocationSettings{Mode: "manual", Manual: loc}
	call(r.c, "PUT", path, map[string]any{"RegionMode": "manual", "Info": store.NodeProbeInfo{Region: "DE", LocationSettings: settings}}, 200)
	call(r.c, "PUT", "/api/admin/settings/probe", map[string]any{"enabled": true, "path": "/status", "show_ip": false}, 200)
	public := func() *domain.GeoLocation {
		t.Helper()
		raw := call(anon, "GET", "/api/probe", nil, 200)
		for _, private := range []string{"location_settings", "region_detection", "country.is", "last_success_at", "checked_at", "198.51.100.20"} {
			if strings.Contains(string(raw), private) {
				t.Fatalf("private metadata %s in public projection", private)
			}
		}
		out := mustJSON[struct {
			Nodes []struct {
				Info store.NodeProbeInfo `json:"info"`
			} `json:"nodes"`
		}](t, raw)
		if len(out.Nodes) != 1 {
			t.Fatalf("%s", raw)
		}
		if out.Nodes[0].Info.LocationSettings != nil {
			t.Fatal("private settings published")
		}
		return out.Nodes[0].Info.Location
	}
	if l := public(); l == nil || l.City != "Tokyo" {
		t.Fatal(l)
	}
	for _, info := range []map[string]any{{"region": "DE"}, {"region": "DE", "location_settings": nil}, {"region": "DE", "location": map[string]any{"country": "US", "precision": "country"}}} {
		call(r.c, "PUT", path, map[string]any{"Info": info}, 200)
		if l := public(); l == nil || l.City != "Tokyo" {
			t.Fatal("legacy write erased settings or trusted read-only location", l)
		}
	}
	for _, bad := range []map[string]any{
		{"mode": "other"},
		{"mode": "manual"},
		{"mode": "manual", "manual": map[string]any{"country": "JP", "city": "Tokyo", "precision": "city", "lat": 35}},
		{"mode": "manual", "manual": map[string]any{"country": "JP", "city": "<script>", "precision": "city", "lat": 35, "lng": 139}},
	} {
		call(r.c, "PUT", path, map[string]any{"Info": map[string]any{"region": "DE", "location_settings": bad}}, 400)
	}
	settings.Mode = "off"
	call(r.c, "PUT", path, map[string]any{"Info": store.NodeProbeInfo{Region: "DE", LocationSettings: settings}}, 200)
	if public() != nil {
		t.Fatal("disabled manual draft published")
	}
	// The same city cache is projected only while automatic location is selected.
	call(r.agent, "POST", "/api/agent/report", agentproto.Report{PublicAddresses: &agentproto.PublicAddresses{SampledAt: time.Now().Unix(), IPv4: "198.51.100.20"}}, 200)
	state, err := r.st.NodeRegionState(context.Background(), r.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.st.SaveRegionDetection(context.Background(), state, store.RegionDetection{IPv4: store.RegionResult{IP: "198.51.100.20", Country: "JP", Location: loc, LocationVersion: 1, Status: "ok", CheckedAt: time.Now().Unix()}}); err != nil {
		t.Fatal(err)
	}
	settings.Mode = "auto"
	call(r.c, "PUT", path, map[string]any{"Info": store.NodeProbeInfo{Region: "DE", LocationSettings: settings}}, 200)
	if l := public(); l == nil || l.City != "Tokyo" {
		t.Fatal(l)
	}
	call(r.c, "PUT", "/api/admin/settings/probe", map[string]any{"enabled": true, "path": "/status", "public_sections": []string{"system"}}, 200)
	if public() != nil {
		t.Fatal("hidden info still published location")
	}
}
