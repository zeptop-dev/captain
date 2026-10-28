package probedata

import (
	"context"
	"math"
	"slices"
	"sort"

	"github.com/zeptop-dev/captain/internal/store"
)

func taskMatches(t PingTask, p store.PingPoint) bool {
	if t.Series != "" {
		return p.TaskID == 0 && p.Series == t.Series && p.Family == t.Family && p.Name == t.Name
	}
	return p.TaskID == t.LegacyID && p.Name == t.Name
}
func taskSelected(t PingTask, n Node, ids []string) bool {
	return slices.Contains(t.EntityIDs, n.UUID) && (len(ids) == 0 || slices.Contains(ids, t.ID))
}
func (e *Engine) pingSeries(ctx context.Context, n Node, tasks []PingTask, q Query, d Definition, res string, step, maxPoints int, algorithm string) ([]Series, error) {
	if res == "r" {
		res = "m"
		step = 60
	}
	points, err := e.Store.NodePingStats(ctx, n.ID, res, q.Start, q.End)
	if err != nil {
		return nil, err
	}
	out := []Series{}
	for _, t := range tasks {
		if !taskSelected(t, n, nil) || (q.Tags["task_id"] != "" && q.Tags["task_id"] != t.ID) {
			continue
		}
		buckets := []metricBucket{}
		for _, p := range points {
			if !taskMatches(t, p) {
				continue
			}
			a := store.ProbeMetricAggregate{}
			legacy := true
			if d.Key == "ping.loss" {
				a.Count = p.Samples
				a.Sum = float64(p.Lost)
			} else {
				a.Count = p.Samples - p.Lost
				if a.Count > 0 {
					a.Sum = p.AvgMs * float64(a.Count)
					if p.Quality != nil && p.Quality.Min != nil && p.Quality.Max != nil && histCount(p.Histogram) == a.Count {
						a.Min = *p.Quality.Min
						a.Max = *p.Quality.Max
						legacy = false
					}
				}
			}
			if algorithm == "first" || algorithm == "last" {
				return nil, ErrUnsupportedAggregation
			}
			buckets = append(buckets, metricBucket{TS: p.TS, Values: map[string]store.ProbeMetricAggregate{d.Key: a}, Legacy: legacy})
		}
		series, err := makeSeries(n.UUID, d, map[string]string{"task_id": t.ID}, buckets, d.Key, q, maxPoints, step, algorithm)
		if err != nil {
			return nil, err
		}
		out = append(out, series)
	}
	return out, nil
}
func histCount(h map[int]int) int {
	n := 0
	for _, v := range h {
		n += v
	}
	return n
}
func histogramQuantile(h map[int]int, q float64, maxValue *float64) *float64 {
	n := histCount(h)
	if n == 0 {
		return nil
	}
	keys := []int{}
	for k := range h {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	want := int(math.Ceil(float64(n) * q))
	seen := 0
	for _, k := range keys {
		seen += h[k]
		if seen >= want {
			v := 0.0
			if k > 0 {
				v = .1 * math.Pow(1.05, float64(k-1))
			}
			if maxValue != nil {
				v = min(v, *maxValue)
			}
			return &v
		}
	}
	return nil
}
func (e *Engine) PingStats(ctx context.Context, scope Scope, q PingQuery) ([]PingStat, error) {
	if len(q.EntityIDs) > 256 || len(q.TaskIDs) > 256 {
		return nil, ErrInvalidQuery
	}
	var err error
	q.Start, q.End, err = e.normalizeWindow(q.Start, q.End)
	if err != nil {
		return nil, err
	}
	policy, err := e.Store.ProbePublicPolicyFingerprint(ctx)
	if err != nil {
		return nil, err
	}
	snap, err := e.Snapshot(ctx, scope)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(snap.Settings.Sections, "latency") || !slices.Contains(snap.Settings.Sections, "history") {
		return []PingStat{}, nil
	}
	res, _ := historyResolution(q.Start, q.End, snap.Now)
	if res == "r" {
		res = "m"
	}
	nodes := selectNodes(snap, q.EntityIDs)
	if len(nodes) > 256 {
		return nil, ErrInvalidQuery
	}
	combinations := 0
	for _, n := range nodes {
		for _, task := range snap.PingTasks {
			if taskSelected(task, n, q.TaskIDs) {
				combinations++
			}
		}
	}
	if combinations > 10000 {
		return nil, ErrInvalidQuery
	}
	out := []PingStat{}
	for _, n := range nodes {
		points, err := e.Store.NodePingStats(ctx, n.ID, res, q.Start, q.End)
		if err != nil {
			return nil, err
		}
		for _, t := range snap.PingTasks {
			if !taskSelected(t, n, q.TaskIDs) {
				continue
			}
			p := PingStat{EntityID: n.UUID, TaskID: t.ID, Name: t.Name, Type: t.Type, Interval: t.Interval, Tags: map[string]string{"task_id": t.ID}}
			hist := map[int]int{}
			sum := 0.0
			quality := &PingQuality{Outcomes: map[string]int{}}
			for _, b := range points {
				if !taskMatches(t, b) {
					continue
				}
				valid := max(0, b.Samples-b.Lost)
				p.Total += b.Samples
				p.Valid += valid
				if valid > 0 {
					sum += b.AvgMs * float64(valid)
				}
				for bin, count := range b.Histogram {
					hist[bin] += count
				}
				if b.Quality != nil {
					for k, v := range b.Quality.Outcomes {
						quality.Outcomes[k] += v
					}
					quality.Skipped += b.Quality.Skipped
					if b.Quality.Min != nil && (p.Min == nil || *b.Quality.Min < *p.Min) {
						v := *b.Quality.Min
						p.Min = &v
					}
					if b.Quality.Max != nil && (p.Max == nil || *b.Quality.Max > *p.Max) {
						v := *b.Quality.Max
						p.Max = &v
					}
				}
			}
			if p.Total > 0 {
				p.Loss = number(float64(p.Total-p.Valid)/float64(p.Total), true)
			}
			if p.Valid > 0 {
				p.Avg = number(sum/float64(p.Valid), true)
			}
			// Partial histograms cannot describe the requested complete window.
			if histCount(hist) == p.Valid && p.Valid > 0 {
				quality.QuantilesApproximate = true
				p.P50 = histogramQuantile(hist, .5, p.Max)
				p.P99 = histogramQuantile(hist, .99, p.Max)
				quality.P95 = histogramQuantile(hist, .95, p.Max)
			} else {
				p.Min, p.Max = nil, nil
			}
			if p.P50 != nil && p.P99 != nil && *p.P50 > 0 {
				p.P99P50Ratio = number(*p.P99 / *p.P50, true)
			}
			// An average of the final bucket is not a latest measurement.
			for _, latest := range n.Pings {
				if latest.TaskID == t.ID && latest.Time != nil && !latest.Time.Before(q.Start) && !latest.Time.After(q.End) {
					p.Latest = latest.Latency
				}
			}
			quality.Attempts, quality.Successes, quality.Min, quality.Max, quality.P50 = p.Total, p.Valid, p.Min, p.Max, p.P50
			p.Quality = quality
			out = append(out, p)
		}
	}
	current, err := e.Snapshot(ctx, scope)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(current.Settings.Sections, "latency") || !slices.Contains(current.Settings.Sections, "history") {
		return []PingStat{}, nil
	}
	allowed := map[string]bool{}
	for _, n := range current.Nodes {
		for _, t := range current.PingTasks {
			if taskSelected(t, n, nil) {
				allowed[n.UUID+"/"+t.ID] = true
			}
		}
	}
	filtered := out[:0]
	for _, p := range out {
		if allowed[p.EntityID+"/"+p.TaskID] {
			filtered = append(filtered, p)
		}
	}
	after, err := e.Store.ProbePublicPolicyFingerprint(ctx)
	if err != nil {
		return nil, err
	}
	if policy != after {
		return nil, ErrPolicyChanged
	}
	return filtered, nil
}
