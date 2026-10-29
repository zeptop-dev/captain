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
	FreshForMS      int64      `json:"fresh_for_ms"`
	Current         bool       `json:"current"`
}
type NodeConfigHistory struct {
	Current *NodeConfigDeliveryRecord  `json:"current,omitempty"`
	History []NodeConfigDeliveryRecord `json:"history"`
}

// PrepareNodeConfigDelivery records issuance, not confirmed network delivery.
// A new process challenges an existing active process only after echoing its
// candidate delivery or sending a valid receipt. Its first delayed GET cannot
// retire the live process. Challenges bind the current head, so a late reply
// also cannot supersede a newer activation. The optional echo keeps callers compatible.
func (s *Store) PrepareNodeConfigDelivery(ctx context.Context, nodeID int64, tokenHash, session, configRevision, stateRevision string, at time.Time, seenDelivery ...string) (*agentproto.ConfigDelivery, error) {
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
	var candidate, candidateParent string
	err = tx.QueryRowContext(ctx, `SELECT retired_at,candidate_delivery_id,candidate_parent_id FROM node_config_sessions WHERE node_id=? AND token_identity=? AND session=?`, nodeID, tokenHash, session).Scan(&retired, &candidate, &candidateParent)
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
	current := headToken == tokenHash && headSession == session
	if current && headState == stateRevision && headConfig == configRevision {
		return &agentproto.ConfigDelivery{ID: headID, Session: session}, nil
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO node_config_sessions(node_id,token_identity,session,created_at) VALUES(?,?,?,?) ON CONFLICT(node_id,token_identity,session) DO NOTHING`, nodeID, tokenHash, session, at.UnixMilli()); err != nil {
		return nil, err
	}
	id := ""
	if candidate != "" && !current && candidateParent == headID {
		var cfg, state string
		err = tx.QueryRowContext(ctx, `SELECT config_revision,state_revision FROM node_config_deliveries WHERE id=? AND node_id=? AND token_identity=? AND session=?`, candidate, nodeID, tokenHash, session).Scan(&cfg, &state)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if err == nil && cfg == configRevision && state == stateRevision {
			id = candidate
		}
	}
	echoed := id != "" && len(seenDelivery) > 0 && seenDelivery[0] == id
	if id == "" {
		id = rand.Text()
		if _, err = tx.ExecContext(ctx, `INSERT INTO node_config_deliveries(id,node_id,token_identity,session,config_revision,state_revision,issued_at) VALUES(?,?,?,?,?,?,?)`, id, nodeID, tokenHash, session, configRevision, stateRevision, at.UnixMilli()); err != nil {
			return nil, err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE node_config_sessions SET candidate_delivery_id=?,candidate_parent_id=? WHERE node_id=? AND token_identity=? AND session=?`, id, headID, nodeID, tokenHash, session); err != nil {
		return nil, err
	}
	// A new authentication generation has no live process under its identity.
	if headToken != tokenHash || current || echoed {
		if err = activateNodeConfigDeliveryTx(ctx, tx, nodeID, tokenHash, session, id, at); err != nil {
			return nil, err
		}
	}
	if err = pruneNodeConfigDeliveriesTx(ctx, tx, nodeID); err != nil {
		return nil, err
	}
	return &agentproto.ConfigDelivery{ID: id, Session: session}, tx.Commit()
}

