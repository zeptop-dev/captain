package komari

import (
	"encoding/json"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/probedata"
)

const maxQueryPoints = 32000

type metricParams struct {
	MetricKey           string            `json:"metric_key"`
	MetricKeys          []string          `json:"metric_keys"`
	Metrics             []string          `json:"metrics"`
	EntityID            string            `json:"entity_id"`
	EntityIDs           []string          `json:"entity_ids"`
	Start               *time.Time        `json:"start"`
	StartTime           *time.Time        `json:"start_time"`
	End                 *time.Time        `json:"end"`
	EndTime             *time.Time        `json:"end_time"`
	Hours               numeric           `json:"hours"`
	Tags                map[string]string `json:"tags"`
	FillEmpty           *bool             `json:"fill_empty"`
	MaxPoints           int               `json:"max_points"`
	MaxPointsByMetric   map[string]int    `json:"max_points_by_metric"`
	PointsByMetric      map[string]int    `json:"points_by_metric"`
	Aggregation         string            `json:"aggregation"`
	Algorithm           string            `json:"algorithm"`
	AggregationByMetric map[string]string `json:"aggregation_by_metric"`
	AlgorithmByMetric   map[string]string `json:"algorithm_by_metric"`
}

func firstTime(a, b *time.Time) *time.Time {
	if a != nil {
		return a
	}
	return b
}
func (h *Handler) window(start, end *time.Time, hours numeric, defaultHours float64) (time.Time, time.Time, *rpcError) {
	t := h.now()
	if end != nil {
		t = end.UTC()
	}
	span := float64(hours)
	if span == 0 {
		span = defaultHours
	}
	if math.IsInf(span, 0) || math.IsNaN(span) || span <= 0 || span > 24*730 {
		return time.Time{}, time.Time{}, invalid("hours must be between 0 and 17520")
	}
	f := t.Add(-time.Duration(span * float64(time.Hour)))
	if start != nil {
		f = start.UTC()
	}
	if !t.After(f) || t.Sub(f) > 730*24*time.Hour || t.After(h.now().Add(time.Minute)) {
		return time.Time{}, time.Time{}, invalid("Invalid time window (maximum 730 days)")
	}
	return f, t, nil
}

func (h *Handler) entities(r *http.Request, requested []string) (probedata.Snapshot, []string, *rpcError) {
	if len(requested) > 128 {
		return probedata.Snapshot{}, nil, invalid("Too many nodes")
	}
	s, err := h.d.Reader.Snapshot(r.Context(), probedata.Scope{})
	if err != nil {
		return s, nil, fromError(err)
	}
	ids := []string{}
	if len(requested) == 0 {
		for _, n := range s.Nodes {
			ids = append(ids, n.UUID)
		}
	} else {
		for _, id := range requested {
			if _, ok := nodeByID(s, id); !ok {
				return s, nil, invalid("Node not found")
			}
			ids = append(ids, id)
		}
	}
	if len(ids) > 128 {
		return s, nil, invalid("Select at most 128 nodes per query")
	}
	return s, ids, nil
}

func (h *Handler) definitions(r *http.Request) (any, *rpcError) {
	defs, err := h.d.Reader.Definitions(r.Context(), probedata.Scope{})
	if err != nil {
		return nil, fromError(err)
	}
	out := make([]map[string]any, 0, len(defs))
	for _, d := range defs {
		out = append(out, map[string]any{"name": d.Key, "description": d.Category, "type": d.Type, "unit": d.Unit, "retention_days": d.RetentionDays, "metadata": map[string]any{"captain": map[string]any{"history": d.History, "aggregations": d.Aggregations}}})
	}
	return out, nil
}

