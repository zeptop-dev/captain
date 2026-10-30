package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
)

const trafficHistoryDays = 7
const trafficHistoryMaxRows = 12000
const trafficHistoryGlobalMaxRows = 100000

type TrafficObservation struct {
	Sample           *agentproto.TrafficSample
	DiscardedSamples int
	// The already-authenticated reporter identity, not an agent-supplied field.
	TokenHash string
}

type TrafficHistoryItem struct {
	ID               int64                     `json:"id"`
	Epoch            string                    `json:"epoch"`
	Sequence         string                    `json:"sequence"` // durable sequences can exceed JavaScript's exact integers
	ReceivedAt       time.Time                 `json:"received_at"`
	AccountingDay    string                    `json:"accounting_day"`
	UpBytes          int64                     `json:"up_bytes"`
	DownBytes        int64                     `json:"down_bytes"`
	TotalsKnown      bool                      `json:"totals_known"`
	DiscardedSamples int                       `json:"discarded_samples"`
	MetadataStatus   string                    `json:"metadata_status"`
	Sample           *agentproto.TrafficSample `json:"sample,omitempty"`
}

type TrafficDeliveryView struct {
	SampledAt     time.Time  `json:"sampled_at"`
	PendingBytes  int64      `json:"pending_bytes"`
	BufferedBytes int64      `json:"buffered_bytes"`
	JournalBytes  int64      `json:"journal_bytes"`
	OldestAt      *time.Time `json:"oldest_at,omitempty"`
	ReceivedAt    time.Time  `json:"received_at"`
	FreshForMS    int64      `json:"fresh_for_ms"`
}

type TrafficHistoryPage struct {
	Items      []TrafficHistoryItem `json:"items"`
	NextBefore int64                `json:"next_before,omitempty"`
	Retention  struct {
		Days          int `json:"days"`
		MaxRows       int `json:"max_rows"`
		GlobalMaxRows int `json:"global_max_rows"`
	} `json:"retention"`
	Delivery *TrafficDeliveryView `json:"delivery,omitempty"`
}

