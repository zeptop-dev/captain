package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
)

// Cancelling a queued plan refunds the buyer and takes the inviter's
// commission back, so the self-invite loop cannot mint money.
func TestCancelQueuedReversesCommission(t *testing.T) {
	conn, _ := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	if err := db.Migrate(context.Background(), conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	s := New(conn)
	ctx := context.Background()
	at := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	_ = s.SetSetting(ctx, SettingInvite, InviteSettings{Enabled: true, Percent: 20, Payout: PayoutBalance})
	inviter := &domain.User{Email: "a@example.com", UUID: "a", SubToken: "ta", Status: "active"}
	if err := s.CreateUser(ctx, inviter); err != nil {
		t.Fatal(err)
	}
	invitee := &domain.User{Email: "b@example.com", UUID: "b", SubToken: "tb", Status: "active", InvitedBy: &inviter.ID}
	if err := s.CreateUser(ctx, invitee); err != nil {
		t.Fatal(err)
	}
	plan := &domain.Plan{Name: "P", PeriodDays: 30, QuotaBytes: 1 << 30, Enabled: true}
	held := &domain.Plan{Name: "H", PeriodDays: 30, QuotaBytes: 1 << 30, Enabled: true}
	for _, pl := range []*domain.Plan{plan, held} {
		if err := s.CreatePlan(ctx, pl); err != nil {
			t.Fatal(err)
		}
	}
	// Hold a different active plan so buying P queues behind it.
	if _, err := s.GrantSubscription(ctx, invitee.ID, held, at); err != nil {
		t.Fatal(err)
	}
	before, _ := s.UserByID(ctx, inviter.ID)
	o := &domain.Order{UserID: invitee.ID, PlanID: plan.ID, No: "N1", AmountCents: 10000, Gateway: "balance", Status: "pending", Activation: "queue", PeriodDays: 30}
	if err := s.CreateOrder(ctx, o); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MarkPaid(ctx, o.No, "", at); err != nil {
		t.Fatal(err)
	}
	paid, _ := s.UserByID(ctx, inviter.ID)
	if paid.BalanceCents != before.BalanceCents+2000 {
		t.Fatalf("commission not paid: %d -> %d", before.BalanceCents, paid.BalanceCents)
	}
	subs, _ := s.Subscriptions(ctx, invitee.ID)
	var queued int64
	for _, sub := range subs {
		if sub.Status == "queued" {
			queued = sub.ID
		}
	}
	if queued == 0 {
		t.Fatalf("no queued subscription: %+v", subs)
	}
	if err := s.CancelQueued(ctx, invitee.ID, queued); err != nil {
		t.Fatal(err)
	}
	after, _ := s.UserByID(ctx, inviter.ID)
	if after.BalanceCents != before.BalanceCents {
		t.Fatalf("commission not reversed: %d, want %d", after.BalanceCents, before.BalanceCents)
	}
}
