package db_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
	"github.com/zeptop-dev/captain/migrations"
)

func TestAddressFallbackMigrationPreservesDefaultsAndReset(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "fallback.db"))
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
	must(goose.UpToContext(ctx, conn, ".", 113))
	_, err = conn.Exec(`INSERT INTO nodes(id,name,token_hash,public_addr,address_auto_v4,address_json,created_at,updated_at) VALUES(7,'Legacy','identity','192.0.2.10',1,'{"ipv4":{"address":"192.0.2.10","confirmed_at":42}}',1,1)`)
	must(err)
	must(db.Migrate(ctx, conn, "sqlite"))
	must(db.Migrate(ctx, conn, "sqlite"))
	s := store.New(conn)
	p, err := s.NodeAddresses(ctx, 7)
	must(err)
	if p.FallbackAutoIPv4 || p.FallbackAutoIPv6 || !p.AutoIPv4 || p.EntryIPv4 != "192.0.2.10" || p.Observed.IPv4.ConfirmedAt != 42 {
		t.Fatal("legacy defaults changed", p)
	}
	yes := true
	must(s.SetAddressFollowPolicy(ctx, 7, true, false, &yes, nil))
	p, err = s.NodeAddresses(ctx, 7)
	must(err)
	if !p.FallbackAutoIPv4 {
		t.Fatal("not stored")
	}
	admin := &domain.User{Email: "admin@example.com", PasswordHash: "test", Role: "admin", Status: "active"}
	must(s.CreateStaff(ctx, admin))
	_, err = s.ResetSite(ctx, admin.ID)
	must(err)
	n := &domain.Node{Name: "replacement"}
	must(s.CreateNode(ctx, n, "pair", time.Hour))
	p, err = s.NodeAddresses(ctx, n.ID)
	must(err)
	if p.FallbackAutoIPv4 || p.FallbackAutoIPv6 || p.Observed.FallbackFollowIPv4.CandidateSince != 0 {
		t.Fatal("replacement inherited policy", p)
	}
}
