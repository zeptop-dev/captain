package http

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/http/admin"
	"github.com/zeptop-dev/captain/internal/service"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestMapLocationAPIPublicationPermissionsAndSubscriptionStability(t *testing.T) {
	p := newPortalRig(t, true)
	ctx := context.Background()
	visitor := &client{t: t, srv: p.srv}
	call := func(c *client, method, path string, input any, want int) []byte {
		t.Helper()
		code, body, _ := c.do(method, path, input, nil)
		if code != want {
			t.Fatalf("%s %s: %d %s", method, path, code, body)
		}
		return body
	}
	post := func(path string, in any) []byte { return call(p.admin, "POST", path, in, 200) }
	n := mustJSON[map[string]any](t, post("/api/admin/nodes", map[string]any{"Name": "Private server name", "PublicAddr": "203.0.113.30"}))
	nid := int64(n["id"].(float64))
	ib := mustJSON[domain.Inbound](t, post("/api/admin/nodes/"+itoa(nid)+"/inbounds", map[string]any{"Tag": "geo", "Protocol": "vless", "Port": 443}))
	create := func(name string, inbound int64, front *int64) domain.Entry {
		return mustJSON[domain.Entry](t, post("/api/admin/entries", map[string]any{"Name": name, "InboundID": inbound, "ClientDialerEntryID": front, "DisplayHost": "private-endpoint.example.com", "DisplayPort": 443, "Region": "US"}))
	}
	e := create("Published node", ib.ID, nil)
	private := create("Private subscription", ib.ID, nil)
	dependent := create("Missing prerequisite", ib.ID, &private.ID)
	user, customer := p.customer("geo-customer@example.com")
	plan := p.plan(map[string]any{"Name": "Geo plan", "PriceCents": 1, "PeriodDays": 30})
	post("/api/admin/users/"+itoa(user.ID)+"/grant", map[string]any{"PlanID": plan})
	before := call(customer, "GET", "/sub/"+user.SubToken+"?client=clash", nil, 200)
	lat, lng := 34.05, -118.24
	city := &domain.GeoLocation{Country: "US", City: "Los Angeles", Precision: "city", Lat: &lat, Lng: &lng}
	manual := domain.LocationSettings{Mode: "manual", Manual: city}
	call(p.admin, "PUT", "/api/admin/nodes/"+itoa(nid)+"/probe", map[string]any{"Info": map[string]any{"location_settings": manual}}, 200)
	path := "/api/admin/entries/" + itoa(e.ID) + "/location"
	for _, c := range []*client{visitor, customer} {
		call(c, "PUT", path, manual, 401)
	}
	for _, role := range []string{"operator", "support"} {
		staff, _ := admin.NewUser("geo-"+role+"@example.com", "password123", role)
		if err := p.st.CreateStaff(ctx, staff); err != nil {
			t.Fatal(err)
		}
		c := &client{t: t, srv: p.srv}
		call(c, "POST", "/api/admin/login", map[string]string{"Email": staff.Email, "Password": "password123"}, 200)
		want := 403
		if role == "operator" {
			want = 200
		}
		call(c, "PUT", path, manual, want)
	}
	call(p.admin, "PUT", "/api/admin/entries/999999/location", manual, 404)
	call(p.admin, "PUT", path, map[string]any{}, 400)
	badLat := 91.0
	bad := *city
	bad.Lat = &badLat
	call(p.admin, "PUT", path, domain.LocationSettings{Mode: "manual", Manual: &bad}, 400)
	call(p.admin, "PUT", path, domain.LocationSettings{Mode: "auto"}, 200)
	rows := mustJSON[[]struct {
		ID               int64
		Location         *domain.GeoLocation
		LocationSettings *domain.LocationSettings
	}](t, call(p.admin, "GET", "/api/admin/entries", nil, 200))
	if len(rows) != 3 || !reflect.DeepEqual(rows[0].Location, city) || rows[0].LocationSettings.Mode != "auto" {
		t.Fatal("admin effective location", rows)
	}
	site := mustJSON[map[string]any](t, call(p.admin, "GET", "/api/admin/settings/site", nil, 200))
	if _, ok := site["map_entry_ids"].([]any); !ok {
		t.Fatal("missing empty map selection", site)
	}
	site["map_source"], site["status_map_source"] = "entries", "entries"
	site["map_entry_ids"] = []int64{e.ID, dependent.ID}
	site = mustJSON[map[string]any](t, call(p.admin, "PUT", "/api/admin/settings/site", site, 200))
	raw := call(visitor, "GET", "/api/site", nil, 200)
	public := mustJSON[struct {
		Locations       []service.MapPoint `json:"locations"`
		Hub             any                `json:"hub"`
		StatusLocations []any              `json:"status_locations"`
	}](t, raw)
	if len(public.Locations) != 1 || public.Locations[0].Name != e.Name || public.Locations[0].City != "Los Angeles" || public.Hub != nil || len(public.StatusLocations) != 0 {
		t.Fatal("publication/chain filter", string(raw))
	}
	for _, secret := range []string{"203.0.113.30", "Private server name", "private-endpoint", "Private subscription", "Missing prerequisite", "location_settings", "map_entry_ids", "map_revision", user.UUID, user.SubToken} {
		if secret != "" && bytes.Contains(raw, []byte(secret)) {
			t.Fatal("private geographic data leaked", secret)
		}
	}
	// A legacy site editor cannot clear selected nodes or the map modes.
	call(p.admin, "PUT", "/api/admin/settings/site", map[string]any{"tagline": "Updated"}, 200)
	state := mustJSON[map[string]any](t, call(p.admin, "GET", "/api/admin/settings/site", nil, 200))
	if state["map_source"] != "entries" || len(state["map_entry_ids"].([]any)) != 2 {
		t.Fatal("legacy save changed map", state)
	}
	// Status publishes only its own explicit set and does not borrow the home map.
	cfg := mustJSON[store.ServiceStatusConfig](t, call(p.admin, "GET", "/api/admin/settings/service-status", nil, 200))
	cfg.PublicEnabled = true
	cfg.PublicEntryIDs = []int64{private.ID}
	call(p.admin, "PUT", "/api/admin/settings/service-status", cfg, 200)
	status := mustJSON[service.ServiceStatusView](t, call(visitor, "GET", "/api/site/service-status", nil, 200))
	if len(status.Entries) != 1 || status.Entries[0].ID != private.ID || !reflect.DeepEqual(status.Entries[0].Location, city) {
		t.Fatal("status scope/location", status)
	}
	if err := p.st.SetUserEntryBlocks(ctx, user.ID, []int64{private.ID}); err != nil {
		t.Fatal(err)
	}
	portal := mustJSON[service.ServiceStatusView](t, call(customer, "GET", "/api/portal/service-status", nil, 200))
	if len(portal.Entries) != 1 || portal.Entries[0].ID != e.ID {
		t.Fatal("portal location bypassed entitlement", portal)
	}
	if err := p.st.SetUserEntryBlocks(ctx, user.ID, nil); err != nil {
		t.Fatal(err)
	}
	after := call(customer, "GET", "/sub/"+user.SubToken+"?client=clash", nil, 200)
	if !bytes.Equal(before, after) {
		t.Fatal("map metadata changed real subscription")
	}
	call(p.admin, "PUT", path, domain.LocationSettings{Mode: "off", Manual: city}, 200)
	public = mustJSON[struct {
		Locations       []service.MapPoint `json:"locations"`
		Hub             any                `json:"hub"`
		StatusLocations []any              `json:"status_locations"`
	}](t, call(visitor, "GET", "/api/site", nil, 200))
	if len(public.Locations) != 0 {
		t.Fatal("off entry retained on map", public)
	}
	// Stale maps cannot publish a replacement Entry with a reused SQLite ID.
	call(p.admin, "DELETE", "/api/admin/entries/"+itoa(dependent.ID), nil, 200)
	call(p.admin, "PUT", "/api/admin/settings/site", site, 409)
}
