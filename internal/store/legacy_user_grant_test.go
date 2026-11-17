package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/testdb"
)

func TestLegacyUserGrantMigrationPreservesBillingAndFutureScheme(t *testing.T) {
	ctx := context.Background()
	s := New(testdb.Open(t))
	now := time.Now()
	g, e := s.CreateGroup(ctx, "Old permission")
	if e != nil {
		t.Fatal(e)
	}
	u := &domain.User{Email: "legacy@example.com", Role: "user", UUID: "uuid", SubToken: "token", Status: "active", GroupID: &g, BalanceCents: 1234}
	if e = s.CreateUser(ctx, u); e != nil {
		t.Fatal(e)
	}
	p := &domain.Plan{Name: "Covered", GroupID: &g, PeriodDays: 30, QuotaBytes: 1024, Enabled: false}
	if e = s.CreatePlan(ctx, p); e != nil {
		t.Fatal(e)
	}
	preview, e := s.LegacyUserGrant(ctx, u.ID, now)
	if e != nil || preview.CanMigrate || preview.Name != "Old permission" {
		t.Fatal(preview, e)
	}
	if e = s.MigrateLegacyUserGrant(ctx, u.ID, preview.Revision, now); !errors.Is(e, ErrLegacyGrantNeedsPlan) {
		t.Fatal(e)
	}
	// Queued or exhausted subscriptions cannot replace an account override.
	sub, e := s.GrantSubscriptionMode(ctx, u.ID, p, now, GrantQueue)
	if e != nil {
		t.Fatal(e)
	}
	// Reproduce a legacy override retained after the subscription was assigned.
	if e = s.UpdateUser(ctx, u.ID, "active", &g, ""); e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec(`UPDATE subscriptions SET status='queued' WHERE id=?`, sub.ID); e != nil {
		t.Fatal(e)
	}
	if e = s.MigrateLegacyUserGrant(ctx, u.ID, preview.Revision, now); !errors.Is(e, ErrLegacyGrantNeedsPlan) {
		t.Fatal("queued accepted", e)
	}
	if _, e = s.db.Exec(`UPDATE subscriptions SET status='active',used_up_bytes=1024 WHERE id=?`, sub.ID); e != nil {
		t.Fatal(e)
	}
	if e = s.MigrateLegacyUserGrant(ctx, u.ID, preview.Revision, now); !errors.Is(e, ErrLegacyGrantNeedsPlan) {
		t.Fatal("exhausted accepted", e)
	}
	if _, e = s.db.Exec(`UPDATE subscriptions SET used_up_bytes=7,used_down_bytes=11 WHERE id=?`, sub.ID); e != nil {
		t.Fatal(e)
	}
	preview, e = s.LegacyUserGrant(ctx, u.ID, now)
	if e != nil || !preview.CanMigrate || len(preview.CoveringPlans) != 1 {
		t.Fatal(preview, e)
	}
	before, e := s.Subscriptions(ctx, u.ID)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MigrateLegacyUserGrant(ctx, u.ID, preview.Revision, now.Add(31*24*time.Hour)); !errors.Is(e, ErrLegacyGrantNeedsPlan) {
		t.Fatal("expired accepted", e)
	}
	g2, e := s.CreateGroup(ctx, "Other")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.UpdateUser(ctx, u.ID, "active", &g2, ""); e != nil {
		t.Fatal(e)
	}
	if e = s.MigrateLegacyUserGrant(ctx, u.ID, preview.Revision, now); !errors.Is(e, ErrLegacyGrantConflict) {
		t.Fatal("stale authorization accepted", e)
	}
	if e = s.UpdateUser(ctx, u.ID, "active", &g, ""); e != nil {
		t.Fatal(e)
	}
	if e = s.MigrateLegacyUserGrant(ctx, u.ID, preview.Revision, now); e != nil {
		t.Fatal(e)
	}
	got, e := s.UserByID(ctx, u.ID)
	if e != nil || got.GroupID != nil || got.BalanceCents != 1234 || got.UUID != "uuid" || got.SubToken != "token" {
		t.Fatal(got, e)
	}
	after, e := s.Subscriptions(ctx, u.ID)
	if e != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("billing changed", before, after, e)
	}
	if e = s.MigrateLegacyUserGrant(ctx, u.ID, preview.Revision, now); !errors.Is(e, ErrLegacyGrantConflict) {
		t.Fatal("replayed migration", e)
	}
	// The old status editor cannot reintroduce the removed override by omission.
	if e = s.UpdateUserFields(ctx, u.ID, "banned", nil, "hash", false); e != nil {
		t.Fatal(e)
	}
	got, e = s.UserByID(ctx, u.ID)
	if e != nil || got.GroupID != nil || got.Status != "banned" || got.PasswordHash != "hash" {
		t.Fatal(got, e)
	}
	// Explicit legacy callers keep set/null semantics; modern omission preserves.
	if e = s.UpdateUser(ctx, u.ID, "active", &g, ""); e != nil {
		t.Fatal(e)
	}
	if e = s.UpdateUserFields(ctx, u.ID, "banned", nil, "", false); e != nil {
		t.Fatal(e)
	}
	got, e = s.UserByID(ctx, u.ID)
	if e != nil || got.GroupID == nil || *got.GroupID != g {
		t.Fatal(got, e)
	}
	if e = s.UpdateUser(ctx, u.ID, "active", nil, ""); e != nil {
		t.Fatal(e)
	}
	got, e = s.UserByID(ctx, u.ID)
	if e != nil || got.GroupID != nil {
		t.Fatal(got, e)
	}
}
func TestLegacyGrantRequiresSameSchemeNotCoincidentallyEmptyPlans(t *testing.T) {
	ctx := context.Background()
	s := New(testdb.Open(t))
	now := time.Now()
	g, _ := s.CreateGroup(ctx, "First")
	g2, _ := s.CreateGroup(ctx, "Second")
	u := &domain.User{Email: "user@example.com", Role: "user", UUID: "u", SubToken: "t", Status: "active", GroupID: &g}
	if e := s.CreateUser(ctx, u); e != nil {
		t.Fatal(e)
	}
	p := &domain.Plan{Name: "Different scheme", GroupID: &g2, PeriodDays: 30}
	if e := s.CreatePlan(ctx, p); e != nil {
		t.Fatal(e)
	}
	if _, e := s.GrantSubscriptionMode(ctx, u.ID, p, now, GrantStack); e != nil {
		t.Fatal(e)
	}
	v, e := s.LegacyUserGrant(ctx, u.ID, now)
	if e != nil || v.CanMigrate {
		t.Fatal(v, e)
	}
	if e = s.MigrateLegacyUserGrant(ctx, u.ID, v.Revision, now); !errors.Is(e, ErrLegacyGrantNeedsPlan) {
		t.Fatal(e)
	}
	if _, e = s.db.Exec(`UPDATE users SET agent_id=agent_id+100 WHERE id=?`, u.ID); e != nil {
		t.Fatal(e)
	}
	if e = s.MigrateLegacyUserGrant(ctx, u.ID, v.Revision, now); !errors.Is(e, ErrLegacyGrantConflict) {
		t.Fatal("identity not bound", e)
	}
}
