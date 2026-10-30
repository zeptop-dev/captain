package store

import (
	"context"
	"encoding/hex"
	"fmt"
	"math"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

func ValidTrafficReceipt(epoch string, seq uint64) bool {
	if seq > math.MaxInt64 {
		return false
	}
	if epoch == "" {
		return true
	}
	_, err := hex.DecodeString(epoch)
	return len(epoch) == 32 && err == nil && seq > 0
}

// AcceptNodeTraffic commits the receipt, customer charge and node counters as
// one unit. Epoch cursors are retained until the node is deleted; a delayed
// old journal must never become a fresh batch after an agent reinstall.
func (s *Store) AcceptNodeTraffic(ctx context.Context, nodeID int64, epoch string, seq uint64, samples []TrafficSample, in map[int64]spec.Traffic, out map[string]spec.Traffic, at time.Time, observations ...TrafficObservation) ([]int64, bool, error) {
	if !ValidTrafficReceipt(epoch, seq) {
		return nil, false, fmt.Errorf("invalid traffic receipt")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	if seq != 0 {
		var count int64
		if epoch != "" {
			res, err := tx.ExecContext(ctx, `INSERT INTO node_traffic_receipts (node_id, epoch, seq) VALUES (?, ?, ?)
				ON CONFLICT(node_id, epoch) DO UPDATE SET seq = excluded.seq WHERE node_traffic_receipts.seq < excluded.seq`, nodeID, epoch, int64(seq))
			if err != nil {
				return nil, false, err
			}
			count, err = res.RowsAffected()
			if err != nil {
				return nil, false, err
			}
		} else {
			res, err := tx.ExecContext(ctx, `UPDATE nodes SET traffic_seq = ? WHERE id = ? AND (traffic_seq < ? OR traffic_seq - ? > ?)`, int64(seq), nodeID, int64(seq), int64(seq), int64(seqRestartGap))
			if err != nil {
				return nil, false, err
			}
			count, err = res.RowsAffected()
			if err != nil {
				return nil, false, err
			}
		}
		if count == 0 {
			return nil, true, nil
		}
	}
	first, err := addTrafficSamplesTx(ctx, tx, samples, at)
	if err != nil {
		return nil, false, err
	}
	if err := addProxyTrafficTx(ctx, tx, nodeID, samples, at); err != nil {
		return nil, false, err
	}
	day := at.UTC().Truncate(24 * time.Hour).Unix()
	for id, t := range in {
		if _, err := tx.ExecContext(ctx, `INSERT INTO inbound_traffic_daily (inbound_id, day, up_bytes, down_bytes) VALUES (?, ?, ?, ?)
			ON CONFLICT(inbound_id, day) DO UPDATE SET up_bytes = up_bytes + excluded.up_bytes, down_bytes = down_bytes + excluded.down_bytes`, id, day, t.Up, t.Down); err != nil {
			return nil, false, err
		}
	}
	for tag, t := range out {
		if _, err := tx.ExecContext(ctx, `INSERT INTO outbound_traffic_daily (node_id, tag, day, up_bytes, down_bytes) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(node_id, tag, day) DO UPDATE SET up_bytes = up_bytes + excluded.up_bytes, down_bytes = down_bytes + excluded.down_bytes`, nodeID, tag, day, t.Up, t.Down); err != nil {
			return nil, false, err
		}
	}
	var observation TrafficObservation
	if len(observations) > 0 {
		observation = observations[0]
	}
	if err := recordTrafficHistoryTx(ctx, tx, nodeID, epoch, seq, samples, observation, at); err != nil {
		return nil, false, err
	}
	return first, false, tx.Commit()
}
