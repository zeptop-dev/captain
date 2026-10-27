package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/migrations"
)

func financeStaff(t *testing.T, s *Store) int64 {
	t.Helper()
	u := &domain.User{Email: "payments@example.com", Role: domain.RoleAdmin, Status: "active"}
	if err := s.CreateStaff(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	return u.ID
}
func TestLinkedOnlineRenewalCalendarAndArchivedBill(t *testing.T) {
	for _, tc := range []struct {
		name, due, at, want string
		months, anchor      int
		archive             bool
	}{
		{"monthly", "2027-01-31", "2027-01-31", "2027-02-28", 1, 31, false},
		{"quarterly clamped", "2027-02-28", "2027-02-28", "2027-05-31", 3, 31, false},
		{"quarterly independent day", "2027-02-15", "2027-02-15", "2027-05-31", 3, 31, false},
		{"yearly independent day", "2027-02-15", "2027-02-15", "2028-02-29", 12, 31, false},
		{"leap year", "2024-02-29", "2025-02-28", "2026-02-28", 12, 29, false},
		{"multiple periods", "2027-02-28", "2028-07-01", "2028-08-31", 3, 31, false},
		{"archived remains archived", "2027-02-28", "2027-02-28", "2027-05-31", 3, 31, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, id, sup := infrastructureRig(t)
			ctx := context.Background()
			a := InfraAsset{SupplierID: sup.ID, Name: "Existing bill", Currency: "USD", AmountMinor: 1000, PeriodMonths: tc.months, AnchorDay: tc.anchor, NextDue: tc.due, Active: true}
			if err := s.SaveInfraAsset(ctx, &a); err != nil {
				t.Fatal(err)
			}
			before := infrastructureView(t, s, id)
			v, err := s.ChangeNodeInfrastructure(ctx, id, NodeInfrastructureChange{Revision: before.Revision, Action: "link_asset", AssetID: a.ID, AssetRevision: a.Revision})
			if err != nil {
				t.Fatal(err)
			}
			if !v.AutoRenewal.Enabled || !v.Asset.AutoRenewalEnabled {
				t.Fatal("link disabled existing switch")
			}
			if tc.archive {
				asset := *v.Asset
				asset.Active = false
				if err = s.SaveInfraAsset(ctx, &asset); err != nil {
					t.Fatal(err)
				}
				v = infrastructureView(t, s, id)
			}
			at, _ := time.Parse("2006-01-02", tc.at)
			rep := agentproto.Report{Host: renewalHost(at, "calendar", 1)}
			if renewed, e := s.RecordNodeReport(ctx, id, "current-token-hash", rep, true, at); e != nil || !renewed {
				t.Fatal(renewed, e)
			}
			after := infrastructureView(t, s, id)
			if after.ExpiresAt != tc.want || after.Asset.NextDue != tc.want || after.Asset.AnchorDay != tc.anchor || after.Asset.Revision != v.Asset.Revision+1 || after.Revision == v.Revision || after.Asset.Active == tc.archive {
				t.Fatal("shared extension", after)
			}
			if _, total, e := s.InfraPayments(ctx, a.ID, 0); e != nil || total != 0 {
				t.Fatal("automatic update created payment", total, e)
			}
			p, _ := s.NodeProbe(ctx, id)
			if day, _ := nodePeriodDay(p); day != tc.anchor {
				t.Fatal("reset day lost", day)
			}
			if renewed, e := s.RecordNodeReport(ctx, id, "current-token-hash", rep, true, at); e != nil || renewed {
				t.Fatal("replayed report", renewed, e)
			}
			// Merely editing unrelated metadata must retain an equivalent original
			// renewal anchor and not need extra shared-finance scope.
			asset := *after.Asset
			asset.Notes = "independent note"
			if err = s.SaveInfraAsset(ctx, &asset, false); err != nil {
				t.Fatal("notes-only write changed anchor", err)
			}
		})
	}
}

