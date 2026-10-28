package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// ProbeThemePackage is an immutable, validated Komari release archive. Archive
// is loaded separately so listing and request routing do not read large blobs.
type ProbeThemePackage struct {
	Digest    string          `json:"digest"`
	Short     string          `json:"short"`
	Version   string          `json:"version"`
	Manifest  json.RawMessage `json:"manifest"`
	Settings  json.RawMessage `json:"settings"`
	Revision  int64           `json:"revision"`
	CreatedAt int64           `json:"created_at"`
	Size      int64           `json:"size"`
}
type ProbeThemeState struct {
	Active   string `json:"active"`
	Revision int64  `json:"revision"`
}

var ErrProbeThemeConflict = errors.New("theme changed; reload before saving")

func (s *Store) ProbeThemes(ctx context.Context) ([]ProbeThemePackage, ProbeThemeState, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, ProbeThemeState{}, err
	}
	defer tx.Rollback()
	state, err := themeState(ctx, tx)
	if err != nil {
		return nil, state, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT digest,short,version,manifest_json,settings_json,revision,created_at,length(archive) FROM probe_theme_packages ORDER BY created_at DESC,digest`)
	if err != nil {
		return nil, state, err
	}
	list := []ProbeThemePackage{}
	for rows.Next() {
		var p ProbeThemePackage
		var m, v string
		if err = rows.Scan(&p.Digest, &p.Short, &p.Version, &m, &v, &p.Revision, &p.CreatedAt, &p.Size); err != nil {
			rows.Close()
			return nil, state, err
		}
		p.Manifest = json.RawMessage(m)
		p.Settings = json.RawMessage(v)
		list = append(list, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, state, err
	}
	return list, state, tx.Commit()
}
func themeState(ctx context.Context, tx *sql.Tx) (ProbeThemeState, error) {
	var st ProbeThemeState
	err := tx.QueryRowContext(ctx, `SELECT coalesce(active_digest,''),revision FROM probe_theme_state WHERE id=1`).Scan(&st.Active, &st.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return st, nil
	}
	return st, err
}
func (s *Store) ProbeThemeState(ctx context.Context) (ProbeThemeState, error) {
	var st ProbeThemeState
	err := s.db.QueryRowContext(ctx, `SELECT coalesce(active_digest,''),revision FROM probe_theme_state WHERE id=1`).Scan(&st.Active, &st.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return st, nil
	}
	return st, err
}
func (s *Store) ProbeThemeArchive(ctx context.Context, digest string) ([]byte, error) {
	var b []byte
	err := s.db.QueryRowContext(ctx, `SELECT archive FROM probe_theme_packages WHERE digest=?`, digest).Scan(&b)
	return b, wrapNotFound(err)
}
func (s *Store) InstallProbeTheme(ctx context.Context, p ProbeThemePackage, b []byte) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT digest FROM probe_theme_packages WHERE short=? AND version=?`, p.Short, p.Version).Scan(&existing)
	if err == nil {
		if existing != p.Digest {
			return fmt.Errorf("theme version already installed with different contents")
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var count, total int64
	if err = tx.QueryRowContext(ctx, `SELECT count(*),coalesce(sum(length(archive)),0) FROM probe_theme_packages`).Scan(&count, &total); err != nil {
		return err
	}
	if count >= 12 || total+int64(len(b)) > 160<<20 {
		return fmt.Errorf("theme storage limit reached; remove an unused version")
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO probe_theme_packages(digest,short,version,manifest_json,archive,created_at) VALUES(?,?,?,?,?,?)`, p.Digest, p.Short, p.Version, string(p.Manifest), b, now())
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) ActivateProbeTheme(ctx context.Context, digest string, revision int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO probe_theme_state(id) VALUES(1)`)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE probe_theme_state SET active_digest=nullif(?,''),revision=revision+1 WHERE id=1 AND revision=?`, digest, revision)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return ErrProbeThemeConflict
	}
	return tx.Commit()
}
func (s *Store) SaveProbeThemeSettings(ctx context.Context, digest string, revision int64, settings []byte) error {
	result, err := s.db.ExecContext(ctx, `UPDATE probe_theme_packages SET settings_json=?,revision=revision+1 WHERE digest=? AND revision=?`, string(settings), digest, revision)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return ErrProbeThemeConflict
	}
	return nil
}
func (s *Store) RemoveProbeTheme(ctx context.Context, digest string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	st, err := themeState(ctx, tx)
	if err != nil {
		return err
	}
	if st.Active == digest {
		return fmt.Errorf("switch to another theme before removing the active version")
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM probe_theme_packages WHERE digest=?`, digest)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return ErrNotFound
	}
	return tx.Commit()
}