func activateNodeConfigDeliveryTx(ctx context.Context, tx *sql.Tx, nodeID int64, tokenHash, session, id string, at time.Time) error {
	_, err := tx.ExecContext(ctx, `UPDATE node_config_sessions SET retired_at=COALESCE(retired_at,?) WHERE node_id=? AND (token_identity,session) IN (SELECT token_identity,session FROM node_config_heads WHERE node_id=?) AND NOT(token_identity=? AND session=?)`, at.UnixMilli(), nodeID, nodeID, tokenHash, session)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO node_config_heads(node_id,token_identity,session,delivery_id) VALUES(?,?,?,?) ON CONFLICT(node_id) DO UPDATE SET token_identity=excluded.token_identity,session=excluded.session,delivery_id=excluded.delivery_id`, nodeID, tokenHash, session, id)
	return err
}
func pruneNodeConfigDeliveriesTx(ctx context.Context, tx *sql.Tx, nodeID int64) error {
	// Retain the active head even after 100 pending candidates. Session tombstones
	// remain; candidates whose delivery was pruned receive a new challenge.
	_, err := tx.ExecContext(ctx, `DELETE FROM node_config_deliveries WHERE node_id=? AND ordinal NOT IN (SELECT d.ordinal FROM node_config_deliveries d LEFT JOIN node_config_heads h ON h.node_id=d.node_id AND h.delivery_id=d.id WHERE d.node_id=? ORDER BY (h.delivery_id IS NOT NULL) DESC,d.ordinal DESC LIMIT 100)`, nodeID, nodeID)
	return err
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
	err := tx.QueryRowContext(ctx, `SELECT s.receipt_sequence,s.observed_at,s.last_receipt_at,d.issued_at FROM node_config_sessions s JOIN node_config_deliveries d ON d.id=s.candidate_delivery_id AND d.node_id=s.node_id AND d.token_identity=s.token_identity AND d.session=s.session WHERE s.node_id=? AND s.token_identity=? AND s.session=? AND d.id=? AND s.retired_at IS NULL AND d.config_revision=? AND d.state_revision=? AND EXISTS(SELECT 1 FROM node_config_heads h WHERE h.node_id=s.node_id AND h.token_identity=s.token_identity AND ((h.session=s.session AND h.delivery_id=d.id) OR h.delivery_id=s.candidate_parent_id))`, nodeID, tokenHash, r.Session, r.DeliveryID, r.ConfigRevision, r.StateRevision).Scan(&sequence, &observed, &received, &issued)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if r.ObservedAt.Before(time.UnixMilli(issued).Add(-30*time.Second)) || int64(r.Sequence) <= sequence || observed.Valid && r.ObservedAt.UnixMilli() < observed.Int64 || received.Valid && at.UnixMilli() < received.Int64 {
		return nil
	}
	if err = activateNodeConfigDeliveryTx(ctx, tx, nodeID, tokenHash, r.Session, r.DeliveryID, at); err != nil {
		return err
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
	if token.Valid && token.String != "" {
		current, _, e := scanNodeConfigDelivery(tx.QueryRowContext(ctx, `SELECT `+nodeConfigDeliveryCols+` FROM node_config_heads h JOIN node_config_sessions s ON s.node_id=h.node_id AND s.token_identity=h.token_identity AND s.session=h.session JOIN node_config_deliveries d ON d.id=h.delivery_id WHERE h.node_id=? AND h.token_identity=? AND s.retired_at IS NULL`, nodeID, token.String))
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return nil, e
		}
		if e == nil {
			current.Current = true
			setNodeConfigFreshness(&current, at)
			out.Current = &current
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+nodeConfigDeliveryCols+` FROM node_config_deliveries d WHERE d.node_id=? ORDER BY d.ordinal DESC LIMIT 20`, nodeID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		item, _, e := scanNodeConfigDelivery(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		if out.Current != nil && item.ID == out.Current.ID {
			item = *out.Current
		}
		out.History = append(out.History, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return out, tx.Commit()
}
func setNodeConfigFreshness(item *NodeConfigDeliveryRecord, at time.Time) {
	if !item.Current || item.LastReceiptAt == nil || item.ObservedAt == nil || item.LastReceiptAt.After(at.Add(30*time.Second)) || item.ObservedAt.After(at.Add(30*time.Second)) {
		return
	}
	expires := item.LastReceiptAt.Add(configEvidenceFresh)
	if other := item.ObservedAt.Add(configEvidenceFresh); other.Before(expires) {
		expires = other
	}
	item.FreshForMS = expires.Sub(at).Milliseconds()
	if item.FreshForMS <= 0 {
		item.FreshForMS = 0
		return
	}
	item.Fresh = true
}
