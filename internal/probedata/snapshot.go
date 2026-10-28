package probedata

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"net/netip"
	"slices"
	"strconv"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/store"
)

var sectionNames = []string{"cpu", "memory", "disk", "network", "system", "latency", "info", "traffic", "history"}

func (e *Engine) clock() time.Time {
	if e.Now != nil {
		return e.Now().UTC()
	}
	return time.Now().UTC()
}
func (e *Engine) settings(ctx context.Context) (store.ProbeSettings, error) {
	var s store.ProbeSettings
	if err := e.Store.GetSetting(ctx, store.SettingProbe, &s); err != nil {
		return s, err
	}
	s.Normalize()
	return s, nil
}
func (e *Engine) generation() uint64 {
	if e.Probe == nil {
		return 0
	}
	return e.Probe.Gen()
}

// Snapshot applies a single allowlist before any native or compatibility view
// can render it. Staff authentication only satisfies the page gate.
func (e *Engine) Snapshot(ctx context.Context, scope Scope) (Snapshot, error) {
	for attempt := 0; attempt < 3; attempt++ {
		gen := e.generation()
		out, err := e.snapshot(ctx)
		if err != nil {
			return Snapshot{}, err
		}
		if gen == e.generation() {
			return out, nil
		}
	}
	return Snapshot{}, fmt.Errorf("probe visibility changed during read")
}
func (e *Engine) snapshot(ctx context.Context) (Snapshot, error) {
	out := Snapshot{Schema: "captain.probe.v2", Now: e.clock(), Nodes: []Node{}, PingTasks: []PingTask{}}
	s, err := e.settings(ctx)
	if err != nil {
		return out, err
	}
	out.Settings = Settings{Title: s.Title, PageTitle: s.Title, Logo: s.Logo, LogoDark: s.LogoDark, Favicon: s.Favicon, Visibility: s.Visibility, Enabled: s.PageOn(), BeatSeconds: s.BeatSeconds, OfflineSeconds: s.Alerts.OfflineSeconds, Sections: []string{}}
	if out.Settings.Title == "" {
		out.Settings.Title = e.SiteName
		out.Settings.PageTitle = e.SiteName
	}
	// Decode only public branding keys rather than depending on the HTTP layer.
	if s.FollowSiteBranding != nil && *s.FollowSiteBranding {
		var site struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Branding    *struct {
				Title     string `json:"title"`
				LogoLight string `json:"logo_light"`
				LogoDark  string `json:"logo_dark"`
				Favicon   string `json:"favicon"`
			} `json:"branding"`
		}
		if e.Store.GetSetting(ctx, "site", &site) == nil {
			if site.Name != "" {
				out.Settings.Title = site.Name
			}
			out.Settings.PageTitle = out.Settings.Title
			out.Settings.Description = site.Description
			out.Settings.Logo, out.Settings.LogoDark, out.Settings.Favicon = "", "", ""
			if site.Branding != nil {
				out.Settings.Logo = site.Branding.LogoLight
				out.Settings.LogoDark = site.Branding.LogoDark
				out.Settings.Favicon = site.Branding.Favicon
				if site.Branding.Title != "" {
					out.Settings.PageTitle = site.Branding.Title
				}
			}
		}
	}
	for _, section := range sectionNames {
		if s.PublicShows(section) {
			out.Settings.Sections = append(out.Settings.Sections, section)
		}
	}
	if !s.PageOn() {
		return out, nil
	}
	nodes, err := e.Store.ListNodes(ctx)
	if err != nil {
		return out, err
	}
	probes, err := e.Store.ListNodeProbes(ctx)
	if err != nil {
		return out, err
	}
	ids, err := e.Store.ProbePublicIDs(ctx)
	if err != nil {
		return out, err
	}
	monitors, err := e.Store.MonitorNodes(ctx, out.Now, time.Duration(s.Alerts.OfflineSeconds)*time.Second)
	if err != nil {
		return out, err
	}
	byID := map[int64]store.MonitorNode{}
	for _, m := range monitors {
		byID[m.ID] = m
	}
	traffic, err := e.Store.NodeHostTrafficViews(ctx, 0)
	if err != nil {
		return out, err
	}
	var tasks []store.PingTask
	if s.PublicShows("latency") {
		tasks, err = e.Store.ListPingTasks(ctx)
		if err != nil {
			return out, err
		}
	}
	for _, n := range nodes {
		np := probes[n.ID]
		if !n.Paired || np == nil || np.Hidden {
			continue
		}
		uuid := ids[n.ID]
		if uuid == "" {
			return out, fmt.Errorf("missing probe identity")
		}
		v := Node{RootDiskPublic: np.Storage.RootVisible(), ID: n.ID, UUID: uuid, Name: n.Name, Group: n.MonitorGroup, Storage: []Storage{}, Pings: []Ping{}, Recent: []Recent{}, Status: Status{LastSeen: n.LastSeenAt, Values: map[string]*float64{}}}
		if s.ShowIP {
			v.Inventory.Address = n.PublicAddr
			if ip, err := netip.ParseAddr(n.PublicAddr); err == nil {
				if ip.Is4() {
					v.Inventory.IPv4 = ip.String()
				} else {
					v.Inventory.IPv6 = ip.String()
				}
			}
		}
		m := byID[n.ID]
		host := m.Host
		received := time.Time{}
		if m.SampledAt > 0 {
			received = time.Unix(m.SampledAt, 0).UTC()
		}
		if e.Probe != nil {
			if l, ok := e.Probe.Live(n.ID); ok {
				if l.At.Unix() >= m.SampledAt {
					h := l.Host
					host = &h
					received = l.At.UTC()
				}
				if v.Status.LastSeen == nil || l.At.After(*v.Status.LastSeen) {
					t := l.At.UTC()
					v.Status.LastSeen = &t
				}
				if s.PublicShows("history") {
					for _, r := range l.Ring {
						valid := spec.SystemStatus{Valid: r.Valid}.Validity()
						values := map[string]*float64{}
						if s.PublicShows("cpu") {
							values["cpu.usage"] = number(r.CPU, valid.CPU)
						}
						if s.PublicShows("memory") {
							values["captain.memory.percent"] = number(r.MemPct, valid.Memory)
						}
						if s.PublicShows("network") {
							values["net.in.rate"] = number(float64(r.NetDown), valid.Network)
							values["net.out.rate"] = number(float64(r.NetUp), valid.Network)
						}
						v.Recent = append(v.Recent, Recent{Time: time.Unix(r.At, 0).UTC(), Values: values})
					}
				}
			}
		}
		grace := time.Duration(s.Alerts.OfflineSeconds) * time.Second
		v.Status.Online = v.Status.LastSeen != nil && !v.Status.LastSeen.After(out.Now.Add(time.Minute)) && out.Now.Sub(*v.Status.LastSeen) <= grace
		if !received.IsZero() {
			v.Status.ReceivedAt = &received
			v.Status.Fresh = !received.After(out.Now.Add(time.Minute)) && out.Now.Sub(received) <= grace
		}
		if host != nil {
			projectHost(&v, *host, s, np.Storage.RootVisible())
			if s.PublicShows("disk") {
				for _, st := range np.Storage.Public(store.StorageCandidates(host, received, out.Now)) {
					v.Storage = append(v.Storage, Storage{ID: st.ID, Name: st.Name, Kind: st.Kind, Total: st.Total, Used: st.Used})
				}
			}
		}
		if s.PublicShows("history") {
			raw, err := e.Store.ProbeMetricHistory(ctx, n.ID, "r", out.Now.Add(-10*time.Minute), out.Now)
			if err != nil {
				return out, err
			}
			if len(raw) > 0 {
				v.Recent = []Recent{}
				for _, b := range raw {
					values := map[string]*float64{}
					for _, d := range metricCatalog {
						if !s.PublicShows(d.Category) || (d.Category == "disk" && !v.RootDiskPublic) {
							continue
						}
						if a, ok := b.Values[d.Key]; ok {
							values[d.Key] = number(a.Last, a.Count > 0)
						}
					}
					v.Recent = append(v.Recent, Recent{Time: time.Unix(b.TS, 0).UTC(), Values: values})
				}
			}
		}
		if s.PublicShows("system") {
			v.Inventory.Version = n.Version
		}
		if s.PublicShows("info") {
			p := np.Info.PublicInfo()
			v.Info = &Info{Region: p.Region, Provider: p.Provider, ProviderURL: p.ProviderURL, Note: p.Note, Price: p.Price, ExpiresAt: p.ExpiresAt}
			if p.Billing != nil {
				v.Info.Cost = &Cost{Amount: p.Billing.Amount, Currency: p.Billing.Currency, Cycle: p.Billing.Cycle, Years: p.Billing.Years}
			}
			if p.AutoRenewal != nil {
				b := p.AutoRenewal.Enabled
				v.Info.AutoRenewal = &b
			}
			if p.Location != nil {
				v.Info.Latitude = p.Location.Lat
				v.Info.Longitude = p.Location.Lng
			}
		}
		if s.PublicShows("traffic") {
			if t := traffic[n.ID]; t != nil {
				v.Traffic = &Traffic{Valid: t.Valid, UsedUp: t.UsedUp, UsedDown: t.UsedDown, Used: np.Billed(), Limit: np.LimitBytes, Mode: np.Mode, CycleStart: t.CycleStart, CycleEnd: t.CycleEnd, UpdatedAt: t.UpdatedAt}
			}
		}
		out.Nodes = append(out.Nodes, v)
	}
	if s.PublicShows("latency") {
		if s.CarrierPing {
			for _, t := range s.CarrierProbe.Tasks() {
				task := PingTask{ID: carrierTaskID(t.Series), LegacyID: carrierLegacyID(t.Series), Name: t.Name, Type: t.Type, Series: t.Series, Family: t.Family, Interval: t.IntervalSeconds, EntityIDs: []string{}}
				if task.Interval <= 0 {
					task.Interval = s.BeatSeconds
				}
				for _, n := range out.Nodes {
					task.EntityIDs = append(task.EntityIDs, n.UUID)
				}
				out.PingTasks = append(out.PingTasks, task)
			}
		}
		for _, t := range tasks {
			if !t.Enabled {
				continue
			}
			task := PingTask{ID: strconv.FormatInt(t.ID, 10), LegacyID: t.ID, Name: t.Name, Type: t.Type, Interval: t.IntervalSeconds, EntityIDs: []string{}}
			for _, n := range out.Nodes {
				if len(t.NodeIDs) == 0 || slices.Contains(t.NodeIDs, n.ID) {
					task.EntityIDs = append(task.EntityIDs, n.UUID)
				}
			}
			if len(task.EntityIDs) > 0 {
				out.PingTasks = append(out.PingTasks, task)
			}
		}
		for i := range out.Nodes {
			n := &out.Nodes[i]
			allowed := map[string]bool{}
			for _, t := range out.PingTasks {
				if slices.Contains(t.EntityIDs, n.UUID) {
					allowed[t.ID] = true
				}
			}
			filtered := []Ping{}
			for _, p := range n.Pings {
				if allowed[p.TaskID] {
					filtered = append(filtered, p)
				}
			}
			n.Pings = filtered
		}
	}
	return out, nil
}
func carrierTaskID(series string) string { return "carrier:" + series }
func number(v float64, valid bool) *float64 {
	if !valid || math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return &v
}
func unsigned(v uint64, valid bool) *uint64 {
	if !valid {
		return nil
	}
	return &v
}
func projectHost(n *Node, h spec.SystemStatus, s store.ProbeSettings, rootDisk bool) {
	v := h.Validity()
	values := n.Status.Values
	if s.PublicShows("cpu") {
		values["cpu.usage"] = number(h.CPUPercent, v.CPU)
	}
	if s.PublicShows("memory") {
		n.Inventory.MemTotal = unsigned(h.MemTotal, v.Memory)
		n.Inventory.SwapTotal = unsigned(h.SwapTotal, v.Swap)
		values["captain.memory.total"] = number(float64(h.MemTotal), v.Memory)
		values["captain.swap.total"] = number(float64(h.SwapTotal), v.Swap)
		values["memory.used"] = number(float64(h.MemUsed), v.Memory)
		values["swap.used"] = number(float64(h.SwapUsed), v.Swap)
	}
	if s.PublicShows("disk") && rootDisk {
		n.Inventory.DiskTotal = unsigned(h.DiskTotal, v.Disk)
		values["captain.disk.total"] = number(float64(h.DiskTotal), v.Disk)
		values["disk.used"] = number(float64(h.DiskUsed), v.Disk)
	}
	if s.PublicShows("network") {
		for k, val := range map[string]uint64{"net.in.rate": h.NetDown, "net.out.rate": h.NetUp, "net.total.up": h.NetTotalUp, "net.total.down": h.NetTotalDown} {
			values[k] = number(float64(val), v.Network)
		}
	}
	if s.PublicShows("system") {
		for k, val := range map[string]float64{"load.average": h.Load1, "captain.load.5": h.Load5, "captain.load.15": h.Load15} {
			values[k] = number(val, v.Load)
		}
		values["connections.tcp"] = number(float64(h.TCP), v.Connections)
		values["connections.udp"] = number(float64(h.UDP), v.Connections)
		values["process.count"] = number(float64(h.Processes), v.Processes)
		values["captain.uptime"] = number(float64(h.Uptime), v.Uptime)
		if h.Info != nil {
			i := h.Info
			n.Inventory.CPUModel = i.CPUModel
			n.Inventory.CPUCores = i.CPUCores
			n.Inventory.CPUPhysicalCores = i.CPUPhysicalCores
			n.Inventory.CPUSockets = i.CPUSockets
			n.Inventory.OS = i.OS
			n.Inventory.Kernel = i.Kernel
			n.Inventory.Arch = i.Arch
			n.Inventory.Virtualization = i.Virt
			n.Inventory.VirtualizationRole = i.VirtualizationRole
		}
	}
	if s.PublicShows("latency") {
		for _, p := range h.Pings {
			id := strconv.FormatInt(p.TaskID, 10)
			if p.TaskID == 0 {
				if !s.CarrierPing || h.CarrierProbeVersion < 1 {
					continue
				}
				matched := false
				for _, t := range s.CarrierProbe.Tasks() {
					if t.Series == p.Series && t.Name == p.Name && t.Family == p.Family {
						matched = true
						break
					}
				}
				if !matched {
					continue
				}
				id = carrierTaskID(p.Series)
			}
			out := Ping{TaskID: id, Name: p.Name, Latency: number(p.LatencyMs, p.LatencyMs >= 0), State: "ok"}
			if p.At > 0 {
				t := time.Unix(p.At, 0).UTC()
				out.Time = &t
			}
			if p.LatencyMs < 0 {
				out.State = "failed"
			}
			if p.Quality != nil {
				out.Reason = p.Quality.Outcome
				if s.PublicShows("history") {
					q := p.Quality.Window
					out.Quality = &PingQuality{Attempts: q.Attempts, Successes: q.Attempts - q.Failed, Min: q.Min, Max: q.Max, P50: q.P50, P95: q.P95, Jitter: q.Jitter}
					if q.Attempts > 0 {
						out.Loss = number(100*float64(q.Failed)/float64(q.Attempts), true)
					}
				}
			} else if s.PublicShows("history") {
				out.Loss = number(p.Loss, true)
			}
			n.Pings = append(n.Pings, out)
		}
	}
}

func carrierLegacyID(series string) int64 {
	h := sha256.Sum256([]byte(series))
	return (1 << 52) | int64(binary.BigEndian.Uint64(h[:8])&((1<<48)-1))
}
