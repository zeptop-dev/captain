package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

type phaseAggregate struct {
	N   int     `json:"n"`
	Sum float64 `json:"sum"`
}
type networkAggregate struct {
	N         int               `json:"n"`
	Type      string            `json:"type"`
	Histogram map[int]int       `json:"histogram,omitempty"`
	Min       *float64          `json:"min,omitempty"`
	Max       *float64          `json:"max,omitempty"`
	JitterSum float64           `json:"jitter_sum,omitempty"`
	JitterN   int               `json:"jitter_n,omitempty"`
	Phases    [4]phaseAggregate `json:"phases"`
	Outcomes  map[string]int    `json:"outcomes,omitempty"`
	Skipped   uint64            `json:"skipped,omitempty"`
}

type PingStatsQuality struct {
	Samples  int               `json:"samples"`
	Type     string            `json:"type"`
	Min      *float64          `json:"min_ms"`
	Max      *float64          `json:"max_ms"`
	P50      *float64          `json:"p50_ms"`
	P95      *float64          `json:"p95_ms"`
	Jitter   *float64          `json:"jitter_ms"`
	Timings  spec.ProbeTimings `json:"timings"`
	Outcomes map[string]int    `json:"outcomes"`
	Skipped  uint64            `json:"skipped"`
}

// A sparse logarithmic histogram bounds storage regardless of retention.
// Quantiles are upper bucket bounds (at most 5% or 0.1 ms above the sample).
func latencyBin(ms float64) int {
	if ms == 0 {
		return 0
	}
	return 1 + max(0, int(math.Ceil(math.Log(ms/.1)/math.Log(1.05))))
}
func (a networkAggregate) quantile(q float64) *float64 {
	count := 0
	keys := []int{}
	for k, n := range a.Histogram {
		count += n
		keys = append(keys, k)
	}
	if count == 0 {
		return nil
	}
	sort.Ints(keys)
	want := int(math.Ceil(float64(count) * q))
	seen := 0
	for _, k := range keys {
		seen += a.Histogram[k]
		if seen >= want {
			v := 0.0
			if k > 0 {
				v = .1 * math.Pow(1.05, float64(k-1))
			}
			if a.Max != nil {
				v = min(v, *a.Max)
			}
			return &v
		}
	}
	return nil
}
func (a networkAggregate) summary() *PingStatsQuality {
	if a.N == 0 {
		return nil
	}
	out := &PingStatsQuality{Samples: a.N, Type: a.Type, Min: a.Min, Max: a.Max, P50: a.quantile(.5), P95: a.quantile(.95), Outcomes: a.Outcomes, Skipped: a.Skipped}
	if a.JitterN > 0 {
		v := a.JitterSum / float64(a.JitterN)
		out.Jitter = &v
	}
	avg := func(i int) *float64 {
		if a.Phases[i].N == 0 {
			return nil
		}
		v := a.Phases[i].Sum / float64(a.Phases[i].N)
		return &v
	}
	out.Timings = spec.ProbeTimings{DNS: avg(0), Connect: avg(1), TLS: avg(2), Response: avg(3)}
	return out
}
func (a *networkAggregate) add(m spec.ProbeMeasurement, kind string, previous *float64, skipped uint64) {
	a.N++
	a.Skipped += skipped
	if a.Type == "" {
		a.Type = kind
	} else if a.Type != kind {
		a.Type = "mixed"
	}
	if a.Outcomes == nil {
		a.Outcomes = map[string]int{}
	}
	a.Outcomes[m.Outcome]++
	if m.LatencyMs >= 0 {
		if a.Histogram == nil {
			a.Histogram = map[int]int{}
		}
		a.Histogram[latencyBin(m.LatencyMs)]++
		if a.Min == nil || m.LatencyMs < *a.Min {
			x := m.LatencyMs
			a.Min = &x
		}
		if a.Max == nil || m.LatencyMs > *a.Max {
			x := m.LatencyMs
			a.Max = &x
		}
		if previous != nil && *previous >= 0 {
			a.JitterSum += math.Abs(m.LatencyMs - *previous)
			a.JitterN++
		}
	}
	if m.Timings != nil {
		for i, v := range []*float64{m.Timings.DNS, m.Timings.Connect, m.Timings.TLS, m.Timings.Response} {
			if v != nil {
				a.Phases[i].N++
				a.Phases[i].Sum += *v
			}
		}
	}
}

