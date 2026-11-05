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

func TestNodeProbePlanMigrationDefaultAndReset(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "probe.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err = goose.SetDialect("sqlite"); err != nil {
		t.Fatal(err)
	}
	if err = goose.UpToContext(ctx, conn, ".", 112); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(`INSERT INTO nodes(id,name,token_hash,public_addr,created_at,updated_at) VALUES(7,'Legacy','identity','192.0.2.10',1,1)`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = db.Migrate(ctx, conn, "sqlite"); err != nil {
			t.Fatal(err)
		}
	}
	s := store.New(conn)
	p, err := s.NodeProbePlan(ctx, 7)
	if err != nil || p.Mode != "inherit" || p.Custom != nil || p.Revision == "" || p.ChangedAt != 0 {
		t.Fatal(p, err)
	}
	n, err := s.NodeByID(ctx, 7)
	if err != nil || !n.Paired || n.PublicAddr != "192.0.2.10" {
		t.Fatal(n, err)
	}
	admin := &domain.User{Email: "admin@example.com", PasswordHash: "test", Role: "admin", Status: "active"}
	if err = s.CreateStaff(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResetSite(ctx, admin.ID); err != nil {
		t.Fatal(err)
	}
	plans, err := s.ListNodeProbePlans(ctx)
	if err != nil || len(plans) != 0 {
		t.Fatal("reset retained plans", plans, err)
	}
	if _, err = conn.Exec(`INSERT INTO nodes(id,name,created_at,updated_at) VALUES(7,'Replacement',1,1)`); err != nil {
		t.Fatal(err)
	}
	after, err := s.NodeProbePlan(ctx, 7)
	if err != nil || after.Mode != "inherit" || after.Revision == p.Revision {
		t.Fatal(after, err)
	}
}
