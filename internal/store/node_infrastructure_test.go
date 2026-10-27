package store

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
)

func infrastructureRig(t *testing.T) (*Store, int64, InfraSupplier) {
	t.Helper()
	s, id := nodeRenewalRig(t)
	sup := InfraSupplier{Name: "Example provider", URL: "https://example.com", Contact: "private contact", Notes: "private notes"}
	if err := s.SaveInfraSupplier(context.Background(), &sup); err != nil {
		t.Fatal(err)
	}
	return s, id, sup
}
func infrastructureView(t *testing.T, s *Store, id int64) *NodeInfrastructure {
	t.Helper()
	v, err := s.NodeInfrastructure(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func linkNewInfrastructure(t *testing.T, s *Store, id int64, sup InfraSupplier) *NodeInfrastructure {
	t.Helper()
	v := infrastructureView(t, s, id)
	v, err := s.ChangeNodeInfrastructure(context.Background(), id, NodeInfrastructureChange{Revision: v.Revision, Action: "create_asset", SupplierID: &sup.ID, Name: "Server cost", RemindDays: 7, ConfirmDisableAutoRenewal: true})
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func TestNodeInfrastructureProviderOnlySnapshotsAndCAS(t *testing.T) {
	s, id, sup := infrastructureRig(t)
	ctx := context.Background()
	old := infrastructureView(t, s, id)
	v, err := s.ChangeNodeInfrastructure(ctx, id, NodeInfrastructureChange{Revision: old.Revision, Action: "supplier", SupplierID: &sup.ID})
	if err != nil || v.Asset != nil || v.Supplier == nil || v.Supplier.ID != sup.ID || v.Billing.Amount != "10" || !v.AutoRenewal.Enabled {
		t.Fatal(v, err)
	}
	if _, err = s.ChangeNodeInfrastructure(ctx, id, NodeInfrastructureChange{Revision: old.Revision, Action: "supplier", SupplierID: &sup.ID}); !errors.Is(err, ErrInfrastructureConflict) {
		t.Fatal("stale association", err)
	}
	if err = s.DeleteInfraSupplier(ctx, sup.ID); !errors.Is(err, ErrAssetHistory) {
		t.Fatal(err)
	}
	before, err := s.NodeProbe(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	sup.Name = "Updated provider"
	if err = s.SaveInfraSupplier(ctx, &sup, false); !errors.Is(err, ErrInfrastructurePermission) {
		t.Fatal("narrow write", err)
	}
	if err = s.SaveInfraSupplier(ctx, &sup, true); err != nil {
		t.Fatal(err)
	}
	after, _ := s.NodeProbe(ctx, id)
	if after.Info.Provider != sup.Name || after.Info.Note != before.Info.Note || !reflect.DeepEqual(after.Info.Billing, before.Info.Billing) {
		t.Fatal("bad fanout", after.Info)
	}
	if infrastructureView(t, s, id).Revision == v.Revision {
		t.Fatal("supplier change kept old revision")
	}
	sup.Notes = "new private note"
	if err = s.SaveInfraSupplier(ctx, &sup, false); err != nil {
		t.Fatal("independent notes blocked", err)
	}
	zero := int64(0)
	v = infrastructureView(t, s, id)
	if _, err = s.ChangeNodeInfrastructure(ctx, id, NodeInfrastructureChange{Revision: v.Revision, Action: "supplier", SupplierID: &zero}); err != nil {
		t.Fatal(err)
	}
	after, _ = s.NodeProbe(ctx, id)
	if after.Info.Provider != sup.Name || infrastructureView(t, s, id).Supplier != nil {
		t.Fatal("unlink erased display snapshot")
	}
	if err = s.DeleteInfraSupplier(ctx, sup.ID); err != nil {
		t.Fatal(err)
	}
}

func TestNodeInfrastructureSharedPaymentCalendarAndRollback(t *testing.T) {
	s, id, sup := infrastructureRig(t)
	ctx := context.Background()
	initial := infrastructureView(t, s, id)
	change := NodeInfrastructureChange{Revision: initial.Revision, Action: "create_asset", SupplierID: &sup.ID, Name: "Server cost"}
	if _, err := s.ChangeNodeInfrastructure(ctx, id, change); !errors.Is(err, ErrInfrastructureConfirmation) {
		t.Fatal(err)
	}
	assets, _ := s.InfraAssets(ctx)
	if len(assets) != 0 {
		t.Fatal("failed confirm created asset")
	}
	v := linkNewInfrastructure(t, s, id, sup)
	if v.Asset == nil || v.Asset.AmountMinor != 1000 || v.Asset.PrimaryNodeID == nil || *v.Asset.PrimaryNodeID != id || v.AutoRenewal.Enabled || v.Asset.AnchorDay != 31 {
		t.Fatal(v)
	}
	if err := s.DeleteInfraAsset(ctx, v.Asset.ID, v.Asset.Revision); !errors.Is(err, ErrInfrastructureLinked) {
		t.Fatal(err)
	}
	staff := &domain.User{Email: "finance@example.com", Role: domain.RoleAdmin, Status: "active"}
	if err := s.CreateStaff(ctx, staff); err != nil {
		t.Fatal(err)
	}
	in := InfraPaymentInput{RequestKey: "payment_link_test_0001", Revision: v.Asset.Revision, AmountMinor: 1000, PaidDate: "2027-01-31"}
	if _, err := s.RecordInfraPayment(ctx, v.Asset.ID, staff.ID, in, false); !errors.Is(err, ErrInfrastructurePermission) {
		t.Fatal(err)
	}
	if _, n, err := s.InfraPayments(ctx, v.Asset.ID, 0); err != nil || n != 0 {
		t.Fatal("unauthorized payment inserted", n, err)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER fail_link BEFORE UPDATE ON nodes BEGIN SELECT RAISE(FAIL,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordInfraPayment(ctx, v.Asset.ID, staff.ID, in, true); err == nil {
		t.Fatal("partial payment committed")
	}
	if _, err := s.db.Exec(`DROP TRIGGER fail_link`); err != nil {
		t.Fatal(err)
	}
	if got := infrastructureView(t, s, id); got.Asset.Revision != v.Asset.Revision || got.ExpiresAt != v.ExpiresAt {
		t.Fatal("rollback lost state", got)
	}
	first, err := s.RecordInfraPayment(ctx, v.Asset.ID, staff.ID, in, true)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.RecordInfraPayment(ctx, v.Asset.ID, staff.ID, in, true)
	if err != nil || first.ID != second.ID {
		t.Fatal("retry", second, err)
	}
	v = infrastructureView(t, s, id)
	if v.ExpiresAt != "2027-02-28" || v.Asset.NextDue != v.ExpiresAt || v.Asset.AnchorDay != 31 || v.AutoRenewal.AnchorDate != "2027-01-31" {
		t.Fatal("short month anchor lost", v)
	}
	p, _ := s.NodeProbe(ctx, id)
	if day, _ := nodePeriodDay(p); day != 31 {
		t.Fatal("reset anchor shifted", day)
	}
	// A malformed old flag must not let a resource heartbeat renew unpaid cost.
	p.Info.AutoRenewal.Enabled = true
	raw, _ := json.Marshal(p.Info)
	if _, err = s.db.Exec(`UPDATE nodes SET probe_info_json=? WHERE id=?`, string(raw), id); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC)
	if renewed, err := s.RecordNodeBeat(ctx, id, "current-token-hash", renewalHost(at, "linked", 1), at); err != nil || renewed {
		t.Fatal("online beat renewed ledger", renewed, err)
	}
	v = infrastructureView(t, s, id)
	if v.ExpiresAt != "2027-02-28" {
		t.Fatal(v)
	}
	in.RequestKey = "payment_link_test_0002"
	in.Revision = v.Asset.Revision
	if _, err = s.RecordInfraPayment(ctx, v.Asset.ID, staff.ID, in, true); err != nil {
		t.Fatal(err)
	}
	v = infrastructureView(t, s, id)
	if v.ExpiresAt != "2027-03-31" || v.AutoRenewal.Enabled {
		t.Fatal(v)
	}
	if _, err = s.ChangeNodeInfrastructure(ctx, id, NodeInfrastructureChange{Revision: v.Revision, Action: "unlink_asset"}); err != nil {
		t.Fatal(err)
	}
	unlinked := infrastructureView(t, s, id)
	if unlinked.Asset != nil || unlinked.Supplier == nil || unlinked.ExpiresAt != v.ExpiresAt || !reflect.DeepEqual(unlinked.Billing, v.Billing) {
		t.Fatal("unlink lost snapshot", unlinked)
	}
	if _, n, _ := s.InfraPayments(ctx, v.Asset.ID, 0); n != 2 {
		t.Fatal("unlink changed payments", n)
	}
}

func TestNodeInfrastructureProbeEditsAndExistingAssetDirection(t *testing.T) {
	s, id, sup := infrastructureRig(t)
	ctx := context.Background()
	a := InfraAsset{SupplierID: sup.ID, NodeID: &id, Name: "Existing cost", Currency: "JPY", AmountMinor: 4321, PeriodMonths: 3, AnchorDay: 31, NextDue: "2027-02-28", RemindDays: 5, Active: true}
	if err := s.SaveInfraAsset(ctx, &a); err != nil {
		t.Fatal(err)
	}
	before := infrastructureView(t, s, id)
	if before.Asset != nil {
		t.Fatal("loose node association auto-linked")
	}
	a.Active = false
	if err := s.SaveInfraAsset(ctx, &a); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ChangeNodeInfrastructure(ctx, id, NodeInfrastructureChange{Revision: before.Revision, Action: "link_asset", AssetID: a.ID, AssetRevision: a.Revision, ConfirmDisableAutoRenewal: true}); !errors.Is(err, ErrAssetInput) {
		t.Fatal("new primary may not be archived", err)
	}
	a.Active = true
	if err := s.SaveInfraAsset(ctx, &a); err != nil {
		t.Fatal(err)
	}
	v, err := s.ChangeNodeInfrastructure(ctx, id, NodeInfrastructureChange{Revision: before.Revision, Action: "link_asset", AssetID: a.ID, AssetRevision: a.Revision, ConfirmDisableAutoRenewal: true})
	if err != nil {
		t.Fatal(err)
	}
	if v.Billing.Amount != "4321" || v.Billing.Currency != "JPY" || v.ExpiresAt != a.NextDue || v.AutoRenewal.AnchorDate != "2027-01-31" {
		t.Fatal("wrong link direction", v)
	}
	p, _ := s.NodeProbe(ctx, id)
	p.Info.Note = "operator changed note"
	put := func(info NodeProbeInfo, g NodeInfrastructureGuard) error {
		return s.UpdateNodeProbeWithInfrastructure(ctx, id, false, info, 10000, 1, "sum", nil, nil, nil, g)
	}
	if err = put(p.Info, NodeInfrastructureGuard{}); err != nil {
		t.Fatal("unchanged legacy echo", err)
	}
	p.Info.Billing.Amount = "5000"
	if err = put(p.Info, NodeInfrastructureGuard{}); !errors.Is(err, ErrInfrastructurePermission) {
		t.Fatal("operator wrote shared bill", err)
	}
	if err = put(p.Info, NodeInfrastructureGuard{CanWrite: true}); !errors.Is(err, ErrInfrastructureConflict) {
		t.Fatal("missing CAS", err)
	}
	if err = put(p.Info, NodeInfrastructureGuard{CanWrite: true, Revision: &v.Revision}); err != nil {
		t.Fatal(err)
	}
	v = infrastructureView(t, s, id)
	if v.Asset.AmountMinor != 5000 || v.Asset.AnchorDay != 31 {
		t.Fatal(v)
	}
	if _, n, _ := s.InfraPayments(ctx, a.ID, 0); n != 0 {
		t.Fatal("editing created payment")
	}
	p, _ = s.NodeProbe(ctx, id)
	oldRevision := v.Revision
	asset := *v.Asset
	asset.NextDue = "2027-05-31"
	asset.Currency = "USD"
	asset.AmountMinor = 1234
	if err = s.SaveInfraAsset(ctx, &asset, false); !errors.Is(err, ErrInfrastructurePermission) {
		t.Fatal(err)
	}
	asset.Revision = v.Asset.Revision // failed methods do not commit their tentative revision
	if err = s.SaveInfraAsset(ctx, &asset, true); err != nil {
		t.Fatal(err)
	}
	if err = put(p.Info, NodeInfrastructureGuard{CanWrite: true, Revision: &oldRevision}); !errors.Is(err, ErrInfrastructureConflict) {
		t.Fatal("stale probe overwrote asset", err)
	}
	v = infrastructureView(t, s, id)
	if v.Billing.Amount != "12.34" || v.ExpiresAt != "2027-05-31" {
		t.Fatal(v)
	}
	asset = *v.Asset
	asset.NodeID = nil
	asset.Notes = "independent note"
	if err = s.SaveInfraAsset(ctx, &asset, false); err != nil || asset.NodeID == nil {
		t.Fatal("old field omission", err)
	}
	asset.Active = false
	if err = s.SaveInfraAsset(ctx, &asset, false); err != nil {
		t.Fatal("archive should retain existing relation", err)
	}
	archived := infrastructureView(t, s, id)
	if archived.Asset == nil || archived.Asset.Active || archived.Billing.Amount != "12.34" {
		t.Fatal("archive erased costs", archived)
	}
	other := id + 100
	asset.NodeID = &other
	if err = s.SaveInfraAsset(ctx, &asset, true); !errors.Is(err, ErrInfrastructureLinked) {
		t.Fatal("moved shared primary", err)
	}
	if err = s.DeleteNode(ctx, id); err != nil {
		t.Fatal(err)
	}
	assets, err := s.InfraAssets(ctx)
	if err != nil || len(assets) != 1 || assets[0].NodeID != nil || assets[0].PrimaryNodeID != nil {
		t.Fatal("node deletion lost asset", assets, err)
	}
}

func TestNodeInfrastructureExactConversion(t *testing.T) {
	for _, tc := range []struct {
		currency, amount string
		want             int64
		valid            bool
	}{
		{"USD", "12.34", 1234, true}, {"USD", "12.3400", 1234, true}, {"USD", "12.345", 0, false},
		{"JPY", "12", 12, true}, {"JPY", "12.1", 0, false}, {"KWD", "12.345", 12345, true}, {"KWD", "12.3456", 0, false},
		{"USD", "", 0, false}, {"USD", "0", 0, true}, {"ZZZ", "1", 0, false}, {"USD", "999999999999", 0, false},
	} {
		t.Run(tc.currency+tc.amount, func(t *testing.T) {
			n, err := costMinor(&NodeCost{Currency: tc.currency, Amount: tc.amount})
			if (err == nil) != tc.valid || tc.valid && n != tc.want {
				t.Fatal(n, err)
			}
		})
	}
	for _, months := range []int{0, 1, 2, 3, 6, 9, 12, 15, 120, 121} {
		b, err := assetNodeCost(InfraAsset{Currency: "USD", AmountMinor: 100, PeriodMonths: months})
		valid := months > 0 && months <= 120 && (months == 1 || months%3 == 0)
		if (err == nil) != valid || valid && nodeBillingMonths(b) != months {
			t.Fatal(months, b, err)
		}
	}
}

func TestNodeInfrastructureConcurrentSelectionAndReset(t *testing.T) {
	s, id, sup := infrastructureRig(t)
	ctx := context.Background()
	v := infrastructureView(t, s, id)
	changes := []NodeInfrastructureChange{
		{Revision: v.Revision, Action: "create_asset", SupplierID: &sup.ID, Name: "Choice A", ConfirmDisableAutoRenewal: true},
		{Revision: v.Revision, Action: "create_asset", SupplierID: &sup.ID, Name: "Choice B", ConfirmDisableAutoRenewal: true},
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, change := range changes {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.ChangeNodeInfrastructure(ctx, id, change); results <- err }()
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrInfrastructureConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	assets, err := s.InfraAssets(ctx)
	if err != nil || success != 1 || conflict != 1 || len(assets) != 1 {
		t.Fatal("duplicate primary creation", success, conflict, assets, err)
	}
	staff := &domain.User{Email: "reset@example.com", Role: domain.RoleAdmin, Status: "active"}
	if err = s.CreateStaff(ctx, staff); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResetSite(ctx, staff.ID); err != nil {
		t.Fatal("new private references prevented reset", err)
	}
	for _, table := range []string{"node_infrastructure", "infra_assets", "infra_suppliers"} {
		var n int
		if err = s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil || n != 0 {
			t.Fatal(table, n, err)
		}
	}
}