func (h *Handler) queryMetrics(r *http.Request, raw json.RawMessage) (any, *rpcError) {
	var p metricParams
	if bind(raw, &p) != nil {
		return nil, invalid("Invalid metric parameters")
	}
	keys := cleanStrings(p.MetricKeys, p.Metrics, []string{p.MetricKey})
	if len(keys) == 0 || len(keys) > 32 {
		return nil, invalid("Select 1 to 32 metrics")
	}
	start, end, e := h.window(firstTime(p.Start, p.StartTime), firstTime(p.End, p.EndTime), p.Hours, 4)
	if e != nil {
		return nil, e
	}
	s, ids, e := h.entities(r, cleanStrings(p.EntityIDs, []string{p.EntityID}))
	if e != nil {
		return nil, e
	}
	if p.MaxPoints == 0 {
		p.MaxPoints = 500
	}
	if p.MaxPoints < 1 {
		return nil, invalid("max_points must be positive")
	}
	requestedMaxPoints := p.MaxPoints
	limited := p.MaxPoints > 4000
	p.MaxPoints = min(p.MaxPoints, 4000)
	if len(p.Tags) > 16 {
		return nil, invalid("Too many tags")
	}
	for k, v := range p.Tags {
		if len(k) > 128 || len(v) > 256 {
			return nil, invalid("Invalid tag")
		}
	}
	if id := p.Tags["task_id"]; id != "" {
		actual, ok := publicTaskID(id, s.PingTasks)
		if !ok {
			return nil, invalid("Task not found")
		}
		p.Tags["task_id"] = actual
	}
	points := map[string]int{}
	aggs := map[string]string{}
	budget := 0
	for _, key := range keys {
		if len(key) > 128 {
			return nil, invalid("Invalid metric key")
		}
		n := p.MaxPoints
		if v, ok := p.PointsByMetric[key]; ok {
			n = v
		}
		if v, ok := p.MaxPointsByMetric[key]; ok {
			n = v
		}
		if n < 1 {
			return nil, invalid("Metric point limit must be positive")
		}
		limited = limited || n > 4000
		n = min(n, 4000)
		points[key] = n
		seriesCount := len(ids)
		if key == "ping.latency_ms" || key == "ping.loss" {
			seriesCount = selectedPingSeries(s.PingTasks, ids, p.Tags["task_id"])
		}
		budget += n * seriesCount
		if v, ok := p.AlgorithmByMetric[key]; ok {
			aggs[key] = v
		}
		if v, ok := p.AggregationByMetric[key]; ok {
			aggs[key] = v
		}
		aggs[key] = normalizeAggregation(aggs[key])
	}
	if budget > maxQueryPoints {
		return nil, invalid("Query exceeds the 32000 point budget; select fewer nodes, metrics or points")
	}
	agg := p.Aggregation
	if agg == "" {
		agg = p.Algorithm
	}
	agg = normalizeAggregation(agg)
	fill := false
	if p.FillEmpty != nil {
		fill = *p.FillEmpty
	}
	q := probedata.Query{MetricKeys: keys, EntityIDs: ids, Start: start, End: end, Tags: p.Tags, FillEmpty: fill, MaxPoints: p.MaxPoints, MaxPointsByMetric: points, Aggregation: agg, AggregationByMetric: aggs}
	series := []probedata.Series{}
	if len(ids) > 0 {
		var err error
		series, err = h.d.Reader.Query(r.Context(), probedata.Scope{}, q)
		if err != nil {
			return nil, fromError(err)
		}
	}
	if series == nil {
		series = []probedata.Series{}
	}
	// The public task catalog uses numeric Komari IDs. Keep those same IDs in
	// modern metric tags, even when Captain internally identifies carrier
	// series by a configuration digest.
	for i := range series {
		series[i].Tags = wireTaskTags(series[i].Tags, s.PingTasks)
		for j := range series[i].Points {
			series[i].Points[j].Tags = wireTaskTags(series[i].Points[j].Tags, s.PingTasks)
		}
	}
	return map[string]any{"start": start, "end": end, "server_downsample_default": true, "default_points": 500, "series": series, "count": len(series), "captain": map[string]any{"request_limited": limited, "requested_max_points": requestedMaxPoints, "effective_max_points": p.MaxPoints, "series_point_limit": 4000, "total_point_limit": maxQueryPoints}}, nil
}

