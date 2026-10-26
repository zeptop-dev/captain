package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
)

type accessFixture struct {
	s        *Store
	ctx      context.Context
	n        *domain.Node
	groups   []int64
	inbounds []*domain.Inbound
	entries  []*domain.Entry
	plans    []*domain.Plan
	users    []*domain.User
}

func newAccessFixture(t *testing.T) *accessFixture {
	t.Helper()
	f := &accessFixture{s: subDesignStore(t), ctx: context.Background()}
	f.n = &domain.Node{Name: "Access server"}
	if err := f.s.CreateNode(f.ctx, f.n, "pair", time.Hour); err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"Basic", "Premium", "Other"} {
		g, err := f.s.CreateGroup(f.ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		f.groups = append(f.groups, g)
		p := &domain.Plan{Name: name, GroupID: &g, Enabled: true, PeriodDays: 30 + i, QuotaBytes: 10000}
		if err = f.s.CreatePlan(f.ctx, p); err != nil {
			t.Fatal(err)
		}
		f.plans = append(f.plans, p)
		u := &domain.User{Email: name + "@example.com", UUID: name, SubToken: name, Status: "active"}
		if err = f.s.CreateUser(f.ctx, u); err != nil {
			t.Fatal(err)
		}
		if _, err = f.s.GrantSubscriptionMode(f.ctx, u.ID, p, time.Now(), GrantStack); err != nil {
			t.Fatal(err)
		}
		f.users = append(f.users, u)
		ib := &domain.Inbound{NodeID: f.n.ID, Tag: name, Protocol: spec.VLESS, Port: 4000 + i, Enabled: true, GroupID: &g}
		if err = f.s.CreateInbound(f.ctx, ib); err != nil {
			t.Fatal(err)
		}
		f.inbounds = append(f.inbounds, ib)
		e := &domain.Entry{Name: name, InboundID: ib.ID, DisplayHost: "example.com", DisplayPort: ib.Port, Enabled: true}
		if err = f.s.CreateEntry(f.ctx, e); err != nil {
			t.Fatal(err)
		}
		f.entries = append(f.entries, e)
	}
	return f
}

