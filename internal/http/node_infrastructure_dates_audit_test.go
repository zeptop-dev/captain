package http

import (
	"context"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/store"
)

// Exercise the whole agent -> server metadata -> private ledger path. A live
// report is evidence for the configured date estimate, never a payment receipt.
func TestNodeInfrastructureOnlineDatesAudit(t *testing.T) {
	r := newRig(t)
	path := nodeInfraBase + "/nodes/" + itoa(r.nodeID)
	probePath := "/api/admin/nodes/" + itoa(r.nodeID) + "/probe"
	supplier := mustJSON[store.InfraSupplier](t, accessAuditCall(t, r.c, "POST", nodeInfraBase+"/suppliers", map[string]string{"name": "Date audit supplier"}, 200))
	expired := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	info := store.NodeProbeInfo{ExpiresAt: expired, Billing: &store.NodeCost{Amount: "12.50", Currency: "USD", Cycle: "monthly"}, AutoRenewal: &store.NodeAutoRenewal{Enabled: true}}
	accessAuditCall(t, r.c, "PUT", probePath, map[string]any{"Info": info, "Mode": "sum", "ResetDay": 1}, 200)
	read := func() store.NodeInfrastructure {
		t.Helper()
		return mustJSON[store.NodeInfrastructure](t, accessAuditCall(t, r.c, "GET", path, nil, 200))
	}
	v := read()
	// Linking no longer asks to disable automatic date maintenance.
	v = mustJSON[store.NodeInfrastructure](t, accessAuditCall(t, r.c, "PUT", path, map[string]any{"revision": v.Revision, "action": "create_asset", "supplier_id": supplier.ID, "name": "Monthly test bill", "remind_days": 7}, 200))
	if v.AutoRenewal == nil || !v.AutoRenewal.Enabled || v.Asset == nil || v.ExpiresAt != expired {
		t.Fatal("link changed automatic date setting or expiry", v)
	}
	payPath := infraBase + "/assets/" + itoa(v.Asset.ID) + "/payments"
	staleRevision := v.Asset.Revision
	accessAuditCall(t, r.c, "PUT", "/api/admin/settings/probe", map[string]any{"enabled": true, "page_enabled": true, "visibility": "public", "public_sections": []string{"info"}}, 200)
	anon := &client{t: t, srv: r.srv}
	accessAuditCall(t, anon, "GET", "/api/probe", nil, 200) // Warm the public cache.
	host := spec.SystemStatus{MemTotal: 1024, Valid: &spec.MetricValidity{Memory: true}, Resources: &spec.Resources{Epoch: "linked-date-audit", Sequence: 1, At: time.Now().UnixMilli()}}
	accessAuditCall(t, r.agent, "POST", "/api/agent/report", map[string]any{"host": host, "traffic_epoch": "invalid", "traffic_seq": 1}, 400)
	if got := read(); got.ExpiresAt != expired || got.Asset.Revision != staleRevision {
		t.Fatal("rejected report changed shared dates", got)
	}
	accessAuditCall(t, r.agent, "POST", "/api/agent/report", map[string]any{"host": host}, 200)
	v = read()
	if v.ExpiresAt <= expired || v.ExpiresAt != v.Asset.NextDue || v.Asset.Revision <= staleRevision || !v.AutoRenewal.Enabled {
		t.Fatal("accepted report failed to advance both views", v)
	}
	advanced, advancedRevision := v.ExpiresAt, v.Asset.Revision
	payments, total, err := r.st.InfraPayments(context.Background(), v.Asset.ID, 0)
	if err != nil || total != 0 || len(payments) != 0 {
		t.Fatal("automatic advance generated a payment", payments, err)
	}
	accessAuditCall(t, r.agent, "POST", "/api/agent/report", map[string]any{"host": host}, 200)
	if got := read(); got.ExpiresAt != advanced || got.Asset.Revision != advancedRevision {
		t.Fatal("report replay advanced shared dates again", got)
	}
	public := mustJSON[struct {
		Nodes []struct {
			Info store.NodeProbeInfo `json:"info"`
		} `json:"nodes"`
	}](t, accessAuditCall(t, anon, "GET", "/api/probe", nil, 200))
	if len(public.Nodes) != 1 || public.Nodes[0].Info.ExpiresAt != advanced {
		t.Fatal("public cache retained the old date", public)
	}
	input := map[string]any{"request_key": "online_dates_audit_payment", "revision": staleRevision, "amount_minor": 1250, "paid_date": expired, "date_action": "set_due_date", "next_due": advanced}
	accessAuditCall(t, r.c, "POST", payPath, input, 409)
	input["revision"], input["date_action"] = advancedRevision, "record_only"
	delete(input, "next_due")
	first := mustJSON[map[string]any](t, accessAuditCall(t, r.c, "POST", payPath, input, 200))
	second := mustJSON[map[string]any](t, accessAuditCall(t, r.c, "POST", payPath, input, 200))
	if first["id"] != second["id"] || first["date_action"] != "record_only" || first["period_start"] != "" || first["next_due"] != "" {
		t.Fatal("record-only payment retry or date semantics", first, second)
	}
	v = read()
	if v.ExpiresAt != advanced || v.Asset.NextDue != advanced || !v.AutoRenewal.Enabled || !v.Asset.Active {
		t.Fatal("record-only payment changed date maintenance", v)
	}
	// A legacy payment client also must not add another month while this new
	// explicitly linked bill is maintained automatically.
	delete(input, "date_action")
	input["request_key"], input["revision"] = "legacy_online_audit_payment", v.Asset.Revision
	legacy := mustJSON[map[string]any](t, accessAuditCall(t, r.c, "POST", payPath, input, 200))
	if legacy["date_action"] != "record_only" || read().ExpiresAt != advanced {
		t.Fatal("legacy client double-extended an automatic linked bill", legacy)
	}
	// Turning automatic maintenance off invalidates already-open payment forms.
	v = read()
	beforeToggle := v.Asset.Revision
	body := nodeInfraProbeBody(t, r)
	currentInfo := body["Info"].(store.NodeProbeInfo)
	currentInfo.AutoRenewal.Enabled = false
	body["Info"] = currentInfo
	accessAuditCall(t, r.c, "PUT", probePath, body, 200)
	v = read()
	if v.AutoRenewal.Enabled || v.Asset.Revision <= beforeToggle {
		t.Fatal("automatic switch did not invalidate payment drafts", v)
	}
	input["request_key"], input["revision"], input["date_action"], input["next_due"] = "explicit_dates_audit_payment", beforeToggle, "set_due_date", advanced
	accessAuditCall(t, r.c, "POST", payPath, input, 409)
	input["revision"] = v.Asset.Revision
	// An already advanced date may be explicitly confirmed without extending it.
	accessAuditCall(t, r.c, "POST", payPath, input, 200)
	if got := read(); got.ExpiresAt != advanced || got.AutoRenewal.Enabled {
		t.Fatal("explicit same-date payment changed automatic setting or expiry", got)
	}
}

