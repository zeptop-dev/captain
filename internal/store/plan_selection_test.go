package store

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/migrations"
)

func TestPlanSelectionUpgradePreservesAccessAndBilling(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	must(goose.SetDialect("sqlite"))
	must(goose.UpToContext(ctx, conn, ".", 118))
	s := New(conn)
	n := &domain.Node{Name: "Server"}
	must(s.CreateNode(ctx, n, "pair", time.Hour))
	g, err := s.CreateGroup(ctx, "Existing")
	must(err)
	plans := []*domain.Plan{{Name: "No group", PeriodDays: 30, Enabled: true}, {Name: "Shared", PeriodDays: 30, Enabled: true, GroupID: &g}, {Name: "Disabled", PeriodDays: 30, Enabled: false}}
	for _, p := range plans {
		must(s.CreatePlan(ctx, p))
	}
	var inbounds []*domain.Inbound
	for i, policy := range []string{"legacy", "all", "groups", "none", "all"} {
		ib := &domain.Inbound{NodeID: n.ID, Tag: policy + string(rune('a'+i)), Protocol: spec.VLESS, Port: 440 + i, Enabled: i != 4}
		must(s.CreateInbound(ctx, ib))
		inbounds = append(inbounds, ib)
		if policy != "legacy" {
			_, err = conn.Exec(`INSERT INTO inbound_access(inbound_id,policy) VALUES(?,?)`, ib.ID, policy)
			must(err)
		}
		if policy == "groups" {
			_, err = conn.Exec(`INSERT INTO inbound_access_groups VALUES(?,?)`, ib.ID, g)
			must(err)
		}
		must(s.CreateEntry(ctx, &domain.Entry{Name: ib.Tag, InboundID: ib.ID, DisplayHost: "node.example.com", DisplayPort: ib.Port, Enabled: true, Rate: 1}))
	}
	users := []*domain.User{{Email: "a@example.com", UUID: "a", SubToken: "a", Status: "active", Role: domain.RoleUser}, {Email: "b@example.com", UUID: "b", SubToken: "b", Status: "active", Role: domain.RoleUser}}
	for i, u := range users {
		must(s.CreateUser(ctx, u))
		_, err = s.GrantSubscriptionMode(ctx, u.ID, plans[i], time.Now(), GrantStack)
		must(err)
	}
	// The same account holds both plans. Global traffic must keep the same earliest usable subscription.
	_, err = s.GrantSubscriptionMode(ctx, users[0].ID, plans[1], time.Now().Add(time.Hour), GrantStack)
	must(err)
	before := map[int64][]int64{}
	for _, u := range users {
		rows, e := s.EntriesForUser(ctx, u)
		must(e)
		for _, r := range rows {
			before[u.ID] = append(before[u.ID], r.Entry.ID)
		}
	}
	charge := func() int64 {
		tx, e := conn.BeginTx(ctx, nil)
		must(e)
		defer tx.Rollback()
		var groups []int64
		rows, e := tx.Query(`SELECT group_id FROM effective_inbound_access_groups WHERE inbound_id=?`, inbounds[0].ID)
		must(e)
		for rows.Next() {
			var id int64
			must(rows.Scan(&id))
			groups = append(groups, id)
		}
		must(rows.Err())
		rows.Close()
		id, ok := chargeableTx(ctx, tx, users[0].ID, groups, time.Now())
		if !ok {
			t.Fatal("missing charge target")
		}
		return id
	}
	oldCharge := charge()
	must(db.Migrate(ctx, conn, "sqlite"))
	must(db.Migrate(ctx, conn, "sqlite"))
	for _, u := range users {
		rows, e := s.EntriesForUser(ctx, u)
		must(e)
		var ids []int64
		for _, r := range rows {
			ids = append(ids, r.Entry.ID)
		}
		if !reflect.DeepEqual(ids, before[u.ID]) {
			t.Fatal("access changed", u.ID, before[u.ID], ids)
		}
	}
	if charge() != oldCharge {
		t.Fatal("billing target changed")
	}
	c, err := s.AccessCatalog(ctx)
	must(err)
	if c.Conversion.Inbounds != 3 || c.Conversion.Plans != 3 {
		t.Fatal(c.Conversion)
	}
	for i, ib := range inbounds {
		a, e := s.InboundAccess(ctx, ib.ID)
		must(e)
		if i == 3 && a.Policy != domain.AccessNone {
			t.Fatal(a)
		}
		if i == 2 && (len(a.GroupIDs) != 1 || a.GroupIDs[0] != g) {
			t.Fatal("restricted grant changed", a)
		}
		if i == 4 {
			var enabled bool
			must(conn.QueryRow(`SELECT enabled FROM inbounds WHERE id=?`, ib.ID).Scan(&enabled))
			if enabled {
				t.Fatal("disabled inbound enabled")
			}
		}
	}
	newPlan := &domain.Plan{Name: "Future", PeriodDays: 30, Enabled: true, Access: &domain.PlanAccess{Mode: "independent", InboundIDs: []int64{}, CategoryIDs: []int64{}, Revision: c.Revision, ConfirmShared: true}}
	must(s.SavePlanAccess(ctx, newPlan, true))
	a, err := s.InboundAccess(ctx, inbounds[0].ID)
	must(err)
	for _, id := range a.GroupIDs {
		if id == *newPlan.GroupID {
			t.Fatal("future plan inherited old global grant")
		}
	}
	must(db.Migrate(ctx, conn, "sqlite"))
	a, err = s.InboundAccess(ctx, inbounds[0].ID)
	must(err)
	if len(a.GroupIDs) != 3 {
		t.Fatal("migration reran or lost disabled plan", a)
	}
}

