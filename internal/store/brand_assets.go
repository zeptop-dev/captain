package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
)

// SaveBrandAsset stores a normalized PNG by content hash. Identical uploads
// reuse the same row and URL; callers validate and re-encode before storing.
func (s *Store) SaveBrandAsset(ctx context.Context, png []byte) (string, error) {
	sum := sha256.Sum256(png)
	digest := hex.EncodeToString(sum[:])
	_, err := s.db.ExecContext(ctx, `INSERT INTO brand_assets (digest, png, created_at) VALUES (?, ?, ?) ON CONFLICT(digest) DO NOTHING`, digest, png, now())
	return digest, err
}

func (s *Store) BrandAsset(ctx context.Context, digest string) ([]byte, error) {
	var png []byte
	err := s.db.QueryRowContext(ctx, `SELECT png FROM brand_assets WHERE digest = ?`, digest).Scan(&png)
	return png, wrapNotFound(err)
}