func TestNodeInfrastructureDateActionsPermissionAudit(t *testing.T) {
	r, _, initial := nodeInfraFixture(t)
	payPath := infraBase + "/assets/" + itoa(initial.Asset.ID) + "/payments"
	settings := accessAuditToken(t, r, 1, "full", []string{"POST /api/admin/settings/infrastructure/assets/{id}/payments"})
	input := map[string]any{"request_key": "record_only_scope_audit", "revision": initial.Asset.Revision, "amount_minor": 1250, "paid_date": "2030-01-30", "date_action": "record_only"}
	accessAuditCall(t, settings, "POST", payPath, input, 200)
	v := mustJSON[store.NodeInfrastructure](t, accessAuditCall(t, r.c, "GET", nodeInfraBase+"/nodes/"+itoa(r.nodeID), nil, 200))
	if v.ExpiresAt != "2030-01-31" || v.Asset.NextDue != "2030-01-31" {
		t.Fatal("bookkeeping-only permission changed server expiry", v)
	}
	input["request_key"], input["revision"], input["date_action"], input["next_due"] = "set_due_scope_audit", v.Asset.Revision, "set_due_date", "2030-02-28"
	accessAuditCall(t, settings, "POST", payPath, input, 403)
	// Changing the automatic switch changes shared finance behavior and needs
	// infrastructure permission even when all visible dates stay the same.
	narrow := accessAuditToken(t, r, 1, "full", []string{"nodes:*"})
	body := nodeInfraProbeBody(t, r)
	info := body["Info"].(store.NodeProbeInfo)
	info.AutoRenewal.Enabled = true
	body["Info"] = info
	accessAuditCall(t, narrow, "PUT", "/api/admin/nodes/"+itoa(r.nodeID)+"/probe", body, 403)
	accessAuditCall(t, r.c, "POST", payPath, input, 200)
	payments, total, err := r.st.InfraPayments(context.Background(), v.Asset.ID, 0)
	if err != nil || total != 2 || len(payments) != 2 {
		t.Fatal("denied payment left a ledger record", total, err)
	}
}

func TestNodeInfrastructureDetachInvalidatesPaymentAudit(t *testing.T) {
	for _, action := range []string{"unlink", "delete"} {
		t.Run(action, func(t *testing.T) {
			r, _, _ := nodeInfraFixture(t)
			path := nodeInfraBase + "/nodes/" + itoa(r.nodeID)
			body := nodeInfraProbeBody(t, r)
			info := body["Info"].(store.NodeProbeInfo)
			info.AutoRenewal.Enabled = true
			body["Info"] = info
			accessAuditCall(t, r.c, "PUT", "/api/admin/nodes/"+itoa(r.nodeID)+"/probe", body, 200)
			before := mustJSON[store.NodeInfrastructure](t, accessAuditCall(t, r.c, "GET", path, nil, 200))
			if action == "unlink" {
				accessAuditCall(t, r.c, "PUT", path, map[string]any{"revision": before.Revision, "action": "unlink_asset"}, 200)
			} else {
				accessAuditCall(t, r.c, "DELETE", "/api/admin/nodes/"+itoa(r.nodeID), nil, 200)
			}
			assets, err := r.st.InfraAssets(context.Background())
			var detached *store.InfraAsset
			for i := range assets {
				if assets[i].ID == before.Asset.ID {
					detached = &assets[i]
				}
			}
			if err != nil || detached == nil || detached.Revision <= before.Asset.Revision {
				t.Fatal("detaching did not preserve and invalidate the asset", assets, err)
			}
			// Omitted action now means a period advance, whereas before detaching
			// it meant bookkeeping only. The old draft must therefore be rejected.
			input := map[string]any{"request_key": "detach_payment_audit", "revision": before.Asset.Revision, "amount_minor": 1250, "paid_date": "2030-01-30"}
			payPath := infraBase + "/assets/" + itoa(before.Asset.ID) + "/payments"
			accessAuditCall(t, r.c, "POST", payPath, input, 409)
			input["revision"] = detached.Revision
			paid := mustJSON[store.InfraPayment](t, accessAuditCall(t, r.c, "POST", payPath, input, 200))
			if paid.NextDue != "2030-02-28" {
				t.Fatal("fresh independent legacy payment lost its original behavior", paid)
			}
		})
	}
}