func TestPlanCategoriesDynamicAndSnapshotStaySeparate(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(db.Migrate(ctx, conn, "sqlite"))
	s := New(conn)
	n := &domain.Node{Name: "Server"}
	must(s.CreateNode(ctx, n, "pair", time.Hour))
	cat := &domain.EntryCategory{Name: "Premium"}
	must(s.CreateEntryCategory(ctx, cat))
	ib := &domain.Inbound{NodeID: n.ID, Tag: "one", Protocol: spec.VLESS, Port: 443, Enabled: true}
	must(s.CreateInbound(WithPlanAccessCreation(ctx), ib))
	e := &domain.Entry{Name: "One", InboundID: ib.ID, DisplayHost: "node.example.com", DisplayPort: 443, Enabled: true, CategoryID: &cat.ID}
	must(s.CreateEntry(ctx, e))
	catalog, err := s.AccessCatalog(ctx)
	must(err)
	p := &domain.Plan{Name: "Follow", PeriodDays: 30, Enabled: true, Access: &domain.PlanAccess{Mode: "independent", InboundIDs: []int64{}, CategoryIDs: []int64{cat.ID}, Revision: catalog.Revision, ConfirmShared: true}}
	must(s.SavePlanAccess(ctx, p, true))
	user := &domain.User{Email: "follow@example.com", UUID: "follow", SubToken: "follow", Status: "active", Role: domain.RoleUser}
	must(s.CreateUser(ctx, user))
	_, err = s.GrantSubscriptionMode(ctx, user.ID, p, time.Now(), GrantStack)
	must(err)
	credentials, err := s.UsersWithInboundAccess(ctx, ib.ID, time.Now())
	must(err)
	if len(credentials) != 1 || credentials[0].ID != user.ID {
		t.Fatal("category missing in agent authorization", credentials)
	}
	catalogue, err := s.AccessCatalog(ctx)
	must(err)
	if err = s.UpdateGroupAccess(ctx, *p.GroupID, GroupAccessChange{InboundIDs: []int64{ib.ID}, Revision: catalogue.Revision, ConfirmShared: true}); !errors.Is(err, ErrAccessCategoryManaged) {
		t.Fatal("legacy writer pinned category grant", err)
	}
	// Returning to the same visible category state still invalidates old drafts.
	must(s.AssignEntryCategory(ctx, []int64{e.ID}, 0))
	must(s.AssignEntryCategory(ctx, []int64{e.ID}, cat.ID))
	p.Access.Revision = catalogue.Revision
	if err = s.SavePlanAccess(ctx, p, false); !errors.Is(err, ErrAccessConflict) {
		t.Fatal("ABA draft accepted", err)
	}

	a, err := s.InboundAccess(ctx, ib.ID)
	must(err)
	if len(a.GroupIDs) != 1 || a.GroupIDs[0] != *p.GroupID {
		t.Fatal("dynamic grant missing", a)
	}
	must(s.AssignEntryCategory(ctx, []int64{e.ID}, 0))
	a, err = s.InboundAccess(ctx, ib.ID)
	must(err)
	if a.Policy != domain.AccessNone {
		t.Fatal("moving out retained dynamic grant", a)
	}
	if err = s.AssignEntryCategory(WithCategoryAccessWriter(ctx, false), []int64{e.ID}, cat.ID); !errors.Is(err, ErrCategoryAccessPermission) {
		t.Fatal("category scope bypass", err)
	}
	must(s.AssignEntryCategory(ctx, []int64{e.ID}, cat.ID))
	// Another plan's edit must not materialize Follow's category grant into static membership.
	catalog, err = s.AccessCatalog(ctx)
	must(err)
	other := &domain.Plan{Name: "Snapshot", PeriodDays: 30, Enabled: true, Access: &domain.PlanAccess{Mode: "independent", InboundIDs: []int64{ib.ID}, CategoryIDs: []int64{}, Revision: catalog.Revision, ConfirmShared: true}}
	must(s.SavePlanAccess(ctx, other, true))
	must(s.AssignEntryCategory(ctx, []int64{e.ID}, 0))
	a, err = s.InboundAccess(ctx, ib.ID)
	must(err)
	if len(a.GroupIDs) != 1 || a.GroupIDs[0] != *other.GroupID {
		t.Fatal("dynamic grant accidentally persisted", a)
	}
	// Future entries become available only to the followed scheme.
	ib2 := &domain.Inbound{NodeID: n.ID, Tag: "two", Protocol: spec.VLESS, Port: 444, Enabled: true}
	must(s.CreateInbound(WithPlanAccessCreation(ctx), ib2))
	e2 := &domain.Entry{Name: "Two", InboundID: ib2.ID, DisplayHost: "node.example.com", DisplayPort: 444, Enabled: true, CategoryID: &cat.ID}
	must(s.CreateEntry(ctx, e2))
	a, err = s.InboundAccess(ctx, ib2.ID)
	must(err)
	if len(a.GroupIDs) != 1 || a.GroupIDs[0] != *p.GroupID {
		t.Fatal("future classification did not grant", a)
	}
	// A reverse receiver cannot gain user credentials through a followed category.
	_, err = conn.Exec(`UPDATE inbounds SET settings_json='{"reverse":{"receiver":true}}' WHERE id=?`, ib2.ID)
	must(err)
	receiver, err := s.InboundAccess(ctx, ib2.ID)
	must(err)
	if receiver.Policy != domain.AccessNone {
		t.Fatal("receiver exposed through category", receiver)
	}
	credentials, err = s.UsersWithInboundAccess(ctx, ib2.ID, time.Now())
	must(err)
	if len(credentials) != 0 {
		t.Fatal("receiver credentials exposed", credentials)
	}
	_, err = conn.Exec(`UPDATE inbounds SET settings_json='{}' WHERE id=?`, ib2.ID)
	must(err)
	if err = s.DeleteEntryCategory(WithCategoryAccessWriter(ctx, false), cat.ID); !errors.Is(err, ErrCategoryAccessPermission) {
		t.Fatal(err)
	}
	// Stale drafts and invalid categories roll back metadata and rules together.
	catalog, err = s.AccessCatalog(ctx)
	must(err)
	p.Access.Revision = catalog.Revision
	p.Access.CategoryIDs = []int64{999999}
	p.Name = "Must roll back"
	if err = s.SavePlanAccess(ctx, p, false); !errors.Is(err, ErrInvalidAccess) {
		t.Fatal(err)
	}
	saved, err := s.PlanByID(ctx, p.ID)
	must(err)
	if saved.Name == p.Name {
		t.Fatal("partial plan write")
	}
	must(s.DeleteEntryCategory(ctx, cat.ID))
	a, err = s.InboundAccess(ctx, ib2.ID)
	must(err)
	if a.Policy != domain.AccessNone {
		t.Fatal("deleted category retained grant", a)
	}
	a, err = s.InboundAccess(ctx, ib.ID)
	must(err)
	if len(a.GroupIDs) != 1 || a.GroupIDs[0] != *other.GroupID {
		t.Fatal("deletion lost static snapshot", a)
	}
}

