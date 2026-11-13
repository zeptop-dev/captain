package db

import (
	"context"
	"github.com/pressly/goose/v3"
	"github.com/zeptop-dev/captain/migrations"
	"path/filepath"
	"testing"
)

func TestOperationsInsightsUpgradePreservesExistingTerms(t *testing.T) {
	c, e := Open("sqlite", filepath.Join(t.TempDir(), "upgrade.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	ctx := context.Background()
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	goose.SetDialect("sqlite")
	if e = goose.UpToContext(ctx, c, ".", 120); e != nil {
		t.Fatal(e)
	}
	if _, e = c.Exec(`INSERT INTO infra_suppliers(id,name,url,contact,notes) VALUES(1,'Example supplier','','','')`); e != nil {
		t.Fatal(e)
	}
	if _, e = c.Exec(`INSERT INTO infra_assets(id,supplier_id,name,currency,amount_minor,period_months,anchor_day,next_due,active) VALUES(1,1,'Example server','USD',1000,3,31,'2027-01-31',1)`); e != nil {
		t.Fatal(e)
	}
	if e = Migrate(ctx, c, "sqlite"); e != nil {
		t.Fatal(e)
	}
	if e = Migrate(ctx, c, "sqlite"); e != nil {
		t.Fatal(e)
	}
	var amount, months, revision, count int
	var due string
	if e = c.QueryRow(`SELECT amount_minor,period_months,next_due,revision FROM infra_assets WHERE id=1`).Scan(&amount, &months, &due, &revision); e != nil {
		t.Fatal(e)
	}
	if amount != 1000 || months != 3 || due != "2027-01-31" || revision != 1 {
		t.Fatal(amount, months, due, revision)
	}
	c.QueryRow(`SELECT COUNT(*) FROM infra_price_history WHERE asset_id=1`).Scan(&count)
	if count != 1 {
		t.Fatal("backfill reran", count)
	}
	if _, e = c.Exec(`UPDATE infra_assets SET notes='metadata only' WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	c.QueryRow(`SELECT COUNT(*) FROM infra_price_history WHERE asset_id=1`).Scan(&count)
	if count != 1 {
		t.Fatal("metadata changed history", count)
	}
	if _, e = c.Exec(`UPDATE infra_assets SET amount_minor=2000 WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	c.QueryRow(`SELECT COUNT(*) FROM infra_price_history WHERE asset_id=1`).Scan(&count)
	if count != 2 {
		t.Fatal(count)
	}
}
