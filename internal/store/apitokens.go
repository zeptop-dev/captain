package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/zeptop-dev/captain/internal/auth"
	"github.com/zeptop-dev/captain/internal/domain"
)

// APIToken is a personal bearer token for the admin API and MCP; it carries
// the owner's role.
type APIToken struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

// CreateAPIToken issues a token and returns its plaintext once.
func (s *Store) CreateAPIToken(ctx context.Context, userID int64, name string) (string, *APIToken, error) {
	plain := "cap_" + auth.Token(24)
	res, err := s.db.ExecContext(ctx, `INSERT INTO api_tokens (user_id, name, token_hash, created_at) VALUES (?, ?, ?, ?)`, userID, name, auth.SHA256Hex(plain), now())
	if err != nil {
		return "", nil, err
	}
	id, _ := res.LastInsertId()
	return plain, &APIToken{ID: id, Name: name, CreatedAt: time.Now()}, nil
}

func (s *Store) ListAPITokens(ctx context.Context, userID int64) ([]APIToken, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, created_at, last_used_at FROM api_tokens WHERE user_id = ? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []APIToken{}
	for rows.Next() {
		var t APIToken
		var created int64
		var used sql.NullInt64
		if err := rows.Scan(&t.ID, &t.Name, &created, &used); err != nil {
			return nil, err
		}
		t.CreatedAt, t.LastUsedAt = unix(created), unixPtr(used)
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) DeleteAPIToken(ctx context.Context, userID, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM api_tokens WHERE id = ? AND user_id = ?`, id, userID)
	return err
}

// UserByAPIToken resolves a bearer token to its owner and stamps last use.
func (s *Store) UserByAPIToken(ctx context.Context, plain string) (*domain.User, error) {
	hash := auth.SHA256Hex(plain)
	var userID, id int64
	if err := s.db.QueryRowContext(ctx, `SELECT id, user_id FROM api_tokens WHERE token_hash = ?`, hash).Scan(&id, &userID); err != nil {
		return nil, wrapNotFound(err)
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE api_tokens SET last_used_at = ? WHERE id = ?`, now(), id)
	return s.UserByID(ctx, userID)
}