// Translate the sample's age into Captain's clock. The enclosing host sample
// shares bosun's clock, so even a skewed node can place queued attempts correctly.
func pingTime(clock, sample int64, received time.Time) time.Time {
	age := clock - sample
	if clock <= 0 || age < 0 || age > 86400 {
		return received
	}
	return received.Add(-time.Duration(age) * time.Second)
}

// MonitorSnapshot strips the transport queue and normalizes new probe times.
// Copy the nested structs, since the caller still needs Recent for persistence.
func MonitorSnapshot(h spec.SystemStatus, at time.Time) spec.SystemStatus {
	out := h
	if h.Resources != nil {
		r := *h.Resources
		out.Resources = &r
		r.At = at.UnixMilli()
		r.Filesystems = append([]spec.FilesystemResource(nil), h.Resources.Filesystems...)
		for i := range r.Filesystems {
			f := &r.Filesystems[i]
			if f.SampledAt == 0 {
				continue // Legacy synchronous or new collector's pending reading.
			}
			age := h.Resources.At - f.SampledAt
			if h.Resources.At <= 0 || f.SampledAt <= 0 || age < -1000 || age > 90000 {
				// Keep invalid readings distinct from legacy timestamp-less data.
				f.SampledAt = -1
				continue
			}
			f.SampledAt = at.UnixMilli() - max(age, 0)
		}
		if h.Resources.GPU != nil {
			g := *h.Resources.GPU
			r.GPU = &g
			if g.At > 0 {
				age := h.Resources.At - g.At
				if h.Resources.At <= 0 || age < -5000 {
					g.At, g.State, g.Devices = 0, "error", nil
				} else {
					g.At = at.UnixMilli() - min(max(age, 0), 24*60*60*1000)
				}
			}
		}
	}
	out.Pings = append([]spec.PingResult(nil), h.Pings...)
	for i := range out.Pings {
		p := &out.Pings[i]
		if p.Quality == nil {
			continue
		}
		q := *p.Quality
		p.Quality = &q
		q.Recent = nil
		if h.Resources != nil {
			p.At = pingTime(h.Resources.At/1000, p.At, at).Unix()
			q.At = p.At
		}
	}
	return out
}

