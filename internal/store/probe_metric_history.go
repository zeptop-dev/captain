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
func recordProbeMetricValues(ctx context.Context, tx *sql.Tx, nodeID int64, values map[string]ProbeMetricAggregate, at time.Time) error {
	if len(values) == 0 {
		return nil
	}
	// Match the legacy calendar-day boundary, including its timezone. Otherwise
	// old and new buckets for the same day appear disjoint during fallback.
	year, month, day := at.Date()
	stamps := []int64{at.Truncate(time.Second).Unix(), at.Truncate(time.Minute).Unix(), at.Truncate(time.Hour).Unix(), time.Date(year, month, day, 0, 0, 0, 0, at.Location()).Unix()}
	resolutions := []string{"r", "m", "h", "d"}
	buckets := map[string]map[string]ProbeMetricAggregate{}
	// Four fixed buckets share one read and one UPSERT. Do not perform one
	// SQL transaction round-trip per metric or per resolution on each beat.
	rows, err := tx.QueryContext(ctx, `SELECT res,values_json FROM node_probe_metric_stats WHERE node_id=? AND ((res='r' AND ts=?) OR (res='m' AND ts=?) OR (res='h' AND ts=?) OR (res='d' AND ts=?))`, nodeID, stamps[0], stamps[1], stamps[2], stamps[3])
	if err != nil {
		return err
	}
	for rows.Next() {
		var res, raw string
		if err = rows.Scan(&res, &raw); err != nil {
			rows.Close()
			return err
		}
		var bucket map[string]ProbeMetricAggregate
		if err = json.Unmarshal([]byte(raw), &bucket); err != nil {
			rows.Close()
			return err
		}
		buckets[res] = bucket
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	args := make([]any, 0, 16)
	for i, res := range resolutions {
		merged := buckets[res]
		if merged == nil {
			merged = map[string]ProbeMetricAggregate{}
		}
		for key, value := range values {
			merged[key] = mergeProbeMetric(merged[key], value)
		}
		encoded, err := json.Marshal(merged)
		if err != nil {
			return err
		}
		args = append(args, nodeID, res, stamps[i], string(encoded))
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO node_probe_metric_stats(node_id,res,ts,values_json) VALUES(?,?,?,?),(?,?,?,?),(?,?,?,?),(?,?,?,?) ON CONFLICT(node_id,res,ts) DO UPDATE SET values_json=excluded.values_json`, args...)
	return err
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
