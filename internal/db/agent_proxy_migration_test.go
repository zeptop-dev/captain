package db

import (
	"context"
	"github.com/pressly/goose/v3"
	"github.com/zeptop-dev/captain/migrations"
	"path/filepath"
	"testing"
)

func TestAgentProxyMigrationRetainsDirectPolicy(t *testing.T) {
	ctx := context.Background()
	c, e := Open("sqlite", filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	goose.SetDialect("sqlite3")
	if e = goose.UpToContext(ctx, c, ".", 122); e != nil {
		t.Fatal(e)
	}
	for _, q := range []string{`INSERT INTO nodes(id,name,token_hash,created_at,updated_at)VALUES(1,'old','identity',1,1)`, `INSERT INTO agent_node_connections(node_id,token_hash,current_url,capable)VALUES(1,'identity','https://old.example.com',1)`, `INSERT INTO agent_connection_migrations(id,batch_id,node_id,token_hash,source_url,target_url,proof_key,status,created_at,updated_at)VALUES('existing-attempt','batch',1,'identity','https://old.example.com','https://new.example.com','proof','queued',1,1)`} {
		if _, e = c.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	if e = Migrate(ctx, c, "sqlite"); e != nil {
		t.Fatal(e)
	}
	var mode, url, identity string
	var proxy, capable bool
	if e = c.QueryRow(`SELECT verification_mode FROM agent_connection_migrations WHERE id='existing-attempt'`).Scan(&mode); e != nil || mode != "direct" {
		t.Fatal(mode, e)
	}
	if e = c.QueryRow(`SELECT current_url,token_hash,capable,proxy_capable FROM agent_node_connections WHERE node_id=1`).Scan(&url, &identity, &capable, &proxy); e != nil || url != "https://old.example.com" || identity != "identity" || !capable || proxy {
		t.Fatal(url, identity, capable, proxy, e)
	}
}
