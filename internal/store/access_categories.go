package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/zeptop-dev/captain/internal/domain"
)

func accessCatalogSelections(ctx context.Context, q accessQuerier, c *InboundAccessCatalog) error {
	c.Categories = []AccessReference{}
	rows, err := q.QueryContext(ctx, `SELECT id,name FROM entry_categories ORDER BY sort,id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var a AccessReference
		if err = rows.Scan(&a.ID, &a.Name); err != nil {
			rows.Close()
			return err
		}
		c.Categories = append(c.Categories, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = q.QueryContext(ctx, `SELECT group_id,category_id FROM group_access_categories ORDER BY group_id,category_id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var g, id int64
		if err = rows.Scan(&g, &id); err != nil {
			rows.Close()
			return err
		}
		for i := range c.Groups {
			if c.Groups[i].ID == g {
				c.Groups[i].CategoryIDs = append(c.Groups[i].CategoryIDs, id)
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = q.QueryContext(ctx, `SELECT g.inbound_id,g.group_id FROM inbound_access_groups g JOIN inbound_access a ON a.inbound_id=g.inbound_id WHERE a.policy='groups' UNION SELECT i.id,i.group_id FROM inbounds i WHERE i.group_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM inbound_access a WHERE a.inbound_id=i.id) ORDER BY 1,2`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, g int64
		if err = rows.Scan(&id, &g); err != nil {
			rows.Close()
			return err
		}
		for i := range c.Inbounds {
			if c.Inbounds[i].InboundID == id {
				c.Inbounds[i].StaticGroupIDs = append(c.Inbounds[i].StaticGroupIDs, g)
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	c.Conversion, err = conversionReceipt(ctx, q)
	return err
}

var ErrCategoryAccessPermission = errors.New("changing a followed category requires node-access write permission")

type categoryWriterKey struct{}

// HTTP supplies the authenticated permission; trusted internal jobs omit it.
func WithCategoryAccessWriter(ctx context.Context, allowed bool) context.Context {
	return context.WithValue(ctx, categoryWriterKey{}, allowed)
}
func guardCategoryAccess(ctx context.Context, q accessQuerier, ids ...*int64) error {
	allowed, provided := ctx.Value(categoryWriterKey{}).(bool)
	if !provided || allowed {
		return nil
	}
	for _, id := range ids {
		if id == nil {
			continue
		}
		var n int
		if err := q.QueryRowContext(ctx, `SELECT count(*) FROM group_access_categories WHERE category_id=?`, *id).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return ErrCategoryAccessPermission
		}
	}
	return nil
}
func guardEntryCategoryAccess(ctx context.Context, tx *sql.Tx, e *domain.Entry, create bool) error {
	if create {
		return guardCategoryAccess(ctx, tx, e.CategoryID)
	}
	var old sql.NullInt64
	var inbound int64
	if err := tx.QueryRowContext(ctx, `SELECT category_id,inbound_id FROM entries WHERE id=?`, e.ID).Scan(&old, &inbound); err != nil {
		return wrapNotFound(err)
	}
	if inbound == e.InboundID && ((!old.Valid && e.CategoryID == nil) || (old.Valid && e.CategoryID != nil && old.Int64 == *e.CategoryID)) {
		return nil
	}
	return guardCategoryAccess(ctx, tx, int64Ptr(old), e.CategoryID)
}

type planCreationKey struct{}

func WithPlanAccessCreation(ctx context.Context) context.Context {
	return context.WithValue(ctx, planCreationKey{}, true)
}
func planAccessCreation(ctx context.Context) bool {
	v, _ := ctx.Value(planCreationKey{}).(bool)
	return v
}
