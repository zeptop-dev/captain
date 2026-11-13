package store

import (
	"context"
	"math"
	"testing"
	"time"
)

func TestInfraAnalyticsHistoryMoneyAndReversal(t *testing.T) {
	s, node, _ := monitorRig(t)
	ctx := context.Background()
	supplier := InfraSupplier{Name: "Example supplier"}
	if e := s.SaveInfraSupplier(ctx, &supplier); e != nil {
		t.Fatal(e)
	}
	a := InfraAsset{SupplierID: supplier.ID, NodeID: &node, Name: "Example server", Currency: "USD", AmountMinor: 3100, PeriodMonths: 1, AnchorDay: 31, NextDue: "2026-12-31", Active: true, RemindDays: 7}
	if e := s.SaveInfraAsset(ctx, &a); e != nil {
		t.Fatal(e)
	}
	first := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	mid := first.AddDate(0, 0, 15)
	at := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	s.db.Exec(`UPDATE infra_price_history SET effective_at=? WHERE asset_id=?`, first.Unix(), a.ID)
	a.AmountMinor = 6200
	if e := s.SaveInfraAsset(ctx, &a); e != nil {
		t.Fatal(e)
	}
	s.db.Exec(`UPDATE infra_price_history SET effective_at=? WHERE asset_id=? AND amount_minor=6200`, mid.Unix(), a.ID)
	v, e := s.InfraAnalytics(ctx, "2026-12", a.ID, at)
	if e != nil || len(v.Totals) != 1 || len(v.Prices) != 2 {
		t.Fatal(v, e)
	}
	total := v.Totals[0]
	if math.Abs(total.MonthAccrued-4500) > 0.01 || total.MonthlyBudget != 6200 || total.AnnualBudget != 74400 || total.Paid != 0 {
		t.Fatal("estimate changed payments or wrong weighted history", total)
	}
	expense := InfraExpense{AssetID: a.ID, RequestKey: "example-extra-key-0001", Currency: "EUR", AmountMinor: 1000, PaidDate: "2026-12-20", Category: "ip", Note: "Additional IP"}
	if e = s.AddInfraExpense(ctx, &expense); e != nil {
		t.Fatal(e)
	}
	if e = s.AddInfraExpense(ctx, &expense); e != nil {
		t.Fatal("same-key retry", e)
	}
	expense.AmountMinor = 2000
	if e = s.AddInfraExpense(ctx, &expense); e != ErrAssetConflict {
		t.Fatal("key reuse changed money", e)
	}
	v, e = s.InfraAnalytics(ctx, "2026-12", a.ID, at)
	if e != nil || len(v.Totals) != 2 || len(v.Expenses) != 1 {
		t.Fatal(v, e)
	}
	if e = s.ReverseInfraExpense(ctx, expense.ID, "duplicate charge", at); e != nil {
		t.Fatal(e)
	}
	if e = s.ReverseInfraExpense(ctx, expense.ID, "duplicate charge", at); e != nil {
		t.Fatal(e)
	}
	v, e = s.InfraAnalytics(ctx, "2026-12", a.ID, at)
	if e != nil || len(v.Expenses) != 2 || !v.Expenses[1].Voided {
		t.Fatal(v, e)
	}
	for _, n := range v.Totals {
		if n.Currency == "EUR" && n.Extras != 0 {
			t.Fatal(n)
		}
	}
	if e = s.DeleteInfraAsset(ctx, a.ID, a.Revision); e != ErrAssetHistory {
		t.Fatal("expense history deleted", e)
	}
	// A different month must not fabricate historical operating costs.
	v, e = s.InfraAnalytics(ctx, "2026-11", a.ID, at)
	if e != nil {
		t.Fatal(e)
	}
	for _, n := range v.Totals {
		if n.MonthAccrued != 0 {
			t.Fatal(n)
		}
	}
}
func TestInfraPriceHistoryAlsoCapturesLinkedAndNoopChanges(t *testing.T) {
	s, _, _ := monitorRig(t)
	ctx := context.Background()
	supplier := InfraSupplier{Name: "Example supplier"}
	s.SaveInfraSupplier(ctx, &supplier)
	a := InfraAsset{SupplierID: supplier.ID, Name: "Example", Currency: "USD", AmountMinor: 1000, PeriodMonths: 3, AnchorDay: 31, NextDue: "2027-03-31", Active: true}
	if e := s.SaveInfraAsset(ctx, &a); e != nil {
		t.Fatal(e)
	}
	if e := s.SaveInfraAsset(ctx, &a); e != nil {
		t.Fatal(e)
	}
	var n int
	s.db.QueryRow("SELECT COUNT(*) FROM infra_price_history WHERE asset_id=?", a.ID).Scan(&n)
	if n != 1 {
		t.Fatal("noop added price version", n)
	}
	// Other supported writers of the shared asset cannot bypass the snapshot.
	if _, e := s.db.Exec("UPDATE infra_assets SET amount_minor=1500,currency='EUR' WHERE id=?", a.ID); e != nil {
		t.Fatal(e)
	}
	s.db.QueryRow("SELECT COUNT(*) FROM infra_price_history WHERE asset_id=?", a.ID).Scan(&n)
	if n != 2 {
		t.Fatal(n)
	}
}
