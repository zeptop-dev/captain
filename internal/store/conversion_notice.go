package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

const conversionNoticeSetting = "plan_access_conversion_notice_dismissed"

func conversionReceipt(ctx context.Context, q accessQuerier) (*AccessConversion, error) {
	receipt := &AccessConversion{}
	err := q.QueryRowContext(ctx, `SELECT converted_at,inbound_count,plan_count FROM plan_access_conversion WHERE id=1`).Scan(&receipt.At, &receipt.Inbounds, &receipt.Plans)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var raw string
	err = q.QueryRowContext(ctx, `SELECT value_json FROM settings WHERE key=?`, conversionNoticeSetting).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return receipt, nil
	}
	if err != nil {
		return nil, err
	}
	var prior AccessConversion
	if err = json.Unmarshal([]byte(raw), &prior); err != nil {
		return nil, err
	}
	receipt.Dismissed = sameConversionReceipt(receipt, &prior)
	return receipt, nil
}
func sameConversionReceipt(a, b *AccessConversion) bool {
	return a != nil && b != nil && a.At == b.At && a.Inbounds == b.Inbounds && a.Plans == b.Plans
}

// DismissConversionNotice hides only this receipt's notice, across browsers and
// administrators. The immutable historical records and permission IDs remain.
func (s *Store) DismissConversionNotice(ctx context.Context, want AccessConversion) error {
	if want.At <= 0 || want.Inbounds <= 0 || want.Plans < 0 {
		return ErrInvalidAccess
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	receipt, err := conversionReceipt(ctx, tx)
	if err != nil {
		return err
	}
	if receipt == nil {
		return ErrNotFound
	}
	if !sameConversionReceipt(receipt, &want) {
		return ErrAccessConflict
	}
	receipt.Dismissed = false
	raw, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO settings(key,value_json) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json`, conversionNoticeSetting, string(raw)); err != nil {
		return err
	}
	return tx.Commit()
}
