package store

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/zeptop-dev/captain/internal/domain"
	"strings"
)

// SavePlanAccess changes the plan and listener grants in one transaction. Legacy
// requests without Access continue using CreatePlan/UpdatePlan as before.
func (s *Store) SavePlanAccess(ctx context.Context, p *domain.Plan, creating bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	draft := p.Access
	if draft == nil || draft.InboundIDs == nil || (draft.Mode != "independent" && draft.Mode != "shared") {
		return ErrInvalidAccess
	}
	c, err := accessCatalog(ctx, tx)
	if err != nil {
		return err
	}
	if !draft.ConfirmShared {
		return ErrAccessConfirmationRequired
	}
	if c.Revision != draft.Revision {
		return ErrAccessConflict
	}
	var old *domain.Plan
	if !creating {
		old, err = planByIDTx(ctx, tx, p.ID)
		if err != nil {
			return err
		}
	}
	var owned sql.NullInt64
	if old != nil {
		if err = tx.QueryRowContext(ctx, `SELECT group_id FROM plan_access_ownership WHERE plan_id=?`, p.ID).Scan(&owned); err != nil && err != sql.ErrNoRows {
			return err
		}
	}
	var group int64
	if draft.Mode == "shared" {
		for _, g := range c.Groups {
			if g.ID == draft.GroupID {
				group = g.ID
			}
		}
		if group == 0 {
			name := strings.TrimSpace(draft.Name)
			if draft.GroupID != 0 || name == "" || len(name) > 200 || strings.ContainsAny(name, "\r\n") {
				return ErrInvalidAccess
			}
			res, e := tx.ExecContext(ctx, `INSERT INTO user_groups(name) VALUES(?)`, name)
			if e != nil {
				return ErrInvalidAccess
			}
			group, _ = res.LastInsertId()
		}
		var n int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM plan_access_ownership WHERE group_id=?`, group).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return ErrInvalidAccess
		}
	} else if old != nil && old.GroupID != nil && owned.Valid && owned.Int64 == *old.GroupID {
		group = owned.Int64
	} else {
		res, e := tx.ExecContext(ctx, `INSERT INTO user_groups(name) VALUES(?)`, p.Name+" · "+GenerateCouponCode())
		if e != nil {
			return e
		}
		group, _ = res.LastInsertId()
	}
	p.GroupID = &group
	if creating {
		res, e := tx.ExecContext(ctx, `INSERT INTO plans(name,price_cents,period_days,quota_bytes,device_limit,speed_limit_mbps,reset_days,reset_mode,prices_json,group_id,sort,enabled,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, p.Name, p.PriceCents, p.PeriodDays, p.QuotaBytes, p.DeviceLimit, p.SpeedLimitMbps, p.ResetDays, p.ResetMode, encodePrices(p.Prices), group, p.Sort, boolInt(p.Enabled), now(), now())
		if e != nil {
			return e
		}
		p.ID, _ = res.LastInsertId()
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE plans SET name=?,price_cents=?,period_days=?,quota_bytes=?,device_limit=?,speed_limit_mbps=?,reset_days=?,reset_mode=?,prices_json=?,group_id=?,sort=?,enabled=?,updated_at=? WHERE id=?`, p.Name, p.PriceCents, p.PeriodDays, p.QuotaBytes, p.DeviceLimit, p.SpeedLimitMbps, p.ResetDays, p.ResetMode, encodePrices(p.Prices), group, p.Sort, boolInt(p.Enabled), now(), p.ID)
		if err != nil {
			return err
		}
	}
	if draft.Mode == "independent" {
		var external any
		if old != nil && old.GroupID != nil {
			external = *old.GroupID
		}
		if !owned.Valid || old == nil || old.GroupID == nil || owned.Int64 != *old.GroupID {
			_, err = tx.ExecContext(ctx, `INSERT INTO plan_access_ownership(plan_id,group_id,external_group_id) VALUES(?,?,?) ON CONFLICT(plan_id) DO UPDATE SET group_id=excluded.group_id,external_group_id=excluded.external_group_id`, p.ID, group, external)
			if err != nil {
				return err
			}
		}
	}
	// Rebind the revision only after verifying the caller's immutable snapshot.
	updated, err := accessCatalog(ctx, tx)
	if err != nil {
		return err
	}
	if err = updateGroupAccessTx(ctx, tx, group, GroupAccessChange{InboundIDs: draft.InboundIDs, Revision: updated.Revision, ConfirmShared: draft.ConfirmShared}); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("save plan access: %w", err)
	}
	return nil
}
