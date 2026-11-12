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

func TestPlanNamesHTTPConflictAndAtomicRename(t *testing.T) {
	r, _, _, _ := accessAuditFixture(t)
	p := mustJSON[domain.Plan](t, accessAuditCall(t, r.c, "POST", "/api/admin/plans", map[string]any{"Name": "Existing", "PriceCents": 100}, 200))
	accessAuditCall(t, r.c, "PATCH", "/api/admin/plans/"+itoa(p.ID), map[string]any{"Enabled": false}, 200)
	accessAuditCall(t, r.c, "POST", "/api/admin/plans", map[string]any{"Name": " existing "}, 409)
	cat := accessAuditGet(t, r)
	var globals []int64
	for _, row := range cat.Inbounds {
		if row.Policy == "all" {
			globals = append(globals, row.InboundID)
		}
	}
	draft := map[string]any{"Name": "  Independent  ", "Access": map[string]any{"mode": "independent", "inbound_ids": globals, "revision": cat.Revision, "confirm_shared": true}}
	own := mustJSON[domain.Plan](t, accessAuditCall(t, r.c, "POST", "/api/admin/plans", draft, 200))
	if own.Name != "Independent" {
		t.Fatal(own.Name)
	}
	accessAuditCall(t, r.c, "PATCH", "/api/admin/plans/"+itoa(own.ID), map[string]any{"Name": "EXISTING", "PriceCents": 999}, 409)
	saved, err := r.st.PlanByID(context.Background(), own.ID)
	if err != nil || saved.Name != "Independent" || saved.PriceCents != 0 {
		t.Fatal(saved, err)
	}
	accessAuditCall(t, r.c, "PATCH", "/api/admin/plans/"+itoa(own.ID), map[string]any{"Name": "New name"}, 200)
	groups, err := r.st.ListGroups(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, g := range groups {
		if g.ID == *own.GroupID {
			found = g.Name == "New name"
		}
	}
	if !found {
		t.Fatal("owned group rename missing")
	}
	accessAuditCall(t, r.c, "POST", "/api/admin/groups", map[string]any{"Name": "Taken scheme"}, 200)
	accessAuditCall(t, r.c, "PATCH", "/api/admin/plans/"+itoa(own.ID), map[string]any{"Name": "Taken scheme"}, 409)
	accessAuditCall(t, r.c, "POST", "/api/admin/plans", map[string]any{"Name": "  "}, 400)
}

func TestConversionNoticeDismissHTTPAuthorization(t *testing.T) {
	r, _, _, _ := accessAuditFixture(t)
	path := "/api/admin/node-access/conversion/dismiss"
	empty := map[string]any{"at": 0, "inbounds": 0, "plans": 0}
	_, operator := accessAuditStaff(t, r, "operator")
	accessAuditCall(t, operator, "POST", path, empty, 403)
	_, support := accessAuditStaff(t, r, "support")
	accessAuditCall(t, support, "POST", path, empty, 403)
	readonly := accessAuditToken(t, r, 1, "read", []string{"node-access:read"})
	accessAuditCall(t, readonly, "POST", path, empty, 403)
	narrow := accessAuditToken(t, r, 1, "full", []string{"plans:write"})
	accessAuditCall(t, narrow, "POST", path, empty, 403)
	writer := accessAuditToken(t, r, 1, "full", []string{"node-access:write"})
	accessAuditCall(t, writer, "POST", path, empty, 400)
	// This fresh-install receipt has no conversions: a fabricated nonempty count
	// cannot dismiss it. A stale browser must refresh the real receipt.
	accessAuditCall(t, writer, "POST", path, map[string]any{"at": 1, "inbounds": 1, "plans": 1}, 409)
	if _, err := r.st.DB().Exec(`UPDATE plan_access_conversion SET converted_at=123,inbound_count=13,plan_count=1 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	before := mustJSON[store.InboundAccessCatalog](t, accessAuditCall(t, r.c, "GET", "/api/admin/node-access", nil, 200))
	receipt := map[string]any{"at": 123, "inbounds": 13, "plans": 1}
	accessAuditCall(t, writer, "POST", path, receipt, 200)
	after := mustJSON[store.InboundAccessCatalog](t, accessAuditCall(t, r.c, "GET", "/api/admin/node-access", nil, 200))
	if after.Conversion == nil || !after.Conversion.Dismissed || before.Revision != after.Revision {
		t.Fatal("dismissal missing or permission revision changed")
	}
	accessAuditCall(t, writer, "POST", path, receipt, 200)
	anonymous := &client{t: t, srv: r.srv}
	accessAuditCall(t, anonymous, "POST", path, receipt, 401)
	code, _, _ := r.c.do("POST", path, receipt, map[string]string{"Origin": "https://example.com"})
	if code != 403 {
		t.Fatal("cross origin dismissal allowed", code)
	}
	if _, err := r.st.DB().Exec(`DELETE FROM plan_access_conversion`); err != nil {
		t.Fatal(err)
	}
	accessAuditCall(t, writer, "POST", path, receipt, 404)

}
