package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/zeptop-dev/captain/internal/domain"
)

var (
	ErrPlanNameConflict      = errors.New("plan name is already in use")
	ErrPlanNameInvalid       = errors.New("plan name is required and must be at most 200 characters without control characters")
	ErrPlanGroupNameConflict = errors.New("plan name is already used by another permission scheme")
)

// Existing duplicate names can still receive unrelated edits. Creating or
// changing a name must be unique across enabled and disabled plans.
func validatePlanNameTx(ctx context.Context, tx *sql.Tx, p *domain.Plan, create bool) error {
	old := ""
	if !create {
		if err := tx.QueryRowContext(ctx, `SELECT name FROM plans WHERE id=?`, p.ID).Scan(&old); err != nil {
			return wrapNotFound(err)
		}
		if p.Name == old {
			return nil
		}
	}
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" || !utf8.ValidString(p.Name) || utf8.RuneCountInString(p.Name) > 200 || strings.ContainsFunc(p.Name, unicode.IsControl) {
		return ErrPlanNameInvalid
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,name FROM plans`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name string
		if err = rows.Scan(&id, &name); err != nil {
			return err
		}
		if id != p.ID && strings.EqualFold(strings.TrimSpace(name), p.Name) {
			return ErrPlanNameConflict
		}
	}
	return rows.Err()
}

func permissionNameAvailableTx(ctx context.Context, tx *sql.Tx, name string, own int64) error {
	rows, err := tx.QueryContext(ctx, `SELECT id,name FROM user_groups`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var other string
		if err = rows.Scan(&id, &other); err != nil {
			return err
		}
		if id != own && strings.EqualFold(strings.TrimSpace(other), strings.TrimSpace(name)) {
			return ErrPlanGroupNameConflict
		}
	}
	return rows.Err()
}

func syncOwnedPlanNameTx(ctx context.Context, tx *sql.Tx, p *domain.Plan, strict bool) error {
	var group int64
	err := tx.QueryRowContext(ctx, `SELECT o.group_id FROM plan_access_ownership o JOIN plans p ON p.id=o.plan_id AND p.group_id=o.group_id WHERE p.id=?`, p.ID).Scan(&group)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var current string
	if err = tx.QueryRowContext(ctx, `SELECT name FROM user_groups WHERE id=?`, group).Scan(&current); err != nil {
		return err
	}
	if current == p.Name {
		return nil
	}
	if err = permissionNameAvailableTx(ctx, tx, p.Name, group); err != nil {
		if !strict && errors.Is(err, ErrPlanGroupNameConflict) {
			return nil
		}
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE user_groups SET name=? WHERE id=?`, p.Name, group)
	return err
}
