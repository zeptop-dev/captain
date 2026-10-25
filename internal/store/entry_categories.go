package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
)

var ErrEntryCategoryInvalid = errors.New("invalid subscription node category")
var ErrEntryCategoryConflict = errors.New("subscription node category conflict")

const MaxEntryCategoryAssignment = 10000

func normalizeEntryCategory(c *domain.EntryCategory) error {
	c.Name = strings.TrimSpace(c.Name)
	c.Description = strings.TrimSpace(c.Description)
	if c.Name == "" || !utf8.ValidString(c.Name) || utf8.RuneCountInString(c.Name) > 80 || !spec.PublicLabel(c.Name) ||
		!utf8.ValidString(c.Description) || utf8.RuneCountInString(c.Description) > 500 {
		return fmt.Errorf("%w: name is required (up to 80 characters); description allows up to 500 characters", ErrEntryCategoryInvalid)
	}
	for _, r := range c.Name {
		if unicode.IsControl(r) {
			return fmt.Errorf("%w: name contains control characters", ErrEntryCategoryInvalid)
		}
	}
	for _, r := range c.Description {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return fmt.Errorf("%w: description contains control characters", ErrEntryCategoryInvalid)
		}
	}
	return nil
}

func scanEntryCategory(row interface{ Scan(...any) error }) (*domain.EntryCategory, error) {
	var c domain.EntryCategory
	err := row.Scan(&c.ID, &c.Name, &c.Description, &c.Sort)
	return &c, wrapNotFound(err)
}

func (s *Store) ListEntryCategories(ctx context.Context) ([]*domain.EntryCategory, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,description,sort FROM entry_categories ORDER BY sort,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*domain.EntryCategory{}
	for rows.Next() {
		c, err := scanEntryCategory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) EntryCategoryByID(ctx context.Context, id int64) (*domain.EntryCategory, error) {
	return scanEntryCategory(s.db.QueryRowContext(ctx, `SELECT id,name,description,sort FROM entry_categories WHERE id=?`, id))
}

// Category writes serialize their validation and mutation in the database.
// Callers editing entries also hold Topology, so a category cannot be deleted
// between loading an entry and preserving its category in a legacy PATCH.
func (s *Store) CreateEntryCategory(ctx context.Context, c *domain.EntryCategory) error {
	if err := normalizeEntryCategory(c); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := entryCategoryNameAvailable(ctx, tx, c.Name, 0); err != nil {
		return err
	}
	var last sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT MAX(sort) FROM entry_categories`).Scan(&last); err != nil {
		return err
	}
	c.Sort = int(last.Int64) + 10
	res, err := tx.ExecContext(ctx, `INSERT INTO entry_categories(name,name_key,description,sort) VALUES(?,?,?,?)`, c.Name, strings.ToLower(c.Name), c.Description, c.Sort)
	if err != nil {
		return err
	}
	c.ID, err = res.LastInsertId()
	if err != nil {
		return err
	}
	return tx.Commit()
}

func entryCategoryNameAvailable(ctx context.Context, tx *sql.Tx, name string, id int64) error {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM entry_categories WHERE name_key=? AND id<>?`, strings.ToLower(name), id).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("%w: name is already in use", ErrEntryCategoryConflict)
	}
	return nil
}

// UpdateEntryCategory preserves position; ordering is changed only by the
// dedicated complete-list operation, never by a stale editor payload.
func (s *Store) UpdateEntryCategory(ctx context.Context, c *domain.EntryCategory) error {
	if err := normalizeEntryCategory(c); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := tx.QueryRowContext(ctx, `SELECT sort FROM entry_categories WHERE id=?`, c.ID).Scan(&c.Sort); err != nil {
		return wrapNotFound(err)
	}
	if err := entryCategoryNameAvailable(ctx, tx, c.Name, c.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE entry_categories SET name=?,name_key=?,description=? WHERE id=?`, c.Name, strings.ToLower(c.Name), c.Description, c.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeleteEntryCategory(ctx context.Context, id int64) error {
	// ON DELETE SET NULL leaves every entry (including managed reverse entries)
	// intact, with their original subscription position and access rules.
	res, err := s.db.ExecContext(ctx, `DELETE FROM entry_categories WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		return ErrNotFound
	}
	return err
}

func validCategoryIDs(ids []int64, allowEmpty bool) error {
	if (!allowEmpty && len(ids) == 0) || len(ids) > MaxEntryCategoryAssignment {
		return fmt.Errorf("%w: provide 1 to %d IDs", ErrEntryCategoryInvalid, MaxEntryCategoryAssignment)
	}
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			return fmt.Errorf("%w: IDs must be positive and unique", ErrEntryCategoryInvalid)
		}
		seen[id] = true
	}
	return nil
}

func (s *Store) ReorderEntryCategories(ctx context.Context, ids []int64) error {
	if err := validCategoryIDs(ids, true); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM entry_categories`).Scan(&count); err != nil {
		return err
	}
	if count != len(ids) {
		return fmt.Errorf("%w: category list changed; reload before sorting", ErrEntryCategoryConflict)
	}
	for i, id := range ids {
		res, err := tx.ExecContext(ctx, `UPDATE entry_categories SET sort=? WHERE id=?`, (i+1)*10, id)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return fmt.Errorf("%w: category list changed; reload before sorting", ErrEntryCategoryConflict)
		}
	}
	return tx.Commit()
}

func validateEntryCategory(ctx context.Context, tx *sql.Tx, e *domain.Entry) error {
	if e.CategoryID == nil {
		return nil
	}
	if *e.CategoryID < 0 {
		return fmt.Errorf("%w: category ID cannot be negative", ErrEntryCategoryInvalid)
	}
	if *e.CategoryID == 0 {
		e.CategoryID = nil
		return nil
	}
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM entry_categories WHERE id=?`, *e.CategoryID).Scan(&exists); err != nil {
		return err
	}
	if exists != 1 {
		return fmt.Errorf("%w: category does not exist", ErrEntryCategoryInvalid)
	}
	return nil
}

// AssignEntryCategory only updates display metadata. Managed entries can be
// categorized without granting permission to edit their protected topology.
func (s *Store) AssignEntryCategory(ctx context.Context, ids []int64, categoryID int64) error {
	if err := validCategoryIDs(ids, false); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	e := &domain.Entry{CategoryID: &categoryID}
	if err := validateEntryCategory(ctx, tx, e); err != nil {
		return err
	}
	for _, id := range ids {
		res, err := tx.ExecContext(ctx, `UPDATE entries SET category_id=?,updated_at=? WHERE id=?`, nullInt64(e.CategoryID), now(), id)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return fmt.Errorf("%w: subscription node does not exist", ErrEntryCategoryInvalid)
		}
	}
	return tx.Commit()
}
