package http

import (
	"bytes"
	"context"
	"testing"

	"github.com/zeptop-dev/captain/internal/http/admin"
)

// Exercise the real routes without an upstream connection: publishing a store
// link is independent of release checks, local packages and legacy custom links.
func TestClientCatalogPublicationAudit(t *testing.T) {
	p := newPortalRig(t, true)
	ctx := context.Background()
	visitor := &client{t: t, srv: p.srv}
	_, customer := p.customer("catalog-reader@example.com")
	const base = "/api/admin/settings/client-catalog"
	call := func(c *client, method, path string, input any, want int) []byte {
		t.Helper()
		code, body, _ := c.do(method, path, input, nil)
		if code != want {
			t.Fatalf("%s %s: want %d, got %d %s", method, path, want, code, body)
		}
		return body
	}
	var catalog struct {
		Clients []map[string]any `json:"clients"`
	}
	catalog = mustJSON[struct {
		Clients []map[string]any `json:"clients"`
	}](t, call(p.admin, "GET", base, nil, 200))
	if len(catalog.Clients) != 7 {
		t.Fatalf("expected seven curated projects, got %d", len(catalog.Clients))
	}
	for _, path := range []string{"/api/site/clients", "/api/portal/client-catalog"} {
		c := visitor
		if path == "/api/portal/client-catalog" {
			c = customer
		}
		if rows := mustJSON[[]any](t, call(c, "GET", path, nil, 200)); len(rows) != 0 {
			t.Fatal("install exposed an unselected client", path, rows)
		}
	}
	call(visitor, "GET", "/api/portal/client-catalog", nil, 401)
	for _, c := range []*client{visitor, customer} {
		call(c, "GET", base, nil, 401)
		call(c, "PUT", base+"/stash", map[string]any{}, 401)
	}
	for _, role := range []string{"operator", "support"} {
		u, err := admin.NewUser("catalog-"+role+"@example.com", "password123", role)
		if err != nil {
			t.Fatal(err)
		}
		if err := p.st.CreateStaff(ctx, u); err != nil {
			t.Fatal(err)
		}
		c := &client{t: t, srv: p.srv}
		call(c, "POST", "/api/admin/login", map[string]string{"Email": u.Email, "Password": "password123"}, 200)
		call(c, "GET", base, nil, 403)
		call(c, "PUT", base+"/stash", map[string]any{}, 403)
		call(c, "POST", base+"/clash-verge-rev/check", nil, 403)
		call(c, "POST", base+"/clash-verge-rev/download", map[string]int{"asset_id": 1}, 403)
		call(c, "DELETE", base+"/clash-verge-rev/cache/1", nil, 403)
	}
	var stash map[string]any
	for _, c := range catalog.Clients {
		if c["id"] == "stash" {
			stash = c
		}
	}
	if stash == nil {
		t.Fatal("missing Stash")
	}
	plats := stash["platforms"].([]any)
	for _, raw := range plats {
		platform := raw.(map[string]any)
		platform["show_home"] = platform["platform"] == "macos"
		platform["show_portal"] = platform["platform"] == "ios"
	}
	request := map[string]any{"revision": stash["revision"], "platforms": plats}
	saved := mustJSON[map[string]any](t, call(p.admin, "PUT", base+"/stash", request, 200))
	call(p.admin, "PUT", base+"/stash", request, 409)
	public := call(visitor, "GET", "/api/site/clients", nil, 200)
	portal := call(customer, "GET", "/api/portal/client-catalog", nil, 200)
	for _, tc := range []struct {
		raw      []byte
		platform string
	}{{public, "macos"}, {portal, "ios"}} {
		rows := mustJSON[[]map[string]any](t, tc.raw)
		if len(rows) != 1 || rows[0]["id"] != "stash" || rows[0]["platform"] != tc.platform {
			t.Fatalf("publication scopes mixed: %s", tc.raw)
		}
		for _, forbidden := range []string{"incarnation", "cached_assets", "asset_ids", "check_error", "revision", "checked_at"} {
			if bytes.Contains(tc.raw, []byte(`"`+forbidden+`"`)) {
				t.Fatalf("private catalog state exposed: %s", forbidden)
			}
		}
	}
	// The old editor still owns only its original custom links.
	custom := map[string]any{"items": []map[string]string{{"name": "Custom installer", "platform": "linux", "url": "https://downloads.example.com/client", "note": "Existing link"}}}
	call(p.admin, "PUT", "/api/admin/settings/clients", custom, 200)
	old := mustJSON[[]map[string]any](t, call(visitor, "GET", "/api/portal/clients", nil, 200))
	if len(old) != 1 || old[0]["name"] != "Custom installer" {
		t.Fatal("legacy custom list changed", old)
	}
	if next := call(visitor, "GET", "/api/site/clients", nil, 200); !bytes.Equal(public, next) {
		t.Fatal("legacy save changed catalog publication")
	}
	for _, raw := range plats {
		platform := raw.(map[string]any)
		platform["show_home"], platform["show_portal"] = false, false
	}
	call(p.admin, "PUT", base+"/stash", map[string]any{"revision": saved["revision"], "platforms": plats}, 200)
	if rows := mustJSON[[]any](t, call(visitor, "GET", "/api/site/clients", nil, 200)); len(rows) != 0 {
		t.Fatal("disabled homepage client remained public")
	}
	if rows := mustJSON[[]any](t, call(customer, "GET", "/api/portal/client-catalog", nil, 200)); len(rows) != 0 {
		t.Fatal("disabled portal client remained visible")
	}
}