func TestLinkedRenewalSwitchCASAndRollback(t *testing.T) {
	s, id, sup := infrastructureRig(t)
	ctx := context.Background()
	v := linkNewInfrastructure(t, s, id, sup)
	staff := financeStaff(t, s)
	stale := InfraPaymentInput{RequestKey: "old_open_payment_001", Revision: v.Asset.Revision, AmountMinor: 1000, PaidDate: "2027-01-31"}
	p, _ := s.NodeProbe(ctx, id)
	p.Info.AutoRenewal = &NodeAutoRenewal{Enabled: false, AnchorDate: "2099-09-09", AnchorDay: 9}
	if err := s.UpdateNodeProbeWithInfrastructure(ctx, id, false, p.Info, 10000, 1, "sum", nil, nil, nil, NodeInfrastructureGuard{Revision: &v.Revision, CanWrite: true}); err != nil {
		t.Fatal(err)
	}
	current := infrastructureView(t, s, id)
	if current.AutoRenewal.Enabled || current.Asset.AutoRenewalEnabled || current.AutoRenewal.AnchorDay != 31 || current.AutoRenewal.AnchorDate != "2027-01-31" || current.Asset.Revision <= v.Asset.Revision {
		t.Fatal("switch or server-owned anchor", current)
	}
	if _, err := s.RecordInfraPayment(ctx, v.Asset.ID, staff, stale); !errors.Is(err, ErrAssetConflict) {
		t.Fatal("open payment failed to notice switch", err)
	}
	at := time.Date(2027, 1, 31, 0, 0, 0, 0, time.UTC)
	if renewed, err := s.RecordNodeBeat(ctx, id, "current-token-hash", renewalHost(at, "disabled", 1), at); err != nil || renewed {
		t.Fatal(renewed, err)
	}
	p, _ = s.NodeProbe(ctx, id)
	p.Info.AutoRenewal.Enabled = true
	if err := s.UpdateNodeProbeWithInfrastructure(ctx, id, false, p.Info, 10000, 1, "sum", nil, nil, nil, NodeInfrastructureGuard{Revision: &current.Revision, CanWrite: true}); err != nil {
		t.Fatal(err)
	}
	current = infrastructureView(t, s, id)
	if _, err := s.db.Exec(`CREATE TRIGGER fail_auto_asset BEFORE UPDATE ON infra_assets BEGIN SELECT RAISE(FAIL,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	host := renewalHost(at, "atomic", 1)
	if _, err := s.RecordNodeBeat(ctx, id, "current-token-hash", host, at); err == nil {
		t.Fatal("partial automatic extension succeeded")
	}
	after := infrastructureView(t, s, id)
	if after.Revision != current.Revision || after.ExpiresAt != current.ExpiresAt {
		t.Fatal("auto rollback lost shared state", after)
	}
	if _, err := s.db.Exec(`DROP TRIGGER fail_auto_asset`); err != nil {
		t.Fatal(err)
	}
	if renewed, err := s.RecordNodeBeat(ctx, id, "current-token-hash", host, at); err != nil || !renewed {
		t.Fatal("rollback consumed report cursor", renewed, err)
	}
	after = infrastructureView(t, s, id)
	if after.ExpiresAt != "2027-02-28" || after.Asset.NextDue != after.ExpiresAt {
		t.Fatal(after)
	}
	// Old callers on an auto-enabled primary safely default to record-only.
	stale.RequestKey = "old_empty_action_001"
	stale.Revision = after.Asset.Revision
	receipt, err := s.RecordInfraPayment(ctx, after.Asset.ID, staff, stale, false)
	if err != nil || receipt.DateAction != "record_only" || receipt.NextDue != "" || receipt.PeriodStart != "" {
		t.Fatal(receipt, err)
	}
	if got := infrastructureView(t, s, id); got.ExpiresAt != after.ExpiresAt || got.Asset.NextDue != after.Asset.NextDue {
		t.Fatal("late recording advanced again", got)
	}
}

func TestInfraPaymentDateActionsAndHistoricalSnapshots(t *testing.T) {
	s, id, sup := infrastructureRig(t)
	ctx := context.Background()
	v := linkNewInfrastructure(t, s, id, sup)
	staff := financeStaff(t, s)
	request := InfraPaymentInput{RequestKey: "explicit_date_00001", Revision: v.Asset.Revision, AmountMinor: 900, PaidDate: "2026-12-31", DateAction: "set_due_date", NextDue: v.ExpiresAt}
	p, err := s.RecordInfraPayment(ctx, v.Asset.ID, staff, request)
	if err != nil || p.DateAction != "set_due_date" || p.PeriodStart != p.NextDue {
		t.Fatal("same date should be legal", p, err)
	}
	retry, err := s.RecordInfraPayment(ctx, v.Asset.ID, staff, request)
	if err != nil || retry.ID != p.ID {
		t.Fatal(retry, err)
	}
	v = infrastructureView(t, s, id)
	request.RequestKey = "explicit_date_00002"
	request.Revision = v.Asset.Revision
	request.NextDue = "2027-04-15"
	if _, err = s.RecordInfraPayment(ctx, v.Asset.ID, staff, request); err != nil {
		t.Fatal(err)
	}
	v = infrastructureView(t, s, id)
	if v.ExpiresAt != "2027-04-15" || v.Asset.AnchorDay != 15 || !v.AutoRenewal.Enabled {
		t.Fatal("manual date did not establish new anchor", v)
	}
	request.RequestKey = "explicit_date_00003"
	request.Revision = v.Asset.Revision
	request.NextDue = "2027-04-14"
	if _, err = s.RecordInfraPayment(ctx, v.Asset.ID, staff, request); !errors.Is(err, ErrAssetInput) {
		t.Fatal("backward date", err)
	}
	// A deliberate manual correction establishes the current date. A new
	// confirmation may use that corrected value without rewriting old history.
	history, _, err := s.InfraPayments(ctx, v.Asset.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	a := *v.Asset
	a.NextDue = "2027-02-15"
	if err = s.SaveInfraAsset(ctx, &a); err != nil {
		t.Fatal(err)
	}
	request.Revision = a.Revision
	request.NextDue = "2027-02-15"
	if _, err = s.RecordInfraPayment(ctx, a.ID, staff, request); err != nil {
		t.Fatal("corrected current date confirmation rejected", err)
	}
	updated, _, err := s.InfraPayments(ctx, a.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, old := range history {
		found := false
		for _, saved := range updated {
			if saved.ID == old.ID {
				found = true
				if !reflect.DeepEqual(saved, old) {
					t.Fatal("historical snapshot changed", old, saved)
				}
			}
		}
		if !found {
			t.Fatal("historical payment removed")
		}
	}
	a = *infrastructureView(t, s, id).Asset
	request.Revision = a.Revision
	request.DateAction = "record_only"
	request.NextDue = ""
	request.RequestKey = "record_only_0000001"
	if _, err = s.RecordInfraPayment(ctx, a.ID, staff, request, false); err != nil {
		t.Fatal(err)
	}
	if got := infrastructureView(t, s, id); got.ExpiresAt != "2027-02-15" || !got.Asset.Active {
		t.Fatal("record-only changed schedule", got)
	}
	request.RequestKey = "record_only_0000002"
	request.NextDue = "2027-03-15"
	if _, err = s.RecordInfraPayment(ctx, a.ID, staff, request, false); !errors.Is(err, ErrAssetInput) {
		t.Fatal("ignored extra target date", err)
	}
}

func TestPrimaryDetachmentInvalidatesPaymentDraft(t *testing.T) {
	for _, operation := range []string{"unlink", "delete"} {
		t.Run(operation, func(t *testing.T) {
			s, id, sup := infrastructureRig(t)
			ctx := context.Background()
			v := linkNewInfrastructure(t, s, id, sup)
			staff := financeStaff(t, s)
			input := InfraPaymentInput{RequestKey: "draft_before_detach_001", Revision: v.Asset.Revision, AmountMinor: 1000, PaidDate: "2027-01-31"}
			loose := InfraAsset{SupplierID: sup.ID, NodeID: &id, Name: "Loose independent cost", Currency: "USD", AmountMinor: 500, PeriodMonths: 1, AnchorDay: 1, NextDue: "2027-02-01", Active: true}
			if err := s.SaveInfraAsset(ctx, &loose); err != nil {
				t.Fatal(err)
			}
			if operation == "unlink" {
				if _, err := s.ChangeNodeInfrastructure(ctx, id, NodeInfrastructureChange{Revision: v.Revision, Action: "unlink_asset"}); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := s.db.Exec(`CREATE TRIGGER fail_node_delete BEFORE DELETE ON nodes BEGIN SELECT RAISE(FAIL,'injected'); END`); err != nil {
					t.Fatal(err)
				}
				if err := s.DeleteNode(ctx, id); err == nil {
					t.Fatal("injected deletion succeeded")
				}
				if got := infrastructureView(t, s, id); got.Asset.Revision != v.Asset.Revision {
					t.Fatal("failed deletion changed revision")
				}
				if _, err := s.db.Exec(`DROP TRIGGER fail_node_delete`); err != nil {
					t.Fatal(err)
				}
				if err := s.DeleteNode(ctx, id); err != nil {
					t.Fatal(err)
				}
			}
			assets, err := s.InfraAssets(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, asset := range assets {
				if asset.ID == v.Asset.ID && (asset.Revision != v.Asset.Revision+1 || asset.PrimaryNodeID != nil || asset.AutoRenewalEnabled || asset.NextDue != v.Asset.NextDue) {
					t.Fatal("detachment did not invalidate payment", asset)
				}
				if asset.ID == loose.ID && asset.Revision != loose.Revision {
					t.Fatal("loose association affected", asset)
				}
			}
			if _, err = s.RecordInfraPayment(ctx, v.Asset.ID, staff, input); !errors.Is(err, ErrAssetConflict) {
				t.Fatal("stale empty action changed from record-only to advance", err)
			}
			if _, total, err := s.InfraPayments(ctx, v.Asset.ID, 0); err != nil || total != 0 {
				t.Fatal(total, err)
			}
		})
	}
}

func TestInfraPaymentMigration95LegacyIdempotency(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err = goose.SetDialect("sqlite"); err != nil {
		t.Fatal(err)
	}
	if err = goose.UpToContext(ctx, conn, ".", 94); err != nil {
		t.Fatal(err)
	}
	s := New(conn)
	sup := InfraSupplier{Name: "Legacy provider"}
	if err = s.SaveInfraSupplier(ctx, &sup); err != nil {
		t.Fatal(err)
	}
	a := InfraAsset{SupplierID: sup.ID, Name: "Legacy invoice", Currency: "USD", AmountMinor: 1000, PeriodMonths: 1, AnchorDay: 31, NextDue: "2027-02-28", Active: false}
	if err = s.SaveInfraAsset(ctx, &a); err != nil {
		t.Fatal(err)
	}
	staff := financeStaff(t, s)
	oldJSON := `{"request_key":"legacy_payment_0001","revision":1,"amount_minor":1000,"paid_date":"2027-01-31","reference":"invoice","notes":"saved note"}`
	input := InfraPaymentInput{RequestKey: "legacy_payment_0001", Revision: 1, AmountMinor: 1000, PaidDate: "2027-01-31", Reference: "invoice", Notes: "saved note"}
	raw, _ := json.Marshal(input)
	if string(raw) != oldJSON {
		t.Fatalf("old hash preimage changed: %s", raw)
	}
	sum := sha256.Sum256([]byte(oldJSON))
	hash := hex.EncodeToString(sum[:])
	if _, err = conn.Exec(`INSERT INTO infra_payments(asset_id,staff_id,request_key,request_hash,supplier_name,asset_name,currency,amount_minor,paid_date,period_start,next_due,reference,notes,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, a.ID, staff, input.RequestKey, hash, sup.Name, a.Name, a.Currency, 1000, input.PaidDate, "2027-01-31", "2027-02-28", input.Reference, input.Notes, now()); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(`UPDATE infra_assets SET revision=8 WHERE id=?`, a.ID); err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx, conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	receipt, err := s.RecordInfraPayment(ctx, a.ID, staff, input, false)
	if err != nil || receipt.DateAction != "advance_period" || receipt.NextDue != "2027-02-28" {
		t.Fatal("legacy replay must predate current archived/CAS checks", receipt, err)
	}
	input.DateAction = "record_only"
	if _, err = s.RecordInfraPayment(ctx, a.ID, staff, input); !errors.Is(err, ErrAssetConflict) {
		t.Fatal("changed action reused old key", err)
	}
	if _, total, err := s.InfraPayments(ctx, a.ID, 0); err != nil || total != 1 {
		t.Fatal(total, err)
	}
}
