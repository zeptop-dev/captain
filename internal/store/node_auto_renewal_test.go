package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
)

func TestNodeAutoRenewalCalendarAndNormalization(t *testing.T) {
	for _, tc := range []struct{ anchor, expiry, at, cycle, years, want string }{
		{"2027-01-31", "2027-01-31", "2027-01-31", "monthly", "", "2027-02-28"},
		{"2027-01-31", "2027-02-28", "2027-02-28", "monthly", "", "2027-03-31"},
		{"2027-01-31", "2027-02-28", "2028-02-29", "monthly", "", "2028-03-31"},
		{"2028-02-29", "2028-02-29", "2031-02-28", "annual", "", "2032-02-29"},
		{"2027-01-31", "2027-01-31", "2027-04-30", "quarterly", "", "2027-07-31"},
		{"2027-08-31", "2027-08-31", "2028-02-29", "semiannual", "", "2028-08-31"},
		{"2027-01-31", "2027-01-31", "2028-07-31", "custom", "1.5", "2030-01-31"},
		{"2027-01-31", "2027-01-31", "2035-06-15", "monthly", "", "2035-06-30"},
		{"2027-01-31", "2027-01-31", "2027-01-30", "monthly", "", ""},
		{"9999-12-31", "9999-12-31", "9999-12-31", "monthly", "", ""},
	} {
		t.Run(tc.at+tc.cycle+tc.expiry, func(t *testing.T) {
			at, _ := time.Parse("2006-01-02", tc.at)
			info := NodeProbeInfo{ExpiresAt: tc.expiry, Billing: &NodeCost{Cycle: tc.cycle, Years: tc.years}, AutoRenewal: &NodeAutoRenewal{Enabled: true, AnchorDate: tc.anchor}}
			got, changed := nextNodeExpiry(info, at.In(time.FixedZone("west", -7*3600)))
			if got != tc.want || changed != (tc.want != "") {
				t.Fatalf("got %s/%v want %s", got, changed, tc.want)
			}
		})
	}
	for _, tc := range []struct {
		years  string
		months int
	}{{"1.5", 18}, {"0.5", 6}, {"0.2500", 3}, {"1", 12}, {"999", 11988}, {"0.1", 0}, {"0", 0}, {"1.0001", 0}} {
		if got := nodeBillingMonths(&NodeCost{Cycle: "custom", Years: tc.years}); got != tc.months {
			t.Fatalf("%s years: %d months", tc.years, got)
		}
	}
	info := NodeProbeInfo{ExpiresAt: "2027-01-31", Billing: &NodeCost{Amount: "", Currency: "USD", Cycle: "monthly"}, AutoRenewal: &NodeAutoRenewal{Enabled: true, AnchorDate: "2000-01-01"}}
	if err := normalizeNodeCost(&info, NodeProbeInfo{}); err != nil || info.AutoRenewal.AnchorDate != "2027-01-31" {
		t.Fatal("server must establish anchor, including with unknown price", info, err)
	}
	old := info
	old.ExpiresAt = "2027-02-28"
	for _, state := range []*NodeAutoRenewal{nil, {Enabled: true, AnchorDate: "2020-03-15"}, {Enabled: false}} {
		info = old
		info.AutoRenewal = state
		if err := normalizeNodeCost(&info, old); err != nil || info.AutoRenewal.AnchorDate != "2027-01-31" {
			t.Fatal("echo/disable lost anchor", info, err)
		}
	}
	info = old
	info.ExpiresAt = "2027-03-20"
	if err := normalizeNodeCost(&info, old); err != nil || info.AutoRenewal.AnchorDate != info.ExpiresAt {
		t.Fatal("manual date did not re-anchor", info, err)
	}
	info = old
	info.Billing = &NodeCost{Amount: "0", Currency: "USD", Cycle: "quarterly"}
	if err := normalizeNodeCost(&info, old); err != nil || info.AutoRenewal.AnchorDate != info.ExpiresAt {
		t.Fatal("changed cycle did not re-anchor", info, err)
	}
	for _, pair := range [][2]NodeCost{
		{{Amount: "1", Currency: "USD", Cycle: "custom", Years: "1.5000"}, {Amount: "2", Currency: "USD", Cycle: "custom", Years: "1.5"}},
		{{Amount: "1", Currency: "USD", Cycle: "quarterly"}, {Amount: "1", Currency: "USD", Cycle: "custom", Years: "0.25"}},
		{{Amount: "1", Currency: "USD", Cycle: "custom", Years: "0.2500"}, {Amount: "1", Currency: "USD", Cycle: "quarterly"}},
	} {
		prior := NodeProbeInfo{ExpiresAt: "2029-02-28", Billing: &pair[0], AutoRenewal: &NodeAutoRenewal{Enabled: true, AnchorDate: "2028-02-29"}}
		next := prior
		next.Billing = &pair[1]
		if err := normalizeNodeCost(&next, prior); err != nil || next.AutoRenewal.AnchorDate != "2028-02-29" {
			t.Fatal("equivalent billing interval changed anchor", next, err)
		}
	}
	for _, invalid := range []NodeProbeInfo{
		{AutoRenewal: &NodeAutoRenewal{Enabled: true}},
		{ExpiresAt: "2027-01-01", AutoRenewal: &NodeAutoRenewal{Enabled: true}},
		{ExpiresAt: "2027-01-01", Billing: &NodeCost{Amount: "1", Currency: "USD", Cycle: "custom", Years: "0.1"}, AutoRenewal: &NodeAutoRenewal{Enabled: true}},
	} {
		if !errors.Is(normalizeNodeCost(&invalid, NodeProbeInfo{}), ErrNodeCost) {
			t.Fatal("invalid automatic renewal accepted", invalid)
		}
	}
}

