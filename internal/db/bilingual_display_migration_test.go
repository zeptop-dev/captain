package db_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
	"github.com/zeptop-dev/captain/migrations"
)

func TestBilingualDisplayMigrationPreservesLegacyIdentities(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "bilingual.db")
	conn, err := db.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err = goose.SetDialect("sqlite"); err != nil {
		t.Fatal(err)
	}
	if err = goose.UpToContext(ctx, conn, ".", 111); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO nodes(id,name,monitor_group,display_order,created_at,updated_at) VALUES(7,'Legacy server','原分组',30,0,0)`,
		`INSERT INTO node_display_groups(name,position) VALUES('原分组',20)`,
		`INSERT INTO entry_categories(id,name,name_key,description,sort) VALUES(9,'Legacy category','legacy category','Existing description',40)`,
		`INSERT INTO inbounds(id,node_id,tag,protocol,port,created_at,updated_at) VALUES(5,7,'legacy','vless',443,0,0)`,
		`INSERT INTO entries(id,name,inbound_id,display_host,display_port,category_id,sort,created_at,updated_at) VALUES(11,'Existing entry',5,'example.com',443,9,70,0,0)`,
	} {
		if _, err = conn.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err = db.Migrate(ctx, conn, "sqlite"); err != nil {
			t.Fatal(err)
		}
	}
	conn.Close()
	conn, err = db.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	s := store.New(conn)
	layout, err := s.NodeLayout(ctx)
	if err != nil || len(layout.Groups) != 1 || layout.Groups[0] != "原分组" || len(layout.GroupNames) != 0 || layout.Nodes[0].ID != 7 || layout.Nodes[0].Group != "原分组" {
		t.Fatal(layout, err)
	}
	c, err := s.EntryCategoryByID(ctx, 9)
	if err != nil || c.Name != "Legacy category" || c.Description != "Existing description" || c.Sort != 40 || c.NameZH != "" || c.NameEN != "" {
		t.Fatal(c, err)
	}
	e, err := s.EntryByID(ctx, 11)
	if err != nil || e.CategoryID == nil || *e.CategoryID != 9 || e.Sort != 70 || e.InboundID != 5 {
		t.Fatal(e, err)
	}
	layout.GroupNames = map[string]store.DisplayGroupName{"原分组": {ZH: "原分组", EN: "Legacy group"}}
	if err = s.SaveNodeLayout(ctx, layout); err != nil {
		t.Fatal(err)
	}
	c.NameZH, c.NameEN = "旧分类", "Legacy category"
	if err = s.UpdateEntryCategory(ctx, c); err != nil {
		t.Fatal(err)
	}
	admin := &domain.User{Email: "admin@example.com", PasswordHash: "test-hash", Role: "admin", Status: "active"}
	if err = s.CreateStaff(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResetSite(ctx, admin.ID); err != nil {
		t.Fatal(err)
	}
	layout, err = s.NodeLayout(ctx)
	if err != nil || len(layout.Groups) != 0 || len(layout.GroupNames) != 0 {
		t.Fatal("reset retained groups", layout, err)
	}
	categories, err := s.ListEntryCategories(ctx)
	if err != nil || len(categories) != 0 {
		t.Fatal("reset retained categories", categories, err)
	}
}