// Ping metrics expand once per actual node/task assignment, not once per node.
// Count only the selected public task and selected nodes. A Cartesian product
// both underestimates fan-out for a single node and overcounts unrelated tasks.
func selectedPingSeries(tasks []probedata.PingTask, ids []string, taskID string) int {
	count := 0
	for _, task := range tasks {
		if taskID != "" && task.ID != taskID {
			continue
		}
		for _, id := range ids {
			if contains(task.EntityIDs, id) {
				count++
			}
		}
	}
	return count
}

type pingParams struct {
	UUID      string            `json:"uuid"`
	EntityID  string            `json:"entity_id"`
	EntityIDs []string          `json:"entity_ids"`
	TaskID    json.RawMessage   `json:"task_id"`
	TaskIDs   []json.RawMessage `json:"task_ids"`
	Start     *time.Time        `json:"start"`
	StartTime *time.Time        `json:"start_time"`
	End       *time.Time        `json:"end"`
	EndTime   *time.Time        `json:"end_time"`
	Hours     numeric           `json:"hours"`
	MaxPoints int               `json:"max_points"`
}

func taskString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", true
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, true
	}
	var n json.Number
	if json.Unmarshal(raw, &n) != nil {
		return "", false
	}
	v, err := strconv.ParseInt(n.String(), 10, 64)
	if err != nil {
		return "", false
	}
	return strconv.FormatInt(v, 10), true
}
func (h *Handler) pingStats(r *http.Request, raw json.RawMessage) (any, *rpcError) {
	var p pingParams
	if bind(raw, &p) != nil || len(p.TaskIDs) > 128 {
		return nil, invalid("Invalid ping parameters")
	}
	start, end, e := h.window(firstTime(p.Start, p.StartTime), firstTime(p.End, p.EndTime), p.Hours, 4)
	if e != nil {
		return nil, e
	}
	s, ids, e := h.entities(r, cleanStrings(p.EntityIDs, []string{p.EntityID, p.UUID}))
	if e != nil {
		return nil, e
	}
	tasks := []string{}
	for _, rawID := range append(p.TaskIDs, p.TaskID) {
		id, ok := taskString(rawID)
		if !ok {
			return nil, invalid("Invalid task ID")
		}
		if id == "" || id == "-1" {
			continue
		}
		actual, ok := publicTaskID(id, s.PingTasks)
		if !ok {
			return nil, invalid("Task not found")
		}
		tasks = append(tasks, actual)
	}
	stats := []probedata.PingStat{}
	if len(ids) > 0 {
		var err error
		stats, err = h.d.Reader.PingStats(r.Context(), probedata.Scope{}, probedata.PingQuery{EntityIDs: ids, TaskIDs: cleanStrings(tasks), Start: start, End: end})
		if err != nil {
			return nil, fromError(err)
		}
	}
	if stats == nil {
		stats = []probedata.PingStat{}
	}
	wireStats := make([]map[string]any, 0, len(stats))
	for _, st := range stats {
		wireStats = append(wireStats, map[string]any{
			"entity_id": st.EntityID, "task_id": strconv.FormatInt(legacyTaskID(st.TaskID, s.PingTasks), 10),
			"name": st.Name, "type": st.Type, "interval": st.Interval,
			"tags": wireTaskTags(st.Tags, s.PingTasks), "total": st.Total, "valid": st.Valid,
			"loss": lossPercent(st.Loss), "loss_approximate": st.LossApproximate, "min": st.Min, "max": st.Max,
			"avg": st.Avg, "latest": st.Latest, "p50": st.P50, "p99": st.P99,
			"stddev": st.Stddev, "p99_p50_ratio": latencyTail(st.P50, st.P99, true),
			"captain": map[string]any{"task_id": st.TaskID, "quality": st.Quality, "loss_ratio": st.Loss, "p99_p50_ratio": st.P99P50Ratio},
		})
	}
	return map[string]any{"start": start, "end": end, "stats": wireStats, "count": len(wireStats)}, nil
}

