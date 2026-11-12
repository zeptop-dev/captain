package http

import (
	"context"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
	"testing"
)

func TestPlanAccessHTTPPermissionsAndAtomicSave(t *testing.T) {
	r, _, _, _ := accessAuditFixture(t)
	catalogue := accessAuditGet(t, r)
	global := []int64{}
	for _, row := range catalogue.Inbounds {
		if row.Policy == "all" {
			global = append(global, row.InboundID)
		}
	}
	draft := map[string]any{"Name": "Independent plan", "PriceCents": 1000, "PeriodDays": 30, "QuotaBytes": 100, "Access": map[string]any{"mode": "independent", "inbound_ids": global, "revision": catalogue.Revision, "confirm_shared": true}}
	narrow := accessAuditToken(t, r, 1, "full", []string{"plans:write"})
	accessAuditCall(t, narrow, "POST", "/api/admin/plans", draft, 403)
	_, operator := accessAuditStaff(t, r, "operator")
	accessAuditCall(t, operator, "POST", "/api/admin/plans", draft, 403)
	both := accessAuditToken(t, r, 1, "full", []string{"plans:write", "node-access:write", "node-access:read"})
	p := mustJSON[domain.Plan](t, accessAuditCall(t, both, "POST", "/api/admin/plans", draft, 200))
	if p.GroupID == nil {
		t.Fatal(p)
	}
	catalogue = accessAuditGet(t, r)
	var saved store.InboundAccessCatalog
	saved = mustJSON[store.InboundAccessCatalog](t, accessAuditCall(t, r.c, "GET", "/api/admin/node-access", nil, 200))
	owned := false
	for _, plan := range saved.Plans {
		if plan.ID == p.ID {
			owned = plan.Independent
		}
	}
	if !owned {
		t.Fatal("private plan not labelled")
	}
	accessAuditCall(t, r.c, "PATCH", "/api/admin/plans/"+itoa(p.ID), map[string]any{"PriceCents": 2000}, 200)
	old, _ := r.st.PlanByID(context.Background(), p.ID)
	if old.PriceCents != 2000 || *old.GroupID != *p.GroupID {
		t.Fatal(old)
	}
	catalogue = accessAuditGet(t, r)
	bad := map[string]any{"Name": "Should roll back", "Access": map[string]any{"mode": "independent", "inbound_ids": []int64{999999}, "revision": catalogue.Revision, "confirm_shared": true}}
	accessAuditCall(t, r.c, "PATCH", "/api/admin/plans/"+itoa(p.ID), bad, 400)
	old, _ = r.st.PlanByID(context.Background(), p.ID)
	if old.Name == "Should roll back" {
		t.Fatal(old)
	}
	accessAuditCall(t, r.c, "POST", "/api/admin/plans", map[string]any{"Name": "No stolen private grants", "GroupID": p.GroupID}, 400)
}

func TestPlanCategoryHTTPGuardsAndNewInboundDefault(t *testing.T) {
	r, ib, entry, _ := accessAuditFixture(t)
	cat := mustJSON[domain.EntryCategory](t, accessAuditCall(t, r.c, "POST", "/api/admin/entry-categories", map[string]any{"Name": "Premium"}, 200))
	catalogue := accessAuditGet(t, r)
	globals := []int64{}
	for _, row := range catalogue.Inbounds {
		if row.Policy == "all" {
			globals = append(globals, row.InboundID)
		}
	}
	plan := mustJSON[domain.Plan](t, accessAuditCall(t, r.c, "POST", "/api/admin/plans", map[string]any{"Name": "Follow", "PeriodDays": 30, "Access": map[string]any{"mode": "independent", "inbound_ids": globals, "category_ids": []int64{cat.ID}, "revision": catalogue.Revision, "confirm_shared": true}}, 200))
	if plan.GroupID == nil {
		t.Fatal("missing group")
	}
	narrow := accessAuditToken(t, r, 1, "full", []string{"entries:write"})
	path := "/api/admin/entries/" + itoa(entry.ID)
	accessAuditCall(t, narrow, "PATCH", path, map[string]any{"CategoryID": cat.ID}, 403)
	accessAuditCall(t, narrow, "POST", "/api/admin/entries", map[string]any{"Name": "Forbidden", "InboundID": ib.ID, "DisplayHost": "node.example.com", "DisplayPort": 443, "CategoryID": cat.ID}, 403)
	accessAuditCall(t, r.c, "PATCH", path, map[string]any{"CategoryID": cat.ID}, 200)
	accessAuditCall(t, narrow, "PATCH", path, map[string]any{"CategoryID": 0}, 403)
	accessAuditCall(t, narrow, "PATCH", path, map[string]any{"Name": "Metadata stays editable"}, 200)
	accessAuditCall(t, narrow, "DELETE", path, nil, 403)
	_, operator := accessAuditStaff(t, r, "operator")
	accessAuditCall(t, operator, "DELETE", "/api/admin/entry-categories/"+itoa(cat.ID), nil, 403)
	// Browser clients explicitly use plan-based defaults; old scripts keep their API contract.
	status, body, _ := r.c.do("POST", "/api/admin/nodes/"+itoa(r.nodeID)+"/inbounds", map[string]any{"Tag": "unassigned", "Protocol": "vless", "Port": 15443}, map[string]string{"X-Captain-Access-Model": "plan"})
	if status != 200 {
		t.Fatal(status, string(body))
	}
	created := mustJSON[domain.Inbound](t, body)
	a, err := r.st.InboundAccess(context.Background(), created.ID)
	if err != nil || a.Policy != "none" {
		t.Fatal(a, err)
	}
	accessAuditCall(t, r.c, "DELETE", "/api/admin/entry-categories/"+itoa(cat.ID), nil, 200)
}
