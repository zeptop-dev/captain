package db

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/zeptop-dev/captain/migrations"
)

func TestIngressAddressMigration117PreservesExplicitEndpoints(t *testing.T) {
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
	must(goose.UpToContext(ctx, conn, ".", 116))
	_, err = conn.Exec(`INSERT INTO nodes(id,name,created_at,updated_at)VALUES(1,'NAT',1,1); INSERT INTO ingresses(id,node_id,name,kind,entry_host,created_at,updated_at)VALUES(1,1,'NAT','nat','203.0.113.30',1,1); INSERT INTO inbounds(id,node_id,tag,protocol,port,ingress_id,created_at,updated_at)VALUES(1,1,'in','vless',443,1,1,1); INSERT INTO entries(id,name,inbound_id,display_host,display_port,created_at,updated_at)VALUES(1,'Legacy',1,'203.0.113.30',30443,1,1)`)
	must(err)
	must(Migrate(ctx, conn, "sqlite"))
	must(Migrate(ctx, conn, "sqlite"))
	var host, source string
	var pending, follow bool
	must(conn.QueryRow(`SELECT entry_host,entry_address_source,address_dns_pending FROM ingresses WHERE id=1`).Scan(&host, &source, &pending))
	if host != "203.0.113.30" || source != "custom" || pending {
		t.Fatal(host, source, pending)
	}
	must(conn.QueryRow(`SELECT display_host,follow_ingress FROM entries WHERE id=1`).Scan(&host, &follow))
	if host != "203.0.113.30" || follow {
		t.Fatal(host, follow)
	}
	if _, err = conn.Exec(`UPDATE ingresses SET entry_address_source='observed'`); err == nil {
		t.Fatal("invalid source accepted")
	}
}
