package store

import (
	"context"
	"database/sql"
	"testing"

	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/testdb"
)

type fixtureDBMode bool

// Reset and upgrade regressions opt in explicitly; ordinary business fixtures
// use independent copies of the schema migrated once by internal/testdb.
const migrateFixture fixtureDBMode = true

func fixtureDB(t *testing.T, path string, mode ...fixtureDBMode) *sql.DB {
	t.Helper()
	if len(mode) == 0 {
		return testdb.OpenAt(t, path)
	}
	if len(mode) != 1 || mode[0] != migrateFixture {
		t.Fatal("invalid fixture database mode")
	}
	conn, err := db.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := db.Migrate(context.Background(), conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	return conn
}
