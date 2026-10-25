package http

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/zeptop-dev/captain/internal/domain"
)

// The catalogue must not expose categories through an inaccessible entry or an
// incomplete client chain. Category edits must leave real subscriptions intact.
func TestPortalEntryCategoriesRespectAccessAndSubscription(t *testing.T) {
	p := newPortalRig(t, true)
	request := func(c *client, method, path string, input any) []byte {
		t.Helper()
		code, body, _ := c.do(method, path, input, nil)
		if code != 200 {
			t.Fatalf("%s %s: %d %s", method, path, code, body)
		}
		return body
	}
	post := func(path string, input any) []byte { return request(p.admin, "POST", path, input) }
	node := mustJSON[map[string]any](t, post("/api/admin/nodes", map[string]any{"Name": "Example server", "PublicAddr": "203.0.113.30"}))
	nid := int64(node["id"].(float64))
	inboundPath := "/api/admin/nodes/" + itoa(nid) + "/inbounds"
	ib := mustJSON[domain.Inbound](t, post(inboundPath, map[string]any{"Tag": "public", "Protocol": "vless", "Port": 443}))
	group := mustJSON[map[string]any](t, post("/api/admin/groups", map[string]string{"Name": "Private plan"}))
	restricted := mustJSON[domain.Inbound](t, post(inboundPath, map[string]any{"Tag": "private", "Protocol": "vless", "Port": 8443, "GroupID": group["id"]}))
	entry := func(name string, inbound int64, front *int64) domain.Entry {
		t.Helper()
		return mustJSON[domain.Entry](t, post("/api/admin/entries", map[string]any{"Name": name, "InboundID": inbound, "DisplayHost": "node.example.com", "DisplayPort": 443, "Tags": []string{"original"}, "ClientDialerEntryID": front}))
	}
	first := entry("First", ib.ID, nil)
	second := entry("Second", ib.ID, nil)
	hidden := entry("Hidden", restricted.ID, nil)
	blocked := entry("Blocked", ib.ID, nil)
	dependent := entry("Dependent", ib.ID, &blocked.ID)
	request(p.admin, "PUT", "/api/admin/entries/order", map[string]any{"IDs": []int64{second.ID, first.ID, hidden.ID, blocked.ID, dependent.ID}})
	plan := p.plan(map[string]any{"Name": "Basic", "PriceCents": 1, "PeriodDays": 30})
	user, customer := p.customer("category@example.com")
	_, noPlan := p.customer("no-plan@example.com")
	post("/api/admin/users/"+itoa(user.ID)+"/grant", map[string]any{"PlanID": plan})
	if err := p.st.SetUserEntryBlocks(context.Background(), user.ID, []int64{blocked.ID}); err != nil {
		t.Fatal(err)
	}
	subPath := "/sub/" + user.SubToken + "?client=clash"
	before := request(customer, "GET", subPath, nil)
	categories := make([]domain.EntryCategory, 0, 5)
	for _, name := range []string{"Public A", "Public B", "Secret group", "Secret blocked", "Secret dependent"} {
		categories = append(categories, mustJSON[domain.EntryCategory](t, post("/api/admin/entry-categories", map[string]string{"Name": name, "Description": "Plain description"})))
	}
	for i, e := range []domain.Entry{first, second, hidden, blocked, dependent} {
		request(p.admin, "PUT", "/api/admin/entries/category", map[string]any{"IDs": []int64{e.ID}, "CategoryID": categories[i].ID})
	}
	request(p.admin, "PUT", "/api/admin/entry-categories/order", map[string]any{"IDs": []int64{categories[0].ID, categories[2].ID, categories[3].ID, categories[4].ID, categories[1].ID}})
	after := request(customer, "GET", subPath, nil)
	if !bytes.Equal(before, after) {
		t.Fatal("display categories changed the rendered subscription")
	}
	body := request(customer, "GET", "/api/portal/servers", nil)
	var servers []struct {
		Name     string   `json:"name"`
		Tags     []string `json:"tags"`
		Category *struct {
			ID          int64  `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
			Sort        int    `json:"sort"`
		} `json:"category"`
	}
	if err := json.Unmarshal(body, &servers); err != nil {
		t.Fatal(err)
	}
	if len(servers) != 2 || servers[0].Name != "Second" || servers[1].Name != "First" {
		t.Fatalf("access or node order changed: %s", body)
	}
	for i, category := range []domain.EntryCategory{categories[1], categories[0]} {
		got := servers[i]
		if got.Category == nil || got.Category.ID != category.ID || got.Category.Name != category.Name || got.Category.Description != "Plain description" || got.Category.Sort != 1-i {
			t.Fatalf("missing category: %s", body)
		}
		if len(got.Tags) != 1 || got.Tags[0] != "original" {
			t.Fatal("category became a subscription tag")
		}
	}
	if bytes.Contains(body, []byte("Secret")) {
		t.Fatal("inaccessible category leaked")
	}
	if got := request(noPlan, "GET", "/api/portal/servers", nil); strings.TrimSpace(string(got)) != "[]" {
		t.Fatalf("no-plan category leak: %s", got)
	}
	request(p.admin, "DELETE", "/api/admin/entry-categories/"+itoa(categories[1].ID), nil)
	body = request(customer, "GET", "/api/portal/servers", nil)
	if err := json.Unmarshal(body, &servers); err != nil {
		t.Fatal(err)
	}
	if len(servers) != 2 || servers[0].Category != nil || servers[1].Category == nil {
		t.Fatalf("category deletion removed a node or cleared unrelated category: %s", body)
	}
	if !bytes.Equal(before, request(customer, "GET", subPath, nil)) {
		t.Fatal("category deletion changed subscription")
	}
}
