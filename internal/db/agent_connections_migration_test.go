package db

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/zeptop-dev/captain/migrations"
)

func TestAgentConnectionMigrationPreservesLegacyIntent(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled bool
		url     string
		cert    int
		rows    int
		def     bool
	}{
		{name: "empty disabled placeholder", rows: 0},
		{name: "disabled selection", url: "https://old.example.com:2096", cert: 1, rows: 1},
		{name: "active entry", enabled: true, url: "https://old.example.com:2096", cert: 1, rows: 1, def: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			conn, err := Open("sqlite", filepath.Join(t.TempDir(), "db"))
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			goose.SetBaseFS(migrations.FS)
			goose.SetLogger(goose.NopLogger())
			if err = goose.SetDialect("sqlite3"); err != nil {
				t.Fatal(err)
			}
			if err = goose.UpToContext(ctx, conn, ".", 121); err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(map[string]any{"enabled": tc.enabled, "url": tc.url, "listen": ":2096", "certificate_id": tc.cert, "revision": "old-revision"})
			if _, err = conn.ExecContext(ctx, "INSERT INTO settings(key,value_json)VALUES('agent_https',?)", string(raw)); err != nil {
				t.Fatal(err)
			}
			if err = Migrate(ctx, conn, "sqlite"); err != nil {
				t.Fatal(err)
			}
			var count int
			if err = conn.QueryRowContext(ctx, "SELECT count(*) FROM agent_endpoints").Scan(&count); err != nil || count != tc.rows {
				t.Fatal(count, err)
			}
			var saved string
			if err = conn.QueryRowContext(ctx, "SELECT value_json FROM settings WHERE key='agent_https'").Scan(&saved); err != nil || saved != string(raw) {
				t.Fatal("legacy setting was rewritten", err)
			}
			if count > 0 {
				var url string
				var enabled, def, was bool
				var cert int
				if err = conn.QueryRowContext(ctx, "SELECT url,enabled,certificate_id,is_default,was_default FROM agent_endpoints").Scan(&url, &enabled, &cert, &def, &was); err != nil || url != tc.url || enabled != tc.enabled || cert != tc.cert || def != tc.def || was != tc.def {
					t.Fatal(url, enabled, cert, def, was, err)
				}
			}
		})
	}
}
