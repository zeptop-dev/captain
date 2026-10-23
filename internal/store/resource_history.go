package store

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

// Bounds apply to both samples and merged buckets: changing device names must
// not make hourly/day buckets grow without limit. Host summaries come first.
const maxResourceSeries = 512
const maxDeviceSeries = maxResourceSeries - 16 // reserve room for summaries that become available later

type resourceAggregate struct {
	N    int     `json:"n"`
	Sum  float64 `json:"s"`
	Peak float64 `json:"p"`
}
type resourceBucket struct {
	Values    map[string]resourceAggregate `json:"v"`
	Truncated bool                         `json:"truncated,omitempty"`
}

func resourceValues(h spec.SystemStatus) resourceBucket {
	b := resourceBucket{Values: make(map[string]resourceAggregate)}
	devices := 0
	add := func(kind, device, metric string, value *float64) {
		if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) || (*value < 0 && !(kind == "gpu" && metric == "temperature" && *value >= -100)) {
			return
		}
		if len(device) > 256 {
			b.Truncated = true
			return
		}
		key := kind + ":" + url.QueryEscape(device) + ":" + metric
		if _, exists := b.Values[key]; exists {
			return
		}
		if len(b.Values) >= maxResourceSeries || (kind != "host" && devices >= maxDeviceSeries) {
			b.Truncated = true
			return
		}
		b.Values[key] = resourceAggregate{N: 1, Sum: *value, Peak: *value}
		if kind != "host" {
			devices++
		}
	}
	host := func(metric string, value float64, valid bool) {
		if valid {
			add("host", "", metric, &value)
		}
	}
	v := h.Validity()
	host("cpu", h.CPUPercent, v.CPU)
	if h.MemTotal > 0 {
		host("memory", 100*float64(h.MemUsed)/float64(h.MemTotal), v.Memory)
	}
	if h.SwapTotal > 0 {
		host("swap", 100*float64(h.SwapUsed)/float64(h.SwapTotal), v.Swap)
	}
	if h.DiskTotal > 0 {
		host("disk", 100*float64(h.DiskUsed)/float64(h.DiskTotal), v.Disk)
	}
	host("up", float64(h.NetUp), v.Network)
	host("down", float64(h.NetDown), v.Network)
	host("load", h.Load1, v.Load)
	host("tcp", float64(h.TCP), v.Connections)
	host("udp", float64(h.UDP), v.Connections)
	host("processes", float64(h.Processes), v.Processes)
	r := h.Resources
	if r == nil {
		return b
	}
	// Prioritize network/storage over per-core series on large hosts.
	for _, n := range r.Networks {
		add("network", n.Name, "up", n.UpRate)
		add("network", n.Name, "down", n.DownRate)
	}
	for _, f := range r.Filesystems {
		if f.Used != nil && f.Total != nil && *f.Total > 0 && *f.Used <= *f.Total {
			// Keep the measured byte pair. A past percentage cannot be converted
			// using today's capacity when a volume has been resized or replaced.
			used, total := float64(*f.Used), float64(*f.Total)
			add("filesystem", f.Mount, "used", &used)
			add("filesystem", f.Mount, "total", &total)
			x := 100 * float64(*f.Used) / float64(*f.Total)
			add("filesystem", f.Mount, "disk", &x)
		}
		if f.InodesUsed != nil && f.InodesTotal != nil && *f.InodesTotal > 0 {
			x := 100 * float64(*f.InodesUsed) / float64(*f.InodesTotal)
			add("filesystem", f.Mount, "inodes", &x)
		}
	}
	for _, d := range r.Disks {
		add("disk", d.Name, "read", d.ReadRate)
		add("disk", d.Name, "write", d.WriteRate)
		add("disk", d.Name, "readIOPS", d.ReadIOPS)
		add("disk", d.Name, "writeIOPS", d.WriteIOPS)
	}
	if g := r.GPU; g != nil && (g.State == "ok" || g.State == "partial") {
		for i, d := range g.Devices {
			if i >= 32 {
				b.Truncated = true
				break
			}
			if d.ID == "" {
				continue
			}
			if d.Utilization != nil && *d.Utilization <= 100 {
				add("gpu", d.ID, "utilization", d.Utilization)
			}
			if d.MemoryUsed != nil && *d.MemoryUsed <= 1<<50 {
				x := float64(*d.MemoryUsed)
				add("gpu", d.ID, "vram", &x)
				if d.MemoryTotal != nil && *d.MemoryTotal > 0 && *d.MemoryUsed <= *d.MemoryTotal {
					pct := 100 * x / float64(*d.MemoryTotal)
					add("gpu", d.ID, "vramPct", &pct)
				}
			}
			if d.Temperature != nil && *d.Temperature <= 250 {
				add("gpu", d.ID, "temperature", d.Temperature)
			}
			if d.Power != nil && *d.Power <= 10000 {
				add("gpu", d.ID, "power", d.Power)
			}
		}
		b.Truncated = b.Truncated || g.Truncated
	}
	for _, p := range r.Processes {
		add("process", p.Name, "cpu", p.CPU)
		if p.RSS != nil {
			x := float64(*p.RSS)
			add("process", p.Name, "rss", &x)
		}
	}
	for _, c := range r.CPUs {
		add("cpu", c.Name, "cpu", c.Percent)
	}
	return b
}

