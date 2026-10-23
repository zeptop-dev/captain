package db_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/store"
	"github.com/zeptop-dev/captain/migrations"
)

func TestProxyTrafficMigrationPreservesKnownHistory(t *testing.T) {
	ctx := context.Background()
	c, err := db.Open("sqlite", filepath.Join(t.TempDir(), "attribution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpToContext(ctx, c, ".", 83); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	for _, statement := range []string{
		`INSERT INTO nodes(id,name,created_at,updated_at) VALUES(1,'A',1,1),(2,'A2',1,1),(3,'B',1,1)`,
		`INSERT INTO inbounds(id,node_id,tag,protocol,port,created_at,updated_at) VALUES(1,1,'wg-user','vless',443,1,1),(2,2,'reverse-user','vless',443,1,1),(3,2,'reverse-receiver','vless',444,1,1),(4,1,'ordinary','vless',445,1,1),(5,1,'reused-id','vless',446,1,1)`,
		`INSERT INTO entries(id,name,inbound_id,display_host,display_port,created_at,updated_at) VALUES(1,'WG',1,'example.com',443,1,1),(2,'Reverse',2,'example.com',443,1,1)`,
		`INSERT INTO node_wireguard_links(id,exit_id,transit_id,user_inbound_id,entry_id,transit_key,exit_key,endpoint,listen_port,mtu) VALUES('wg',3,1,1,1,'test-key-a','test-key-b','192.0.2.10:51820',51820,1380)`,
		`INSERT INTO node_reverse_links(id,exit_id,transit_id,user_inbound_id,receiver_inbound_id,entry_id) VALUES('reverse',3,2,2,3,2)`,
	} {
		if _, err := c.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	// One ID was deleted and reused during this UTC day. The daily bucket has
	// no event timestamps, so even same-day rows cannot safely be assigned.
	if _, err := c.Exec(`UPDATE inbounds SET created_at=? WHERE id=5`, day.Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct{ inbound, up, down int64 }{{1, 100, 200}, {2, 7, 15}, {4, 1, 10}, {5, 999, 999}, {9999, 888, 888}} {
		if _, err := c.Exec(`INSERT INTO traffic_daily(user_id,inbound_id,day,up_bytes,down_bytes) VALUES(1,?,?,?,?)`, row.inbound, day.Unix(), row.up, row.down); err != nil {
			t.Fatal(err)
		}
	}
	var before int64
	if err := c.QueryRow(`SELECT SUM(up_bytes+down_bytes) FROM traffic_daily`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpToContext(ctx, c, ".", 84); err != nil {
		t.Fatal(err)
	}
	s := store.New(c)
	views, err := s.NodeProxyTrafficViews(ctx, 0, day)
	if err != nil {
		t.Fatal(err)
	}
	if views[1].TodayBytes != 11 || views[1].CollectedTodayBytes != 311 || views[2].TodayBytes != 0 || views[3].TodayBytes != 322 || views[3].ReceivedTodayBytes != 322 {
		t.Fatalf("legacy mapping: A=%+v A2=%+v B=%+v", views[1], views[2], views[3])
	}
	if views[3].HistoryComplete || views[3].AttributionStartedAt == 0 {
		t.Fatalf("migration claimed complete past history: %+v", views[3])
	}
	var after int64
	if err := c.QueryRow(`SELECT SUM(up_bytes+down_bytes) FROM traffic_daily`).Scan(&after); err != nil || after != before {
		t.Fatal("legacy ledger rewritten", before, after, err)
	}
	if _, err := c.Exec(`DELETE FROM node_wireguard_links WHERE id='wg'`); err != nil {
		t.Fatal(err)
	}
	views, err = s.NodeProxyTrafficViews(ctx, 3, day)
	if err != nil || views[3].TodayBytes != 322 {
		t.Fatal("migration history followed deleted topology", views, err)
	}
	if _, err := s.PruneHistory(ctx, day.AddDate(0, 0, 401), 400); err != nil {
		t.Fatal(err)
	}
	views, err = s.NodeProxyTrafficViews(ctx, 3, day)
	if err != nil || views[3].TodayBytes != 0 {
		t.Fatal("attribution history retention", views, err)
	}
}
