package store

import (
	"context"
	"database/sql"
	"errors"
)

// MaxAccountID is exactly representable by the JSON/JavaScript clients.
const MaxAccountID int64 = 9007199254740991

var ErrAccountID = errors.New("ID must be a positive safe integer")
var ErrIDInUse = errors.New("ID is already in use")

// availableAccountID also checks history without foreign keys: assigning its
// numeric owner to a new account would expose another customer's records.
func availableAccountID(ctx context.Context, tx *sql.Tx, table string, refs []accountRef, id int64) error {
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM `+table+` WHERE id = ?`, id).Scan(&exists); err == nil {
		return ErrIDInUse
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	for _, ref := range refs {
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM `+ref.table+` WHERE `+ref.column+` = ? LIMIT 1`, id).Scan(&exists); err == nil {
			return ErrIDInUse
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	return nil
}

// nextCustomerID advances only for automatic creation. Manual creation and
// renumbering must not move the cursor to a large administrator-selected ID.
func nextCustomerID(ctx context.Context, tx *sql.Tx) (int64, error) {
	var id int64
	if err := tx.QueryRowContext(ctx, `SELECT value FROM account_sequences WHERE name = 'user'`).Scan(&id); err != nil {
		return 0, err
	}
	for id < MaxAccountID {
		id++
		if err := availableAccountID(ctx, tx, "users", customerRefs, id); errors.Is(err, ErrIDInUse) {
			continue
		} else if err != nil {
			return 0, err
		}
		_, err := tx.ExecContext(ctx, `UPDATE account_sequences SET value = ? WHERE name = 'user'`, id)
		return id, err
	}
	return 0, ErrAccountID
}

type accountRef struct{ table, column string }

var customerRefs = []accountRef{
	{"login_subjects", "user_id"}, {"wireguard_devices", "user_id"},
	{"user_subscription_profiles", "user_id"},
	{"sessions", "user_id"}, {"subscriptions", "user_id"}, {"orders", "user_id"},
	{"identities", "user_id"}, {"notifications", "user_id"}, {"users", "invited_by"},
	{"commissions", "inviter_id"}, {"commissions", "invitee_id"}, {"withdrawals", "user_id"},
	{"tickets", "user_id"}, {"gift_codes", "redeemed_by"}, {"telegram_bind_codes", "user_id"},
	{"sub_links", "user_id"}, {"user_entry_blocks", "user_id"}, {"hwid_devices", "user_id"},
	{"sub_requests", "user_id"}, {"conn_log", "user_id"}, {"audit_log", "user_id"},
	{"dyn_limits", "user_id"}, {"traffic_daily", "user_id"}, {"online_devices", "user_id"},
}
var staffRefs = []accountRef{{"infra_payments", "staff_id"}, {"staff_passkey_users", "staff_id"}, {"staff_passkeys", "staff_id"}, {"passkey_challenges", "staff_id"}, {"user_bulk_jobs", "staff_id"}, {"sessions", "staff_id"}, {"api_tokens", "user_id"}, {"staff_identities", "user_id"}, {"admin_log", "user_id"}}

// ChangeAccountID runs under Accounts' exclusive request lock. Foreign keys
// are deferred only inside this transaction; every reference moves atomically.
func (s *Store) ChangeAccountID(ctx context.Context, oldID, newID int64, staff bool) error {
	if oldID <= 0 || newID <= 0 || newID > MaxAccountID {
		return ErrAccountID
	}
	table, refs := "users", customerRefs
	if staff {
		table, refs = "staff", staffRefs
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM `+table+` WHERE id = ?`, oldID).Scan(&exists); err != nil {
		return wrapNotFound(err)
	}
	if oldID == newID {
		return tx.Commit()
	}
	if err := availableAccountID(ctx, tx, table, refs, newID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `PRAGMA defer_foreign_keys = ON`); err != nil {
		return err
	}
	for _, ref := range refs {
		if _, err = tx.ExecContext(ctx, `UPDATE `+ref.table+` SET `+ref.column+` = ? WHERE `+ref.column+` = ?`, newID, oldID); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE `+table+` SET id = ?, updated_at = ? WHERE id = ?`, newID, now(), oldID); err != nil {
		return err
	}
	return tx.Commit()
}
