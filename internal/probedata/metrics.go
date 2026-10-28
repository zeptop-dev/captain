package probedata

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/zeptop-dev/captain/internal/store"
)

var metricCatalog = []Definition{
	{Key: "cpu.usage", Type: "gauge", Unit: "%", Category: "cpu", History: true},
	{Key: "gpu.usage", Type: "gauge", Unit: "%", Category: "system"},
	{Key: "gpu.device.usage", Type: "gauge", Unit: "%", Category: "system"},
	{Key: "gpu.memory.used", Type: "gauge", Unit: "bytes", Category: "system"},
	{Key: "gpu.memory.total", Type: "gauge", Unit: "bytes", Category: "system"},
	{Key: "gpu.temperature", Type: "gauge", Unit: "°C", Category: "system"},
	{Key: "memory.used", Type: "gauge", Unit: "bytes", Category: "memory", History: true},
	{Key: "swap.used", Type: "gauge", Unit: "bytes", Category: "memory", History: true},
	{Key: "load.average", Type: "gauge", Category: "system", History: true},
	{Key: "disk.used", Type: "gauge", Unit: "bytes", Category: "disk", History: true},
	{Key: "net.in.rate", Type: "gauge", Unit: "bytes/s", Category: "network", History: true},
	{Key: "net.out.rate", Type: "gauge", Unit: "bytes/s", Category: "network", History: true},
	{Key: "net.total.up", Type: "counter", Unit: "bytes", Category: "network", History: true},
	{Key: "net.total.down", Type: "counter", Unit: "bytes", Category: "network", History: true},
	{Key: "traffic.up", Type: "gauge", Unit: "bytes", Category: "traffic", History: true},
	{Key: "traffic.down", Type: "gauge", Unit: "bytes", Category: "traffic", History: true},
	{Key: "process.count", Type: "gauge", Unit: "count", Category: "system", History: true},
	{Key: "connections.tcp", Type: "gauge", Unit: "count", Category: "system", History: true},
	{Key: "connections.udp", Type: "gauge", Unit: "count", Category: "system", History: true},
	{Key: "ping.latency_ms", Type: "gauge", Unit: "ms", Category: "latency", History: true},
	{Key: "ping.loss", Type: "gauge", Unit: "ratio", Category: "latency", History: true},
	{Key: "captain.memory.total", Type: "gauge", Unit: "bytes", Category: "memory", History: true},
	{Key: "captain.swap.total", Type: "gauge", Unit: "bytes", Category: "memory", History: true},
	{Key: "captain.disk.total", Type: "gauge", Unit: "bytes", Category: "disk", History: true},
	{Key: "captain.load.5", Type: "gauge", Category: "system", History: true},
	{Key: "captain.load.15", Type: "gauge", Category: "system", History: true},
	{Key: "captain.uptime", Type: "gauge", Unit: "seconds", Category: "system", History: true},
}

