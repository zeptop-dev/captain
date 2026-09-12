// Package db opens the database and applies embedded migrations.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"

	"github.com/zeptop-dev/captain/migrations"
)

// Open connects to the configured database. Only sqlite is wired for now;
// the schema is written to stay portable to Postgres later.
func Open(driver, dsn string) (*sql.DB, error) {
	switch driver {
	case "sqlite":
		if err := os.MkdirAll(filepath.Dir(dsn), 0o750); err != nil {
			return nil, err
		}
		db, err := sql.Open("sqlite", dsn+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)")
		if err != nil {
			return nil, err
		}
		db.SetMaxOpenConns(1) // sqlite: one writer; reads are fast enough behind it
		return db, db.Ping()
	default:
		return nil, fmt.Errorf("db: driver %q not supported yet", driver)
	}
}

// Migrate applies pending migrations.
func Migrate(ctx context.Context, db *sql.DB, driver string) error {
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect(driver); err != nil {
		return err
	}
	return goose.UpContext(ctx, db, ".")
}

var _ embed.FS
