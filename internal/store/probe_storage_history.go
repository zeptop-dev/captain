package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"net/url"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

// PublicStorageHistory never contains filesystem keys, mount paths or device
// identifiers. Every volume is selected by the current private display policy.
type PublicStorageHistory struct {
	Res     string                       `json:"res"`
	From    int64                        `json:"from"`
	To      int64                        `json:"to"`
	Step    int64                        `json:"step"`
	Volumes []PublicStorageHistoryVolume `json:"volumes"`
}

type PublicStorageHistoryVolume struct {
	ID     string                      `json:"id"`
	Name   string                      `json:"name"`
	Kind   string                      `json:"kind"`
	Points []PublicStorageHistoryPoint `json:"points"`
}

type PublicStorageHistoryPoint struct {
	TS    int64    `json:"ts"`
	Used  *float64 `json:"used"`
	Total *float64 `json:"total"`
}

var ErrStorageHistoryRange = errors.New("invalid storage history range")

// StorageHistoryRange respects detailed retention even for a short custom
// window in the distant past. Each query has at most 1441 source buckets.
func StorageHistoryRange(from, to, now time.Time) (res string, step time.Duration, err error) {
	if from.IsZero() || !to.After(from) || to.After(now) || from.Before(now.Add(-90*24*time.Hour)) {
		return "", 0, ErrStorageHistoryRange
	}
	if !from.Before(now.Add(-24 * time.Hour)) {
		return "m", time.Minute, nil
	}
	if !from.Before(now.Add(-14 * 24 * time.Hour)) {
		return "h", time.Hour, nil
	}
	return "d", 24 * time.Hour, nil
}

// StorageHistory reads the display policy, latest host and historical values
// from one database snapshot. Decode each compressed bucket only once for all
// volumes. Missing bytes stay null: older percent-only buckets cannot safely
// be expanded using the volume's current capacity.
func (s *Store) StorageHistory(ctx context.Context, nodeID int64, from, to, now time.Time, staff bool) (PublicStorageHistory, error) {
	out := PublicStorageHistory{Volumes: []PublicStorageHistoryVolume{}}
	res, step, err := StorageHistoryRange(from, to, now)
	if err != nil {
		return out, err
	}
	start, end := from.UTC().Truncate(step), to.UTC().Truncate(step)
	out.Res, out.Step, out.From, out.To = res, int64(step/time.Second), start.Unix(), end.Unix()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	var hidden bool
	var storageJSON, beatJSON, reportJSON string
	var beatAt, reportAt int64
	var token sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT probe_hidden,probe_storage_json,monitor_at,monitor_host_json,host_reported_at,host_status_json,token_hash FROM nodes WHERE id=?`, nodeID).Scan(&hidden, &storageJSON, &beatAt, &beatJSON, &reportAt, &reportJSON, &token)
	if err != nil {
		return out, err
	}
	if hidden && !staff || !token.Valid || token.String == "" {
		return out, sql.ErrNoRows
	}
	var policy NodeStorage
	if err := json.Unmarshal([]byte(storageJSON), &policy); err != nil {
		return out, err
	}
	if err := policy.Validate(); err != nil {
		return out, err
	}
	var host spec.SystemStatus
	if reportAt > beatAt {
		beatAt, beatJSON = reportAt, reportJSON
	}
	// An unavailable snapshot retains the legacy root candidate, and missing
	// manual paths still retain their configured public display positions.
	_ = json.Unmarshal([]byte(beatJSON), &host)
	candidates := StorageCandidates(&host, time.Unix(beatAt, 0), now)
	items, visible := policy.selectedItems(candidates), policy.Public(candidates)
	if len(items) == 0 {
		return out, tx.Commit()
	}
	keys := make([]string, len(items))
	root := -1
	for i, item := range items {
		keys[i] = "filesystem:" + url.QueryEscape(item.Mount) + ":"
		if item.Mount == "/" {
			root = i
		}
		v := visible[i]
		volume := PublicStorageHistoryVolume{ID: v.ID, Name: v.Name, Kind: v.Kind, Points: []PublicStorageHistoryPoint{}}
		for ts := out.From; ts <= out.To; ts += out.Step {
			volume.Points = append(volume.Points, PublicStorageHistoryPoint{TS: ts})
		}
		out.Volumes = append(out.Volumes, volume)
	}
	rows, err := tx.QueryContext(ctx, `SELECT ts,data FROM node_resource_stats WHERE node_id=? AND res=? AND ts>=? AND ts<=? ORDER BY ts`, nodeID, res, out.From, out.To)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var ts int64
		var raw []byte
		if err = rows.Scan(&ts, &raw); err != nil {
			break
		}
		var bucket resourceBucket
		if bucket, err = decodeResourceBucket(raw); err != nil {
			break
		}
		index := (ts - out.From) / out.Step
		if index < 0 || index >= int64(len(out.Volumes[0].Points)) || (ts-out.From)%out.Step != 0 {
			continue
		}
		for i, key := range keys {
			used, total := bucket.Values[key+"used"], bucket.Values[key+"total"]
			// Both metrics must represent the same observations; a truncated
			// bucket must not combine a partial pair into an invented capacity.
			if used.N <= 0 || used.N != total.N {
				continue
			}
			u, t := used.Sum/float64(used.N), total.Sum/float64(total.N)
			if validStorageHistoryPair(u, t) {
				out.Volumes[i].Points[index].Used, out.Volumes[i].Points[index].Total = &u, &t
			}
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return out, err
	}
	// Old agents and pre-upgrade root history already have measured byte
	// values in node_stats. This fallback is only reachable for a selected root.
	if root >= 0 {
		rows, err = tx.QueryContext(ctx, `SELECT ts,disk_used,disk_total,disk_n FROM node_stats WHERE node_id=? AND res=? AND ts>=? AND ts<=? ORDER BY ts`, nodeID, res, out.From, out.To)
		if err != nil {
			return out, err
		}
		for rows.Next() {
			var ts int64
			var used, total float64
			var samples int
			if err = rows.Scan(&ts, &used, &total, &samples); err != nil {
				break
			}
			index := (ts - out.From) / out.Step
			if samples <= 0 || index < 0 || index >= int64(len(out.Volumes[root].Points)) || (ts-out.From)%out.Step != 0 {
				continue
			}
			used /= float64(samples)
			point := &out.Volumes[root].Points[index]
			if point.Used == nil && validStorageHistoryPair(used, total) {
				point.Used, point.Total = &used, &total
			}
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return out, err
		}
	}
	return out, tx.Commit()
}

func validStorageHistoryPair(used, total float64) bool {
	return !math.IsNaN(used) && !math.IsInf(used, 0) && !math.IsNaN(total) && !math.IsInf(total, 0) && used >= 0 && total > 0 && used <= total
}
