package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"regexp"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
)

const configEvidenceFresh = 3 * time.Minute

var configSessionPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
var configRevisionPattern = regexp.MustCompile(`^[a-zA-Z0-9._:-]{1,128}$`)

// NodeConfigDeliveryRecord contains private operational evidence, never the
// rendered configuration, customer credentials or node authentication identity.
type NodeConfigDeliveryRecord struct {
	ID              string     `json:"id"`
	Session         string     `json:"session"`
	ConfigRevision  string     `json:"config_revision"`
	StateRevision   string     `json:"state_revision"`
	IssuedAt        time.Time  `json:"issued_at"`
	ConfirmedAt     *time.Time `json:"confirmed_at,omitempty"`
	LastReceiptAt   *time.Time `json:"last_receipt_at,omitempty"`
	ObservedAt      *time.Time `json:"observed_at,omitempty"`
	ReceiptSequence uint64     `json:"receipt_sequence"`
	Applied         bool       `json:"applied"`
	Fresh           bool       `json:"fresh"`
	Current         bool       `json:"current"`
}
type NodeConfigHistory struct {
	Current *NodeConfigDeliveryRecord  `json:"current,omitempty"`
	History []NodeConfigDeliveryRecord `json:"history"`
}

// PrepareNodeConfigDelivery records issuance, not confirmed network delivery.
// A repeated current state in the same process session reuses its ID. Returning
// to an earlier state after another delivery creates a new ID, fencing old replies.
func (s *Store) PrepareNodeConfigDelivery(ctx context.Context, nodeID int64, tokenHash, session, configRevision, stateRevision string, at time.Time) (*agentproto.ConfigDelivery, error) {
	if !configSessionPattern.MatchString(session) || !configRevisionPattern.MatchString(configRevision) || !configRevisionPattern.MatchString(stateRevision) || at.UnixMilli() <= 0 {
		return nil, fmt.Errorf("invalid configuration delivery metadata")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireNodeTokenTx(ctx, tx, nodeID, tokenHash); err != nil {
		return nil, err
	}
	var retired sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT retired_at FROM node_config_sessions WHERE node_id=? AND token_identity=? AND session=?`, nodeID, tokenHash, session).Scan(&retired)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err == nil && retired.Valid {
		return nil, nil
	}
	var headToken, headSession, headID, headConfig, headState string
	err = tx.QueryRowContext(ctx, `SELECT h.token_identity,h.session,h.delivery_id,d.config_revision,d.state_revision FROM node_config_heads h JOIN node_config_deliveries d ON d.id=h.delivery_id WHERE h.node_id=?`, nodeID).Scan(&headToken, &headSession, &headID, &headConfig, &headState)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err == nil && headToken == tokenHash && headSession == session && headState == stateRevision && headConfig == configRevision {
		return &agentproto.ConfigDelivery{ID: headID, Session: session}, nil
	}
	if headSession != "" && (headToken != tokenHash || headSession != session) {
		if _, err = tx.ExecContext(ctx, `UPDATE node_config_sessions SET retired_at=COALESCE(retired_at,?) WHERE node_id=? AND token_identity=? AND session=?`, at.UnixMilli(), nodeID, headToken, headSession); err != nil {
			return nil, err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO node_config_sessions(node_id,token_identity,session,created_at) VALUES(?,?,?,?) ON CONFLICT(node_id,token_identity,session) DO NOTHING`, nodeID, tokenHash, session, at.UnixMilli()); err != nil {
		return nil, err
	}
	delivery := &agentproto.ConfigDelivery{ID: rand.Text(), Session: session}
	if _, err = tx.ExecContext(ctx, `INSERT INTO node_config_deliveries(id,node_id,token_identity,session,config_revision,state_revision,issued_at) VALUES(?,?,?,?,?,?,?)`, delivery.ID, nodeID, tokenHash, session, configRevision, stateRevision, at.UnixMilli()); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO node_config_heads(node_id,token_identity,session,delivery_id) VALUES(?,?,?,?) ON CONFLICT(node_id) DO UPDATE SET token_identity=excluded.token_identity,session=excluded.session,delivery_id=excluded.delivery_id`, nodeID, tokenHash, session, delivery.ID); err != nil {
		return nil, err
	}
	// Keep 100 deliveries including the current one. Retired sessions are not
	// pruned: forgetting them could let an old process reclaim the active session.
	if _, err = tx.ExecContext(ctx, `DELETE FROM node_config_deliveries WHERE node_id=? AND id<>? AND ordinal NOT IN (SELECT ordinal FROM node_config_deliveries WHERE node_id=? ORDER BY ordinal DESC LIMIT 100)`, nodeID, delivery.ID, nodeID); err != nil {
		return nil, err
	}
	return delivery, tx.Commit()
}

// Independently numbered process receipts must not borrow freshness from a
// customer traffic batch. Invalid, old and repeated evidence is ignored, while
// storage failures abort the enclosing full-report transaction.
func recordNodeConfigReceiptTx(ctx context.Context, tx *sql.Tx, nodeID int64, tokenHash string, r *agentproto.ConfigReceipt, appliedRevision string, at time.Time) error {
	if r == nil || r.Sequence == 0 || r.Sequence > math.MaxInt64 || !configSessionPattern.MatchString(r.Session) || !configRevisionPattern.MatchString(r.DeliveryID) || !configRevisionPattern.MatchString(r.ConfigRevision) || !configRevisionPattern.MatchString(r.StateRevision) || r.ObservedAt.IsZero() || r.ObservedAt.Before(at.Add(-configEvidenceFresh)) || r.ObservedAt.After(at.Add(30*time.Second)) {
		return nil
	}
	var sequence, issued int64
	var observed, received sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT s.receipt_sequence,s.observed_at,s.last_receipt_at,d.issued_at FROM node_config_heads h JOIN node_config_sessions s ON s.node_id=h.node_id AND s.token_identity=h.token_identity AND s.session=h.session JOIN node_config_deliveries d ON d.id=h.delivery_id WHERE h.node_id=? AND h.token_identity=? AND h.session=? AND h.delivery_id=? AND s.retired_at IS NULL AND d.config_revision=? AND d.state_revision=?`, nodeID, tokenHash, r.Session, r.DeliveryID, r.ConfigRevision, r.StateRevision).Scan(&sequence, &observed, &received, &issued)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if r.ObservedAt.Before(time.UnixMilli(issued).Add(-30*time.Second)) || int64(r.Sequence) <= sequence || observed.Valid && r.ObservedAt.UnixMilli() < observed.Int64 || received.Valid && at.UnixMilli() < received.Int64 {
		return nil
	}
	if _, err = tx.ExecContext(ctx, `UPDATE node_config_sessions SET receipt_sequence=?,observed_at=?,last_receipt_at=? WHERE node_id=? AND token_identity=? AND session=?`, int64(r.Sequence), r.ObservedAt.UnixMilli(), at.UnixMilli(), nodeID, tokenHash, r.Session); err != nil {
		return err
	}
	applied := r.Applied && appliedRevision == r.StateRevision
	_, err = tx.ExecContext(ctx, `UPDATE node_config_deliveries SET confirmed_at=CASE WHEN ? THEN COALESCE(confirmed_at,?) ELSE confirmed_at END,last_receipt_at=?,observed_at=?,receipt_sequence=?,applied=? WHERE id=?`, applied, at.UnixMilli(), at.UnixMilli(), r.ObservedAt.UnixMilli(), int64(r.Sequence), applied, r.DeliveryID)
	return err
}

