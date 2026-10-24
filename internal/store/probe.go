package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

// ProbeSettings is the admin-edited monitoring configuration.
type ProbeSettings struct {
	Appearance     *ProbeAppearance `json:"appearance,omitempty"`
	PublicSections *[]string        `json:"public_sections,omitempty"`
	Layout         string           `json:"layout,omitempty"` // grid | compact

	PageEnabled *bool `json:"page_enabled,omitempty"` // nil preserves the pre-v1.7 enabled-page behavior
	Enabled     bool  `json:"enabled"`
	BeatSeconds int   `json:"beat_seconds"` // 5..60, default 10
	CarrierPing bool  `json:"carrier_ping"` // TCP-connect latency to the carrier points
	// Carriers replaces the default CT/CU/CM probe points when set.
	Carriers   []spec.Carrier `json:"carriers"`
	Path       string         `json:"path"`       // "/status"; "" disables the path mode
	Hosts      []string       `json:"hosts"`      // dedicated hostnames serving only the probe page
	Visibility string         `json:"visibility"` // public | users | admins
	Title      string         `json:"title"`
	Logo       string         `json:"logo"`
	ShowGlobe  bool           `json:"show_globe"`
	ShowIP     bool           `json:"show_ip"`
	Alerts     struct {
		OfflineSeconds int  `json:"offline_seconds"` // grace before an offline notice, default 180
		CPUPct         int  `json:"cpu_pct"`         // 0 = off
		MemPct         int  `json:"mem_pct"`
		DiskPct        int  `json:"disk_pct"`
		WindowMinutes  int  `json:"window_minutes"` // sustained-average window, default 5
		Traffic        bool `json:"traffic"`        // 80% / 100% of the node monthly limit
	} `json:"alerts"`
}

// Presets are bundled UI styles, never operator-supplied CSS or scripts.
type ProbeAppearance struct {
	Preset string `json:"preset"` // inherit | aurora | paper | terminal | glass
	Scheme string `json:"scheme"` // inherit | auto | light | dark
}

func (p *ProbeAppearance) Normalize() {
	if p.Preset == "" {
		p.Preset = "inherit"
	}
	if p.Scheme == "" {
		p.Scheme = "inherit"
	}
}

func (p ProbeAppearance) Valid() bool {
	p.Normalize()
	return (p.Preset == "inherit" || p.Preset == "aurora" || p.Preset == "paper" || p.Preset == "terminal" || p.Preset == "glass") &&
		(p.Scheme == "inherit" || p.Scheme == "auto" || p.Scheme == "light" || p.Scheme == "dark")
}

func (p ProbeSettings) PageOn() bool { return p.Enabled && (p.PageEnabled == nil || *p.PageEnabled) }

// PublicShows defaults to the pre-1.7 display when the field is omitted.
func (p ProbeSettings) PublicShows(section string) bool {
	if p.PublicSections == nil {
		return true
	}
	for _, s := range *p.PublicSections {
		if s == section {
			return true
		}
	}
	return false
}

// SettingProbe is the settings key.
const SettingProbe = "probe"

// Normalize fills defaults.
func (p *ProbeSettings) Normalize() {
	if p.BeatSeconds < 3 || p.BeatSeconds > 300 {
		p.BeatSeconds = 10
	}
	switch p.Visibility {
	case "users", "admins":
	default:
		p.Visibility = "public"
	}
	if p.Path == "" && len(p.Hosts) == 0 {
		p.Path = "/status"
	}
	if p.Alerts.OfflineSeconds <= 0 {
		p.Alerts.OfflineSeconds = 180
	}
	if p.Alerts.WindowMinutes <= 0 {
		p.Alerts.WindowMinutes = 5
	}
}

// PingTask is a latency check the nodes run.
type PingTask struct {
	ID              int64   `json:"id"`
	Name            string  `json:"name"`
	Type            string  `json:"type"`
	Target          string  `json:"target"`
	IntervalSeconds int     `json:"interval_seconds"`
	NodeIDs         []int64 `json:"node_ids"` // empty = all
	Enabled         bool    `json:"enabled"`
}

