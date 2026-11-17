package db

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/zeptop-dev/captain/migrations"
)

func TestRegistrationMigrationLeavesOldAccountsUnverified(t *testing.T) {
	ctx := context.Background()
	conn, e := Open("sqlite", filepath.Join(t.TempDir(), "old.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	goose.SetDialect("sqlite3")
	if e = goose.UpToContext(ctx, conn, ".", 124); e != nil {
		t.Fatal(e)
	}
	if _, e = conn.Exec(`INSERT INTO users(id,email,password_hash,role,uuid,sub_token,status,created_at,updated_at,agent_id) VALUES(1,'old@example.com','password-hash','user','uuid','token','active',1,1,1)`); e != nil {
		t.Fatal(e)
	}
	for range 2 {
		if e = Migrate(ctx, conn, "sqlite"); e != nil {
			t.Fatal(e)
		}
	}
	var n int
	if e = conn.QueryRow(`SELECT COUNT(*) FROM user_email_verifications`).Scan(&n); e != nil || n != 0 {
		t.Fatal(n, e)
	}
	var email, uuid, token, hash string
	if e = conn.QueryRow(`SELECT email,uuid,sub_token,password_hash FROM users WHERE id=1`).Scan(&email, &uuid, &token, &hash); e != nil || email != "old@example.com" || uuid != "uuid" || token != "token" || hash != "password-hash" {
		t.Fatal(email, uuid, token, hash, e)
	}
	var integrity string
	if e = conn.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); e != nil || integrity != "ok" {
		t.Fatal(integrity, e)
	}
}
