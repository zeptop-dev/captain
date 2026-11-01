package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"time"
)

// Upgrade history records requests and authenticated version observations. It
// never asserts executable integrity or changes the existing delivery channel.
func requestNodeUpgradeTx(ctx context.Context, tx *sql.Tx, id int64, version string, at time.Time) error {
	var name, token, deliveryTarget string
	if err := tx.QueryRowContext(ctx, `SELECT name,COALESCE(token_hash,''),upgrade_to FROM nodes WHERE id=?`, id).Scan(&name, &token, &deliveryTarget); err != nil {
		// Preserve the old setter's no-op semantics for a missing node.
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	var previous, previousVersion, previousToken string
	err := tx.QueryRowContext(ctx, `SELECT id,target_version,token_identity FROM node_upgrade_operations WHERE node_id=? AND status='pending'`, id).Scan(&previous, &previousVersion, &previousToken)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	// Retrying the same in-flight command is idempotent. A completed or cancelled
	// request has no active row and a later request receives a new identity.
	if previous != "" && version == previousVersion && token == previousToken && deliveryTarget == version {
		return nil
	}
	status := "superseded"
	if version == "" {
		status = "cancelled"
	}
	if _, err = tx.ExecContext(ctx, `UPDATE node_upgrade_operations SET status=?,updated_at=?,completed_at=? WHERE node_id=? AND status='pending'`, status, at.UnixMilli(), at.UnixMilli(), id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE nodes SET upgrade_to=?,updated_at=? WHERE id=?`, version, at.Unix(), id); err != nil {
		return err
	}
	if version != "" {
		status = "pending"
		var done any
		if token == "" {
			status = "identity_changed"
			done = at.UnixMilli()
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO node_upgrade_operations(id,node_id,node_name,token_identity,target_version,status,actor,created_at,updated_at,completed_at,supersedes) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, rand.Text(), id, name, token, version, status, lineChangeActor(ctx), at.UnixMilli(), at.UnixMilli(), done, previous); err != nil {
			return err
		}
	}
	return pruneUpgradeOperationsTx(ctx, tx, id)
}

func pruneUpgradeOperationsTx(ctx context.Context, tx *sql.Tx, nodeID int64) error {
	// Hard budgets for completed history; active requests are never evicted.
	if _, err := tx.ExecContext(ctx, `DELETE FROM node_upgrade_operations WHERE node_id=? AND status<>'pending' AND id NOT IN (SELECT id FROM node_upgrade_operations WHERE node_id=? AND status<>'pending' ORDER BY created_at DESC,id DESC LIMIT 200)`, nodeID, nodeID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM node_upgrade_operations WHERE status<>'pending' AND id NOT IN (SELECT id FROM node_upgrade_operations WHERE status<>'pending' ORDER BY created_at DESC,id DESC LIMIT 10000)`)
	return err
}

func observeNodeUpgradeTx(ctx context.Context, tx *sql.Tx, nodeID int64, token, version string, at time.Time) error {
	if token == "" || version == "" {
		return nil
	}
	res, err := tx.ExecContext(ctx, `UPDATE node_upgrade_operations SET status='observed',updated_at=?,completed_at=? WHERE node_id=? AND token_identity=? AND target_version=? AND status='pending' AND created_at<=? AND EXISTS(SELECT 1 FROM nodes n WHERE n.id=node_id AND n.token_hash=token_identity AND n.upgrade_to=target_version)`, at.UnixMilli(), at.UnixMilli(), nodeID, token, version, at.UnixMilli())
	if err != nil {
		return err
	}
	if n, e := res.RowsAffected(); e != nil {
		return e
	} else if n == 0 {
		return nil
	}
	return pruneUpgradeOperationsTx(ctx, tx, nodeID)
}
