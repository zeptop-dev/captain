package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/zeptop-dev/captain/internal/auth"
	"github.com/zeptop-dev/captain/internal/domain"
)

// APIToken is a personal bearer token for the admin API and MCP; it carries
// the owner's role, narrowed by its scope.
type APIToken struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	// Scope is "full" or "read" (GET requests and MCP read tools only).
	Scope      string     `json:"scope"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
}

// ScopeRead is the read-only token scope; anything else is full access.
const ScopeRead = "read"

// CreateAPIToken issues a token and returns its plaintext once. expires is
// optional; scope "read" limits it to reads.
func (s *Store) CreateAPIToken(ctx context.Context, userID int64, name, scope string, expires *time.Time) (string, *APIToken, error) {
	if scope != ScopeRead {
		scope = "full"
	}
	var exp any
	if expires != nil {
		exp = expires.Unix()
	}
	plain := "cap_" + auth.Token(24)
	res, err := s.db.ExecContext(ctx, `INSERT INTO api_tokens (user_id, name, token_hash, created_at, scope, expires_at) VALUES (?, ?, ?, ?, ?, ?)`, userID, name, auth.SHA256Hex(plain), now(), scope, exp)
	if err != nil {
		return "", nil, err
	}
	id, _ := res.LastInsertId()
	return plain, &APIToken{ID: id, Name: name, Scope: scope, CreatedAt: time.Now(), ExpiresAt: expires}, nil
}

func (s *Store) ListAPITokens(ctx context.Context, userID int64) ([]APIToken, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, created_at, last_used_at, scope, expires_at FROM api_tokens WHERE user_id = ? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []APIToken{}
	for rows.Next() {
		var t APIToken
		var created int64
		var used, exp sql.NullInt64
		if err := rows.Scan(&t.ID, &t.Name, &created, &used, &t.Scope, &exp); err != nil {
			return nil, err
		}
		t.CreatedAt, t.LastUsedAt, t.ExpiresAt = unix(created), unixPtr(used), unixPtr(exp)
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) DeleteAPIToken(ctx context.Context, userID, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM api_tokens WHERE id = ? AND user_id = ?`, id, userID)
	return err
}

// UserByAPIToken resolves a bearer token to its owner and its scope, and
// stamps last use. An expired token resolves to nothing.
func (s *Store) UserByAPIToken(ctx context.Context, plain string) (*domain.User, string, error) {
	hash := auth.SHA256Hex(plain)
	var userID, id int64
	var scope string
	var exp sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT id, user_id, scope, expires_at FROM api_tokens WHERE token_hash = ?`, hash).Scan(&id, &userID, &scope, &exp); err != nil {
		return nil, "", wrapNotFound(err)
	}
	if exp.Valid && exp.Int64 <= now() {
		return nil, "", ErrNotFound
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE api_tokens SET last_used_at = ? WHERE id = ?`, now(), id)
	u, err := s.UserByID(ctx, userID)
	return u, scope, err
}