func nodeRenewalRig(t *testing.T) (*Store, int64) {
	t.Helper()
	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "renewal.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx, conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	s := New(conn)
	n := &domain.Node{Name: "renewal fixture"}
	if err := s.CreateNode(ctx, n, "RENEW", time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RedeemPairCode(ctx, "RENEW", "current-token-hash", "example", "test", "linux"); err != nil {
		t.Fatal(err)
	}
	info := NodeProbeInfo{ExpiresAt: "2027-01-31", Billing: &NodeCost{Amount: "10", Currency: "USD", Cycle: "monthly"}, TrafficReset: &NodeTrafficReset{Mode: "expiry"}, AutoRenewal: &NodeAutoRenewal{Enabled: true}, Note: "preserve note"}
	if err := s.UpdateNodeProbe(ctx, n.ID, false, info, 10000, 1, "sum"); err != nil {
		t.Fatal(err)
	}
	return s, n.ID
}

func renewalHost(at time.Time, epoch string, seq uint64) spec.SystemStatus {
	return spec.SystemStatus{MemTotal: 1024, Valid: &spec.MetricValidity{Memory: true, Network: true}, Resources: &spec.Resources{At: at.UnixMilli(), Epoch: epoch, Sequence: seq, Networks: []spec.NetworkResource{{Name: "eth0", ID: "boot:1", Included: true, Up: 1000 + seq*100, Down: 2000 + seq*100}}}}
}

func TestNodeAutoRenewalAcceptedResourcesAndTraffic(t *testing.T) {
	s, id := nodeRenewalRig(t)
	ctx := context.Background()
	due := time.Date(2027, 1, 31, 0, 0, 0, 0, time.UTC)
	read := func(want string) *NodeProbe {
		t.Helper()
		p, err := s.NodeProbe(ctx, id)
		if err != nil || p.Info.ExpiresAt != want || p.Info.AutoRenewal.AnchorDate != "2027-01-31" || p.Info.Note != "preserve note" {
			t.Fatalf("date/metadata: %+v %v", p, err)
		}
		return p
	}
	pre := renewalHost(due.Add(-time.Second), "one", 1)
	if changed, err := s.RecordNodeBeat(ctx, id, "current-token-hash", pre, due.Add(-time.Second)); changed || err != nil {
		t.Fatal(changed, err)
	}
	// Repeated pre-expiry samples do not become evidence merely by arriving late.
	if changed, err := s.RecordNodeReport(ctx, id, "current-token-hash", agentproto.Report{Host: pre}, true, due); changed || err != nil {
		t.Fatal(changed, err)
	}
	read("2027-01-31")
	for _, h := range []spec.SystemStatus{{}, {MemTotal: 1024, Valid: &spec.MetricValidity{}}, renewalHost(due.Add(-6*time.Minute), "old", 1), renewalHost(due.Add(time.Hour), "future", 1)} {
		if changed, err := s.RecordNodeReport(ctx, id, "current-token-hash", agentproto.Report{Host: h}, true, due); changed || err != nil {
			t.Fatal("invalid/stale sample renewed", changed, err)
		}
	}
	// A valid sample still works after a far-future timestamp was rejected.
	h := renewalHost(due, "one", 2)
	if changed, err := s.RecordNodeBeat(ctx, id, "current-token-hash", h, due); !changed || err != nil {
		t.Fatal(changed, err)
	}
	p := read("2027-02-28")
	if p.UsedUp != 100 || p.UsedDown != 100 || !p.PeriodStart.Equal(due) {
		t.Fatalf("renewal lost normal traffic rollover: %+v", p)
	}
	// A second transport reporting the same resource is harmless.
	if changed, err := s.RecordNodeReport(ctx, id, "current-token-hash", agentproto.Report{Host: h}, true, due); changed || err != nil {
		t.Fatal(changed, err)
	}
	next := time.Date(2027, 2, 28, 0, 0, 0, 0, time.UTC)
	if changed, err := s.RecordNodeBeat(ctx, id, "current-token-hash", renewalHost(next, "one", 3), next); !changed || err != nil {
		t.Fatal(changed, err)
	}
	p = read("2027-03-31")
	if p.PrevUsed != 200 || p.UsedUp != 100 || p.UsedDown != 100 {
		t.Fatalf("second rollover: %+v", p)
	}
	if got := nodePeriodEnd(next, p).Format("2006-01-02"); got != "2027-03-31" {
		t.Fatal("reset day drifted", got)
	}
	entries, err := s.AdminLog(ctx, 0, 100)
	if err != nil || len(entries) != 2 || entries[0].Target != "2027-02-28 → 2027-03-31" || entries[0].Via != "probe_auto_renewal" || !entries[0].At.Equal(next) {
		t.Fatalf("audit: %+v %v", entries, err)
	}
}

func TestNodeAutoRenewalIdentityConcurrencyAndFormGuard(t *testing.T) {
	s, id := nodeRenewalRig(t)
	ctx := context.Background()
	due := time.Date(2027, 1, 31, 0, 0, 0, 0, time.UTC)
	h := renewalHost(due, "one", 1)
	if _, err := s.RecordNodeReport(ctx, id, "revoked-token", agentproto.Report{Host: h}, true, due); !errors.Is(err, ErrNotFound) {
		t.Fatal("wrong report identity", err)
	}
	if _, err := s.RecordNodeBeat(ctx, id, "revoked-token", h, due); !errors.Is(err, ErrNotFound) {
		t.Fatal("wrong beat identity", err)
	}
	before, _ := s.NodeProbe(ctx, id)
	var wg sync.WaitGroup
	results := make(chan bool, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			changed, err := s.RecordNodeReport(ctx, id, "current-token-hash", agentproto.Report{Host: h}, true, due)
			results <- changed
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	changes := 0
	for changed := range results {
		if changed {
			changes++
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if changes != 1 {
		t.Fatal("concurrent double renewal", changes)
	}
	// Saving a stale form's unrelated field retains the automatically advanced date.
	expected := before.Info.ExpiresAt
	info := before.Info
	info.Note = "edited note"
	if err := s.UpdateNodeProbeWithExpiryGuard(ctx, id, false, info, 10000, 1, "sum", nil, nil, &expected); err != nil {
		t.Fatal(err)
	}
	after, _ := s.NodeProbe(ctx, id)
	if after.Info.ExpiresAt != "2027-02-28" || after.Info.Note != "edited note" || after.Info.AutoRenewal.AnchorDate != "2027-01-31" {
		t.Fatalf("stale echo rolled back expiry: %+v", after.Info)
	}
	info.ExpiresAt = "2027-04-15"
	if err := s.UpdateNodeProbeWithExpiryGuard(ctx, id, false, info, 10000, 1, "sum", nil, nil, &expected); !errors.Is(err, ErrNodeExpiryConflict) {
		t.Fatal("stale explicit date accepted", err)
	}
	expected = after.Info.ExpiresAt
	if err := s.UpdateNodeProbeWithExpiryGuard(ctx, id, false, info, 10000, 1, "sum", nil, nil, &expected); err != nil {
		t.Fatal(err)
	}
	after, _ = s.NodeProbe(ctx, id)
	if after.Info.AutoRenewal.AnchorDate != "2027-04-15" {
		t.Fatal("manual change did not reset anchor")
	}
	if err := s.ResetPairCode(ctx, id, "NEW", time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordNodeReport(ctx, id, "current-token-hash", agentproto.Report{Host: h}, true, due); !errors.Is(err, ErrNotFound) {
		t.Fatal("revoked in-flight report", err)
	}
	if _, err := s.RedeemPairCode(ctx, "NEW", "replacement-token", "example", "test", "linux"); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := s.db.QueryRowContext(ctx, `SELECT probe_renewal_json FROM nodes WHERE id=?`, id).Scan(&raw); err != nil || raw != "{}" {
		t.Fatal("re-pair retained old receipt", raw, err)
	}
}

func TestNodeAutoRenewalBoundedCursorAndAuditRollback(t *testing.T) {
	s, id := nodeRenewalRig(t)
	ctx := context.Background()
	start := time.Date(2027, 1, 30, 23, 58, 0, 0, time.UTC)
	for i := 0; i < 20; i++ {
		at := start.Add(time.Duration(i) * time.Second)
		if _, err := s.RecordNodeReport(ctx, id, "current-token-hash", agentproto.Report{Host: renewalHost(at, fmt.Sprint(i), 1)}, true, at); err != nil {
			t.Fatal(err)
		}
	}
	var raw string
	if err := s.db.QueryRowContext(ctx, `SELECT probe_renewal_json FROM nodes WHERE id=?`, id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var cursor nodeRenewalCursor
	if err := json.Unmarshal([]byte(raw), &cursor); err != nil || len(cursor.Epochs) != 8 {
		t.Fatal("unbounded epochs", raw, err)
	}
	due := start.Add(2 * time.Minute)
	// The evicted old epoch remains stale because its sample timestamp is old.
	if changed, err := s.RecordNodeReport(ctx, id, "current-token-hash", agentproto.Report{Host: renewalHost(start, "0", 1)}, true, due); changed || err != nil {
		t.Fatal(changed, err)
	}
	if _, err := s.db.ExecContext(ctx, `CREATE TRIGGER reject_renewal_audit BEFORE INSERT ON admin_log BEGIN SELECT RAISE(FAIL,'fixture'); END`); err != nil {
		t.Fatal(err)
	}
	h := renewalHost(due, "fresh", 1)
	if changed, err := s.RecordNodeReport(ctx, id, "current-token-hash", agentproto.Report{Host: h}, true, due); changed || err == nil {
		t.Fatal("audit failure committed renewal", changed, err)
	}
	p, _ := s.NodeProbe(ctx, id)
	if p.Info.ExpiresAt != "2027-01-31" {
		t.Fatal("failed audit changed expiry")
	}
	if _, err := s.db.ExecContext(ctx, `DROP TRIGGER reject_renewal_audit`); err != nil {
		t.Fatal(err)
	}
	if changed, err := s.RecordNodeReport(ctx, id, "current-token-hash", agentproto.Report{Host: h}, true, due); !changed || err != nil {
		t.Fatal("rollback consumed cursor", changed, err)
	}
}
