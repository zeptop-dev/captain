package store

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/migrations"
)

func TestPlanNameUniquenessAndOwnedRename(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "names.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	must(db.Migrate(ctx, conn, "sqlite"))
	s := New(conn)
	cat, err := s.AccessCatalog(ctx)
	must(err)
	p := &domain.Plan{Name: "  Premium  ", Enabled: true, Access: &domain.PlanAccess{Mode: "independent", InboundIDs: []int64{}, Revision: cat.Revision, ConfirmShared: true}}
	must(s.SavePlanAccess(ctx, p, true))
	if p.Name != "Premium" {
		t.Fatal(p.Name)
	}
	group := *p.GroupID
	var name string
	must(conn.QueryRow(`SELECT name FROM user_groups WHERE id=?`, group).Scan(&name))
	if name != "Premium" {
		t.Fatal(name)
	}
	for _, n := range []string{"Premium", "premium", " PREMIUM "} {
		if e := s.CreatePlan(ctx, &domain.Plan{Name: n}); !errors.Is(e, ErrPlanNameConflict) {
			t.Fatal(n, e)
		}
		cat, err = s.AccessCatalog(ctx)
		must(err)
		if e := s.SavePlanAccess(ctx, &domain.Plan{Name: n, Access: &domain.PlanAccess{Mode: "independent", InboundIDs: []int64{}, Revision: cat.Revision, ConfirmShared: true}}, true); !errors.Is(e, ErrPlanNameConflict) {
			t.Fatal(n, e)
		}
	}
	disabled := &domain.Plan{Name: "Disabled", Enabled: false}
	must(s.CreatePlan(ctx, disabled))
	if e := s.CreatePlan(ctx, &domain.Plan{Name: "disabled"}); !errors.Is(e, ErrPlanNameConflict) {
		t.Fatal(e)
	}
	p.Name = "Renamed"
	must(s.UpdatePlan(ctx, p))
	must(conn.QueryRow(`SELECT name FROM user_groups WHERE id=?`, group).Scan(&name))
	if name != "Renamed" || *p.GroupID != group {
		t.Fatal(name, p.GroupID)
	}
	oldPrice := p.PriceCents
	p.Name = "Disabled"
	p.PriceCents = 999
	if e := s.UpdatePlan(ctx, p); !errors.Is(e, ErrPlanNameConflict) {
		t.Fatal(e)
	}
	saved, err := s.PlanByID(ctx, p.ID)
	must(err)
	if saved.Name != "Renamed" || saved.PriceCents != oldPrice {
		t.Fatal("partial edit", saved)
	}
	_, err = s.CreateGroup(ctx, "Occupied")
	must(err)
	saved.Name = "Occupied"
	if e := s.UpdatePlan(ctx, saved); !errors.Is(e, ErrPlanGroupNameConflict) {
		t.Fatal(e)
	}
	saved, err = s.PlanByID(ctx, p.ID)
	must(err)
	if saved.Name != "Renamed" {
		t.Fatal("permission conflict did not roll back")
	}
	cat, err = s.AccessCatalog(ctx)
	must(err)
	saved.Access = &domain.PlanAccess{Mode: "independent", InboundIDs: []int64{}, Revision: cat.Revision, ConfirmShared: true}
	saved.Name = "Again"
	must(s.SavePlanAccess(ctx, saved, false))
	must(conn.QueryRow(`SELECT name FROM user_groups WHERE id=?`, group).Scan(&name))
	if name != "Again" {
		t.Fatal(name)
	}
	for _, n := range []string{" ", "bad\nname"} {
		if e := s.CreatePlan(ctx, &domain.Plan{Name: n}); !errors.Is(e, ErrPlanNameInvalid) {
			t.Fatal(e)
		}
	}
	unicodePlan := &domain.Plan{Name: "Élite"}
	must(s.CreatePlan(ctx, unicodePlan))
	if e := s.CreatePlan(ctx, &domain.Plan{Name: "élite"}); !errors.Is(e, ErrPlanNameConflict) {
		t.Fatal(e)
	}
	// Requests serialize name checks and writes; only one same-name writer succeeds.
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; results <- s.CreatePlan(ctx, &domain.Plan{Name: "Concurrent"}) }()
	}
	close(start)
	wg.Wait()
	close(results)
	ok, conflicts := 0, 0
	for e := range results {
		if e == nil {
			ok++
		} else if errors.Is(e, ErrPlanNameConflict) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if ok != 1 || conflicts != 1 {
		t.Fatal(ok, conflicts)
	}
}

