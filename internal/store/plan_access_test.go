package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
)

func TestPlanAccessAtomicAndIndependent(t *testing.T) {
	c, err := db.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx := context.Background()
	if err = db.Migrate(ctx, c, "sqlite"); err != nil {
		t.Fatal(err)
	}
	s := New(c)
	node := &domain.Node{Name: "Example"}
	if err = s.CreateNode(ctx, node, "pair", time.Hour); err != nil {
		t.Fatal(err)
	}
	group, err := s.CreateGroup(ctx, "Shared")
	if err != nil {
		t.Fatal(err)
	}
	ib := &domain.Inbound{NodeID: node.ID, Tag: "test", Protocol: spec.VLESS, Port: 443, Enabled: true, GroupID: &group}
	if err = s.CreateInbound(ctx, ib); err != nil {
		t.Fatal(err)
	}
	old := &domain.Plan{Name: "Old", PeriodDays: 30, QuotaBytes: 100, Enabled: true, GroupID: &group}
	if err = s.CreatePlan(ctx, old); err != nil {
		t.Fatal(err)
	}
	other := &domain.Plan{Name: "Other", PeriodDays: 30, QuotaBytes: 100, Enabled: true, GroupID: &group}
	if err = s.CreatePlan(ctx, other); err != nil {
		t.Fatal(err)
	}
	if err = s.CreateEntry(ctx, &domain.Entry{Name: "Example", InboundID: ib.ID, DisplayHost: "node.example.com", DisplayPort: 443, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	user := &domain.User{Email: "a@example.com", UUID: "a", SubToken: "token-a", Status: "active", Role: domain.RoleUser}
	otherUser := &domain.User{Email: "b@example.com", UUID: "b", SubToken: "token-b", Status: "active", Role: domain.RoleUser}
	if err = s.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	if err = s.CreateUser(ctx, otherUser); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GrantSubscriptionMode(ctx, user.ID, old, time.Now(), GrantStack); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GrantSubscriptionMode(ctx, otherUser.ID, other, time.Now(), GrantStack); err != nil {
		t.Fatal(err)
	}
	external := &ExternalNode{Name: "External", URI: "ss://YWVzLTEyOC1nY206cGFzcw@example.com:443#Example", Enabled: true, GroupID: &group}
	if err = s.SaveExternalNode(ctx, external); err != nil {
		t.Fatal(err)
	}
	catalog, err := s.AccessCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	old.Access = &domain.PlanAccess{Mode: "independent", InboundIDs: []int64{ib.ID}, Revision: catalog.Revision, ConfirmShared: true}
	if err = s.SavePlanAccess(ctx, old, false); err != nil {
		t.Fatal(err)
	}
	if *old.GroupID == group {
		t.Fatal("not detached")
	}
	a, err := s.InboundAccess(ctx, ib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.GroupIDs) != 2 {
		t.Fatal(a)
	}
	catalog, _ = s.AccessCatalog(ctx)
	old.Access.InboundIDs = []int64{}
	old.Access.Revision = catalog.Revision
	if err = s.SavePlanAccess(ctx, old, false); err != nil {
		t.Fatal(err)
	}
	a, _ = s.InboundAccess(ctx, ib.ID)
	if len(a.GroupIDs) != 1 || a.GroupIDs[0] != group {
		t.Fatal("shared plan changed", a)
	}
	lines, err := s.EntriesForUser(ctx, user)
	if err != nil || len(lines) != 0 {
		t.Fatal("independent subscriber retained shared inlet access", lines, err)
	}
	lines, err = s.EntriesForUser(ctx, otherUser)
	if err != nil || len(lines) != 1 {
		t.Fatal("other plan lost access", lines, err)
	}
	grants, err := s.AccessGroups(ctx, user, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := s.ExternalNodesForGroup(ctx, grants)
	if err != nil || len(nodes) != 1 {
		t.Fatal("legacy external grant lost", nodes, err)
	}
	old.Name = "Must roll back"
	old.Access.Revision = "stale"
	if err = s.SavePlanAccess(ctx, old, false); !errors.Is(err, ErrAccessConflict) {
		t.Fatal(err)
	}
	persisted, _ := s.PlanByID(ctx, old.ID)
	if persisted.Name == old.Name {
		t.Fatal("partial plan save")
	}
	catalog, _ = s.AccessCatalog(ctx)
	create := &domain.Plan{Name: "New", PeriodDays: 30, Enabled: true, Access: &domain.PlanAccess{Mode: "independent", InboundIDs: []int64{999999}, Revision: catalog.Revision, ConfirmShared: true}}
	if err = s.SavePlanAccess(ctx, create, true); !errors.Is(err, ErrInvalidAccess) {
		t.Fatal(err)
	}
	var count int
	c.QueryRow(`SELECT count(*) FROM plans WHERE name='New'`).Scan(&count)
	if count != 0 {
		t.Fatal("partial create")
	}
	// Explicit shared schemes are reusable, but an owned backing group cannot be shared.
	catalog, _ = s.AccessCatalog(ctx)
	create.ID = 0
	create.Access = &domain.PlanAccess{Mode: "shared", GroupID: *persisted.GroupID, Revision: catalog.Revision, ConfirmShared: true}
	if err = s.SavePlanAccess(ctx, create, true); !errors.Is(err, ErrInvalidAccess) {
		t.Fatal(err)
	}
	catalog, _ = s.AccessCatalog(ctx)
	create.Access = &domain.PlanAccess{Mode: "shared", Name: "New shared", InboundIDs: []int64{ib.ID}, Revision: catalog.Revision, ConfirmShared: true}
	if err = s.SavePlanAccess(ctx, create, true); err != nil {
		t.Fatal(err)
	}
}
