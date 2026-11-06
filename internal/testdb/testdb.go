// Package testdb supplies isolated, fully migrated SQLite databases for business
// tests. Migration, upgrade, and reset tests should use db.Open and db.Migrate
// directly so their setup continues to exercise the migration path.
package testdb

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/zeptop-dev/captain/internal/db"
)

type snapshotCache struct {
	once  sync.Once
	image []byte
	err   error
}

func (c *snapshotCache) load(build func() ([]byte, error)) ([]byte, error) {
	c.once.Do(func() { c.image, c.err = build() })
	return c.image, c.err
}

var schema snapshotCache

// Open returns a fresh database in t.TempDir and closes it at test cleanup.
// Each test owns a separate disk file and connection, with the production WAL,
// foreign-key, busy-timeout, and synchronous settings supplied by db.Open.
func Open(t testing.TB) *sql.DB {
	t.Helper()
	return OpenAt(t, filepath.Join(t.TempDir(), "captain.db"))
}

// OpenAt is Open with an explicit, unused path, for tests that later close and
// reopen their database. It never overwrites an existing file. The caller owns
// the path's directory (normally t.TempDir) and any subsequently opened handle.
func OpenAt(t testing.TB, path string) *sql.DB {
	t.Helper()
	image, err := schema.load(func() ([]byte, error) {
		return buildSnapshot("", db.Migrate)
	})
	if err != nil {
		t.Fatalf("prepare test database schema: %v", err)
	}
	conn, err := cloneSnapshot(path, image)
	if err != nil {
		t.Fatalf("clone test database: %v", err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Errorf("close test database: %v", err)
		}
	})
	return conn
}

// The template is built from the embedded migrations once per test process,
// never from a checked-in database. Only bytes from a checkpointed, closed file
// survive initialization; neither an open template nor its WAL is shared.
func buildSnapshot(parent string, migrate func(context.Context, *sql.DB, string) error) ([]byte, error) {
	dir, err := os.MkdirTemp(parent, "captain-test-schema-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "schema.db")
	conn, err := db.Open("sqlite", path)
	if conn != nil {
		defer conn.Close()
	}
	if err != nil {
		return nil, err
	}
	if err := migrate(context.Background(), conn, "sqlite"); err != nil {
		return nil, err
	}
	var busy, remaining, checkpointed int
	if err := conn.QueryRow("PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &remaining, &checkpointed); err != nil {
		return nil, err
	}
	if busy != 0 || remaining != 0 || checkpointed != 0 {
		return nil, fmt.Errorf("incomplete schema checkpoint: busy=%d remaining=%d checkpointed=%d", busy, remaining, checkpointed)
	}
	if err := conn.Close(); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func cloneSnapshot(path string, image []byte) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	_, writeErr := f.Write(image)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(path)
		if writeErr != nil {
			return nil, writeErr
		}
		return nil, closeErr
	}
	conn, err := db.Open("sqlite", path)
	if err != nil {
		if conn != nil {
			_ = conn.Close()
		}
		_ = os.Remove(path)
		return nil, err
	}
	return conn, nil
}
