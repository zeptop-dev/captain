package db_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/migrations"
)

func TestStructuralReleaseMigrationsPreserveImmediateLineState(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "pre-release.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err = goose.SetDialect("sqlite"); err != nil {
		t.Fatal(err)
	}
	if err = goose.UpToContext(ctx, conn, ".", 99); err != nil {
		t.Fatal(err)
	}
	groups := `[{"strategy":"failover","members":[{"id":"stable-member","node_id":1,"port":18443,"weight":1}]}]`
	forwards := `[{"tag":"line-5-stable-member-existing","protocol":"tcp","port":18443,"target":"192.0.2.2:443","line_id":5,"line_member_id":"stable-member"}]`
	for _, q := range []string{
		`INSERT INTO nodes(id,name,public_addr,token_hash,created_at,updated_at) VALUES(1,'entry','192.0.2.1','existing-agent-token',1,1),(2,'landing','192.0.2.2',NULL,1,1)`,
		`INSERT INTO inbounds(id,node_id,tag,protocol,port,settings_json,created_at,updated_at) VALUES(3,2,'existing','vless',443,'{"password":"migration-test-credential"}',1,1)`,
		`INSERT INTO entries(id,name,inbound_id,display_host,display_port,created_at,updated_at) VALUES(7,'existing published entry',3,'old.example.test',18443,1,1)`,
	} {
		if _, err = conn.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = conn.Exec(`INSERT INTO forward_lines(id,name,version,protocol,backend,target_inbound_id,groups_json,updated_at) VALUES(5,'existing line',9,'tcp','relay',3,?,1)`, groups); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(`INSERT INTO forward_line_members(line_id,member_id,node_id) VALUES(5,'stable-member',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(`UPDATE entries SET line_id=5,line_member_id='stable-member' WHERE id=7`); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(`UPDATE nodes SET forwards_json=? WHERE id=1`, forwards); err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx, conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct{ query, want string }{
		{`SELECT groups_json FROM forward_lines WHERE id=5`, groups},
		{`SELECT forwards_json FROM nodes WHERE id=1`, forwards},
		{`SELECT token_hash FROM nodes WHERE id=1`, "existing-agent-token"},
		{`SELECT settings_json FROM inbounds WHERE id=3`, `{"password":"migration-test-credential"}`},
		{`SELECT display_host||':'||display_port||':'||line_id||':'||line_member_id FROM entries WHERE id=7`, "old.example.test:18443:5:stable-member"},
		{`SELECT name||':'||version||':'||protocol||':'||backend||':'||target_inbound_id FROM forward_lines WHERE id=5`, "existing line:9:tcp:relay:3"},
	} {
		var got string
		if err = conn.QueryRow(check.query).Scan(&got); err != nil || got != check.want {
			t.Fatal("migration changed live state", check.query, got, err)
		}
	}
	for _, table := range []string{"forward_paths", "forward_path_revisions", "forward_path_hops", "forward_path_line_versions", "forward_path_refs", "forward_path_routes", "forward_path_route_refs", "line_rollouts", "line_rollout_nodes", "line_rollout_steps", "line_changes", "node_config_deliveries", "node_config_heads", "node_config_sessions"} {
		var n int
		if err = conn.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil || n != 0 {
			t.Fatal("migration fabricated feature data", table, n, err)
		}
	}
	var scopes int
	if err = conn.QueryRow(`SELECT count(*) FROM line_change_scopes WHERE line_id=5`).Scan(&scopes); err != nil || scopes != 1 {
		t.Fatal("existing line incarnation missing", scopes, err)
	}
	var version int
	if err = conn.QueryRow(`SELECT max(version_id) FROM goose_db_version WHERE is_applied=1`).Scan(&version); err != nil || version < 103 {
		t.Fatal("new schemas not applied", version, err)
	}
	if err = db.Migrate(ctx, conn, "sqlite"); err != nil {
		t.Fatal("migration replay was not idempotent", err)
	}
	var broken int
	if err = conn.QueryRow(`SELECT count(*) FROM pragma_foreign_key_check`).Scan(&broken); err != nil || broken != 0 {
		t.Fatal("foreign key violations", broken, err)
	}
}
