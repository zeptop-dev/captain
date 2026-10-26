package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/zeptop-dev/captain/internal/clientcatalog"
)

func (s *Store) ClientCatalog(ctx context.Context, id string) (clientcatalog.SavedClient, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT document_json FROM client_catalog WHERE id = ?`, id).Scan(&raw)
	if err != nil {
		return clientcatalog.SavedClient{}, wrapNotFound(err)
	}
	var out clientcatalog.SavedClient
	err = json.Unmarshal([]byte(raw), &out)
	return out, err
}

// MutateClientCatalog serializes all metadata/publication changes in one short
// database transaction. The callback must never access the network or files.
func (s *Store) MutateClientCatalog(ctx context.Context, id string, fn func(*clientcatalog.SavedClient) error) error {
	p, ok := clientcatalog.Find(id)
	if !ok {
		return clientcatalog.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw string
	err = tx.QueryRowContext(ctx, `SELECT document_json FROM client_catalog WHERE id = ?`, id).Scan(&raw)
	var cur clientcatalog.SavedClient
	if errors.Is(wrapNotFound(err), ErrNotFound) {
		b := make([]byte, 16)
		if _, err = rand.Read(b); err != nil {
			return err
		}
		cur = clientcatalog.SavedClient{Incarnation: hex.EncodeToString(b), Platforms: []clientcatalog.PlatformSettings{}, CachedAssets: []clientcatalog.CachedAsset{}, Published: []clientcatalog.PublishedClient{}}
		for i, v := range p.Platforms {
			source := "official"
			if v.Kind == "store" {
				source = "store"
			}
			cur.Platforms = append(cur.Platforms, clientcatalog.PlatformSettings{Platform: v.Platform, Recommended: v.Recommended, Sort: i, Source: source, AssetIDs: []int64{}})
		}
	} else if err != nil {
		return err
	} else if err = json.Unmarshal([]byte(raw), &cur); err != nil {
		return err
	}
	if err = fn(&cur); err != nil {
		return err
	}
	b, err := json.Marshal(cur)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO client_catalog(id, document_json) VALUES(?, ?) ON CONFLICT(id) DO UPDATE SET document_json = excluded.document_json`, id, string(b)); err != nil {
		return err
	}
	return tx.Commit()
}