func TestPlanCategorySharedSchemeAndLegacyOmission(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "shared.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(db.Migrate(ctx, conn, "sqlite"))
	s := New(conn)
	node := &domain.Node{Name: "Server"}
	must(s.CreateNode(ctx, node, "pair", time.Hour))
	category := &domain.EntryCategory{Name: "Premium"}
	must(s.CreateEntryCategory(ctx, category))
	inbound := &domain.Inbound{NodeID: node.ID, Tag: "shared", Protocol: spec.VLESS, Port: 443, Enabled: true}
	must(s.CreateInbound(WithPlanAccessCreation(ctx), inbound))
	entry := &domain.Entry{Name: "Shared node", InboundID: inbound.ID, DisplayHost: "node.example.com", DisplayPort: 443, CategoryID: &category.ID, Enabled: true}
	must(s.CreateEntry(ctx, entry))
	catalogue, err := s.AccessCatalog(ctx)
	must(err)
	p := &domain.Plan{Name: "Monthly", PeriodDays: 30, Enabled: true, Access: &domain.PlanAccess{Mode: "shared", Name: "Premium scheme", InboundIDs: []int64{}, CategoryIDs: []int64{category.ID}, Revision: catalogue.Revision, ConfirmShared: true}}
	must(s.SavePlanAccess(ctx, p, true))
	catalogue, err = s.AccessCatalog(ctx)
	must(err)
	q := &domain.Plan{Name: "Annual", PeriodDays: 365, Enabled: true, Access: &domain.PlanAccess{Mode: "shared", GroupID: *p.GroupID, InboundIDs: []int64{}, Revision: catalogue.Revision, ConfirmShared: true}}
	must(s.SavePlanAccess(ctx, q, true))
	users := []*domain.User{{Email: "monthly@example.com", UUID: "m", SubToken: "m", Role: domain.RoleUser, Status: "active"}, {Email: "annual@example.com", UUID: "y", SubToken: "y", Role: domain.RoleUser, Status: "active"}}
	for i, user := range users {
		must(s.CreateUser(ctx, user))
		_, err = s.GrantSubscriptionMode(ctx, user.ID, []*domain.Plan{p, q}[i], time.Now(), GrantStack)
		must(err)
	}
	members, err := s.UsersWithInboundAccess(ctx, inbound.ID, time.Now())
	must(err)
	if len(members) != 2 {
		t.Fatal("shared category unavailable", members)
	}
	// An old client switching to independent without category_ids copies existing rules.
	catalogue, err = s.AccessCatalog(ctx)
	must(err)
	p.Access = &domain.PlanAccess{Mode: "independent", InboundIDs: []int64{}, Revision: catalogue.Revision, ConfirmShared: true}
	must(s.SavePlanAccess(ctx, p, false))
	if *p.GroupID == *q.GroupID {
		t.Fatal("not detached")
	}
	members, err = s.UsersWithInboundAccess(ctx, inbound.ID, time.Now())
	must(err)
	if len(members) != 2 {
		t.Fatal("omitted rules lost during detach", members)
	}
	catalogue, err = s.AccessCatalog(ctx)
	must(err)
	p.Access.Revision = catalogue.Revision
	p.Access.CategoryIDs = []int64{}
	must(s.SavePlanAccess(ctx, p, false))
	members, err = s.UsersWithInboundAccess(ctx, inbound.ID, time.Now())
	must(err)
	if len(members) != 1 || members[0].ID != users[1].ID {
		t.Fatal("independent changes affected shared plan", members)
	}
	catalogue, err = s.AccessCatalog(ctx)
	must(err)
	q.Access.Revision = catalogue.Revision
	q.Access.CategoryIDs = []int64{}
	must(s.SavePlanAccess(ctx, q, false))
	members, err = s.UsersWithInboundAccess(ctx, inbound.ID, time.Now())
	must(err)
	if len(members) != 0 {
		t.Fatal("explicit clear retained access", members)
	}
}