func normalizeAggregation(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "average", "mean":
		return "avg"
	case "std_dev", "stddev_pop", "std_dev_pop":
		return "stddev"
	}
	return value
}

func lossPercent(ratio *float64) *float64 {
	if ratio == nil {
		return nil
	}
	percent := *ratio * 100
	return &percent
}
func latencyTail(p50, p99 *float64, adjust bool) *float64 {
	if p50 == nil || p99 == nil || *p50 <= 0 || *p99 < *p50 {
		return nil
	}
	base := *p50
	if adjust {
		base = math.Max(math.Min(base, 50), 10)
	}
	tail := (*p99 - *p50) / base
	return &tail
}

func wireTaskTags(tags map[string]string, tasks []probedata.PingTask) map[string]string {
	if tags == nil {
		return nil
	}
	out := make(map[string]string, len(tags))
	for key, value := range tags {
		out[key] = value
	}
	if task := out["task_id"]; task != "" {
		out["task_id"] = strconv.FormatInt(legacyTaskID(task, tasks), 10)
	}
	return out
}

type recordParams struct {
	Type          string          `json:"type"`
	UUID          string          `json:"uuid"`
	Hours         numeric         `json:"hours"`
	Start         *time.Time      `json:"start"`
	End           *time.Time      `json:"end"`
	LoadType      string          `json:"load_type"`
	TaskID        json.RawMessage `json:"task_id"`
	MaxCount      numeric         `json:"maxCount"`
	MaxCountSnake numeric         `json:"max_count"`
}

func (h *Handler) records(r *http.Request, method string, raw json.RawMessage) (any, *rpcError) {
	var p recordParams
	// Public methods predate common:getRecords and have different positional
	// argument orders; named object parameters are identical.
	if len(raw) > 0 && raw[0] == '[' && method != "common:getRecords" {
		if method == "public:getRecordsByUUID" {
			var a struct {
				UUID     string  `json:"uuid"`
				LoadType string  `json:"load_type"`
				Hours    numeric `json:"hours"`
			}
			if bind(raw, &a) != nil {
				return nil, invalid("Invalid record parameters")
			}
			p.UUID = a.UUID
			p.LoadType = a.LoadType
			p.Hours = a.Hours
		} else {
			var a struct {
				UUID     string          `json:"uuid"`
				TaskID   json.RawMessage `json:"task_id"`
				Hours    numeric         `json:"hours"`
				MaxCount numeric         `json:"max_count"`
			}
			if bind(raw, &a) != nil {
				return nil, invalid("Invalid record parameters")
			}
			p.UUID = a.UUID
			p.TaskID = a.TaskID
			p.Hours = a.Hours
			p.MaxCount = a.MaxCount
		}
	} else if bind(raw, &p) != nil {
		return nil, invalid("Invalid record parameters")
	}
	if p.Type == "" {
		p.Type = "load"
	}
	if method == "public:getPingRecords" {
		p.Type = "ping"
	}
	if p.Type != "load" && p.Type != "ping" {
		return nil, invalid("type must be load or ping")
	}
	if method == "public:getRecordsByUUID" && p.UUID == "" {
		return nil, invalid("UUID is required")
	}
	taskID, ok := taskString(p.TaskID)
	if !ok {
		return nil, invalid("Invalid task_id")
	}
	if method == "public:getPingRecords" && p.UUID == "" && taskID == "" {
		return nil, invalid("UUID or task_id is required")
	}
	defaultHours := 1.0
	if method != "common:getRecords" {
		defaultHours = 4
	}
	start, end, e := h.window(p.Start, p.End, p.Hours, defaultHours)
	if e != nil {
		return nil, e
	}
	s, ids, e := h.entities(r, cleanStrings([]string{p.UUID}))
	if e != nil {
		return nil, e
	}
	maxCount := p.MaxCount
	if maxCount == 0 {
		maxCount = p.MaxCountSnake
	}
	if maxCount == 0 || maxCount == -1 {
		maxCount = 4000
	}
	if maxCount < 1 || math.Trunc(float64(maxCount)) != float64(maxCount) {
		return nil, invalid("maxCount must be a positive integer")
	}
	requestedMaxCount := maxCount
	maxCount = min(maxCount, 4000)
	var result any
	if p.Type == "ping" {
		result, e = h.pingRecords(r, s, ids, taskID, start, end, int(maxCount), method == "common:getRecords")
	} else {
		result, e = h.loadRecords(r, s, ids, p.LoadType, start, end, int(maxCount), method == "common:getRecords")
	}
	if out, ok := result.(map[string]any); ok && e == nil {
		out["captain"] = map[string]any{"request_limited": requestedMaxCount > maxCount, "requested_max_count": requestedMaxCount, "effective_max_count": maxCount, "total_point_limit": maxQueryPoints}
	}
	return result, e
}

