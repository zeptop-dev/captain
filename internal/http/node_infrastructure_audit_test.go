package http

import (
	"context"
	"strings"
	"testing"

	"github.com/zeptop-dev/captain/internal/store"
)

const nodeInfraBase = "/api/admin/infrastructure"

func nodeInfraFixture(t *testing.T) (*rig, store.InfraSupplier, store.NodeInfrastructure) {
	t.Helper()
	r := newRig(t)
	supplier, _ := infraFixture(t, r)
	supplier.Contact, supplier.Notes = "private-finance-contact", "private-finance-notes"
	if err := r.st.SaveInfraSupplier(context.Background(), &supplier); err != nil {
		t.Fatal(err)
	}
	info := store.NodeProbeInfo{Provider: "Legacy vendor", ExpiresAt: "2030-01-31", Billing: &store.NodeCost{Currency: "USD", Amount: "12.50", Cycle: "monthly"}}
	accessAuditCall(t, r.c, "PUT", "/api/admin/nodes/"+itoa(r.nodeID)+"/probe", map[string]any{"Info": info, "Mode": "sum", "ResetDay": 1}, 200)
	path := nodeInfraBase + "/nodes/" + itoa(r.nodeID)
	v := mustJSON[store.NodeInfrastructure](t, accessAuditCall(t, r.c, "GET", path, nil, 200))
	v = mustJSON[store.NodeInfrastructure](t, accessAuditCall(t, r.c, "PUT", path, map[string]any{"revision": v.Revision, "action": "create_asset", "supplier_id": supplier.ID, "name": "Private main bill", "remind_days": 7, "confirm_disable_auto_renewal": true}, 200))
	if v.Asset == nil || v.Asset.AmountMinor != 1250 || v.Supplier == nil || v.Supplier.ID != supplier.ID {
		t.Fatal("link did not use saved server values", v)
	}
	return r, supplier, v
}