func (e *Engine) Definitions(ctx context.Context, scope Scope) ([]Definition, error) {
	s, err := e.settings(ctx)
	if err != nil {
		return nil, err
	}
	out := []Definition{}
	if !s.PageOn() {
		return out, nil
	}
	for _, d := range metricCatalog {
		if !s.PublicShows(d.Category) {
			continue
		}
		d.History = d.History && s.PublicShows("history")
		if d.History {
			d.RetentionDays = 730
			d.Aggregations = []string{"avg", "sum", "count", "min", "max", "first", "last"}
			if d.Key == "ping.loss" {
				d.Aggregations = []string{"avg", "sum", "count"}
			}
		}
		out = append(out, d)
	}
	return out, nil
}
func (e *Engine) normalizeWindow(start, end time.Time) (time.Time, time.Time, error) {
	now := e.clock()
	if end.IsZero() {
		end = now
	}
	if start.IsZero() {
		start = end.Add(-24 * time.Hour)
	}
	if !end.After(start) || end.Sub(start) > 730*24*time.Hour || end.After(now.Add(time.Minute)) {
		return time.Time{}, time.Time{}, ErrInvalidQuery
	}
	return start.UTC(), end.UTC(), nil
}
func historyResolution(start, end, now time.Time) (string, int) {
	age := now.Sub(start)
	if age <= 2*time.Hour && end.Sub(start) <= 15*time.Minute {
		return "r", 1
	}
	if age <= 48*time.Hour {
		return "m", 60
	}
	if age <= 60*24*time.Hour {
		return "h", 3600
	}
	return "d", 86400
}
func selectNodes(s Snapshot, ids []string) []Node {
	out := []Node{}
	for _, n := range s.Nodes {
		if len(ids) == 0 || slices.Contains(ids, n.UUID) {
			out = append(out, n)
		}
	}
	return out
}
func (e *Engine) Query(ctx context.Context, scope Scope, q Query) ([]Series, error) {
	if len(q.MetricKeys) == 0 || len(q.MetricKeys) > 40 || len(q.EntityIDs) > 256 || len(q.Tags) > 4 {
		return nil, ErrInvalidQuery
	}
	var err error
	q.Start, q.End, err = e.normalizeWindow(q.Start, q.End)
	if err != nil {
		return nil, err
	}
	snap, err := e.Snapshot(ctx, scope)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(snap.Settings.Sections, "history") {
		return []Series{}, nil
	}
	defs, err := e.Definitions(ctx, scope)
	if err != nil {
		return nil, err
	}
	catalog := map[string]Definition{}
	for _, d := range defs {
		catalog[d.Key] = d
	}
	all := map[string]bool{}
	for _, d := range metricCatalog {
		all[d.Key] = true
	}
	for _, k := range q.MetricKeys {
		if !all[k] {
			return nil, fmt.Errorf("%w: unknown metric", ErrInvalidQuery)
		}
	}
	for k := range q.Tags {
		if k != "task_id" {
			return []Series{}, nil
		}
	}
	nodes := selectNodes(snap, q.EntityIDs)
	if len(nodes) > 256 {
		return nil, ErrInvalidQuery
	}
	if q.MaxPoints == 0 {
		q.MaxPoints = 500
	}
	if q.MaxPoints < 1 || q.MaxPoints > 5000 {
		return nil, ErrInvalidQuery
	}
	// Cap the whole response as well as individual series to keep anonymous
	// queries bounded when an installation has many nodes and metrics.
	totalPoints := 0
	for _, k := range q.MetricKeys {
		points := q.MaxPoints
		if v, ok := q.MaxPointsByMetric[k]; ok {
			points = v
		}
		if points < 1 || points > 5000 {
			return nil, ErrInvalidQuery
		}
		totalPoints += points
	}
	if len(nodes)*totalPoints > 500000 {
		return nil, ErrInvalidQuery
	}
	res, step := historyResolution(q.Start, q.End, snap.Now)
	out := []Series{}
	for _, n := range nodes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		buckets, err := e.loadMetricBuckets(ctx, n, res, q.Start, q.End)
		if err != nil {
			return nil, err
		}
		for _, key := range q.MetricKeys {
			def, ok := catalog[key]
			if !ok || !def.History {
				continue
			}
			maxPoints := q.MaxPoints
			if v, ok := q.MaxPointsByMetric[key]; ok {
				maxPoints = v
			}
			if maxPoints < 1 || maxPoints > 5000 {
				return nil, ErrInvalidQuery
			}
			algorithm := q.Aggregation
			if a := q.AggregationByMetric[key]; a != "" {
				algorithm = a
			}
			if algorithm == "" {
				algorithm = "avg"
			}
			if algorithm == "auto" {
				algorithm = "avg"
			}
			if !slices.Contains(def.Aggregations, algorithm) {
				return nil, ErrUnsupportedAggregation
			}
			if key == "ping.latency_ms" || key == "ping.loss" {
				series, err := e.pingSeries(ctx, n, snap.PingTasks, q, def, res, step, maxPoints, algorithm)
				if err != nil {
					return nil, err
				}
				out = append(out, series...)
				continue
			}
			if len(q.Tags) > 0 {
				continue
			}
			// The root disk is an independently configured public volume.
			if (key == "disk.used" || key == "captain.disk.total") && !n.RootDiskPublic {
				continue
			}
			series, err := makeSeries(n.UUID, def, nil, buckets, key, q, maxPoints, step, algorithm)
			if err != nil {
				return nil, err
			}
			out = append(out, series)
		}
	}
	// Re-project after query work so a concurrent hide cannot disclose its data.
	current, err := e.Snapshot(ctx, scope)
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	rootDisk := map[string]bool{}
	tasksAllowed := map[string]bool{}
	for _, n := range current.Nodes {
		allowed[n.UUID] = true
		rootDisk[n.UUID] = n.RootDiskPublic
		for _, t := range current.PingTasks {
			if taskSelected(t, n, nil) {
				tasksAllowed[n.UUID+"/"+t.ID] = true
			}
		}
	}
	defs, err = e.Definitions(ctx, scope)
	if err != nil {
		return nil, err
	}
	visible := map[string]bool{}
	for _, d := range defs {
		visible[d.Key] = d.History
	}
	filtered := out[:0]
	for _, s := range out {
		if allowed[s.EntityID] && visible[s.MetricKey] && ((s.MetricKey != "disk.used" && s.MetricKey != "captain.disk.total") || rootDisk[s.EntityID]) && (s.Tags["task_id"] == "" || tasksAllowed[s.EntityID+"/"+s.Tags["task_id"]]) {
			filtered = append(filtered, s)
		}
	}
	return filtered, nil
}

type metricBucket struct {
	TS     int64
	Values map[string]store.ProbeMetricAggregate
	Legacy bool
}