func recordPingHistory(ctx context.Context, tx *sql.Tx, nodeID int64, h spec.SystemStatus, at time.Time) error {
	for _, p := range h.Pings {
		if len(p.Name) > 128 || !validPingSeries(p) {
			continue
		}
		// Preserve the original tables and conflict keys for old Captain
		// binaries. Only explicit measurement identities use the new tables.
		statsTable, cursorTable := "node_ping_stats", "node_ping_cursors"
		keyColumns, keyValues := "node_id,task_id,name", "?,?,?"
		keyWhere := "node_id=? AND task_id=? AND name=?"
		keyArgs := []any{nodeID, p.TaskID, p.Name}
		if p.Series != "" || p.Family != "" {
			statsTable, cursorTable = "node_ping_series_stats", "node_ping_series_cursors"
			keyColumns, keyValues = keyColumns+",series,family", keyValues+",?,?"
			keyWhere += " AND series=? AND family=?"
			keyArgs = append(keyArgs, p.Series, p.Family)
		}
		args := func(extra ...any) []any { return append(append([]any(nil), keyArgs...), extra...) }
		q := p.Quality
		// Preserve legacy data shape; timestamps deduplicate cached custom-task
		// results where available. Old carriers had a fresh timestamp on every read.
		epoch := "legacy"
		kind := ""
		events := []spec.ProbeMeasurement{{Sequence: uint64(max(0, p.At)), At: p.At, LatencyMs: p.LatencyMs, Mbps: p.Mbps}}
		clock := int64(0)
		if q != nil {
			if len(q.Epoch) == 0 || len(q.Epoch) > 128 || !spec.Plain(q.Epoch) || len(q.Recent) > 60 {
				continue
			}
			switch q.Type {
			case "icmp", "tcp", "tcp_reachability", "http", "download":
			default:
				continue
			}
			epoch, kind = q.Epoch, q.Type
			events = append([]spec.ProbeMeasurement(nil), q.Recent...)
			if len(events) == 0 {
				events = []spec.ProbeMeasurement{q.ProbeMeasurement}
			}
			sort.Slice(events, func(i, j int) bool { return events[i].Sequence < events[j].Sequence })
			clock = q.At
			if h.Resources != nil {
				clock = h.Resources.At / 1000
			}
		}
		var sequence uint64
		previous := -1.0
		err := tx.QueryRowContext(ctx, `SELECT sequence,last_ms FROM `+cursorTable+` WHERE `+keyWhere+` AND epoch=?`, args(epoch)...).Scan(&sequence, &previous)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		for _, m := range events {
			if q != nil && (!m.Valid() || m.Sequence > q.Sequence || (m.LatencyMs >= 0 && m.Outcome != "ok" && !(q.Type == "tcp_reachability" && m.Outcome == "refused"))) {
				continue
			}
			if m.Sequence > 0 && m.Sequence <= sequence {
				continue
			}
			if math.IsNaN(m.LatencyMs) || math.IsInf(m.LatencyMs, 0) || math.IsNaN(m.Mbps) || math.IsInf(m.Mbps, 0) {
				continue
			}
			var prev *float64
			skipped := uint64(0)
			if q != nil {
				if m.Sequence == sequence+1 && sequence > 0 {
					prev = &previous
				} else if m.Sequence > sequence+1 {
					skipped = m.Sequence - sequence - 1
				}
			}
			sampleAt := at
			if q != nil {
				sampleAt = pingTime(clock, m.At, at)
			}
			for _, b := range []struct {
				res  string
				step time.Duration
			}{{"m", time.Minute}, {"h", time.Hour}, {"d", 24 * time.Hour}} {
				ts := sampleAt.UTC().Truncate(b.step).Unix()
				qualityJSON := "{}"
				if q != nil {
					err := tx.QueryRowContext(ctx, `SELECT quality_json FROM `+statsTable+` WHERE `+keyWhere+` AND res=? AND ts=?`, args(b.res, ts)...).Scan(&qualityJSON)
					if err != nil && !errors.Is(err, sql.ErrNoRows) {
						return err
					}
					var a networkAggregate
					if err = json.Unmarshal([]byte(qualityJSON), &a); err != nil {
						return err
					}
					a.add(m, kind, prev, skipped)
					raw, err := json.Marshal(a)
					if err != nil {
						return err
					}
					qualityJSON = string(raw)
				}
				lost, sum, mbps := 0, m.LatencyMs, m.Mbps
				if m.LatencyMs < 0 {
					lost = 1
					sum = 0
					mbps = 0
				}
				if _, err := tx.ExecContext(ctx, `INSERT INTO `+statsTable+`(`+keyColumns+`,res,ts,samples,lost,sum_ms,sum_mbps,quality_json) VALUES(`+keyValues+`,?,?,1,?,?,?,?) ON CONFLICT(`+keyColumns+`,res,ts) DO UPDATE SET samples=samples+1,lost=lost+excluded.lost,sum_ms=sum_ms+excluded.sum_ms,sum_mbps=sum_mbps+excluded.sum_mbps,quality_json=CASE WHEN ? THEN excluded.quality_json ELSE quality_json END`, args(b.res, ts, lost, sum, mbps, qualityJSON, q != nil)...); err != nil {
					return err
				}
			}
			sequence, previous = m.Sequence, m.LatencyMs
		}
		if sequence > 0 {
			if _, err := tx.ExecContext(ctx, `INSERT INTO `+cursorTable+`(`+keyColumns+`,epoch,sequence,last_ms,seen_at) VALUES(`+keyValues+`,?,?,?,?) ON CONFLICT(`+keyColumns+`,epoch) DO UPDATE SET sequence=excluded.sequence,last_ms=excluded.last_ms,seen_at=excluded.seen_at`, args(epoch, sequence, previous, at.Unix())...); err != nil {
				return err
			}
		}
	}
	return nil
}

// New carrier identities are only accepted with complete measurement metadata.
// An invalid identity must not fall through into the legacy empty namespace.
// Custom tasks may constrain a family without claiming a generated carrier ID.
func validPingSeries(p spec.PingResult) bool {
	if p.Series == "" && p.Family == "" {
		return true
	}
	if p.Quality == nil || (p.Family != "ipv4" && p.Family != "ipv6") {
		return false
	}
	if p.Series == "" {
		return p.TaskID != 0
	}
	return p.TaskID == 0 && spec.ValidCarrierSeries(p.Series) &&
		(p.Quality.Type == "icmp" || p.Quality.Type == "tcp_reachability")
}