var loadFields = map[string][]string{
	"cpu": {"cpu"}, "gpu": {"gpu"}, "ram": {"ram", "ram_total"}, "swap": {"swap", "swap_total"}, "load": {"load"}, "temp": {"temp"}, "disk": {"disk", "disk_total"},
	"network": {"net_in", "net_out", "net_total_up", "net_total_down", "traffic_up", "traffic_down"}, "process": {"process"}, "connections": {"connections", "connections_udp"},
}

func (h *Handler) loadRecords(r *http.Request, s probedata.Snapshot, ids []string, loadType string, start, end time.Time, max int, grouped bool) (any, *rpcError) {
	fields, filter := loadFields[loadType]
	if loadType != "" && loadType != "all" && !filter {
		return nil, invalid("Invalid load_type")
	}
	defs, err := h.d.Reader.Definitions(r.Context(), probedata.Scope{})
	if err != nil {
		return nil, fromError(err)
	}
	available := map[string]bool{}
	for _, d := range defs {
		available[d.Key] = d.History
	}
	keys := []string{}
	missing := []string{}
	for field, key := range recordKeys {
		if filter && !contains(fields, field) {
			continue
		}
		if available[key] {
			keys = append(keys, key)
		} else {
			missing = append(missing, field)
		}
	}
	sort.Strings(keys)
	sort.Strings(missing)
	perNode := max
	if len(ids) > 0 {
		perNode = max / len(ids)
		if perNode < 1 {
			perNode = 1
		}
	}
	if len(keys)*len(ids)*perNode > maxQueryPoints && len(ids)*len(keys) > 0 {
		perNode = maxQueryPoints / (len(ids) * len(keys))
		if perNode < 1 {
			return nil, invalid("Too many record series")
		}
	}
	series := []probedata.Series{}
	if len(keys) > 0 && len(ids) > 0 {
		// Legacy load records use sums for interval traffic and the last sample
		// for lifetime counters. Averaging either would understate transferred
		// bytes or turn a counter into a value that was never observed.
		aggregations := map[string]string{}
		for _, key := range keys {
			switch key {
			case "traffic.up", "traffic.down":
				aggregations[key] = "sum"
			case "net.total.up", "net.total.down":
				aggregations[key] = "last"
			}
		}
		series, err = h.d.Reader.Query(r.Context(), probedata.Scope{}, probedata.Query{MetricKeys: keys, EntityIDs: ids, Start: start, End: end, MaxPoints: perNode, Aggregation: "avg", AggregationByMetric: aggregations})
		if err != nil {
			return nil, fromError(err)
		}
	}
	byNode := map[string]map[int64]map[string]*float64{}
	for _, ser := range series {
		if !contains(ids, ser.EntityID) {
			continue
		}
		for _, pt := range ser.Points {
			if pt.Time == nil || pt.Value == nil {
				continue
			}
			if byNode[ser.EntityID] == nil {
				byNode[ser.EntityID] = map[int64]map[string]*float64{}
			}
			ts := pt.Time.UnixNano()
			if byNode[ser.EntityID][ts] == nil {
				byNode[ser.EntityID][ts] = map[string]*float64{}
			}
			byNode[ser.EntityID][ts][ser.MetricKey] = pt.Value
		}
	}
	all := []map[string]any{}
	groups := map[string][]map[string]any{}
	for _, id := range ids {
		n, _ := nodeByID(s, id)
		times := []int64{}
		for ts := range byNode[id] {
			times = append(times, ts)
		}
		sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
		arr := []map[string]any{}
		for _, ts := range times {
			at := time.Unix(0, ts).UTC()
			row := flatRecord(n, &at, byNode[id][ts], false)
			if filter {
				row2 := map[string]any{"client": id, "time": at, "captain": row["captain"]}
				for _, field := range fields {
					row2[field] = row[field]
				}
				row = row2
			}
			arr = append(arr, row)
		}
		if len(arr) > perNode {
			arr = sampleRows(arr, perNode)
		}
		groups[id] = arr
		all = append(all, arr...)
	}
	if len(all) > max {
		all = sampleRows(all, max)
		groups = map[string][]map[string]any{}
		for _, row := range all {
			id := row["client"].(string)
			groups[id] = append(groups[id], row)
		}
	}
	out := map[string]any{"count": len(all), "records": all, "captain": map[string]any{"unavailable_fields": missing, "historical_totals": "unknown unless recorded", "max_points": max}}
	if filter {
		out["load_type"] = loadType
	}
	if grouped {
		out["records"] = groups
		out["from"] = start
		out["to"] = end
	}
	if loadType == "" || loadType == "all" || loadType == "gpu" {
		out["has_gpu_data"] = false
	}
	return out, nil
}
func sampleRows(in []map[string]any, n int) []map[string]any {
	if len(in) <= n {
		return in
	}
	out := make([]map[string]any, n)
	for i := range out {
		at := 0
		if n > 1 {
			at = i * (len(in) - 1) / (n - 1)
		}
		out[i] = in[at]
	}
	return out
}

