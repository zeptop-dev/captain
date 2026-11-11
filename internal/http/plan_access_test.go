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
	bad := map[string]any{"Name": "Should roll back", "Access": map[string]any{"mode": "independent", "inbound_ids": []int64{999999}, "revision": catalogue.Revision, "confirm_shared": true}}
	accessAuditCall(t, r.c, "PATCH", "/api/admin/plans/"+itoa(p.ID), bad, 400)
	old, _ = r.st.PlanByID(context.Background(), p.ID)
	if old.Name == "Should roll back" {
		t.Fatal(old)
	}
	accessAuditCall(t, r.c, "POST", "/api/admin/plans", map[string]any{"Name": "No stolen private grants", "GroupID": p.GroupID}, 400)
}
