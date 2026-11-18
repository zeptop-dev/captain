package db_test

import (
	"context"
	"github.com/pressly/goose/v3"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/store"
	"github.com/zeptop-dev/captain/migrations"
	"path/filepath"
	"testing"
)

func TestWireGuardBindingsMigrationPreservesOwnershipAndSlots(t *testing.T) {
	for _, empty := range []bool{false, true} {
		t.Run(map[bool]string{false: "populated", true: "deleted-high-water"}[empty], func(t *testing.T) {
			ctx := context.Background()
			conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "wg.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			goose.SetBaseFS(migrations.FS)
			goose.SetLogger(goose.NopLogger())
			if err = goose.SetDialect("sqlite"); err != nil {
				t.Fatal(err)
			}
			if err = goose.UpToContext(ctx, conn, ".", 125); err != nil {
				t.Fatal(err)
			}
			must := func(q string, args ...any) {
				t.Helper()
				if _, e := conn.Exec(q, args...); e != nil {
					t.Fatal(e)
				}
			}
			must(`INSERT INTO nodes(id,name,created_at,updated_at) VALUES(1,'transit',1,1),(2,'exit',1,1)`)
			must(`INSERT INTO inbounds(id,node_id,tag,protocol,port,core,settings_json,enabled,created_at,updated_at) VALUES(1,1,'wg-entry','vless',18443,'xray','{"wireguard_slot":1}',1,1,1)`)
			must(`INSERT INTO entries(id,name,inbound_id,display_host,display_port,created_at,updated_at) VALUES(1,'legacy',1,'example.com',18443,1,1)`)
			key := "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="
			must(`INSERT INTO node_wireguard_links(slot,id,exit_id,transit_id,user_inbound_id,entry_id,transit_key,exit_key,endpoint,listen_port,mtu) VALUES(1,'legacy',2,1,1,1,?,?,'198.51.100.20:51820',51820,1380)`, key, key)
			if empty {
				must("DELETE FROM node_wireguard_links")
			}
			must("UPDATE sqlite_sequence SET seq=100 WHERE name='node_wireguard_links'")
			if err = db.Migrate(ctx, conn, "sqlite"); err != nil {
				t.Fatal(err)
			}
			s := store.New(conn)
			links, err := s.WireGuardLinks(ctx, 1)
			if err != nil {
				t.Fatal(err)
			}
			if !empty {
				if len(links) != 1 || links[0].Name != "legacy" || links[0].TransitKey != key || links[0].EntryID != 1 || links[0].UserInboundID != 1 || !links[0].Enabled {
					t.Fatal("legacy identities changed")
				}
			}
			link := store.WireGuardLink{ID: "new", Name: "new", TransitID: 1, ExitID: 2, Endpoint: "198.51.100.20:51821", ListenPort: 51821, MTU: 1380}
			if err = s.SaveWireGuardTransport(ctx, &link); err != nil {
				t.Fatal(err)
			}
			if link.Slot <= 100 {
				t.Fatal("deleted transport identity reused")
			}
			var violations int
			rows, e := conn.Query("PRAGMA foreign_key_check")
			if e != nil {
				t.Fatal(e)
			}
			for rows.Next() {
				violations++
			}
			rows.Close()
			if violations != 0 {
				t.Fatal("migration broke foreign keys")
			}
		})
	}
}
