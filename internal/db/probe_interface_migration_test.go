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

func TestProbeInterfaceMigrationPreservesLegacyTask(t *testing.T) {
	ctx := context.Background()
	c, err := db.Open("sqlite", filepath.Join(t.TempDir(), "probe.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err = goose.SetDialect("sqlite"); err != nil {
		t.Fatal(err)
	}
	if err = goose.UpToContext(ctx, c, ".", 110); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Exec(`INSERT INTO ping_tasks(id,name,type,target,interval_seconds,node_ids_json,enabled,created_at) VALUES(11,'Existing','tcp','probe.example.com:443',30,'[4]',1,1)`); err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx, c, "sqlite"); err != nil {
		t.Fatal(err)
	}
	tasks, err := store.New(c).ListPingTasks(ctx)
	if err != nil || len(tasks) != 1 || tasks[0].ID != 11 || tasks[0].Target != "probe.example.com:443" || tasks[0].SourceInterface != "" || len(tasks[0].NodeIDs) != 1 || tasks[0].NodeIDs[0] != 4 || !tasks[0].Enabled {
		t.Fatalf("migration changed task %+v %v", tasks, err)
	}
	if wire := tasks[0].AgentTask(); wire.Series != "" || wire.Family != "" || wire.SourceInterface != "" {
		t.Fatal("migration bound legacy task", wire)
	}
}