func TestPlanNameMigrationPreservesAmbiguousHistory(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	must(goose.SetDialect("sqlite"))
	must(goose.UpToContext(ctx, conn, ".", 119))
	_, err = conn.Exec(`INSERT INTO user_groups(id,name) VALUES(1,'Alpha · RANDOM'),(2,'Occupied'),(3,'Old name'),(4,'Dup old');
 INSERT INTO plans(id,name,price_cents,period_days,quota_bytes,group_id,enabled,created_at,updated_at) VALUES(1,'Alpha',0,30,0,1,1,1,1),(2,'Occupied',0,30,0,3,1,1,1),(3,'Duplicate',0,30,0,4,1,1,1),(4,' duplicate ',0,30,0,NULL,0,1,1);
 INSERT INTO plan_access_ownership(plan_id,group_id,external_group_id) VALUES(1,1,NULL),(2,3,2),(3,4,NULL);`)
	must(err)
	must(db.Migrate(ctx, conn, "sqlite"))
	must(db.Migrate(ctx, conn, "sqlite"))
	for id, want := range map[int]string{1: "Alpha", 2: "Occupied", 3: "Old name", 4: "Dup old"} {
		var got string
		must(conn.QueryRow(`SELECT name FROM user_groups WHERE id=?`, id).Scan(&got))
		if got != want {
			t.Fatal(id, got, want)
		}
	}
	s := New(conn)
	legacy, err := s.PlanByID(ctx, 3)
	must(err)
	legacy.PriceCents = 100
	must(s.UpdatePlan(ctx, legacy))
	var groupName string
	must(conn.QueryRow(`SELECT name FROM user_groups WHERE id=4`).Scan(&groupName))
	if groupName != "Duplicate" {
		t.Fatal(groupName)
	}
	// Existing name collisions retain IDs and access. No auto merge or new duplicate.
	if e := s.CreatePlan(ctx, &domain.Plan{Name: "Duplicate"}); !errors.Is(e, ErrPlanNameConflict) {
		t.Fatal(e)
	}
	occupied, err := s.PlanByID(ctx, 2)
	must(err)
	occupied.PriceCents = 200
	must(s.UpdatePlan(ctx, occupied))
	must(conn.QueryRow(`SELECT name FROM user_groups WHERE id=3`).Scan(&groupName))
	if groupName != "Old name" {
		t.Fatal("shared name stolen", groupName)
	}
	legacy.Name = "Unique"
	must(s.UpdatePlan(ctx, legacy))
	must(conn.QueryRow(`SELECT name FROM user_groups WHERE id=4`).Scan(&groupName))
	if groupName != "Unique" {
		t.Fatal(groupName)
	}
	var n int
	must(conn.QueryRow(`SELECT count(*) FROM plans`).Scan(&n))
	if n != 4 {
		t.Fatal("history merged")
	}
	if _, e := conn.Exec(`UPDATE plans SET name='Occupied' WHERE id=1`); e == nil {
		t.Fatal("SQL writer bypassed duplicate protection")
	}
	if _, e := conn.Exec(`INSERT INTO plans(name,price_cents,period_days,quota_bytes,created_at,updated_at) VALUES('occupied',0,30,0,1,1)`); e == nil {
		t.Fatal("SQL insert bypass")
	}
	if v, err := conn.Query(`PRAGMA foreign_key_check`); err != nil {
		t.Fatal(err)
	} else {
		defer v.Close()
		if v.Next() {
			t.Fatal("broken foreign key")
		}
	}
}
