package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/http/admin"
	"github.com/zeptop-dev/captain/internal/service"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestServiceStatusPublicationAndPortalIsolation(t *testing.T) {
	p := newPortalRig(t, true)
	ctx := context.Background()
	call := func(c *client, method, path string, in any, want int) []byte {
		t.Helper()
		code, body, headers := c.do(method, path, in, nil)
		if code != want {
			t.Fatalf("%s %s: %d %s", method, path, code, body)
		}
		if code == 200 && strings.Contains(path, "service-status") && headers.Get("Cache-Control") != "no-store" {
			t.Fatal("status cached", headers)
		}
		return body
	}
	post := func(path string, in any) []byte { return call(p.admin, "POST", path, in, 200) }
	visitor := &client{t: t, srv: p.srv}
	call(visitor, "GET", "/api/site/service-status", nil, 404)
	call(visitor, "GET", "/api/portal/service-status", nil, 401)
	node := mustJSON[map[string]any](t, post("/api/admin/nodes", map[string]any{"Name": "Private infrastructure name", "PublicAddr": "203.0.113.30"}))
	nid := int64(node["id"].(float64))
	path := "/api/admin/nodes/" + itoa(nid) + "/inbounds"
	ib := mustJSON[domain.Inbound](t, post(path, map[string]any{"Tag": "ordinary", "Protocol": "vless", "Port": 443}))
	group := mustJSON[map[string]any](t, post("/api/admin/groups", map[string]string{"Name": "Restricted plan"}))
	private := mustJSON[domain.Inbound](t, post(path, map[string]any{"Tag": "restricted", "Protocol": "vless", "Port": 8443, "GroupID": group["id"]}))
	entry := func(name string, inbound int64, front *int64) domain.Entry {
		return mustJSON[domain.Entry](t, post("/api/admin/entries", map[string]any{"Name": name, "InboundID": inbound, "DisplayHost": "private-endpoint.example.com", "DisplayPort": 443, "ClientDialerEntryID": front}))
	}
	a := entry("Visible A", ib.ID, nil)
	hidden := entry("Restricted node", private.ID, nil)
	b := entry("Visible B", ib.ID, nil)
	blocked := entry("Blocked node", ib.ID, nil)
	dependent := entry("Dependent node", ib.ID, &blocked.ID)
	for _, e := range []domain.Entry{a, hidden, b, blocked, dependent} {
		c := mustJSON[domain.EntryCategory](t, post("/api/admin/entry-categories", map[string]string{"Name": "Category " + e.Name}))
		if err := p.st.AssignEntryCategory(ctx, []int64{e.ID}, c.ID); err != nil {
			t.Fatal(err)
		}
	}
	u, customer := p.customer("status-customer@example.com")
	_, noPlan := p.customer("status-no-plan@example.com")
	plan := p.plan(map[string]any{"Name": "Basic", "PriceCents": 1, "PeriodDays": 30})
	post("/api/admin/users/"+itoa(u.ID)+"/grant", map[string]any{"PlanID": plan})
	if err := p.st.SetUserEntryBlocks(ctx, u.ID, []int64{blocked.ID}); err != nil {
		t.Fatal(err)
	}
	before := call(customer, "GET", "/sub/"+u.SubToken+"?client=clash", nil, 200)
	view := mustJSON[service.ServiceStatusView](t, call(customer, "GET", "/api/portal/service-status", nil, 200))
	if view.Summary.Total != 2 || len(view.Entries) != 2 || view.Entries[0].ID != a.ID || view.Entries[1].ID != b.ID || view.Entries[0].Category.Sort != 0 || view.Entries[1].Category.Sort != 1 {
		t.Fatalf("caller scope/category leak: %+v", view)
	}
	for _, e := range view.Entries {
		if e.State != "unknown" || e.ObservedAt != nil || e.EndToEnd {
			t.Fatal("unobserved node called healthy", e)
		}
	}
	empty := mustJSON[service.ServiceStatusView](t, call(noPlan, "GET", "/api/portal/service-status?user_id="+itoa(u.ID), nil, 200))
	if empty.Summary.Total != 0 || len(empty.Entries) != 0 {
		t.Fatal("cross-user status leak", empty)
	}
	cfg := mustJSON[store.ServiceStatusConfig](t, call(p.admin, "GET", "/api/admin/settings/service-status", nil, 200))
	cfg.PublicEnabled = true
	cfg.PublicEntryIDs = []int64{b.ID, dependent.ID}
	cfg.Maintenance = []store.ServiceMaintenance{{EntryID: b.ID, StartsAt: time.Now().Add(-time.Minute).Unix(), EndsAt: time.Now().Add(time.Hour).Unix(), Note: "Scheduled upgrade"}}
	cfg = mustJSON[store.ServiceStatusConfig](t, call(p.admin, "PUT", "/api/admin/settings/service-status", cfg, 200))
	raw := call(visitor, "GET", "/api/site/service-status", nil, 200)
	public := mustJSON[service.ServiceStatusView](t, raw)
	if public.Summary.Total != 1 || len(public.Entries) != 1 || public.Entries[0].ID != b.ID || public.Entries[0].State != "maintenance" || public.Entries[0].Maintenance.Note != "Scheduled upgrade" || public.Entries[0].Category.Sort != 0 {
		t.Fatal("public allowlist/maintenance/chain mismatch", public)
	}
	for _, secret := range []string{"203.0.113.30", "private-endpoint", "Private infrastructure", u.UUID, u.SubToken, "Restricted", "Blocked", "Dependent"} {
		if secret != "" && bytes.Contains(raw, []byte(secret)) {
			t.Fatal("private data exposed", secret)
		}
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"id": true, "name": true, "rate": true, "region": true, "category": true, "location": true, "state": true, "reason": true, "evidence": true, "observed_at": true, "end_to_end": true, "maintenance": true}
	for _, v := range decoded["entries"].([]any) {
		for key := range v.(map[string]any) {
			if !allowed[key] {
				t.Fatal("unexpected public field", key)
			}
		}
	}
	// New nodes in an already published category are not automatically public.
	newEntry := entry("New unpublished", ib.ID, nil)
	if err := p.st.AssignEntryCategory(ctx, []int64{newEntry.ID}, public.Entries[0].Category.ID); err != nil {
		t.Fatal(err)
	}
	if got := mustJSON[service.ServiceStatusView](t, call(visitor, "GET", "/api/site/service-status", nil, 200)); got.Summary.Total != 1 {
		t.Fatal("implicit new publication", got)
	}
	// Maintenance is display-only, and an expired interval no longer overrides observations.
	cfg.Maintenance[0].StartsAt = time.Now().Add(-2 * time.Hour).Unix()
	cfg.Maintenance[0].EndsAt = time.Now().Add(-time.Hour).Unix()
	cfg = mustJSON[store.ServiceStatusConfig](t, call(p.admin, "PUT", "/api/admin/settings/service-status", cfg, 200))
	if got := mustJSON[service.ServiceStatusView](t, call(visitor, "GET", "/api/site/service-status", nil, 200)); got.Entries[0].State == "maintenance" || got.Entries[0].Maintenance != nil {
		t.Fatal("maintenance persisted beyond its window", got)
	}
	// Remove the new node again before comparing the original subscription.
	call(p.admin, "DELETE", "/api/admin/entries/"+itoa(newEntry.ID), nil, 200)
	if after := call(customer, "GET", "/sub/"+u.SubToken+"?client=clash", nil, 200); !bytes.Equal(before, after) {
		t.Fatal("service status changed subscription")
	}
	// Deletion invalidates any draft that may have selected the retired identity.
	call(p.admin, "PUT", "/api/admin/settings/service-status", cfg, 409)
	cfg = mustJSON[store.ServiceStatusConfig](t, call(p.admin, "GET", "/api/admin/settings/service-status", nil, 200))
	cfg.PublicEnabled = false
	call(p.admin, "PUT", "/api/admin/settings/service-status", cfg, 200)
	call(visitor, "GET", "/api/site/service-status", nil, 404)
	site := mustJSON[map[string]any](t, call(visitor, "GET", "/api/site", nil, 200))
	if site["service_status_url"] != "" {
		t.Fatal("disabled page still advertised")
	}
}

