package store

import (
	"context"
	"database/sql"
	"errors"
)

// UserEntryBlocks lists the entry ids hidden from one user.
func (s *Store) UserEntryBlocks(ctx context.Context, userID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT entry_id FROM user_entry_blocks WHERE user_id = ? ORDER BY entry_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SetUserEntryBlocks replaces the user's blacklist with exactly these entries.
func (s *Store) SetUserEntryBlocks(ctx context.Context, userID int64, entryIDs []int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_entry_blocks WHERE user_id = ?`, userID); err != nil {
		return err
	}
	for _, id := range entryIDs {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO user_entry_blocks (user_id, entry_id) VALUES (?, ?)`, userID, id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return err
		}
	}
	return tx.Commit()
}