const nodeConfigDeliveryCols = `d.id,d.session,d.config_revision,d.state_revision,d.issued_at,d.confirmed_at,d.last_receipt_at,d.observed_at,d.receipt_sequence,d.applied,d.token_identity`

func scanNodeConfigDelivery(row rowScanner) (NodeConfigDeliveryRecord, string, error) {
	var out NodeConfigDeliveryRecord
	var identity string
	var issued, seq int64
	var confirmed, received, observed sql.NullInt64
	err := row.Scan(&out.ID, &out.Session, &out.ConfigRevision, &out.StateRevision, &issued, &confirmed, &received, &observed, &seq, &out.Applied, &identity)
	out.IssuedAt = time.UnixMilli(issued)
	out.ReceiptSequence = uint64(seq)
	toTime := func(v sql.NullInt64) *time.Time {
		if !v.Valid {
			return nil
		}
		t := time.UnixMilli(v.Int64)
		return &t
	}
	out.ConfirmedAt = toTime(confirmed)
	out.LastReceiptAt = toTime(received)
	out.ObservedAt = toTime(observed)
	return out, identity, err
}

// NodeConfigHistory separates historical confirmation from current fresh
// evidence. The caller compares the current desired digests with this delivery.
func (s *Store) NodeConfigHistory(ctx context.Context, nodeID int64, at time.Time) (*NodeConfigHistory, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var token sql.NullString
	if err = tx.QueryRowContext(ctx, `SELECT token_hash FROM nodes WHERE id=?`, nodeID).Scan(&token); err != nil {
		return nil, wrapNotFound(err)
	}
	out := &NodeConfigHistory{History: []NodeConfigDeliveryRecord{}}
	var currentID string
	err = tx.QueryRowContext(ctx, `SELECT h.delivery_id FROM node_config_heads h JOIN node_config_sessions s ON s.node_id=h.node_id AND s.token_identity=h.token_identity AND s.session=h.session WHERE h.node_id=? AND h.token_identity=? AND s.retired_at IS NULL`, nodeID, token.String).Scan(&currentID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+nodeConfigDeliveryCols+` FROM node_config_deliveries d WHERE d.node_id=? ORDER BY d.ordinal DESC LIMIT 20`, nodeID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		item, identity, e := scanNodeConfigDelivery(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		item.Current = token.Valid && token.String != "" && identity == token.String && item.ID == currentID
		item.Fresh = item.Current && item.LastReceiptAt != nil && item.ObservedAt != nil && !item.LastReceiptAt.Before(at.Add(-configEvidenceFresh)) && !item.LastReceiptAt.After(at.Add(30*time.Second)) && !item.ObservedAt.Before(at.Add(-configEvidenceFresh)) && !item.ObservedAt.After(at.Add(30*time.Second))
		out.History = append(out.History, item)
		if item.Current {
			cp := item
			out.Current = &cp
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return out, tx.Commit()
}