func (s *Store) ListPingTasks(ctx context.Context) ([]PingTask, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, type, target, interval_seconds, node_ids_json, enabled FROM ping_tasks ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PingTask{}
	for rows.Next() {
		var t PingTask
		var ids string
		var en int
		if err := rows.Scan(&t.ID, &t.Name, &t.Type, &t.Target, &t.IntervalSeconds, &ids, &en); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(ids), &t.NodeIDs)
		t.Enabled = en == 1
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) SavePingTask(ctx context.Context, t *PingTask) error {
	ids, _ := json.Marshal(t.NodeIDs)
	if t.NodeIDs == nil {
		ids = []byte("[]")
	}
	if t.ID == 0 {
		res, err := s.db.ExecContext(ctx, `INSERT INTO ping_tasks (name, type, target, interval_seconds, node_ids_json, enabled, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			t.Name, t.Type, t.Target, t.IntervalSeconds, string(ids), boolInt(t.Enabled), now())
		if err != nil {
			return err
		}
		t.ID, _ = res.LastInsertId()
		return nil
	}
	_, err := s.db.ExecContext(ctx, `UPDATE ping_tasks SET name = ?, type = ?, target = ?, interval_seconds = ?, node_ids_json = ?, enabled = ? WHERE id = ?`,
		t.Name, t.Type, t.Target, t.IntervalSeconds, string(ids), boolInt(t.Enabled), t.ID)
	return err
}

func (s *Store) DeletePingTask(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM ping_tasks WHERE id = ?`, id)
	return err
}

// NodeProbe is the per-node probe configuration and monthly traffic state.
type NodeProbe struct {
	Storage          NodeStorage          `json:"storage"`
	RegionMode       string               `json:"region_mode"`
	RegionDetection  RegionDetection      `json:"region_detection"`
	Resources        spec.ResourceOptions `json:"resources"`
	counters         networkCounters
	NodeID           int64         `json:"node_id"`
	Hidden           bool          `json:"hidden"`
	Info             NodeProbeInfo `json:"info"`
	LimitBytes       int64         `json:"limit_bytes"`
	ResetDay         int           `json:"reset_day"`
	Mode             string        `json:"mode"`
	PeriodStart      time.Time     `json:"period_start"`
	UsedUp           int64         `json:"used_up"`
	UsedDown         int64         `json:"used_down"`
	PrevUsed         int64         `json:"prev_used"`
	lastUp, lastDown int64
}

// NodeProbeInfo is what the public page may show about a server.
type NodeProbeInfo struct {
	AutoRenewal  *NodeAutoRenewal  `json:"auto_renewal,omitempty"`
	Bandwidth    *NodeBandwidth    `json:"bandwidth,omitempty"`
	Billing      *NodeCost         `json:"billing,omitempty"`
	TrafficReset *NodeTrafficReset `json:"traffic_reset,omitempty"`
	Region       string            `json:"region,omitempty"`   // ISO country code, e.g. "JP"
	Provider     string            `json:"provider,omitempty"` //
	ProviderURL  string            `json:"provider_url,omitempty"`
	Price        string            `json:"price,omitempty"`      // free text, "$5/mo"
	ExpiresAt    string            `json:"expires_at,omitempty"` // YYYY-MM-DD
	Note         string            `json:"note,omitempty"`
}

// Billed returns the bytes that count against the limit under Mode.
func (p NodeProbe) Billed() int64 {
	switch p.Mode {
	case "up":
		return p.UsedUp
	case "down":
		return p.UsedDown
	case "max":
		if p.UsedUp > p.UsedDown {
			return p.UsedUp
		}
		return p.UsedDown
	}
	return p.UsedUp + p.UsedDown
}

func scanNodeProbe(row interface{ Scan(...any) error }) (*NodeProbe, error) {
	var p NodeProbe
	var hidden int
	var info, resources, counters, observation, detection, storage string
	var start int64
	if err := row.Scan(&p.NodeID, &hidden, &info, &p.LimitBytes, &p.ResetDay, &p.Mode, &start, &p.UsedUp, &p.UsedDown, &p.lastUp, &p.lastDown, &p.PrevUsed, &resources, &counters, &p.RegionMode, &observation, &detection, &storage); err != nil {
		return nil, wrapNotFound(err)
	}
	p.Hidden = hidden == 1
	_ = json.Unmarshal([]byte(info), &p.Info)
	_ = json.Unmarshal([]byte(storage), &p.Storage)
	p.Storage.normalize()
	var obs AddressObservation
	_ = json.Unmarshal([]byte(observation), &obs)
	_ = json.Unmarshal([]byte(detection), &p.RegionDetection)
	p.RegionMode, p.RegionDetection = resolveRegion(&p.Info, p.RegionMode, obs, p.RegionDetection)
	_ = json.Unmarshal([]byte(resources), &p.Resources)
	_ = json.Unmarshal([]byte(counters), &p.counters)
	if start > 0 {
		p.PeriodStart = time.Unix(start, 0)
	}
	return &p, nil
}

const nodeProbeCols = `id, probe_hidden, probe_info_json, traffic_limit_bytes, traffic_reset_day, traffic_mode, traffic_period_start, traffic_used_up, traffic_used_down, traffic_last_up, traffic_last_down, traffic_prev_used, probe_resources_json, probe_counters_json, region_mode, address_json, region_detection_json, probe_storage_json`

func (s *Store) NodeProbe(ctx context.Context, nodeID int64) (*NodeProbe, error) {
	return scanNodeProbe(s.db.QueryRowContext(ctx, `SELECT `+nodeProbeCols+` FROM nodes WHERE id = ?`, nodeID))
}

func (s *Store) ListNodeProbes(ctx context.Context) (map[int64]*NodeProbe, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+nodeProbeCols+` FROM nodes`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]*NodeProbe{}
	for rows.Next() {
		p, err := scanNodeProbe(rows)
		if err != nil {
			return nil, err
		}
		out[p.NodeID] = p
	}
	return out, rows.Err()
}