func TestServiceStatusAdminAuthorizationAndConflicts(t *testing.T) {
	p := newPortalRig(t, true)
	ctx := context.Background()
	for _, role := range []string{domain.RoleOperator, domain.RoleSupport} {
		u, err := admin.NewUser(role+"-status@example.com", "password123", role)
		if err != nil {
			t.Fatal(err)
		}
		if err = p.st.CreateStaff(ctx, u); err != nil {
			t.Fatal(err)
		}
		c := &client{t: t, srv: p.srv}
		if code, _, _ := c.do("POST", "/api/admin/login", map[string]string{"Email": u.Email, "Password": "password123"}, nil); code != 200 {
			t.Fatal(code)
		}
		for _, req := range []struct{ method, path string }{{"GET", "/api/admin/settings/service-status"}, {"PUT", "/api/admin/settings/service-status"}, {"GET", "/api/admin/settings/service-status/preview"}} {
			if code, _, _ := c.do(req.method, req.path, map[string]any{}, nil); code != 403 {
				t.Fatal("staff gained publication control", role, req, code)
			}
		}
	}
	visitor := &client{t: t, srv: p.srv}
	for _, grant := range []string{"entries:read", "nodes:read", "settings:read"} {
		token, _, err := p.st.CreateAPIToken(ctx, 1, "status scope", "read", nil, []string{grant})
		if err != nil {
			t.Fatal(err)
		}
		headers := map[string]string{"Authorization": "Bearer " + token}
		want := http.StatusForbidden
		if grant == "settings:read" {
			want = http.StatusOK
		}
		for _, path := range []string{"/api/admin/settings/service-status", "/api/admin/settings/service-status/preview"} {
			if code, _, _ := visitor.do("GET", path, nil, headers); code != want {
				t.Fatal("token scope escaped", grant, path, code)
			}
		}
		if code, _, _ := visitor.do("PUT", "/api/admin/settings/service-status", map[string]any{}, headers); code != http.StatusForbidden {
			t.Fatal("read token gained writes", grant, code)
		}
	}
	_, customer := p.customer("ordinary-status@example.com")
	if code, _, _ := customer.do("GET", "/api/admin/settings/service-status", nil, nil); code != 401 && code != 403 {
		t.Fatal(code)
	}
	for _, in := range []any{map[string]any{}, map[string]any{"revision": 0, "public_enabled": false, "public_entry_ids": nil, "maintenance": []any{}}, map[string]any{"revision": 0, "public_enabled": false, "public_entry_ids": []int64{999999}, "maintenance": []any{}}} {
		if code, _, _ := p.admin.do("PUT", "/api/admin/settings/service-status", in, nil); code != 400 {
			t.Fatal("incomplete/invalid settings accepted", code)
		}
	}
	cfg := store.ServiceStatusConfig{Revision: 0, PublicEnabled: true, PublicEntryIDs: []int64{}, Maintenance: []store.ServiceMaintenance{}}
	if code, body, _ := p.admin.do("PUT", "/api/admin/settings/service-status", cfg, nil); code != 200 {
		t.Fatal(code, string(body))
	}
	if code, _, _ := p.admin.do("PUT", "/api/admin/settings/service-status", cfg, nil); code != 409 {
		t.Fatal("lost update allowed", code)
	}
	if code, _, _ := p.admin.do("PUT", "/api/admin/settings/service-status", cfg, map[string]string{"Origin": "https://other.example.com"}); code != http.StatusForbidden {
		t.Fatal("cross origin write", code)
	}
	if code, _, _ := p.admin.do("PUT", "/api/admin/settings/probe", store.ProbeSettings{Enabled: true, Path: store.ServiceStatusPath}, nil); code != 409 {
		t.Fatal("monitoring hijacked public service path", code)
	}
}
