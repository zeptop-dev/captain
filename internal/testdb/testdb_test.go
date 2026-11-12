package testdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/zeptop-dev/captain/internal/db"
)

func TestSnapshotMatchesRealMigrations(t *testing.T) {
	cloned := Open(t)
	fresh, err := db.Open("sqlite", filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fresh.Close() })
	if err := db.Migrate(context.Background(), fresh, "sqlite"); err != nil {
		t.Fatal(err)
	}
	query := `SELECT type,name,tbl_name,rootpage,COALESCE(sql,'') FROM sqlite_schema ORDER BY type,name`
	if got, want := rows(t, cloned, query), rows(t, fresh, query); !reflect.DeepEqual(got, want) {
		t.Fatalf("cloned schema differs from real migrations\ngot: %v\nwant: %v", got, want)
	}
	tables, err := fresh.Query(`SELECT name FROM sqlite_schema WHERE type='table' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for tables.Next() {
		var name string
		if err := tables.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	if err := tables.Err(); err != nil {
		t.Fatal(err)
	}
	tables.Close()
	for _, name := range names {
		query := `SELECT * FROM "` + strings.ReplaceAll(name, `"`, `""`) + `"`
		switch name {
		case "goose_db_version":
			// The actual migration time is expected to differ between runs.
			query = `SELECT id,version_id,is_applied FROM goose_db_version`
		case "access_catalog_epoch":
			// Independently migrated databases intentionally have different fences.
			// Compare identity, length and valid hex shape, not random contents.
			query = `SELECT id,length(nonce),nonce NOT GLOB '*[^0-9a-f]*' FROM access_catalog_epoch`
		case "plan_access_conversion":
			// The immutable receipt has the actual time of each independent upgrade.
			query = `SELECT id,converted_at>0,inbound_count,plan_count FROM plan_access_conversion`
		}
		if got, want := rows(t, cloned, query), rows(t, fresh, query); !reflect.DeepEqual(got, want) {
			t.Fatalf("cloned data in %s differs: got %v, want %v", name, got, want)
		}
	}
	if got := rows(t, cloned, "PRAGMA quick_check"); !reflect.DeepEqual(got, []string{`["ok"]`}) {
		t.Fatalf("quick_check: %v", got)
	}
	if got := rows(t, cloned, "PRAGMA foreign_key_check"); len(got) != 0 {
		t.Fatalf("foreign_key_check: %v", got)
	}
}