func nodeInfraProbeBody(t *testing.T, r *rig) map[string]any {
	t.Helper()
	p, err := r.st.NodeProbe(context.Background(), r.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	v, err := r.st.NodeInfrastructure(context.Background(), r.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{"Info": p.Info, "Mode": p.Mode, "ResetDay": p.ResetDay, "ExpectedExpiresAt": p.Info.ExpiresAt, "InfrastructureRevision": v.Revision}
}

func TestNodeInfrastructureAuditAuthorization(t *testing.T) {
	r, supplier, initial := nodeInfraFixture(t)
	path := nodeInfraBase + "/nodes/" + itoa(r.nodeID)
	probePath := "/api/admin/nodes/" + itoa(r.nodeID) + "/probe"
	change := map[string]any{"revision": initial.Revision, "action": "unlink_asset"}
	for _, role := range []string{"operator", "support"} {
		u, session := accessAuditStaff(t, r, role)
		for _, c := range []*client{session, accessAuditToken(t, r, u.ID, "full", []string{"*"})} {
			accessAuditCall(t, c, "GET", path, nil, 403)
			accessAuditCall(t, c, "GET", nodeInfraBase+"/suppliers", nil, 403)
			accessAuditCall(t, c, "POST", nodeInfraBase+"/suppliers", map[string]string{"name": "Blocked"}, 403)
			accessAuditCall(t, c, "PUT", path, change, 403)
			body := nodeInfraProbeBody(t, r)
			info := body["Info"].(store.NodeProbeInfo)
			info.Billing = &store.NodeCost{Currency: "USD", Amount: "99", Cycle: "monthly"}
			body["Info"] = info
			accessAuditCall(t, c, "PUT", probePath, body, 403)
			if role == "operator" {
				body = nodeInfraProbeBody(t, r)
				delete(body, "InfrastructureRevision") // Older callers can edit unrelated metadata.
				info = body["Info"].(store.NodeProbeInfo)
				info.Note = "Allowed operations note"
				body["Info"] = info
				accessAuditCall(t, c, "PUT", probePath, body, 200)
			}
		}
	}
	narrow := accessAuditToken(t, r, 1, "full", []string{"nodes:*"})
	accessAuditCall(t, narrow, "GET", path, nil, 403)
	body := nodeInfraProbeBody(t, r)
	body["SupplierID"] = supplier.ID
	accessAuditCall(t, narrow, "PUT", probePath, body, 403)
	read := accessAuditToken(t, r, 1, "read", []string{"infrastructure:*"})
	accessAuditCall(t, read, "GET", path, nil, 200)
	accessAuditCall(t, read, "PUT", path, change, 403)
	// Old settings-only grants must not gain implicit writes to server finance.
	settings := accessAuditToken(t, r, 1, "full", []string{"PUT /api/admin/settings/infrastructure/assets/{id}", "PUT /api/admin/settings/infrastructure/suppliers/{id}", "POST /api/admin/settings/infrastructure/assets/{id}/payments"})
	a := *initial.Asset
	a.AmountMinor++
	accessAuditCall(t, settings, "PUT", infraBase+"/assets/"+itoa(a.ID), a, 403)
	supplier.Name = "Blocked rename"
	accessAuditCall(t, settings, "PUT", infraBase+"/suppliers/"+itoa(supplier.ID), supplier, 403)
	accessAuditCall(t, settings, "POST", infraBase+"/assets/"+itoa(a.ID)+"/payments", store.InfraPaymentInput{RequestKey: "finance_scope_payment", Revision: a.Revision, AmountMinor: 1250, PaidDate: "2030-01-30"}, 403)
	current := mustJSON[store.NodeInfrastructure](t, accessAuditCall(t, r.c, "GET", path, nil, 200))
	if current.Asset.AmountMinor != 1250 || current.Supplier.Name != "Vendor" || current.ExpiresAt != "2030-01-31" {
		t.Fatal("denied finance request mutated shared values", current)
	}
	combined := accessAuditToken(t, r, 1, "full", []string{"nodes:write", "infrastructure:write"})
	body = nodeInfraProbeBody(t, r)
	info := body["Info"].(store.NodeProbeInfo)
	info.Billing = &store.NodeCost{Currency: "USD", Amount: "13.50", Cycle: "monthly"}
	body["Info"] = info
	accessAuditCall(t, combined, "PUT", probePath, body, 200)
	current = mustJSON[store.NodeInfrastructure](t, accessAuditCall(t, r.c, "GET", path, nil, 200))
	if current.Asset.AmountMinor != 1350 {
		t.Fatal("combined permissions did not synchronize bill")
	}
}

func TestNodeInfrastructureAuditPaymentConflictAndPublicProjection(t *testing.T) {
	r, supplier, v := nodeInfraFixture(t)
	path := nodeInfraBase + "/nodes/" + itoa(r.nodeID)
	probePath := "/api/admin/nodes/" + itoa(r.nodeID) + "/probe"
	stale := nodeInfraProbeBody(t, r)
	input := store.InfraPaymentInput{RequestKey: "linked_payment_http", Revision: v.Asset.Revision, AmountMinor: 1250, PaidDate: "2030-01-30"}
	payPath := infraBase + "/assets/" + itoa(v.Asset.ID) + "/payments"
	first := mustJSON[store.InfraPayment](t, accessAuditCall(t, r.c, "POST", payPath, input, 200))
	second := mustJSON[store.InfraPayment](t, accessAuditCall(t, r.c, "POST", payPath, input, 200))
	if first.ID != second.ID || first.NextDue != "2030-02-28" {
		t.Fatal("payment retry/calendar", first, second)
	}
	accessAuditCall(t, r.c, "PUT", probePath, stale, 409)
	delete(stale, "InfrastructureRevision")
	// The old expiry guard preserves the newer expiry for an unchanged old form.
	accessAuditCall(t, r.c, "PUT", probePath, stale, 200)
	v = mustJSON[store.NodeInfrastructure](t, accessAuditCall(t, r.c, "GET", path, nil, 200))
	if v.ExpiresAt != "2030-02-28" || v.Asset.NextDue != v.ExpiresAt {
		t.Fatal("old form undid paid renewal", v)
	}
	accessAuditCall(t, r.c, "PUT", "/api/admin/settings/probe", map[string]any{"enabled": true, "page_enabled": true, "visibility": "public", "public_sections": []string{"info"}}, 200)
	anon := &client{t: t, srv: r.srv}
	public := accessAuditCall(t, anon, "GET", "/api/probe", nil, 200)
	for _, forbidden := range []string{"private-finance-contact", "private-finance-notes", "Private main bill", "supplier_id", "primary_node_id", "infrastructure_revision", "asset_id"} {
		if strings.Contains(string(public), forbidden) {
			t.Fatalf("public probe exposed %q", forbidden)
		}
	}
	supplier.Name, supplier.URL = "Updated vendor", "https://supplier.example.com"
	accessAuditCall(t, r.c, "PUT", infraBase+"/suppliers/"+itoa(supplier.ID), supplier, 200)
	public = accessAuditCall(t, anon, "GET", "/api/probe", nil, 200)
	if !strings.Contains(string(public), "Updated vendor") || !strings.Contains(string(public), "2030-02-28") {
		t.Fatal("public probe retained stale linked display", string(public))
	}
	choices := accessAuditCall(t, r.c, "GET", nodeInfraBase+"/suppliers", nil, 200)
	if strings.Contains(string(choices), "private-finance-") {
		t.Fatal("supplier picker exposed contact/notes")
	}
}
