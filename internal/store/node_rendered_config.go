package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
)

type NodeRenderedConfigRecord struct {
	NodeConfigDeliveryRecord
	MetadataStatus string                           `json:"metadata_status"`
	Evidence       *agentproto.RenderedConfigReport `json:"evidence"`
}
type NodeRenderedConfigHistory struct {
	Current *NodeRenderedConfigRecord  `json:"current"`
	History []NodeRenderedConfigRecord `json:"history"`
}

// Called only after the enclosing transaction accepts a new process receipt.
// Missing/invalid optional metadata supersedes old evidence without rejecting
// customer traffic. Database errors still roll back the whole report.
func recordRenderedConfigTx(ctx context.Context, tx *sql.Tx, receipt *agentproto.ConfigReceipt, applied bool, values []*agentproto.RenderedConfigReport) error {
	status, raw := "unavailable", ""
	if len(values) > 0 && values[0] != nil {
		status = "invalid"
		valid := agentproto.ValidateRenderedConfigReport(values[0]) == nil
		for _, artifact := range values[0].Artifacts {
			if artifact.ActivatedAt.After(receipt.ObservedAt.Add(30 * time.Second)) {
				valid = false
			}
		}
		if valid {
			evidence := *values[0]
			if !applied {
				evidence.Status = "unavailable"
			}
			data, err := json.Marshal(&evidence)
			if err != nil {
				return err
			}
			if len(data) <= 65536 {
				status = "valid"
				raw = string(data)
			}
		}
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO node_rendered_configs(delivery_id,receipt_sequence,metadata_status,evidence_json) VALUES(?,?,?,?) ON CONFLICT(delivery_id) DO UPDATE SET receipt_sequence=excluded.receipt_sequence,metadata_status=excluded.metadata_status,evidence_json=excluded.evidence_json`, receipt.DeliveryID, int64(receipt.Sequence), status, raw)
	return err
}

// Identity, active receipt and evidence are read in one SQLite snapshot. Parent
// delivery retention bounds storage and cascade removes retired node evidence.
func (s *Store) NodeRenderedConfigHistory(ctx context.Context, nodeID int64, at time.Time) (*NodeRenderedConfigHistory, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var token sql.NullString
	if err = tx.QueryRowContext(ctx, `SELECT token_hash FROM nodes WHERE id=?`, nodeID).Scan(&token); err != nil {
		return nil, wrapNotFound(err)
	}
	out := &NodeRenderedConfigHistory{History: []NodeRenderedConfigRecord{}}
	var head string
	if token.Valid && token.String != "" {
		err = tx.QueryRowContext(ctx, `SELECT h.delivery_id FROM node_config_heads h JOIN node_config_sessions s ON s.node_id=h.node_id AND s.token_identity=h.token_identity AND s.session=h.session WHERE h.node_id=? AND h.token_identity=? AND s.retired_at IS NULL`, nodeID, token.String).Scan(&head)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+nodeConfigDeliveryCols+` FROM node_config_deliveries d WHERE d.node_id=? AND (d.id=? OR d.ordinal IN (SELECT ordinal FROM node_config_deliveries WHERE node_id=? ORDER BY ordinal DESC LIMIT 20)) ORDER BY d.ordinal DESC`, nodeID, head, nodeID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		item, identity, e := scanNodeConfigDelivery(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		if item.ID == head && identity == token.String {
			item.Current = true
			setNodeConfigFreshness(&item, at)
		}
		out.History = append(out.History, NodeRenderedConfigRecord{NodeConfigDeliveryRecord: item, MetadataStatus: "unavailable"})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range out.History {
		item := &out.History[i]
		var raw string
		var seq uint64
		err = tx.QueryRowContext(ctx, `SELECT receipt_sequence,metadata_status,evidence_json FROM node_rendered_configs WHERE delivery_id=?`, item.ID).Scan(&seq, &item.MetadataStatus, &raw)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if err == nil && seq == item.ReceiptSequence && item.MetadataStatus == "valid" {
			var evidence agentproto.RenderedConfigReport
			if json.Unmarshal([]byte(raw), &evidence) == nil && agentproto.ValidateRenderedConfigReport(&evidence) == nil {
				item.Evidence = &evidence
			} else {
				item.MetadataStatus = "invalid"
			}
		} else if err != nil || seq != item.ReceiptSequence {
			item.MetadataStatus = "unavailable"
		}
		if item.Current {
			v := *item
			out.Current = &v
		}
	}
	return out, tx.Commit()
}
