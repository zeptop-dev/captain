package db_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/store"
	"github.com/zeptop-dev/captain/migrations"
)

func TestAgentConnectionMigrationPreservesNodeIdentity(t *testing.T) {
	ctx := context.Background()
	c, err := db.Open("sqlite", filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err = goose.SetDialect("sqlite"); err != nil {
		t.Fatal(err)
	}
	if err = goose.UpToContext(ctx, c, ".", 109); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Exec(`INSERT INTO nodes (id,name,token_hash,public_addr,created_at,updated_at) VALUES (1,'Existing','existing-identity','198.51.100.20',1,1)`); err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx, c, "sqlite"); err != nil {
		t.Fatal(err)
	}
	n, err := store.New(c).NodeByTokenHash(ctx, "existing-identity")
	if err != nil || n.ID != 1 || n.PublicAddr != "198.51.100.20" || !n.Paired || n.AgentURL != "" || n.DownloadSource != "github" {
		t.Fatalf("migration changed existing node: %+v %v", n, err)
	}
}