func decodeResourceBucket(data []byte) (resourceBucket, error) {
	var b resourceBucket
	z, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return b, err
	}
	defer z.Close()
	err = json.NewDecoder(io.LimitReader(z, 1<<20)).Decode(&b)
	if b.Values == nil {
		b.Values = make(map[string]resourceAggregate)
	}
	return b, err
}

// BestSpeed's compressor has a large scratch buffer. Reuse it so every
// three-resolution beat does not allocate several megabytes of transient RAM.
var resourceCompressors = sync.Pool{New: func() any {
	z, _ := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed)
	return z
}}

func encodeResourceBucket(b resourceBucket) ([]byte, error) {
	var buf bytes.Buffer
	z := resourceCompressors.Get().(*gzip.Writer)
	z.Reset(&buf)
	defer func() { z.Reset(io.Discard); resourceCompressors.Put(z) }()
	if err := json.NewEncoder(z).Encode(b); err != nil {
		z.Close()
		return nil, err
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Called inside RecordBeat's transaction, after replay detection. The same
// receiver timestamp is used for all resolutions; agent clock skew is ignored.
func recordResourceHistory(ctx context.Context, tx *sql.Tx, nodeID int64, h spec.SystemStatus, at time.Time, previous networkCounters) error {
	sample := resourceValues(h)
	if r := h.Resources; r != nil {
		for _, f := range r.Filesystems {
			old := int64(0)
			if previous.Epoch == r.Epoch && r.Epoch != "" {
				old = previous.Filesystems[f.ID]
			}
			if !freshFilesystem(f, r, old) {
				delete(sample.Values, "filesystem:"+url.QueryEscape(f.Mount)+":used")
				delete(sample.Values, "filesystem:"+url.QueryEscape(f.Mount)+":total")
				delete(sample.Values, "filesystem:"+url.QueryEscape(f.Mount)+":disk")
				delete(sample.Values, "filesystem:"+url.QueryEscape(f.Mount)+":inodes")
			}
		}
	}
	if !freshGPU(h.Resources, previous.GPU) {
		for key := range sample.Values {
			if strings.HasPrefix(key, "gpu:") {
				delete(sample.Values, key)
			}
		}
	}
	keys := make([]string, 0, len(sample.Values))
	for key := range sample.Values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	// Reserve summary series before introducing any new device series.
	sort.SliceStable(keys, func(i, j int) bool {
		return strings.HasPrefix(keys[i], "host:") && !strings.HasPrefix(keys[j], "host:")
	})
	for _, res := range []struct {
		name string
		size time.Duration
	}{{"m", time.Minute}, {"h", time.Hour}, {"d", 24 * time.Hour}} {
		ts := at.UTC().Truncate(res.size).Unix()
		var data []byte
		err := tx.QueryRowContext(ctx, `SELECT data FROM node_resource_stats WHERE node_id=? AND res=? AND ts=?`, nodeID, res.name, ts).Scan(&data)
		b := resourceBucket{Values: make(map[string]resourceAggregate)}
		if err == nil {
			b, err = decodeResourceBucket(data)
		} else if errors.Is(err, sql.ErrNoRows) {
			err = nil
		}
		if err != nil {
			return err
		}
		b.Truncated = b.Truncated || sample.Truncated
		devices := 0
		for k := range b.Values {
			if !strings.HasPrefix(k, "host:") {
				devices++
			}
		}
		for _, key := range keys {
			x := sample.Values[key]
			prev, exists := b.Values[key]
			isDevice := !strings.HasPrefix(key, "host:")
			if !exists && (len(b.Values) >= maxResourceSeries || (isDevice && devices >= maxDeviceSeries)) {
				b.Truncated = true
				continue
			}
			if !exists && isDevice {
				devices++
			}
			if prev.N == 0 || x.Peak > prev.Peak {
				prev.Peak = x.Peak
			}
			prev.N += x.N
			prev.Sum += x.Sum
			b.Values[key] = prev
		}
		data, err = encodeResourceBucket(b)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO node_resource_stats(node_id,res,ts,data) VALUES(?,?,?,?) ON CONFLICT(node_id,res,ts) DO UPDATE SET data=excluded.data`, nodeID, res.name, ts, data); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(MonitorSnapshot(h, at))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE nodes SET monitor_host_json=?, monitor_at=? WHERE id=?`, string(raw), at.Unix(), nodeID)
	return err
}

type ResourceSeries struct {
	Key    string `json:"key"`
	Kind   string `json:"kind"`
	Device string `json:"device"`
	Metric string `json:"metric"`
}
type ResourcePoint struct {
	TS      int64    `json:"ts"`
	Average *float64 `json:"average"`
	Peak    *float64 `json:"peak"`
	Samples int      `json:"samples"`
}
type ResourceHistory struct {
	Step      int64            `json:"step"`
	Points    []ResourcePoint  `json:"points"`
	Series    []ResourceSeries `json:"series"`
	Truncated bool             `json:"truncated"`
}

// ResourceRange deliberately bounds queries to the detailed retention window.
func ResourceRange(r string) (res string, step, window time.Duration, ok bool) {
	switch r {
	case "", "1h":
		return "m", time.Minute, time.Hour, true
	case "24h":
		return "m", time.Minute, 24 * time.Hour, true
	case "7d":
		return "h", time.Hour, 7 * 24 * time.Hour, true
	case "14d":
		return "h", time.Hour, 14 * 24 * time.Hour, true
	case "30d":
		return "d", 24 * time.Hour, 30 * 24 * time.Hour, true
	case "90d":
		return "d", 24 * time.Hour, 90 * 24 * time.Hour, true
	}
	return "", 0, 0, false
}

func (s *Store) ResourceHistory(ctx context.Context, nodeID int64, span, key string, at time.Time) (ResourceHistory, error) {
	res, step, window, valid := ResourceRange(span)
	out := ResourceHistory{Series: []ResourceSeries{}, Points: []ResourcePoint{}}
	if !valid {
		return out, errors.New("invalid history range")
	}
	if key == "" {
		key = "host::cpu"
	}
	end := at.UTC().Truncate(step)
	start := end.Add(-window).Add(step)
	out.Step = int64(step / time.Second)
	rows, err := s.db.QueryContext(ctx, `SELECT ts,data FROM node_resource_stats WHERE node_id=? AND res=? AND ts>=? AND ts<=? ORDER BY ts`, nodeID, res, start.Unix(), end.Unix())
	if err != nil {
		return out, err
	}
	defer rows.Close()
	points := make(map[int64]ResourcePoint)
	catalog := make(map[string]bool)
	for rows.Next() {
		var ts int64
		var raw []byte
		if err := rows.Scan(&ts, &raw); err != nil {
			return out, err
		}
		b, err := decodeResourceBucket(raw)
		if err != nil {
			return out, err
		}
		out.Truncated = out.Truncated || b.Truncated
		for k := range b.Values {
			if len(catalog) < 1024 || catalog[k] {
				catalog[k] = true
			} else {
				out.Truncated = true
			}
		}
		if x, ok := b.Values[key]; ok && x.N > 0 {
			avg, peak := x.Sum/float64(x.N), x.Peak
			points[ts] = ResourcePoint{TS: ts, Average: &avg, Peak: &peak, Samples: x.N}
		}
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	for ts := start.Unix(); ts <= end.Unix(); ts += out.Step {
		p, ok := points[ts]
		if !ok {
			p = ResourcePoint{TS: ts}
		}
		out.Points = append(out.Points, p)
	}
	for k := range catalog {
		parts := strings.Split(k, ":")
		if len(parts) != 3 {
			continue
		}
		device, _ := url.QueryUnescape(parts[1])
		out.Series = append(out.Series, ResourceSeries{Key: k, Kind: parts[0], Device: device, Metric: parts[2]})
	}
	sort.Slice(out.Series, func(i, j int) bool { return out.Series[i].Key < out.Series[j].Key })
	return out, nil
}

func (s *Store) pruneResourceHistory(ctx context.Context, at time.Time) error {
	for _, r := range []struct {
		res  string
		keep time.Duration
	}{{"m", 24 * time.Hour}, {"h", 14 * 24 * time.Hour}, {"d", 90 * 24 * time.Hour}} {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM node_resource_stats WHERE res=? AND ts<?`, r.res, at.Add(-r.keep).Unix()); err != nil {
			return err
		}
	}
	return nil
}

type MonitorNode struct {
	Stale     bool               `json:"stale"`
	ID        int64              `json:"id"`
	Name      string             `json:"name"`
	Group     string             `json:"group"`
	Paired    bool               `json:"paired"`
	Online    bool               `json:"online"`
	LastSeen  *time.Time         `json:"last_seen"`
	SampledAt int64              `json:"sampled_at"`
	Info      NodeProbeInfo      `json:"info"`
	Host      *spec.SystemStatus `json:"host"`
}

// The persisted snapshot survives panel restarts. Receipt time is independent
// from liveness: a recent status/task request must not make old metrics fresh.
func (s *Store) MonitorNodes(ctx context.Context, at time.Time, grace time.Duration, nodeIDs ...int64) ([]MonitorNode, error) {
	query := `SELECT id,name,monitor_group,token_hash,last_seen_at,probe_info_json,monitor_at,monitor_host_json,host_reported_at,host_status_json,region_mode,address_json,region_detection_json FROM nodes`
	var args []any
	if len(nodeIDs) > 0 {
		query += ` WHERE id=?`
		args = append(args, nodeIDs[0])
	}
	rows, err := s.db.QueryContext(ctx, query+` ORDER BY display_order,id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MonitorNode{}
	for rows.Next() {
		var n MonitorNode
		var token sql.NullString
		var info, beatHost, reportHost, regionMode, addressJSON, regionJSON string
		var last sql.NullInt64
		var beatAt, reportAt int64
		if err := rows.Scan(&n.ID, &n.Name, &n.Group, &token, &last, &info, &beatAt, &beatHost, &reportAt, &reportHost, &regionMode, &addressJSON, &regionJSON); err != nil {
			return nil, err
		}
		n.Paired = token.Valid && token.String != ""
		if last.Valid {
			t := time.Unix(last.Int64, 0)
			n.LastSeen = &t
			n.Online = n.Paired && at.Sub(t) <= grace
		}
		_ = json.Unmarshal([]byte(info), &n.Info)
		var obs AddressObservation
		var detection RegionDetection
		_ = json.Unmarshal([]byte(addressJSON), &obs)
		_ = json.Unmarshal([]byte(regionJSON), &detection)
		resolveRegion(&n.Info, regionMode, obs, detection)
		n.SampledAt = beatAt
		raw := beatHost
		if reportAt > beatAt {
			n.SampledAt = reportAt
			raw = reportHost
		}
		if n.SampledAt > 0 {
			var h spec.SystemStatus
			if json.Unmarshal([]byte(raw), &h) == nil {
				if reportAt > beatAt {
					var beat spec.SystemStatus
					if json.Unmarshal([]byte(beatHost), &beat) == nil {
						h.Pings = beat.Pings
					}
				}
				n.Host = &h
			}
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
