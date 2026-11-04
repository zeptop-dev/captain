package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
)

func TestResetSiteAtomicAndFreshIDs(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "reset.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := db.Migrate(ctx, conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	s := New(conn)
	a := &domain.User{Email: "admin@example.com", PasswordHash: "retained-hash", Role: "admin", Status: "active"}
	if err := s.CreateStaff(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeAccountID(ctx, a.ID, 9, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTOTP(ctx, 9, "test-authenticator-secret", true); err != nil {
		t.Fatal(err)
	}
	u := &domain.User{Email: "old@example.com", UUID: "old", SubToken: "old-sub", Status: "active"}
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeAccountID(ctx, u.ID, 100, false); err != nil {
		t.Fatal(err)
	}
	token, _, err := s.CreateAPIToken(ctx, 9, "old-token", "full", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting(ctx, "sample", map[string]string{"value": "old"}); err != nil {
		t.Fatal(err)
	}
	// An added feature with its own FK and sequence must not escape cleanup.
	for _, q := range []string{
		`CREATE TABLE future_feature (id INTEGER PRIMARY KEY AUTOINCREMENT, owner INTEGER REFERENCES users(id), value TEXT)`,
		`INSERT INTO future_feature VALUES (55, 100, 'old')`,
		`CREATE TRIGGER reject_reset BEFORE INSERT ON staff WHEN NEW.id = 1 BEGIN SELECT RAISE(ABORT, 'test rollback'); END`,
	} {
		if _, err := conn.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	var migrationsBefore int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM goose_db_version`).Scan(&migrationsBefore); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResetSite(ctx, 9); err == nil {
		t.Fatal("injected failure was ignored")
	}
	if _, err := s.UserByID(ctx, 100); err != nil {
		t.Fatal("partial deletion", err)
	}
	if _, _, err := s.UserByAPIToken(ctx, token); err != nil {
		t.Fatal("token lost on rollback", err)
	}
	var n int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM future_feature`).Scan(&n); err != nil || n != 1 {
		t.Fatal("partial deletion", n, err)
	}
	if _, err := conn.Exec(`DROP TRIGGER reject_reset`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResetSite(ctx, 100); !errors.Is(err, ErrNotFound) {
		t.Fatal("customer accepted as administrator", err)
	}
	fresh, err := s.ResetSite(ctx, 9)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.ID != 1 || fresh.Email != a.Email || fresh.PasswordHash != a.PasswordHash {
		t.Fatal("administrator login changed")
	}
	if secret, on, err := s.TOTP(ctx, 1); err != nil || !on || secret != "test-authenticator-secret" {
		t.Fatal("TOTP not retained", err)
	}
	if _, _, err := s.UserByAPIToken(ctx, token); err == nil {
		t.Fatal("old API token still valid")
	}
	rows, err := conn.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	rows.Close()
	for _, table := range tables {
		if err := conn.QueryRow(`SELECT COUNT(*) FROM "` + table + `"`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		want := 0
		switch table {
		case "staff":
			want = 1
		case "settings":
			want = 1 // a fresh, disabled Agent entry incarnation only
		case "account_sequences":
			want = 2
		case "goose_db_version":
			want = migrationsBefore
		}
		if n != want {
			t.Errorf("%s: got %d rows, want %d", table, n, want)
		}
	}
	// Running migrations again is a no-op and the two ID namespaces start fresh.
	if err := db.Migrate(ctx, conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	u = &domain.User{Email: "new@example.com", UUID: "new", SubToken: "new-sub", Status: "active"}
	if err := s.CreateUser(ctx, u); err != nil || u.ID != 1 || u.AgentID != 1 {
		t.Fatal("customer IDs not reset", u.ID, u.AgentID, err)
	}
	a = &domain.User{Email: "staff@example.com", Role: "support", Status: "active"}
	if err := s.CreateStaff(ctx, a); err != nil || a.ID != 2 {
		t.Fatal("staff sequence not reset", a.ID, err)
	}
	if err := conn.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&n); err != nil || n != 0 {
		t.Fatal("broken foreign keys", n, err)
	}
}
