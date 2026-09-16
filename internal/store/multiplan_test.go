package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
)

// Two plans in different groups stack: the user sees both groups' entries,
// traffic lands on the plan that owns the inbound's group, limits take the
// widest value, and a queued plan starts once the active ones lapse.
func TestMultiPlan(t *testing.T) {
	conn, _ := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	_ = db.Migrate(context.Background(), conn, "sqlite")
	s := New(conn)
	ctx := context.Background()
	at := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

	gA, _ := s.CreateGroup(ctx, "A")
	gB, _ := s.CreateGroup(ctx, "B")
	planA := &domain.Plan{Name: "A", PeriodDays: 30, QuotaBytes: 100, DeviceLimit: 2, SpeedLimitMbps: 10, GroupID: &gA, Enabled: true}
	planB := &domain.Plan{Name: "B", PeriodDays: 10, QuotaBytes: 50, DeviceLimit: 0, SpeedLimitMbps: 30, GroupID: &gB, Enabled: true}
	planC := &domain.Plan{Name: "C", PeriodDays: 7, QuotaBytes: 10, Enabled: true}
	for _, p := range []*domain.Plan{planA, planB, planC} {
		if err := s.CreatePlan(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	n := &domain.Node{Name: "n"}
	_ = s.CreateNode(ctx, n, "CODE", time.Hour)
	ibA := &domain.Inbound{NodeID: n.ID, Tag: "a", Protocol: spec.VLESS, Port: 443, GroupID: &gA, Enabled: true}
	ibB := &domain.Inbound{NodeID: n.ID, Tag: "b", Protocol: spec.VLESS, Port: 444, GroupID: &gB, Enabled: true}
	ibPub := &domain.Inbound{NodeID: n.ID, Tag: "p", Protocol: spec.VLESS, Port: 445, Enabled: true}
	for _, ib := range []*domain.Inbound{ibA, ibB, ibPub} {
		_ = s.CreateInbound(ctx, ib)
		_ = s.CreateEntry(ctx, &domain.Entry{Name: ib.Tag, InboundID: ib.ID, DisplayHost: "203.0.113.30", DisplayPort: ib.Port, Enabled: true})
	}
	u := &domain.User{Email: "m@x.y", UUID: "u", Status: "active"}
	_ = s.CreateUser(ctx, u)

	if _, err := s.GrantSubscriptionMode(ctx, u.ID, planA, at, GrantStack); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantSubscriptionMode(ctx, u.ID, planB, at, GrantStack); err != nil {
		t.Fatal(err)
	}
	subs, _ := s.ActiveSubscriptions(ctx, u.ID)
	if len(subs) != 2 || subs[0].PlanID != planB.ID { // B expires first
		t.Fatalf("active subs: %+v", subs)
	}
	u, _ = s.UserByID(ctx, u.ID)
	rows, _ := s.EntriesForUser(ctx, u)
	if len(rows) != 3 {
		t.Fatalf("entries = %d, want a, b and the public one", len(rows))
	}
	// Traffic on B's inbound charges B; on the public inbound the soonest
	// expiring (B) too; on A's inbound A.
	_ = s.AddTraffic(ctx, u.ID, ibB.ID, 5, 5, at)
	_ = s.AddTraffic(ctx, u.ID, ibA.ID, 20, 0, at)
	_ = s.AddTraffic(ctx, u.ID, ibPub.ID, 1, 0, at)
	subs, _ = s.ActiveSubscriptions(ctx, u.ID)
	byPlan := map[int64]*domain.Subscription{}
	for _, sub := range subs {
		byPlan[sub.PlanID] = sub
	}
	if got := byPlan[planB.ID].UsedUpBytes + byPlan[planB.ID].UsedDownBytes; got != 11 {
		t.Fatalf("B used = %d, want 11", got)
	}
	if got := byPlan[planA.ID].UsedUpBytes; got != 20 {
		t.Fatalf("A used = %d, want 20", got)
	}
	// Limits: device limit unlimited (B has 0), speed = max(10, 30).
	dl, _ := s.DeviceLimits(ctx)
	sl, _ := s.SpeedLimits(ctx)
	if dl[u.ID] != 0 || sl[u.ID] != 30 {
		t.Fatalf("limits device=%d speed=%d", dl[u.ID], sl[u.ID])
	}
	// Same plan again renews B in place instead of adding a row.
	if _, err := s.GrantSubscriptionMode(ctx, u.ID, planB, at, GrantStack); err != nil {
		t.Fatal(err)
	}
	if subs, _ = s.ActiveSubscriptions(ctx, u.ID); len(subs) != 2 {
		t.Fatalf("renewal added a row: %d", len(subs))
	}
	// Queue C: parked until nothing usable is left.
	if _, err := s.GrantSubscriptionMode(ctx, u.ID, planC, at, GrantQueue); err != nil {
		t.Fatal(err)
	}
	all, _ := s.Subscriptions(ctx, u.ID)
	if len(all) != 3 || all[2].Status != "queued" {
		t.Fatalf("queued row missing: %+v", all)
	}
	if n, _ := s.PromoteQueued(ctx, at); n != 0 {
		t.Fatalf("promoted too early")
	}
	later := at.AddDate(0, 0, 31)
	_, _ = s.ExpireSubscriptions(ctx, later)
	if n, _ := s.PromoteQueued(ctx, later); n != 1 {
		t.Fatalf("queued plan not started")
	}
	subs, _ = s.ActiveSubscriptions(ctx, u.ID)
	if len(subs) != 1 || subs[0].PlanID != planC.ID || subs[0].ExpiresAt == nil || !subs[0].ExpiresAt.Equal(later.AddDate(0, 0, 7)) {
		t.Fatalf("started sub: %+v", subs)
	}
	// Replace mode expires everything else.
	if _, err := s.GrantSubscriptionMode(ctx, u.ID, planA, later, GrantReplace); err != nil {
		t.Fatal(err)
	}
	if subs, _ = s.ActiveSubscriptions(ctx, u.ID); len(subs) != 1 || subs[0].PlanID != planA.ID {
		t.Fatalf("replace: %+v", subs)
	}
}

func TestUserQuotas(t *testing.T) {
	conn, _ := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	_ = db.Migrate(context.Background(), conn, "sqlite")
	s := New(conn)
	ctx := context.Background()
	at := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	days := &domain.Plan{Name: "d", PeriodDays: 90, QuotaBytes: 10 << 30, ResetMode: "days", ResetDays: 7, Enabled: true}
	period := &domain.Plan{Name: "p", PeriodDays: 30, QuotaBytes: 5 << 30, Enabled: true}
	unlimited := &domain.Plan{Name: "u", PeriodDays: 30, Enabled: true}
	for _, p := range []*domain.Plan{days, period, unlimited} {
		if err := s.CreatePlan(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	mk := func(email string, p *domain.Plan) int64 {
		u := &domain.User{Email: email, UUID: email, SubToken: "tok-" + email, Status: "active"}
		if err := s.CreateUser(ctx, u); err != nil {
			t.Fatalf("create user: %v", err)
		}
		if _, err := s.GrantSubscriptionMode(ctx, u.ID, p, at, GrantStack); err != nil {
			t.Fatal(err)
		}
		return u.ID
	}
	a, b, c := mk("a@x", days), mk("b@x", period), mk("c@x", unlimited)
	q, err := s.UserQuotas(ctx, at)
	if err != nil {
		t.Fatal(err)
	}
	if q[a] != (QuotaWindow{Bytes: 10 << 30, Days: 7}) {
		t.Fatalf("days plan: %+v", q[a])
	}
	if q[b] != (QuotaWindow{Bytes: 5 << 30, Days: 31}) { // 30-day period + 1
		t.Fatalf("period plan: %+v", q[b])
	}
	if _, has := q[c]; has {
		t.Fatalf("unlimited plan got a quota: %+v", q[c])
	}
}

// A renewal extends the expiry and keeps what was used; a plan without a
// reset cycle gains the new period's allowance, one with a cycle keeps its
// counter and next reset.
func TestRenewalKeepsUsage(t *testing.T) {
	conn, _ := db.Open("sqlite", filepath.Join(t.TempDir(), "r.db"))
	_ = db.Migrate(context.Background(), conn, "sqlite")
	s := New(conn)
	ctx := context.Background()
	at := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	flat := &domain.Plan{Name: "flat", PeriodDays: 30, QuotaBytes: 100, Enabled: true}
	cyc := &domain.Plan{Name: "cyc", PeriodDays: 30, QuotaBytes: 100, ResetDays: 30, Enabled: true}
	for _, p := range []*domain.Plan{flat, cyc} {
		if err := s.CreatePlan(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	u := &domain.User{Email: "r@x.y", UUID: "r", Status: "active"}
	_ = s.CreateUser(ctx, u)
	for _, p := range []*domain.Plan{flat, cyc} {
		if _, err := s.GrantSubscriptionMode(ctx, u.ID, p, at, GrantStack); err != nil {
			t.Fatal(err)
		}
	}
	_ = s.AddTraffic(ctx, u.ID, 0, 10, 20, at) // charged to the soonest-expiring (both expire together: lowest id = flat)
	before, _ := s.ActiveSubscriptions(ctx, u.ID)
	byPlan := map[int64]*domain.Subscription{}
	for _, sub := range before {
		byPlan[sub.PlanID] = sub
	}
	usedFlat := byPlan[flat.ID].UsedUpBytes + byPlan[flat.ID].UsedDownBytes
	if usedFlat != 30 {
		t.Fatalf("setup: flat used %d", usedFlat)
	}
	cycReset := byPlan[cyc.ID].ResetAt
	for _, p := range []*domain.Plan{flat, cyc} {
		if _, err := s.GrantSubscriptionMode(ctx, u.ID, p, at.Add(time.Hour), GrantStack); err != nil {
			t.Fatal(err)
		}
	}
	after, _ := s.ActiveSubscriptions(ctx, u.ID)
	if len(after) != 2 {
		t.Fatalf("renewal must not add rows: %d", len(after))
	}
	for _, sub := range after {
		prev := byPlan[sub.PlanID]
		if sub.ID != prev.ID {
			t.Fatalf("renewal replaced the row for plan %d", sub.PlanID)
		}
		if !sub.ExpiresAt.Equal(prev.ExpiresAt.AddDate(0, 0, 30)) {
			t.Fatalf("plan %d: expiry %v, want %v", sub.PlanID, sub.ExpiresAt, prev.ExpiresAt.AddDate(0, 0, 30))
		}
		if sub.UsedUpBytes != prev.UsedUpBytes || sub.UsedDownBytes != prev.UsedDownBytes {
			t.Fatalf("plan %d: usage changed on renewal: %+v -> %+v", sub.PlanID, prev, sub)
		}
		switch sub.PlanID {
		case flat.ID:
			if sub.QuotaBytes != 200 {
				t.Fatalf("flat quota %d, want 200 (allowance added)", sub.QuotaBytes)
			}
		case cyc.ID:
			if sub.QuotaBytes != 100 || sub.ResetAt == nil || cycReset == nil || !sub.ResetAt.Equal(*cycReset) {
				t.Fatalf("cyc quota %d reset %v, want 100 and the same reset %v", sub.QuotaBytes, sub.ResetAt, cycReset)
			}
		}
	}
}