func rows(t *testing.T, conn *sql.DB, query string) []string {
	t.Helper()
	r, err := conn.Query(query)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	cols, err := r.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for r.Next() {
		values := make([]any, len(cols))
		args := make([]any, len(cols))
		for i := range values {
			args[i] = &values[i]
		}
		if err := r.Scan(args...); err != nil {
			t.Fatal(err)
		}
		out = append(out, fmt.Sprintf("%q", values))
	}
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

func TestIsolationProductionSettingsAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "fixture.db")
	a := OpenAt(t, path)
	b := Open(t)
	for name, want := range map[string]string{"journal_mode": "wal", "foreign_keys": "1", "busy_timeout": "5000", "synchronous": "1"} {
		var got string
		if err := a.QueryRow("PRAGMA " + name).Scan(&got); err != nil || got != want {
			t.Fatalf("%s=%q, want %q: %v", name, got, want, err)
		}
	}
	if a.Stats().MaxOpenConnections != 1 {
		t.Fatal("fixture must preserve production connection limit")
	}
	if _, err := a.Exec(`INSERT INTO sessions(id,user_id,expires_at,created_at) VALUES('bad',999,1,0)`); err == nil {
		t.Fatal("foreign-key violation accepted")
	}
	if _, err := a.Exec(`INSERT INTO nodes(id,name,created_at,updated_at) VALUES(1,'isolated',0,0)`); err != nil {
		t.Fatal(err)
	}
	// This trigger is installed by the latest migration. It must survive cloning.
	var revision string
	if err := a.QueryRow(`SELECT revision FROM node_probe_plans WHERE node_id=1`).Scan(&revision); err != nil || revision == "" {
		t.Fatalf("latest migration trigger missing: %q %v", revision, err)
	}
	for _, conn := range []*sql.DB{b, Open(t)} {
		var count int
		if err := conn.QueryRow("SELECT count(*) FROM nodes").Scan(&count); err != nil || count != 0 {
			t.Fatalf("fixture data leaked: %d %v", count, err)
		}
	}
	if _, err := b.Exec(`INSERT INTO nodes(id,name,created_at,updated_at) VALUES(1,'independent',0,0)`); err != nil {
		t.Fatal(err)
	}
	var independentRevision string
	if err := b.QueryRow(`SELECT revision FROM node_probe_plans WHERE node_id=1`).Scan(&independentRevision); err != nil || independentRevision == "" || independentRevision == revision {
		t.Fatalf("clones reused a random identity: %q %q %v", revision, independentRevision, err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := db.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var name string
	if err := reopened.QueryRow("SELECT name FROM nodes WHERE id=1").Scan(&name); err != nil || name != "isolated" {
		t.Fatalf("reopen lost committed data: %q %v", name, err)
	}
}

func TestCleanupClosesConnection(t *testing.T) {
	var conn *sql.DB
	t.Run("fixture", func(t *testing.T) { conn = Open(t) })
	if err := conn.Ping(); err == nil {
		t.Fatal("test cleanup left database open")
	}
}

func TestConcurrentFirstSnapshotAndIndependentClones(t *testing.T) {
	var cache snapshotCache
	var calls atomic.Int32
	parent := t.TempDir()
	build := func() ([]byte, error) {
		calls.Add(1)
		return buildSnapshot(parent, db.Migrate)
	}
	const workers = 8
	start := make(chan struct{})
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := range workers {
		path := filepath.Join(t.TempDir(), "concurrent.db")
		wg.Go(func() {
			<-start
			image, err := cache.load(build)
			if err != nil {
				errs <- err
				return
			}
			conn, err := cloneSnapshot(path, image)
			if err != nil {
				errs <- err
				return
			}
			defer conn.Close()
			_, err = conn.Exec(`INSERT INTO nodes(id,name,created_at,updated_at) VALUES(1,?,0,0)`, fmt.Sprintf("worker-%d", i))
			errs <- err
		})
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("template built %d times", calls.Load())
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 0 {
		t.Fatalf("template files retained: %v %v", entries, err)
	}
}

func TestSnapshotFailureIsCachedAndCleansUp(t *testing.T) {
	parent := t.TempDir()
	var conn *sql.DB
	want := errors.New("migration failed")
	var calls int
	build := func() ([]byte, error) {
		calls++
		return buildSnapshot(parent, func(_ context.Context, c *sql.DB, _ string) error {
			conn = c
			if _, err := c.Exec("CREATE TABLE partially_migrated(id INTEGER)"); err != nil {
				return err
			}
			return want
		})
	}
	var cache snapshotCache
	for range 2 {
		if image, err := cache.load(build); !errors.Is(err, want) || len(image) != 0 {
			t.Fatalf("failed migration returned usable snapshot: %d %v", len(image), err)
		}
	}
	if calls != 1 {
		t.Fatalf("failed initializer retried %d times", calls)
	}
	if err := conn.Ping(); err == nil {
		t.Fatal("failed migration left source database open")
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed migration left files behind: %v %v", entries, err)
	}
}

func TestCloneFailureDoesNotOverwriteOrLeaveInvalidDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.db")
	want := []byte("keep existing file")
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := cloneSnapshot(path, nil); !errors.Is(err, os.ErrExist) {
		t.Fatalf("existing file accepted: %v", err)
	}
	if got, err := os.ReadFile(path); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("existing file changed: %q %v", got, err)
	}
	bad := filepath.Join(t.TempDir(), "invalid.db")
	if _, err := cloneSnapshot(bad, []byte("not a database")); err == nil {
		t.Fatal("invalid snapshot accepted")
	}
	if _, err := os.Stat(bad); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid clone left behind: %v", err)
	}
}

// Compare only database setup, with the same production connection settings and
// independent disk files. The clone benchmark explicitly warms the once-per-
// process migration; the full-migration result also measures that cold cost.
func BenchmarkDatabaseSetup(b *testing.B) {
	b.Run("FullMigrations", func(b *testing.B) {
		for range b.N {
			conn, err := db.Open("sqlite", filepath.Join(b.TempDir(), "fresh.db"))
			if err != nil {
				b.Fatal(err)
			}
			if err := db.Migrate(context.Background(), conn, "sqlite"); err != nil {
				conn.Close()
				b.Fatal(err)
			}
			if err := conn.Close(); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("ClonedSchema", func(b *testing.B) {
		Open(b).Close()
		b.ResetTimer()
		for range b.N {
			if err := Open(b).Close(); err != nil {
				b.Fatal(err)
			}
		}
	})
}