func (h *Handler) pingRecords(r *http.Request, s probedata.Snapshot, ids []string, legacyID string, start, end time.Time, max int, common bool) (any, *rpcError) {
	tags := map[string]string{}
	taskIDs := []string{}
	if legacyID != "" && legacyID != "-1" && legacyID != "0" {
		id, ok := publicTaskID(legacyID, s.PingTasks)
		if !ok {
			return nil, invalid("Task not found")
		}
		tags["task_id"] = id
		taskIDs = append(taskIDs, id)
	}
	seriesCount := selectedPingSeries(s.PingTasks, ids, tags["task_id"])
	per := max
	if seriesCount > 0 {
		per = max / seriesCount
		if per < 1 {
			per = 1
		}
		// Each task consumes latency and loss observations before they are
		// merged into one legacy row, so both series count against the bound.
		if 2*seriesCount*per > maxQueryPoints {
			return nil, invalid("Too many ping series")
		}
	}
	series := []probedata.Series{}
	stats := []probedata.PingStat{}
	if seriesCount > 0 {
		var err error
		series, err = h.d.Reader.Query(r.Context(), probedata.Scope{}, probedata.Query{MetricKeys: []string{"ping.latency_ms", "ping.loss"}, EntityIDs: ids, Start: start, End: end, Tags: tags, MaxPoints: per, Aggregation: "avg"})
		if err != nil {
			return nil, fromError(err)
		}
		stats, err = h.d.Reader.PingStats(r.Context(), probedata.Scope{}, probedata.PingQuery{EntityIDs: ids, TaskIDs: taskIDs, Start: start, End: end})
		if err != nil {
			return nil, fromError(err)
		}
	}
	type pointKey struct {
		node, task string
		ts         int64
	}
	points := map[pointKey]map[string]any{}
	for _, ser := range series {
		task := ser.Tags["task_id"]
		if _, ok := publicTaskID(task, s.PingTasks); !ok {
			continue
		}
		for _, p := range ser.Points {
			if p.Time == nil || p.Value == nil {
				continue
			}
			key := pointKey{ser.EntityID, task, p.Time.UnixNano()}
			row := points[key]
			if row == nil {
				row = map[string]any{"task_id": legacyTaskID(task, s.PingTasks), "client": ser.EntityID, "time": p.Time, "value": nil, "captain": map[string]any{"task_id": task, "aggregated": ser.Downsampled}}
				points[key] = row
			}
			if ser.MetricKey == "ping.latency_ms" {
				row["value"] = *p.Value
			} else {
				row["captain"].(map[string]any)["loss_ratio"] = *p.Value
				if *p.Value >= 1 {
					row["value"] = -1
				}
			}
		}
	}
	order := make([]pointKey, 0, len(points))
	for key := range points {
		order = append(order, key)
	}
	sort.Slice(order, func(i, j int) bool {
		if order[i].ts != order[j].ts {
			return order[i].ts < order[j].ts
		}
		if order[i].node != order[j].node {
			return order[i].node < order[j].node
		}
		return order[i].task < order[j].task
	})
	rows := []map[string]any{}
	for _, key := range order {
		row := points[key]
		if row["value"] != nil {
			rows = append(rows, row)
		}
	}
	rows = sampleRows(rows, max)
	tasks := []map[string]any{}
	basic := map[string]map[string]any{}
	for _, task := range s.PingTasks {
		if len(taskIDs) > 0 && !contains(taskIDs, task.ID) {
			continue
		}
		included := false
		for _, id := range ids {
			if contains(task.EntityIDs, id) {
				included = true
			}
		}
		if !included {
			continue
		}
		var total, valid int
		sum := 0.0
		var min, maxValue *float64
		for _, st := range stats {
			if st.TaskID != task.ID {
				continue
			}
			total += st.Total
			valid += st.Valid
			if st.Avg != nil {
				sum += *st.Avg * float64(st.Valid)
			}
			min = lower(min, st.Min)
			maxValue = higher(maxValue, st.Max)
		}
		item := taskDTO(task, 0)
		item["total"] = total
		item["loss"] = percentage(total-valid, total)
		item["min"] = min
		item["max"] = maxValue
		item["avg"] = nil
		if valid > 0 {
			item["avg"] = sum / float64(valid)
		}
		tasks = append(tasks, item)
	}
	for _, id := range ids {
		total, valid := 0, 0
		var min, maxValue *float64
		for _, st := range stats {
			if st.EntityID != id {
				continue
			}
			total += st.Total
			valid += st.Valid
			min = lower(min, st.Min)
			maxValue = higher(maxValue, st.Max)
		}
		if total > 0 {
			basic[id] = map[string]any{"client": id, "loss": percentage(total-valid, total), "min": min, "max": maxValue}
		}
	}
	basicRows := []map[string]any{}
	for _, id := range ids {
		if row, ok := basic[id]; ok {
			basicRows = append(basicRows, row)
		}
	}
	out := map[string]any{"count": len(rows), "records": rows, "basic_info": basicRows, "tasks": tasks, "captain": map[string]any{"statistics": stats, "loss_semantics": "attempt-weighted", "point_limit": max}}
	if common {
		out["from"] = start
		out["to"] = end
	}
	return out, nil
}
func percentage(n, d int) any {
	if d == 0 {
		return nil
	}
	return float64(n) * 100 / float64(d)
}
func lower(a, b *float64) *float64 {
	if b == nil {
		return a
	}
	if a == nil || *b < *a {
		return b
	}
	return a
}
func higher(a, b *float64) *float64 {
	if b == nil {
		return a
	}
	if a == nil || *b > *a {
		return b
	}
	return a
}