func recordTrafficHistoryTx(ctx context.Context, tx *sql.Tx, nodeID int64, epoch string, sequence uint64, samples []TrafficSample, observation TrafficObservation, at time.Time) error {
	status, raw := "legacy", "null"
	if observation.Sample != nil {
		status = "invalid"
		if agentproto.ValidateTrafficSample(observation.Sample) == nil {
			status = "valid"
			encoded, _ := json.Marshal(observation.Sample)
			raw = string(encoded)
		}
	}
	var up, down int64
	totalsKnown := true
	for _, sample := range samples {
		// Existing billing validates individual rows. Never let an optional
		// aggregate overflow turn those valid rows into a rejected batch.
		if sample.Up < 0 || sample.Down < 0 || sample.Up > math.MaxInt64-up || sample.Down > math.MaxInt64-down {
			totalsKnown, up, down = false, 0, 0
			break
		}
		up += sample.Up
		down += sample.Down
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO node_traffic_history(node_id,token_hash,epoch,sequence,received_at,up_bytes,down_bytes,totals_known,discarded_samples,metadata_status,sample_json)
		SELECT id,COALESCE(NULLIF(?,''),token_hash,''),?,?,?,?,?,?,?,?,? FROM nodes WHERE id=?`, observation.TokenHash, epoch, int64(sequence), at.UnixMilli(), up, down, totalsKnown, max(0, observation.DiscardedSamples), status, raw, nodeID)
	if err != nil {
		return err
	}
	// A hard per-node bound also applies during delayed periodic maintenance.
	_, err = tx.ExecContext(ctx, `DELETE FROM node_traffic_history WHERE id = (SELECT id FROM node_traffic_history WHERE node_id=? ORDER BY id DESC LIMIT 1 OFFSET ?)`, nodeID, trafficHistoryMaxRows)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	// Monotonic IDs bound the global row count without a full-table COUNT or
	// a large delete. At most one eviction is needed per newly inserted row.
	_, err = tx.ExecContext(ctx, `DELETE FROM node_traffic_history WHERE id=(SELECT id FROM node_traffic_history WHERE id<=? ORDER BY id LIMIT 1)`, id-trafficHistoryGlobalMaxRows)
	return err
}

func recordTrafficDeliveryTx(ctx context.Context, tx *sql.Tx, nodeID int64, tokenHash string, delivery *agentproto.TrafficDelivery, at time.Time) error {
	if !agentproto.ValidateTrafficDelivery(delivery) || delivery.SampledAt.After(at.Add(30*time.Second)) {
		return nil
	}
	var oldToken, oldClock string
	err := tx.QueryRowContext(ctx, `SELECT token_hash,sampled_at FROM node_traffic_delivery WHERE node_id=?`, nodeID).Scan(&oldToken, &oldClock)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if oldToken == tokenHash && oldClock != "" {
		previous, err := time.Parse(time.RFC3339Nano, oldClock)
		if err != nil || !delivery.SampledAt.After(previous) {
			return nil
		}
	}
	raw, _ := json.Marshal(delivery)
	_, err = tx.ExecContext(ctx, `INSERT INTO node_traffic_delivery(node_id,token_hash,sampled_at,received_at,delivery_json) VALUES(?,?,?,?,?)
		ON CONFLICT(node_id) DO UPDATE SET token_hash=excluded.token_hash,sampled_at=excluded.sampled_at,received_at=excluded.received_at,delivery_json=excluded.delivery_json`, nodeID, tokenHash, delivery.SampledAt.UTC().Format(time.RFC3339Nano), at.UnixMilli(), string(raw))
	return err
}

func (s *Store) NodeTrafficHistory(ctx context.Context, nodeID, before int64, limit int, at time.Time) (TrafficHistoryPage, error) {
	page := TrafficHistoryPage{Items: []TrafficHistoryItem{}}
	page.Retention.Days, page.Retention.MaxRows = trafficHistoryDays, trafficHistoryMaxRows
	page.Retention.GlobalMaxRows = trafficHistoryGlobalMaxRows
	if nodeID <= 0 || before < 0 || limit < 1 || limit > 100 {
		return page, fmt.Errorf("invalid traffic history query")
	}
	// Identity and both observation projections must come from one snapshot.
	// A re-pairing between independent SELECTs must not mix old history with
	// missing or newly replaced current delivery evidence.
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return page, err
	}
	defer tx.Rollback()
	var token string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(token_hash,'') FROM nodes WHERE id=?`, nodeID).Scan(&token); err != nil {
		return page, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,epoch,sequence,received_at,up_bytes,down_bytes,totals_known,discarded_samples,metadata_status,sample_json FROM node_traffic_history WHERE node_id=? AND token_hash=? AND received_at>=? AND (?=0 OR id<?) ORDER BY id DESC LIMIT ?`, nodeID, token, at.Add(-trafficHistoryDays*24*time.Hour).UnixMilli(), before, before, limit+1)
	if err != nil {
		return page, err
	}
	for rows.Next() {
		var item TrafficHistoryItem
		var seq, received int64
		var raw string
		if err := rows.Scan(&item.ID, &item.Epoch, &seq, &received, &item.UpBytes, &item.DownBytes, &item.TotalsKnown, &item.DiscardedSamples, &item.MetadataStatus, &raw); err != nil {
			rows.Close()
			return page, err
		}
		item.Sequence = strconv.FormatInt(seq, 10)
		item.ReceivedAt = time.UnixMilli(received).UTC()
		item.AccountingDay = item.ReceivedAt.Format("2006-01-02")
		if item.MetadataStatus == "valid" {
			if json.Unmarshal([]byte(raw), &item.Sample) != nil || agentproto.ValidateTrafficSample(item.Sample) != nil {
				item.MetadataStatus, item.Sample = "invalid", nil
			}
		}
		page.Items = append(page.Items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return page, err
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		page.NextBefore = page.Items[len(page.Items)-1].ID
	}
	var raw string
	var received int64
	err = tx.QueryRowContext(ctx, `SELECT delivery_json,received_at FROM node_traffic_delivery WHERE node_id=? AND token_hash=?`, nodeID, token).Scan(&raw, &received)
	if err == sql.ErrNoRows {
		return page, nil
	}
	if err != nil {
		return page, err
	}
	var delivery agentproto.TrafficDelivery
	view := &TrafficDeliveryView{ReceivedAt: time.UnixMilli(received).UTC()}
	if json.Unmarshal([]byte(raw), &delivery) == nil && agentproto.ValidateTrafficDelivery(&delivery) {
		view.SampledAt, view.PendingBytes, view.BufferedBytes, view.JournalBytes, view.OldestAt = delivery.SampledAt, delivery.PendingBytes, delivery.BufferedBytes, delivery.JournalBytes, delivery.OldestAt
		view.FreshForMS = max(0, min((3*time.Minute).Milliseconds(), view.ReceivedAt.Add(3*time.Minute).Sub(at).Milliseconds()))
		// Clock disagreement withdraws current status. It does not rewrite the
		// agent timestamp or manufacture a cross-clock delivery latency.
		if view.ReceivedAt.After(at.Add(30*time.Second)) || view.SampledAt.After(view.ReceivedAt.Add(30*time.Second)) {
			view.FreshForMS = 0
		} else {
			view.FreshForMS = max(0, min(view.FreshForMS, view.SampledAt.Add(3*time.Minute).Sub(at).Milliseconds()))
		}
		page.Delivery = view
	}
	return page, nil
}
