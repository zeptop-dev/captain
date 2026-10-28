package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeptop-dev/captain/internal/domain"
)

// ResetPreview counts the main records that a site reset will remove.
func (s *Store) ResetPreview(ctx context.Context) (map[string]int64, error) {
	out := make(map[string]int64)
	for _, table := range []string{"users", "staff", "nodes", "plans", "orders", "subscriptions"} {
		var count int64
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil {
			return nil, err
		}
		out[table] = count
	}
	return out, nil
}

// ResetSite restores an empty, already-migrated database and recreates the
// calling administrator as ID 1 with the same password and enabled TOTP.
// The caller must hold Accounts exclusively through cache invalidation.
// No files or remote services are removed. The entire database change rolls
// back on failure; migrations are deliberately neither deleted nor replayed.
// The probe-only origin registry is retained: browsers may still have a former
// theme's service worker, so reset cannot turn that host into a privileged site.
func (s *Store) ResetSite(ctx context.Context, adminID int64) (*domain.User, error) {
	s.Topology.Lock()
	defer s.Topology.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var email, hash, secret string
	var totp bool
	if err := tx.QueryRowContext(ctx, `SELECT email, password_hash, totp_secret, totp_enabled FROM staff WHERE id = ? AND role = 'admin' AND status = 'active'`, adminID).Scan(&email, &hash, &secret, &totp); err != nil {
		return nil, wrapNotFound(err)
	}
	if !totp {
		secret = "" // an unfinished authenticator setup is not a login credential
	}
	rows, err := tx.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' AND name <> 'goose_db_version' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			rows.Close()
			return nil, err
		}
		tables = append(tables, table)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `PRAGMA defer_foreign_keys = ON`); err != nil {
		return nil, err
	}
	// Discover tables instead of maintaining a deletion list that could leave
	// a future feature's credentials or historical rows behind.
	for _, table := range tables {
		// This append-only security boundary contains only canonical hostnames,
		// never credentials or settings, and must outlive every theme package.
		if table == "probe_origins" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM "`+strings.ReplaceAll(table, `"`, `""`)+`"`); err != nil {
			return nil, fmt.Errorf("reset %s: %w", table, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sqlite_sequence WHERE name <> 'goose_db_version'`); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO account_sequences (name, value) VALUES ('agent', 0), ('user', 0)`); err != nil {
		return nil, err
	}
	at := now()
	if _, err := tx.ExecContext(ctx, `INSERT INTO staff (id, email, password_hash, role, status, totp_secret, totp_enabled, created_at, updated_at) VALUES (1, ?, ?, 'admin', 'active', ?, ?, ?, ?)`, email, hash, secret, totp, at, at); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &domain.User{ID: 1, Email: email, PasswordHash: hash, Role: domain.RoleAdmin, Status: "active", CreatedAt: unix(at), UpdatedAt: unix(at)}, nil
}