// UpdateNodeProbe stores the admin-edited part (not the counters).
func (s *Store) UpdateNodeProbe(ctx context.Context, nodeID int64, hidden bool, info NodeProbeInfo, limit int64, resetDay int, mode string, resources ...*spec.ResourceOptions) error {
	var opts *spec.ResourceOptions
	if len(resources) > 0 {
		opts = resources[0]
	}
	return s.UpdateNodeProbeWithRegion(ctx, nodeID, hidden, info, limit, resetDay, mode, opts, nil)
}

// Omitted mode preserves auto when a legacy client echoes the effective region.
// A changed country from an old client remains an explicit manual/off choice.
func (s *Store) UpdateNodeProbeWithRegion(ctx context.Context, nodeID int64, hidden bool, info NodeProbeInfo, limit int64, resetDay int, mode string, resources *spec.ResourceOptions, requested *string, storage ...*NodeStorage) error {
	return s.UpdateNodeProbeWithExpiryGuard(ctx, nodeID, hidden, info, limit, resetDay, mode, resources, requested, nil, storage...)
}

// Expected expiry distinguishes an unchanged form echo from an intentional
// date edit when an automatic renewal happened while the form was open.
func (s *Store) UpdateNodeProbeWithExpiryGuard(ctx context.Context, nodeID int64, hidden bool, info NodeProbeInfo, limit int64, resetDay int, mode string, resources *spec.ResourceOptions, requested, expectedExpiry *string, storage ...*NodeStorage) error {
	if resetDay < 1 || resetDay > 31 {
		resetDay = 1
	}
	switch mode {
	case "up", "down", "max":
	default:
		mode = "sum"
	}
	info.Region = strings.ToUpper(strings.TrimSpace(info.Region))
	var resourceJSON any
	var storageJSON any
	if len(storage) > 0 && storage[0] != nil {
		selected := *storage[0]
		selected.Items = append([]NodeStorageItem(nil), selected.Items...)
		if err := selected.Validate(); err != nil {
			return err
		}
		raw, _ := json.Marshal(selected)
		storageJSON = string(raw)
	}
	if resources != nil {
		if err := resources.Validate(); err != nil {
			return err
		}
		raw, _ := json.Marshal(resources)
		resourceJSON = string(raw)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	old, err := scanNodeProbe(tx.QueryRowContext(ctx, `SELECT `+nodeProbeCols+` FROM nodes WHERE id=?`, nodeID))
	if err != nil {
		return err
	}
	if expectedExpiry != nil && old.Info.ExpiresAt != *expectedExpiry {
		if info.ExpiresAt != *expectedExpiry {
			return ErrNodeExpiryConflict
		}
		info.ExpiresAt = old.Info.ExpiresAt
	}
	if err := normalizeNodeBandwidth(&info, old.Info); err != nil {
		return err
	}
	if err := normalizeNodeCost(&info, old.Info); err != nil {
		return err
	}
	selected := old.RegionMode
	if requested != nil {
		selected = *requested
	} else if info.Region != old.Info.Region {
		selected = "manual"
		if info.Region == "" {
			selected = "off"
		}
	}
	switch selected {
	case "auto", "off":
		info.Region = ""
	case "manual":
		if !ValidRegion(info.Region) {
			return ErrNodeRegion
		}
	default:
		return ErrNodeRegion
	}
	// A schedule edit keeps accumulated usage; the next new boundary resets it.
	next := *old
	next.Info, next.ResetDay = info, resetDay
	at := time.Now()
	if !nodePeriodStart(at, old).Equal(nodePeriodStart(at, &next)) {
		if _, err := tx.ExecContext(ctx, `UPDATE nodes SET traffic_period_start=? WHERE id=?`, nodePeriodStart(at, &next).Unix(), nodeID); err != nil {
			return err
		}
	}
	b, _ := json.Marshal(info)
	_, err = tx.ExecContext(ctx, `UPDATE nodes SET probe_hidden=?,probe_info_json=?,traffic_limit_bytes=?,traffic_reset_day=?,traffic_mode=?,probe_resources_json=COALESCE(?,probe_resources_json),probe_storage_json=COALESCE(?,probe_storage_json),region_mode=?,updated_at=? WHERE id=?`, boolInt(hidden), string(b), limit, resetDay, mode, resourceJSON, storageJSON, selected, now(), nodeID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// ResetNodeTraffic zeroes the current period by hand.
func (s *Store) ResetNodeTraffic(ctx context.Context, nodeID int64, at time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	p, err := scanNodeProbe(tx.QueryRowContext(ctx, `SELECT `+nodeProbeCols+` FROM nodes WHERE id=?`, nodeID))
	if err != nil {
		return err
	}
	// Clearing a counter is not a network observation. Keep the NIC baselines,
	// but wait for a real delta before presenting the new period as measured.
	p.counters.TrafficKnown, p.counters.TrafficAt = false, 0
	counters, err := json.Marshal(p.counters)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE nodes SET traffic_used_up = 0, traffic_used_down = 0, traffic_period_start = ?, probe_counters_json=?, updated_at = ? WHERE id = ?`, at.Unix(), string(counters), now(), nodeID); err != nil {
		return err
	}
	return tx.Commit()
}

// periodStart returns the start of the billing period containing at.
func periodStart(at time.Time, resetDay int) time.Time {
	if resetDay < 1 || resetDay > 31 {
		resetDay = 1
	}
	return nodePeriodStart(at, &NodeProbe{ResetDay: resetDay})
}

// RecordBeat folds one host sample into the minute/hour/day buckets and the
// node's monthly NIC traffic. Counter deltas are reset-aware: a counter
// that went backwards (agent restart, reboot) contributes nothing.
func (s *Store) RecordBeat(ctx context.Context, nodeID int64, h spec.SystemStatus, at time.Time) error {
	_, err := s.recordBeat(ctx, nodeID, h, at, "")
	return err
}

// RecordNodeBeat requires the current pairing identity and may maintain expiry.
// The legacy internal entry point remains available to import/test samples.
func (s *Store) RecordNodeBeat(ctx context.Context, nodeID int64, tokenHash string, h spec.SystemStatus, at time.Time) (bool, error) {
	if tokenHash == "" {
		return false, ErrNotFound
	}
	return s.recordBeat(ctx, nodeID, h, at, tokenHash)
}

func (s *Store) recordBeat(ctx context.Context, nodeID int64, h spec.SystemStatus, at time.Time, tokenHash string) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if tokenHash != "" {
		if err := requireNodeTokenTx(ctx, tx, nodeID, tokenHash); err != nil {
			return false, err
		}
	}
	p, err := scanNodeProbe(tx.QueryRowContext(ctx, `SELECT `+nodeProbeCols+` FROM nodes WHERE id = ?`, nodeID))
	if err != nil {
		return false, err
	}
	if r := h.Resources; r != nil && r.Epoch != "" && r.Epoch == p.counters.Epoch && r.Sequence <= p.counters.Sequence {
		return false, ErrStaleBeat
	}
	renewed := false
	if tokenHash != "" {
		renewed, err = renewNodeExpiryTx(ctx, tx, p, tokenHash, h, at, true)
		if err != nil {
			return false, err
		}
	}
	if err := recordResourceHistory(ctx, tx, nodeID, h, at, p.counters); err != nil {
		return false, err
	}
	valid := h.Validity()
	h = validHost(h)
	for _, b := range []struct {
		res  string
		size time.Duration
	}{{"m", time.Minute}, {"h", time.Hour}, {"d", 24 * time.Hour}} {
		ts := at.Truncate(b.size).Unix()
		if b.res == "d" {
			y, m, d := at.Date()
			ts = time.Date(y, m, d, 0, 0, 0, 0, at.Location()).Unix()
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO node_stats (node_id, res, ts, samples, cpu, mem_used, mem_total, swap_used, disk_used, disk_total, net_up, net_down, load1, tcp, udp, procs, cpu_n, mem_n, swap_n, disk_n, net_n, load_n, connections_n, procs_n)
			VALUES (?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(node_id, res, ts) DO UPDATE SET samples = samples + 1, cpu = cpu + excluded.cpu, mem_used = mem_used + excluded.mem_used, mem_total = CASE WHEN excluded.mem_n > 0 THEN excluded.mem_total ELSE mem_total END,
			swap_used = swap_used + excluded.swap_used, disk_used = disk_used + excluded.disk_used, disk_total = CASE WHEN excluded.disk_n > 0 THEN excluded.disk_total ELSE disk_total END, net_up = net_up + excluded.net_up, net_down = net_down + excluded.net_down,
			load1 = load1 + excluded.load1, tcp = tcp + excluded.tcp, udp = udp + excluded.udp, procs = procs + excluded.procs, cpu_n = cpu_n + excluded.cpu_n, mem_n = mem_n + excluded.mem_n, swap_n = swap_n + excluded.swap_n, disk_n = disk_n + excluded.disk_n, net_n = net_n + excluded.net_n, load_n = load_n + excluded.load_n, connections_n = connections_n + excluded.connections_n, procs_n = procs_n + excluded.procs_n`,
			nodeID, b.res, ts, h.CPUPercent, float64(h.MemUsed), h.MemTotal, float64(h.SwapUsed), float64(h.DiskUsed), h.DiskTotal, float64(h.NetUp), float64(h.NetDown), h.Load1, h.TCP, h.UDP, h.Processes, boolInt(valid.CPU), boolInt(valid.Memory), boolInt(valid.Swap), boolInt(valid.Disk), boolInt(valid.Network), boolInt(valid.Load), boolInt(valid.Connections), boolInt(valid.Processes)); err != nil {
			return false, err
		}
	}
	if err := recordPingHistory(ctx, tx, nodeID, h, at); err != nil {
		return false, err
	}

	// Monthly NIC traffic.
	start := nodePeriodStart(at, p)
	usedUp, usedDown, prev := p.UsedUp, p.UsedDown, p.PrevUsed
	if p.PeriodStart.IsZero() || start.After(p.PeriodStart) {
		if !p.PeriodStart.IsZero() {
			prev = p.Billed()
		}
		usedUp, usedDown = 0, 0
		p.counters.TrafficKnown, p.counters.TrafficAt = false, 0
	} else {
		// A manual reset starts a partial period. Keep its actual start until
		// the next scheduled boundary instead of relabeling it on every beat.
		start = p.PeriodStart
	}
	measured := false
	if h.Resources != nil {
		deltaUp, deltaDown, counters, validDelta := nextNetworkCounters(p.counters, h.Resources)
		usedUp += deltaUp
		usedDown += deltaDown
		p.counters = counters
		measured = validDelta
		// Do not bridge the old aggregate baseline across a change of protocol.
		p.lastUp, p.lastDown = 0, 0
	} else if h.NetTotalUp > 0 || h.NetTotalDown > 0 {
		up, down := int64(h.NetTotalUp), int64(h.NetTotalDown)
		if p.lastUp > 0 && up >= p.lastUp {
			usedUp += up - p.lastUp
			measured = true
		}
		if p.lastDown > 0 && down >= p.lastDown {
			usedDown += down - p.lastDown
			measured = true
		}
		p.lastUp, p.lastDown = up, down
	}
	if measured {
		p.counters.TrafficKnown, p.counters.TrafficAt = true, at.Unix()
	}
	counterJSON, _ := json.Marshal(p.counters)
	if _, err := tx.ExecContext(ctx, `UPDATE nodes SET traffic_period_start = ?, traffic_used_up = ?, traffic_used_down = ?, traffic_last_up = ?, traffic_last_down = ?, traffic_prev_used = ?, probe_counters_json = ?, last_seen_at = ? WHERE id = ?`,
		start.Unix(), usedUp, usedDown, p.lastUp, p.lastDown, prev, string(counterJSON), at.Unix(), nodeID); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return renewed, nil
}

// StatPoint is one averaged bucket.
type StatPoint struct {
	Valid     spec.MetricValidity `json:"valid"`
	TS        int64               `json:"ts"`
	Samples   int                 `json:"n"`
	CPU       float64             `json:"cpu"`
	MemUsed   float64             `json:"mem_used"`
	MemTotal  int64               `json:"mem_total"`
	SwapUsed  float64             `json:"swap_used"`
	DiskUsed  float64             `json:"disk_used"`
	DiskTotal int64               `json:"disk_total"`
	NetUp     float64             `json:"net_up"`
	NetDown   float64             `json:"net_down"`
	Load1     float64             `json:"load1"`
	TCP       float64             `json:"tcp"`
	UDP       float64             `json:"udp"`
	Procs     float64             `json:"procs"`
}

// NodeStats returns averaged buckets in [from, to].
func (s *Store) NodeStats(ctx context.Context, nodeID int64, res string, from, to time.Time) ([]StatPoint, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT ts, samples, cpu, mem_used, mem_total, swap_used, disk_used, disk_total, net_up, net_down, load1, tcp, udp, procs, cpu_n, mem_n, swap_n, disk_n, net_n, load_n, connections_n, procs_n
		FROM node_stats WHERE node_id = ? AND res = ? AND ts >= ? AND ts <= ? ORDER BY ts`, nodeID, res, from.Unix(), to.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StatPoint{}
	for rows.Next() {
		var p StatPoint
		var cpuN, memN, swapN, diskN, netN, loadN, connN, procsN int
		if err := rows.Scan(&p.TS, &p.Samples, &p.CPU, &p.MemUsed, &p.MemTotal, &p.SwapUsed, &p.DiskUsed, &p.DiskTotal, &p.NetUp, &p.NetDown, &p.Load1, &p.TCP, &p.UDP, &p.Procs, &cpuN, &memN, &swapN, &diskN, &netN, &loadN, &connN, &procsN); err != nil {
			return nil, err
		}
		p.Valid = spec.MetricValidity{CPU: cpuN > 0, Memory: memN > 0, Swap: swapN > 0, Disk: diskN > 0, Network: netN > 0, Load: loadN > 0, Connections: connN > 0, Processes: procsN > 0}
		for _, v := range []struct {
			value *float64
			count int
		}{{&p.CPU, cpuN}, {&p.MemUsed, memN}, {&p.SwapUsed, swapN}, {&p.DiskUsed, diskN}, {&p.NetUp, netN}, {&p.NetDown, netN}, {&p.Load1, loadN}, {&p.TCP, connN}, {&p.UDP, connN}, {&p.Procs, procsN}} {
			if v.count > 0 {
				*v.value /= float64(v.count)
			}
		}

		out = append(out, p)
	}
	return out, rows.Err()
}

// PingPoint is one averaged latency bucket.
type PingPoint struct {
	Quality *PingStatsQuality `json:"quality,omitempty"`
	TaskID  int64             `json:"task_id"`
	Name    string            `json:"name"`
	TS      int64             `json:"ts"`
	Samples int               `json:"n"`
	Lost    int               `json:"lost"`
	AvgMs   float64           `json:"avg_ms"`   // -1 when every sample was lost
	AvgMbps float64           `json:"avg_mbps"` // download tasks only
}

func (s *Store) NodePingStats(ctx context.Context, nodeID int64, res string, from, to time.Time) ([]PingPoint, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT task_id, name, ts, samples, lost, sum_ms, sum_mbps, quality_json FROM node_ping_stats WHERE node_id = ? AND res = ? AND ts >= ? AND ts <= ? ORDER BY task_id, name, ts`, nodeID, res, from.Unix(), to.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PingPoint{}
	for rows.Next() {
		var p PingPoint
		var sum, sumMbps float64
		var qualityJSON string
		if err := rows.Scan(&p.TaskID, &p.Name, &p.TS, &p.Samples, &p.Lost, &sum, &sumMbps, &qualityJSON); err != nil {
			return nil, err
		}
		var quality networkAggregate
		if err := json.Unmarshal([]byte(qualityJSON), &quality); err != nil {
			return nil, err
		}
		p.Quality = quality.summary()
		if okN := p.Samples - p.Lost; okN > 0 {
			p.AvgMs = sum / float64(okN)
			p.AvgMbps = sumMbps / float64(okN)
		} else {
			p.AvgMs = -1
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SustainedAverage returns the average of a metric over the last window
// of minute buckets (for threshold alerts); ok=false without data.
func (s *Store) SustainedAverage(ctx context.Context, nodeID int64, metric string, window time.Duration, at time.Time) (float64, bool) {
	var expr string
	switch metric {
	case "cpu":
		expr = "SUM(cpu) / NULLIF(SUM(cpu_n), 0)"
	case "mem":
		expr = "100.0 * SUM(mem_used) / NULLIF(SUM(mem_n), 0) / MAX(mem_total)"
	case "disk":
		expr = "100.0 * SUM(disk_used) / NULLIF(SUM(disk_n), 0) / MAX(disk_total)"
	default:
		return 0, false
	}
	var v sql.NullFloat64
	err := s.db.QueryRowContext(ctx, `SELECT `+expr+` FROM node_stats WHERE node_id = ? AND res = 'm' AND ts >= ?`, nodeID, at.Add(-window).Unix()).Scan(&v)
	if err != nil || !v.Valid {
		return 0, false
	}
	return v.Float64, true
}

// PruneStats drops buckets past their retention: minutes 48h, hours 60d,
// days 2y.
func (s *Store) PruneStats(ctx context.Context, at time.Time) error {
	for _, r := range []struct {
		res  string
		keep time.Duration
	}{{"m", 48 * time.Hour}, {"h", 60 * 24 * time.Hour}, {"d", 730 * 24 * time.Hour}} {
		cut := at.Add(-r.keep).Unix()
		if _, err := s.db.ExecContext(ctx, `DELETE FROM node_stats WHERE res = ? AND ts < ?`, r.res, cut); err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, `DELETE FROM node_ping_stats WHERE res = ? AND ts < ?`, r.res, cut); err != nil {
			return err
		}
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM node_ping_cursors WHERE seen_at<?`, at.Add(-7*24*time.Hour).Unix()); err != nil {
		return err
	}
	if err := s.pruneResourceHistory(ctx, at); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM node_jobs WHERE kind = 'network_diagnostic' AND route_check = 0 AND created_at < ?`, at.Add(-24*time.Hour).Unix()); err != nil {
		return err
	}
	return s.pruneMonitoring(ctx, at)
}

// AlertOnce records kind for node unless it fired within the cool-down;
// true means the caller should notify.
func (s *Store) AlertOnce(ctx context.Context, nodeID int64, kind string, cooldown time.Duration, at time.Time) (bool, error) {
	var fired sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT fired_at FROM probe_alerts WHERE node_id = ? AND kind = ?`, nodeID, kind).Scan(&fired)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if fired.Valid && at.Sub(time.Unix(fired.Int64, 0)) < cooldown {
		return false, nil
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO probe_alerts (node_id, kind, fired_at) VALUES (?, ?, ?) ON CONFLICT(node_id, kind) DO UPDATE SET fired_at = excluded.fired_at`, nodeID, kind, at.Unix())
	return err == nil, err
}

// ClearAlert forgets a fired alert so the next occurrence notifies again
// (e.g. node back online).
func (s *Store) ClearAlert(ctx context.Context, nodeID int64, kind string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM probe_alerts WHERE node_id = ? AND kind = ?`, nodeID, kind)
	return err
}