func (e *Engine) loadMetricBuckets(ctx context.Context, n Node, res string, start, end time.Time) ([]metricBucket, error) {
	fresh, err := e.Store.ProbeMetricHistory(ctx, n.ID, res, start, end)
	if err != nil {
		return nil, err
	}
	out := []metricBucket{}
	seen := map[int64]int{}
	for _, p := range fresh {
		out = append(out, metricBucket{TS: p.TS, Values: p.Values})
		seen[p.TS] = len(out) - 1
	}
	legacyRes := res
	if res == "r" {
		legacyRes = "m"
	}
	old, err := e.Store.NodeStats(ctx, n.ID, legacyRes, start, end)
	if err != nil {
		return nil, err
	}
	for _, p := range old {
		// A raw recent query must not overlay a minute average on newer exact
		// samples from the same minute, including a partial minute after upgrade.
		if res == "r" {
			overlap := false
			for _, f := range fresh {
				if f.TS >= p.TS && f.TS < p.TS+60 {
					overlap = true
					break
				}
			}
			if overlap {
				continue
			}
		}
		if index, ok := seen[p.TS]; ok {
			// An upgrade may land halfway through an existing bucket. Use the
			// complete old sum/count for average queries, not only its tail.
			for k, v := range map[string]float64{"cpu.usage": p.CPU, "memory.used": p.MemUsed, "swap.used": p.SwapUsed, "disk.used": p.DiskUsed, "net.in.rate": p.NetDown, "net.out.rate": p.NetUp, "load.average": p.Load1, "connections.tcp": p.TCP, "connections.udp": p.UDP, "process.count": p.Procs} {
				if c := p.Counts[k]; c > out[index].Values[k].Count {
					out[index].Values[k] = store.ProbeMetricAggregate{Count: c, Sum: v * float64(c)}
					out[index].Legacy = true
				}
			}
			continue
		}

		values := map[string]store.ProbeMetricAggregate{}
		for k, v := range map[string]float64{"cpu.usage": p.CPU, "memory.used": p.MemUsed, "swap.used": p.SwapUsed, "disk.used": p.DiskUsed, "net.in.rate": p.NetDown, "net.out.rate": p.NetUp, "load.average": p.Load1, "connections.tcp": p.TCP, "connections.udp": p.UDP, "process.count": p.Procs} {
			if c := p.Counts[k]; c > 0 {
				values[k] = store.ProbeMetricAggregate{Count: c, Sum: v * float64(c)}
			}
		}
		out = append(out, metricBucket{TS: p.TS, Values: values, Legacy: true})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TS < out[j].TS })
	return out, nil
}
func mergeAggregate(a, b store.ProbeMetricAggregate) store.ProbeMetricAggregate {
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
func aggregateValue(a store.ProbeMetricAggregate, algorithm string) *float64 {
	if a.Count == 0 {
		return nil
	}
	var v float64
	switch algorithm {
	case "avg":
		v = a.Sum / float64(a.Count)
	case "sum":
		v = a.Sum
	case "count":
		v = float64(a.Count)
	case "min":
		v = a.Min
	case "max":
		v = a.Max
	case "first":
		v = a.First
	case "last":
		v = a.Last
	default:
		return nil
	}
	return number(v, true)
}
func makeSeries(entity string, d Definition, tags map[string]string, buckets []metricBucket, key string, q Query, maxPoints, step int, algorithm string) (Series, error) {
	series := Series{MetricKey: d.Key, EntityID: entity, Type: d.Type, Unit: d.Unit, RetentionDays: d.RetentionDays, Tags: tags, Aggregation: algorithm, FillEmpty: q.FillEmpty, MaxPoints: maxPoints, IntervalSeconds: step, Points: []Point{}}
	duration := q.End.Sub(q.Start).Seconds()
	interval := max(step, int((duration+float64(maxPoints)-1)/float64(maxPoints)))
	interval = ((interval + step - 1) / step) * step
	series.IntervalSeconds = interval
	series.Downsampled = interval > step
	grouped := map[int64]store.ProbeMetricAggregate{}
	start := q.Start.Unix()
	for _, b := range buckets {
		a, ok := b.Values[key]
		if !ok || a.Count == 0 {
			continue
		}
		if b.Legacy && algorithm != "avg" && algorithm != "sum" && algorithm != "count" {
			return series, ErrUnsupportedAggregation
		}
		ts := start + ((b.TS-start)/int64(interval))*int64(interval)
		grouped[ts] = mergeAggregate(grouped[ts], a)
	}
	times := []int64{}
	if q.FillEmpty {
		for ts := start; ts <= q.End.Unix() && len(times) < maxPoints; ts += int64(interval) {
			times = append(times, ts)
		}
	} else {
		for ts := range grouped {
			times = append(times, ts)
		}
		slices.Sort(times)
	}
	for _, ts := range times {
		a := grouped[ts]
		t := time.Unix(ts, 0).UTC()
		series.Points = append(series.Points, Point{Time: &t, Value: aggregateValue(a, algorithm), Count: a.Count})
		series.Count++
	}
	return series, nil
}
