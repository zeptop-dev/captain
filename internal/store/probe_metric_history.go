package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

// ProbeMetricAggregate preserves measurements, not estimates reconstructed from
// averaged legacy buckets. The sample times are accepted receipt times.
type ProbeMetricAggregate struct {
	Count   int     `json:"n"`
	Sum     float64 `json:"sum"`
	Min     float64 `json:"min"`
	Max     float64 `json:"max"`
	First   float64 `json:"first"`
	Last    float64 `json:"last"`
	FirstAt int64   `json:"first_at"`
	LastAt  int64   `json:"last_at"`
}
type ProbeMetricBucket struct {
	TS     int64
	Values map[string]ProbeMetricAggregate
}

func probeMetricValues(h spec.SystemStatus, at time.Time) map[string]ProbeMetricAggregate {
	out := map[string]ProbeMetricAggregate{}
	v := h.Validity()
	add := func(k string, n float64, valid bool) {
		if valid && !math.IsNaN(n) && !math.IsInf(n, 0) && n >= 0 {
			out[k] = ProbeMetricAggregate{Count: 1, Sum: n, Min: n, Max: n, First: n, Last: n, FirstAt: at.UnixMilli(), LastAt: at.UnixMilli()}
		}
	}
	add("cpu.usage", h.CPUPercent, v.CPU)
	add("memory.used", float64(h.MemUsed), v.Memory)
	add("captain.memory.total", float64(h.MemTotal), v.Memory)
	add("swap.used", float64(h.SwapUsed), v.Swap)
	add("captain.swap.total", float64(h.SwapTotal), v.Swap)
	add("disk.used", float64(h.DiskUsed), v.Disk)
	add("captain.disk.total", float64(h.DiskTotal), v.Disk)
	add("load.average", h.Load1, v.Load)
	add("captain.load.5", h.Load5, v.Load)
	add("captain.load.15", h.Load15, v.Load)
	add("net.in.rate", float64(h.NetDown), v.Network)
	add("net.out.rate", float64(h.NetUp), v.Network)
	add("net.total.up", float64(h.NetTotalUp), v.Network)
	add("net.total.down", float64(h.NetTotalDown), v.Network)
	add("process.count", float64(h.Processes), v.Processes)
	add("connections.tcp", float64(h.TCP), v.Connections)
	add("connections.udp", float64(h.UDP), v.Connections)
	add("captain.uptime", float64(h.Uptime), v.Uptime)
	return out
}
func mergeProbeMetric(a, b ProbeMetricAggregate) ProbeMetricAggregate {
	if a.Count == 0 {
		return b
	}
	if b.Count == 0 {
		return a
	}
	a.Count += b.Count
	a.Sum += b.Sum
	a.Min = min(a.Min, b.Min)
	a.Max = max(a.Max, b.Max)
	if b.FirstAt < a.FirstAt {
		a.FirstAt, a.First = b.FirstAt, b.First
	}
	if b.LastAt >= a.LastAt {
		a.LastAt, a.Last = b.LastAt, b.Last
	}
	return a
}
func recordProbeMetricHistory(ctx context.Context, tx *sql.Tx, nodeID int64, h spec.SystemStatus, at time.Time) error {
	return recordProbeMetricValues(ctx, tx, nodeID, probeMetricValues(h, at), at)
}
func recordProbeMetricValues(ctx context.Context, tx *sql.Tx, nodeID int64, values map[string]ProbeMetricAggregate, at time.Time) error {
	if len(values) == 0 {
		return nil
	}
	// Keep a bounded exact recent window as well as long-term rollups. All are
	// written in the accepted-beat transaction, including token/sequence checks.
	for _, b := range []struct {
		res  string
		step time.Duration
	}{{"r", time.Second}, {"m", time.Minute}, {"h", time.Hour}, {"d", 24 * time.Hour}} {
		ts := at.UTC().Truncate(b.step).Unix()
		merged := map[string]ProbeMetricAggregate{}
		var raw string
		err := tx.QueryRowContext(ctx, `SELECT values_json FROM node_probe_metric_stats WHERE node_id=? AND res=? AND ts=?`, nodeID, b.res, ts).Scan(&raw)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if raw != "" {
			if err = json.Unmarshal([]byte(raw), &merged); err != nil {
				return err
			}
		}
		for k, v := range values {
			merged[k] = mergeProbeMetric(merged[k], v)
		}
		encoded, err := json.Marshal(merged)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO node_probe_metric_stats(node_id,res,ts,values_json) VALUES(?,?,?,?) ON CONFLICT(node_id,res,ts) DO UPDATE SET values_json=excluded.values_json`, nodeID, b.res, ts, string(encoded)); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) ProbeMetricHistory(ctx context.Context, nodeID int64, res string, from, to time.Time) ([]ProbeMetricBucket, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT ts,values_json FROM node_probe_metric_stats WHERE node_id=? AND res=? AND ts>=? AND ts<=? ORDER BY ts`, nodeID, res, from.Unix(), to.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProbeMetricBucket{}
	for rows.Next() {
		var b ProbeMetricBucket
		var raw string
		if err = rows.Scan(&b.TS, &raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &b.Values); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
func (s *Store) pruneProbeMetricHistory(ctx context.Context, at time.Time) error {
	for _, r := range []struct {
		res  string
		keep time.Duration
	}{{"r", 2 * time.Hour}, {"m", 48 * time.Hour}, {"h", 60 * 24 * time.Hour}, {"d", 730 * 24 * time.Hour}} {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM node_probe_metric_stats WHERE res=? AND ts<?`, r.res, at.Add(-r.keep).Unix()); err != nil {
			return err
		}
	}
	return nil
}
