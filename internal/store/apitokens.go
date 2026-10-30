package store

import (
	"context"
	"database/sql"
	"encoding/json"
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
	Scopes     []string   `json:"scopes"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
}

// ScopeRead is the read-only token scope; anything else is full access.
const ScopeRead = "read"

// CreateAPIToken issues a token and returns its plaintext once. expires is
// optional; scope "read" limits it to reads.
func (s *Store) CreateAPIToken(ctx context.Context, userID int64, name, scope string, expires *time.Time, restrictions ...[]string) (string, *APIToken, error) {
	if scope != ScopeRead {
		scope = "full"
	}
	var scopes []string
	if len(restrictions) > 0 {
		scopes = restrictions[0]
	}
	if err := ValidateTokenScopes(scopes); err != nil {
		return "", nil, err
	}
	raw, _ := json.Marshal(scopes)
	var exp any
	if expires != nil {
		exp = expires.Unix()
	}
	plain := "cap_" + auth.Token(24)
	res, err := s.db.ExecContext(ctx, `INSERT INTO api_tokens (user_id, name, token_hash, created_at, scope, expires_at, scopes_json) VALUES (?, ?, ?, ?, ?, ?, ?)`, userID, name, auth.SHA256Hex(plain), now(), scope, exp, string(raw))
	if err != nil {
		return "", nil, err
	}
	id, _ := res.LastInsertId()
	return plain, &APIToken{ID: id, Name: name, Scope: scope, Scopes: scopes, CreatedAt: time.Now(), ExpiresAt: expires}, nil
}

func (s *Store) ListAPITokens(ctx context.Context, userID int64) ([]APIToken, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, created_at, last_used_at, scope, expires_at, scopes_json FROM api_tokens WHERE user_id = ? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []APIToken{}
	for rows.Next() {
		var t APIToken
		var created int64
		var used, exp sql.NullInt64
		var raw string
		if err := rows.Scan(&t.ID, &t.Name, &created, &used, &t.Scope, &exp, &raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &t.Scopes); err != nil {
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
	return s.resolveAPIToken(ctx, plain, true)
}

// ReadAPIToken checks current authorization without stamping last use. Passive
// diagnostics may check each second and must not turn observation into writes.
func (s *Store) ReadAPIToken(ctx context.Context, plain string) (*domain.User, string, error) {
	return s.resolveAPIToken(ctx, plain, false)
}

func (s *Store) resolveAPIToken(ctx context.Context, plain string, touch bool) (*domain.User, string, error) {
	hash := auth.SHA256Hex(plain)
	var userID, id int64
	var scope, raw string
	var exp sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT id, user_id, scope, expires_at, scopes_json FROM api_tokens WHERE token_hash = ?`, hash).Scan(&id, &userID, &scope, &exp, &raw); err != nil {
		return nil, "", wrapNotFound(err)
	}
	if exp.Valid && exp.Int64 <= now() {
		return nil, "", ErrNotFound
	}
	if touch {
		_, _ = s.db.ExecContext(ctx, `UPDATE api_tokens SET last_used_at = ? WHERE id = ?`, now(), id)
	}
	var scopes []string
	if err := json.Unmarshal([]byte(raw), &scopes); err != nil {
		return nil, "", err
	}
	u, err := s.StaffByID(ctx, userID)
	return u, tokenPolicy(scope, scopes), err
}
