package db

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/zeptop-dev/captain/migrations"
)

func TestLocationIPMigration115PreservesDataAndSelectionRevision(t *testing.T) {
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
	must(goose.UpToContext(ctx, conn, ".", 114))
	info := `{"region":"JP","provider":"example"}`
	obs := `{"ipv4":{"address":"198.51.100.20","candidate_since":100,"last_success":110},"fallback_follow_ipv4":{"address":"203.0.113.30","run_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","candidate_since":100,"confirmed_at":110,"last_success":110}}`
	_, err = conn.Exec(`INSERT INTO nodes(name,token_hash,created_at,updated_at,probe_info_json,region_mode,address_json,public_addr,address_auto_v4,address_fallback_auto_v4) VALUES('example','token',1,1,?,'manual',?,'192.0.2.10',1,1)`, info, obs)
	must(err)
	must(Migrate(ctx, conn, "sqlite"))
	must(Migrate(ctx, conn, "sqlite"))
	var gotInfo, gotObs, settings, token, entry, mode string
	var revision, auto, fallback int
	must(conn.QueryRow(`SELECT probe_info_json,address_json,location_ip_json,location_ip_revision,token_hash,public_addr,region_mode,address_auto_v4,address_fallback_auto_v4 FROM nodes WHERE id=1`).Scan(&gotInfo, &gotObs, &settings, &revision, &token, &entry, &mode, &auto, &fallback))
	if gotInfo != info || gotObs != obs || revision != 0 || token != "token" || entry != "192.0.2.10" || mode != "manual" || auto != 1 || fallback != 1 {
		t.Fatal("migration changed existing state")
	}
	var defaults struct {
		Mode   string `json:"mode"`
		Manual string `json:"manual_ip"`
		Entry  bool   `json:"allow_entry_fallback"`
	}
	must(json.Unmarshal([]byte(settings), &defaults))
	if defaults.Mode != "auto" || defaults.Manual != "" || defaults.Entry {
		t.Fatal(settings)
	}
	revisionIs := func(want int) {
		t.Helper()
		must(conn.QueryRow(`SELECT location_ip_revision FROM nodes WHERE id=1`).Scan(&revision))
		if revision != want {
			t.Fatalf("revision %d want %d", revision, want)
		}
	}
	// A fresh sample in the same primary/fallback run is not a new identity.
	_, err = conn.Exec(`UPDATE nodes SET address_json=json_set(address_json,'$.ipv4.last_success',120,'$.fallback_follow_ipv4.last_success',120,'$.fallback_follow_ipv4.confirmed_at',120),last_seen_at=120 WHERE id=1`)
	must(err)
	revisionIs(0)
	// Every writer participates, including direct entry edits and their ABA.
	_, err = conn.Exec(`UPDATE nodes SET public_addr='203.0.113.30' WHERE id=1`)
	must(err)
	_, err = conn.Exec(`UPDATE nodes SET public_addr='192.0.2.10' WHERE id=1`)
	must(err)
	revisionIs(2)
	_, err = conn.Exec(`UPDATE nodes SET location_ip_json='{"mode":"entry"}' WHERE id=1`)
	must(err)
	revisionIs(3)
	_, err = conn.Exec(`UPDATE nodes SET address_json=json_set(address_json,'$.fallback_follow_ipv4.run_id','bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb') WHERE id=1`)
	must(err)
	revisionIs(4)
	_, err = conn.Exec(`UPDATE nodes SET token_hash='replacement' WHERE id=1`)
	must(err)
	revisionIs(5)
}

func TestLocationIPLegacyNodeIncarnations(t *testing.T) {
	ctx := context.Background()
	conn, err := Open("sqlite", filepath.Join(t.TempDir(), "legacy-identities.db"))
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
	must(goose.UpToContext(ctx, conn, ".", 96))
	// These predate the introduction of public per-incarnation UUIDs.
	_, err = conn.Exec(`INSERT INTO nodes(id,name,created_at,updated_at) VALUES(1,'legacy-1',1,1),(2,'legacy-2',1,1)`)
	must(err)
	must(Migrate(ctx, conn, "sqlite"))
	var total, distinct int
	must(conn.QueryRow(`SELECT COUNT(*),COUNT(DISTINCT p.uuid) FROM nodes n JOIN probe_public_ids p ON p.node_id=n.id WHERE length(p.uuid)=36`).Scan(&total, &distinct))
	if total != 2 || distinct != 2 {
		t.Fatal("legacy nodes lack unique incarnation UUIDs", total, distinct)
	}
	var before, after string
	must(conn.QueryRow(`SELECT uuid FROM probe_public_ids WHERE node_id=2`).Scan(&before))
	_, err = conn.Exec(`DELETE FROM nodes WHERE id=2`)
	must(err)
	_, err = conn.Exec(`INSERT INTO nodes(id,name,created_at,updated_at) VALUES(2,'replacement',1,1)`)
	must(err)
	must(conn.QueryRow(`SELECT uuid FROM probe_public_ids WHERE node_id=2`).Scan(&after))
	if before == after || len(after) != 36 {
		t.Fatal("replacement reused incarnation identity", before, after)
	}
}
