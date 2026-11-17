package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrLegacyGrantConflict = errors.New("account authorization changed; refresh the migration preview")
var ErrLegacyGrantNeedsPlan = errors.New("a usable subscription must first cover the same permission scheme")

type LegacyGrantPlan struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}
type LegacyUserGrant struct {
	GroupID       *int64            `json:"group_id"`
	Name          string            `json:"name"`
	Revision      string            `json:"revision"`
	CoveringPlans []LegacyGrantPlan `json:"covering_plans"`
	CanMigrate    bool              `json:"can_migrate"`
}

func legacyUserGrantTx(ctx context.Context, tx *sql.Tx, id int64, at time.Time) (LegacyUserGrant, error) {
	v := LegacyUserGrant{CoveringPlans: []LegacyGrantPlan{}}
	var agent int64
	var group sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT u.agent_id,u.group_id,COALESCE(g.name,'') FROM users u LEFT JOIN user_groups g ON g.id=u.group_id WHERE u.id=? AND u.role='user'`, id).Scan(&agent, &group, &v.Name)
	if err != nil {
		return v, wrapNotFound(err)
	}
	if !group.Valid {
		return v, nil
	}
	v.GroupID = &group.Int64
	v.Revision = fmt.Sprintf("%x", sha256.Sum256(fmt.Appendf(nil, "legacy-grant:%d:%d", agent, group.Int64)))
	// Compare scheme identity, not a coincidentally equal current node list:
	// the same scheme also preserves future category/external-source membership.
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT p.id,p.name FROM subscriptions sub JOIN plans p ON p.id=sub.plan_id WHERE sub.user_id=? AND sub.status='active' AND p.group_id=? AND `+usableSQL+` ORDER BY p.id`, id, group.Int64, at.Unix())
	if err != nil {
		return v, err
	}
	defer rows.Close()
	for rows.Next() {
		var p LegacyGrantPlan
		if err = rows.Scan(&p.ID, &p.Name); err != nil {
			return v, err
		}
		v.CoveringPlans = append(v.CoveringPlans, p)
	}
	if err = rows.Err(); err != nil {
		return v, err
	}
	v.CanMigrate = len(v.CoveringPlans) > 0
	return v, nil
}
func (s *Store) LegacyUserGrant(ctx context.Context, id int64, at time.Time) (LegacyUserGrant, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return LegacyUserGrant{}, err
	}
	defer tx.Rollback()
	v, err := legacyUserGrantTx(ctx, tx, id, at)
	if err != nil {
		return v, err
	}
	return v, tx.Commit()
}

// MigrateLegacyUserGrant removes only a redundant account override. It never
// creates a subscription, edits a plan, expands permissions or changes billing.
func (s *Store) MigrateLegacyUserGrant(ctx context.Context, id int64, revision string, at time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	v, err := legacyUserGrantTx(ctx, tx, id, at)
	if err != nil {
		return err
	}
	if v.GroupID == nil || revision == "" || revision != v.Revision {
		return ErrLegacyGrantConflict
	}
	if !v.CanMigrate {
		return ErrLegacyGrantNeedsPlan
	}
	if _, err = tx.ExecContext(ctx, `UPDATE users SET group_id=NULL,updated_at=? WHERE id=? AND role='user'`, at.Unix(), id); err != nil {
		return err
	}
	return tx.Commit()
}