func (f *accessFixture) change(t *testing.T, id int64, policy string, groups ...int64) domain.InboundAccessChange {
	t.Helper()
	c, err := f.s.AccessCatalog(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	return domain.InboundAccessChange{InboundID: id, Policy: policy, GroupIDs: groups, Revision: c.Revision}
}

func TestInboundAccessPublicationCredentialsAndBilling(t *testing.T) {
	f := newAccessFixture(t)
	s, ctx := f.s, f.ctx
	ib := f.inbounds[0]
	at := time.Now()
	change := f.change(t, ib.ID, domain.AccessGroups, f.groups[0], f.groups[1])
	if err := s.UpdateInboundAccess(ctx, ib.ID, change); err != nil {
		t.Fatal(err)
	}
	access, err := s.InboundAccess(ctx, ib.ID)
	if err != nil || access.Legacy || !reflect.DeepEqual(access.GroupIDs, f.groups[:2]) {
		t.Fatalf("access=%+v err=%v", access, err)
	}
	members, err := s.UsersWithInboundAccess(ctx, ib.ID, at)
	if err != nil || len(members) != 2 {
		t.Fatalf("members=%v err=%v", members, err)
	}
	for i, u := range f.users {
		rows, err := s.EntriesForUser(ctx, u)
		if err != nil {
			t.Fatal(err)
		}
		has := false
		for _, r := range rows {
			has = has || r.Inbound.ID == ib.ID
		}
		if has != (i < 2) {
			t.Fatalf("user %d publication=%v", i, has)
		}
	}
	// Two usable plans grant a union, but a report with stale legacy group
	// must still charge the matching current policy before an unrelated plan.
	u := f.users[2]
	if _, err = s.GrantSubscriptionMode(ctx, u.ID, f.plans[1], at, GrantStack); err != nil {
		t.Fatal(err)
	}
	if err = s.UpdateInboundAccess(ctx, ib.ID, f.change(t, ib.ID, domain.AccessGroups, f.groups[1])); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AddTrafficSamples(ctx, []TrafficSample{{UserID: u.ID, InboundID: ib.ID, Groups: []int64{f.groups[2]}, Up: 9, Down: 11}}, at); err != nil {
		t.Fatal(err)
	}
	subs, err := s.ActiveSubscriptions(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, sub := range subs {
		want := int64(0)
		if sub.PlanID == f.plans[1].ID {
			want = 20
		}
		if sub.UsedUpBytes+sub.UsedDownBytes != want {
			t.Fatalf("wrong charged plan: %+v", sub)
		}
	}
	if err = s.UpdateInboundAccess(ctx, ib.ID, f.change(t, ib.ID, domain.AccessNone)); err != nil {
		t.Fatal(err)
	}
	members, err = s.UsersWithInboundAccess(ctx, ib.ID, at)
	if err != nil || len(members) != 0 {
		t.Fatalf("none credentials=%v err=%v", members, err)
	}
	rows, err := s.EntriesForUser(ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Inbound.ID == ib.ID {
			t.Fatal("none published")
		}
	}
	// Tail accounting remains possible without granting any new access.
	if err = s.AddTraffic(ctx, u.ID, ib.ID, 1, 0, at); err != nil {
		t.Fatal(err)
	}
}

func TestInboundAccessRevisionSharedAndLegacyWriter(t *testing.T) {
	f := newAccessFixture(t)
	s, ctx := f.s, f.ctx
	ib := f.inbounds[0]
	draft := f.change(t, ib.ID, domain.AccessGroups, f.groups[0], f.groups[1])
	shared := &domain.Entry{Name: "Shared disabled endpoint", InboundID: ib.ID, DisplayHost: "example.com", DisplayPort: 443, Enabled: false}
	if err := s.CreateEntry(ctx, shared); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateInboundAccess(ctx, ib.ID, draft); !errors.Is(err, ErrAccessConflict) {
		t.Fatalf("stale shared relation: %v", err)
	}
	draft = f.change(t, ib.ID, domain.AccessGroups, f.groups[0], f.groups[1])
	if err := s.UpdateInboundAccess(ctx, ib.ID, draft); !errors.Is(err, ErrAccessConfirmationRequired) {
		t.Fatalf("shared confirmation: %v", err)
	}
	draft.ConfirmShared = true
	if err := s.UpdateInboundAccess(ctx, ib.ID, draft); err != nil {
		t.Fatal(err)
	}
	ib.Port++
	if err := s.UpdateInbound(ctx, ib); err != nil {
		t.Fatalf("unchanged legacy group: %v", err)
	}
	ib.GroupID = &f.groups[2]
	if err := s.UpdateInbound(ctx, ib); !errors.Is(err, ErrAccessManaged) {
		t.Fatalf("legacy overwrite: %v", err)
	}
	actual, err := s.InboundByID(ctx, ib.ID)
	if err != nil || actual.GroupID == nil || *actual.GroupID != f.groups[0] {
		t.Fatalf("legacy writer changed group: %+v %v", actual, err)
	}
	// A changed plan relation invalidates the draft even if listener policy
	// and the selected group set are unchanged.
	draft = f.change(t, ib.ID, domain.AccessNone)
	draft.ConfirmShared = true
	p := f.plans[2]
	p.GroupID = &f.groups[0]
	if err := s.UpdatePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateInboundAccess(ctx, ib.ID, draft); !errors.Is(err, ErrAccessConflict) {
		t.Fatalf("plan stale: %v", err)
	}
}

func TestGroupAccessAllLockedLastRemovalAndAtomicValidation(t *testing.T) {
	f := newAccessFixture(t)
	s, ctx := f.s, f.ctx
	id := f.inbounds[0].ID
	g := f.groups[0]
	if err := s.UpdateInboundAccess(ctx, id, f.change(t, id, domain.AccessAll)); err != nil {
		t.Fatal(err)
	}
	c, _ := s.AccessCatalog(ctx)
	if err := s.UpdateGroupAccess(ctx, g, GroupAccessChange{Revision: c.Revision, InboundIDs: []int64{}}); !errors.Is(err, ErrInvalidAccess) {
		t.Fatalf("all exclusion: %v", err)
	}
	// Unknown IDs must roll back earlier writes in the same membership edit.
	if err := s.UpdateGroupAccess(ctx, g, GroupAccessChange{Revision: c.Revision, InboundIDs: []int64{id, f.inbounds[1].ID, 999999}}); !errors.Is(err, ErrInvalidAccess) {
		t.Fatalf("unknown: %v", err)
	}
	a, _ := s.InboundAccess(ctx, f.inbounds[1].ID)
	if !reflect.DeepEqual(a.GroupIDs, []int64{f.groups[1]}) {
		t.Fatalf("partial write: %+v", a)
	}
	if err := s.UpdateInboundAccess(ctx, id, f.change(t, id, domain.AccessGroups, g)); err != nil {
		t.Fatal(err)
	}
	c, _ = s.AccessCatalog(ctx)
	if err := s.UpdateGroupAccess(ctx, g, GroupAccessChange{Revision: c.Revision, InboundIDs: []int64{}}); err != nil {
		t.Fatal(err)
	}
	a, _ = s.InboundAccess(ctx, id)
	if a.Policy != domain.AccessNone {
		t.Fatalf("removed last group became %s", a.Policy)
	}
	if err := s.UpdateInboundAccess(ctx, id, f.change(t, id, domain.AccessGroups)); !errors.Is(err, ErrInvalidAccess) {
		t.Fatalf("empty groups accepted: %v", err)
	}
	if err := s.UpdateInboundAccess(ctx, id, f.change(t, id, domain.AccessGroups, 99999)); !errors.Is(err, ErrInvalidAccess) {
		t.Fatalf("missing group accepted: %v", err)
	}
}

func TestEntryAccessAtomicAndBoundToInbound(t *testing.T) {
	f := newAccessFixture(t)
	s, ctx := f.s, f.ctx
	e := f.entries[0]
	oldName := e.Name
	draft := f.change(t, e.InboundID, domain.AccessNone)
	e.Name = "Must roll back"
	e.Access = &draft
	draft.Revision = "stale"
	if err := s.UpdateEntry(ctx, e); !errors.Is(err, ErrAccessConflict) {
		t.Fatalf("stale save: %v", err)
	}
	got, _ := s.EntryByID(ctx, e.ID)
	if got.Name != oldName {
		t.Fatal("metadata was partially committed")
	}
	draft = f.change(t, e.InboundID, domain.AccessNone)
	e.InboundID = f.inbounds[1].ID
	if err := s.UpdateEntry(ctx, e); !errors.Is(err, ErrInvalidAccess) {
		t.Fatalf("draft rebound: %v", err)
	}
	e.InboundID = f.inbounds[0].ID
	e.Name = "Saved atomically"
	draft = f.change(t, e.InboundID, domain.AccessNone)
	if err := s.UpdateEntry(ctx, e); err != nil {
		t.Fatal(err)
	}
	a, _ := s.InboundAccess(ctx, e.InboundID)
	got, _ = s.EntryByID(ctx, e.ID)
	if a.Policy != domain.AccessNone || got.Name != e.Name {
		t.Fatal("atomic change missing")
	}
	e.Access = nil
	e.Name = "Metadata only"
	if err := s.UpdateEntry(ctx, e); err != nil {
		t.Fatal(err)
	}
	a, _ = s.InboundAccess(ctx, e.InboundID)
	if a.Policy != domain.AccessNone {
		t.Fatal("metadata cleared policy")
	}
	// Creating a second endpoint adds another affected reference and requires
	// shared confirmation before changing the first endpoint's credentials.
	draft = f.change(t, e.InboundID, domain.AccessAll)
	newEntry := &domain.Entry{Name: "New shared", InboundID: e.InboundID, DisplayHost: "example.com", DisplayPort: 443, Enabled: true, Access: &draft}
	if err := s.CreateEntry(ctx, newEntry); !errors.Is(err, ErrAccessConfirmationRequired) {
		t.Fatalf("new shared: %v", err)
	}
	draft.ConfirmShared = true
	if err := s.CreateEntry(ctx, newEntry); err != nil {
		t.Fatal(err)
	}
}

func TestReceiverCannotGrantAccessAndTailReportsRemainAccepted(t *testing.T) {
	f := newAccessFixture(t)
	s, ctx := f.s, f.ctx
	ib := f.inbounds[0]
	ib.GroupID = nil
	ib.Settings.Reverse = &spec.ReverseInbound{ID: "private-tunnel", Receiver: true, UUID: "receiver"}
	if err := s.UpdateInbound(ctx, ib); err != nil {
		t.Fatal(err)
	}
	c, err := s.AccessCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range c.Inbounds {
		if row.InboundID == ib.ID && (row.Editable || row.Policy != domain.AccessNone) {
			t.Fatalf("receiver policy=%+v", row)
		}
	}
	if err = s.UpdateInboundAccess(ctx, ib.ID, f.change(t, ib.ID, domain.AccessAll)); !errors.Is(err, ErrInvalidAccess) {
		t.Fatalf("receiver editable: %v", err)
	}
	members, err := s.UsersWithInboundAccess(ctx, ib.ID, time.Now())
	if err != nil || len(members) > 0 {
		t.Fatalf("receiver customers=%v err=%v", members, err)
	}
	// The first user's old listener is now a receiver; no other listener
	// matches its group, so its reusable identity must not reach this server.
	ids, err := s.NodeAgentUserIDs(ctx, f.n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ids[f.users[0].AgentID]; ok {
		t.Fatal("receiver allowed user report identity")
	}
	u := f.users[1]
	if _, err = s.db.ExecContext(ctx, `UPDATE subscriptions SET used_up_bytes=quota_bytes,expires_at=? WHERE user_id=?`, time.Now().Add(-time.Minute).Unix(), u.ID); err != nil {
		t.Fatal(err)
	}
	members, err = s.UsersWithInboundAccess(ctx, f.inbounds[1].ID, time.Now())
	if err != nil || len(members) > 0 {
		t.Fatalf("expired credentials=%v %v", members, err)
	}
	ids, err = s.NodeAgentUserIDs(ctx, f.n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ids[u.AgentID]; !ok {
		t.Fatal("expired/exhausted tail report identity lost")
	}
}

func TestInboundAccessMultiPlanLifecycleCompatibility(t *testing.T) {
	f := newAccessFixture(t)
	s, ctx, u, ib := f.s, f.ctx, f.users[0], f.inbounds[0]
	at := time.Now().UTC().Truncate(time.Second)
	if err := s.UpdateInboundAccess(ctx, ib.ID, f.change(t, ib.ID, domain.AccessGroups, f.groups[1])); err != nil {
		t.Fatal(err)
	}
	allowed := func(want bool) {
		t.Helper()
		members, err := s.UsersWithInboundAccess(ctx, ib.ID, at)
		if err != nil {
			t.Fatal(err)
		}
		has := false
		for _, member := range members {
			has = has || member.ID == u.ID
		}
		if has != want {
			t.Fatalf("credential eligibility = %v, want %v", has, want)
		}
	}
	if _, err := s.GrantSubscriptionMode(ctx, u.ID, f.plans[1], at, GrantQueue); err != nil {
		t.Fatal(err)
	}
	allowed(false) // A queued plan must not grant its selected node set.
	if _, err := s.db.ExecContext(ctx, `UPDATE subscriptions SET expires_at=? WHERE user_id=? AND plan_id=?`, at.Add(-time.Second).Unix(), u.ID, f.plans[0].ID); err != nil {
		t.Fatal(err)
	}
	allowed(false)
	if n, err := s.PromoteQueued(ctx, at); err != nil || n != 1 {
		t.Fatalf("promote=%d err=%v", n, err)
	}
	allowed(true)
	if _, err := s.GrantSubscriptionMode(ctx, u.ID, f.plans[2], at, GrantStack); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE subscriptions SET used_up_bytes=quota_bytes WHERE user_id=? AND plan_id=?`, u.ID, f.plans[1].ID); err != nil {
		t.Fatal(err)
	}
	allowed(false) // The other usable plan does not keep the exhausted group.
	if _, err := s.db.ExecContext(ctx, `UPDATE subscriptions SET used_up_bytes=0,expires_at=? WHERE user_id=? AND plan_id=?`, at.Add(-time.Second).Unix(), u.ID, f.plans[1].ID); err != nil {
		t.Fatal(err)
	}
	allowed(false)
	if err := s.UpdateUser(ctx, u.ID, "active", &f.groups[1], ""); err != nil {
		t.Fatal(err)
	}
	allowed(true) // An existing account group still combines with another usable plan.
	if _, err := s.db.ExecContext(ctx, `UPDATE subscriptions SET expires_at=? WHERE user_id=? AND status='active'`, at.Add(-time.Second).Unix(), u.ID); err != nil {
		t.Fatal(err)
	}
	allowed(false) // The account group alone cannot supply an active allowance.
}

func TestInboundAccessRetainsLegacyReplaceGroupAcrossQueue(t *testing.T) {
	f := newAccessFixture(t)
	s, ctx, u, ib := f.s, f.ctx, f.users[0], f.inbounds[0]
	at := time.Now().UTC().Truncate(time.Second)
	if err := s.UpdateInboundAccess(ctx, ib.ID, f.change(t, ib.ID, domain.AccessGroups, f.groups[1])); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantSubscriptionMode(ctx, u.ID, f.plans[1], at, GrantReplace); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantSubscriptionMode(ctx, u.ID, f.plans[2], at, GrantQueue); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE subscriptions SET expires_at=? WHERE user_id=? AND status='active'`, at.Add(-time.Second).Unix(), u.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := s.PromoteQueued(ctx, at); err != nil || n != 1 {
		t.Fatalf("promote=%d err=%v", n, err)
	}
	current, err := s.UserByID(ctx, u.ID)
	if err != nil || current.GroupID == nil || *current.GroupID != f.groups[1] {
		t.Fatalf("legacy account group changed: %+v %v", current, err)
	}
	members, err := s.UsersWithInboundAccess(ctx, ib.ID, at)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, member := range members {
		found = found || member.ID == u.ID
	}
	if !found {
		t.Fatal("new policy changed existing single-plan group semantics")
	}
}

func TestGroupAccessConfirmsSharedPlansAndPreservesOtherGroups(t *testing.T) {
	f := newAccessFixture(t)
	s, ctx, group := f.s, f.ctx, f.groups[0]
	p := &domain.Plan{Name: "Another basic plan", GroupID: &group, Enabled: true, PeriodDays: 30}
	if err := s.CreatePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	c, err := s.AccessCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	change := GroupAccessChange{Revision: c.Revision, InboundIDs: []int64{f.inbounds[0].ID, f.inbounds[1].ID}}
	if err = s.UpdateGroupAccess(ctx, group, change); !errors.Is(err, ErrAccessConfirmationRequired) {
		t.Fatalf("shared plans accepted: %v", err)
	}
	change.ConfirmShared = true
	if err = s.UpdateGroupAccess(ctx, group, change); err != nil {
		t.Fatal(err)
	}
	a, err := s.InboundAccess(ctx, f.inbounds[1].ID)
	if err != nil || !reflect.DeepEqual(a.GroupIDs, f.groups[:2]) {
		t.Fatalf("other membership lost: %+v %v", a, err)
	}
	c, err = s.AccessCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	change.Revision = c.Revision
	change.InboundIDs = []int64{f.inbounds[0].ID}
	if err = s.UpdateGroupAccess(ctx, group, change); err != nil {
		t.Fatal(err)
	}
	a, err = s.InboundAccess(ctx, f.inbounds[1].ID)
	if err != nil || !reflect.DeepEqual(a.GroupIDs, []int64{f.groups[1]}) {
		t.Fatalf("removal changed another group: %+v %v", a, err)
	}
}
