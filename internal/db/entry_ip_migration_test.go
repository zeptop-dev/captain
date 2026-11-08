package db

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/zeptop-dev/captain/migrations"
)

func TestEntryIPMigration116PreservesLegacyEntries(t *testing.T) {
	ctx := context.Background()
	conn, err := Open("sqlite", filepath.Join(t.TempDir(), "legacy.db"))
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
	must(goose.UpToContext(ctx, conn, ".", 115))
	_, err = conn.Exec(`INSERT INTO nodes(id,name,created_at,updated_at)VALUES(1,'Node',1,1); INSERT INTO inbounds(id,node_id,tag,protocol,port,created_at,updated_at)VALUES(1,1,'in','vless',443,1,1); INSERT INTO entries(id,name,inbound_id,display_host,display_port,rate,client_extra,created_at,updated_at)VALUES(1,'Legacy',1,'node.example.com',443,2,'{"ip-version":"ipv6"}',1,1)`)
	must(err)
	must(Migrate(ctx, conn, "sqlite"))
	must(Migrate(ctx, conn, "sqlite"))
	var name, host, extra, mode string
	var rate float64
	must(conn.QueryRow(`SELECT name,display_host,rate,client_extra,ip_version FROM entries WHERE id=1`).Scan(&name, &host, &rate, &extra, &mode))
	if name != "Legacy" || host != "node.example.com" || rate != 2 || extra != `{"ip-version":"ipv6"}` || mode != "" {
		t.Fatal("legacy entry changed", name, host, rate, extra, mode)
	}
	if _, err = conn.Exec(`UPDATE entries SET ip_version='unsupported' WHERE id=1`); err == nil {
		t.Fatal("invalid enum persisted")
	}
}
