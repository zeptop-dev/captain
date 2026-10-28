package komari

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/probedata"
)

// These are wire projections, not shared collector models. In particular,
// net.total.* remains interface lifetime traffic; exact billing-period usage
// is available under captain.traffic, never substituted into those counters.
func client(n probedata.Node, weight int, publicREST bool) map[string]any {
	i := n.Inventory
	out := map[string]any{
		"uuid": n.UUID, "name": n.Name, "cpu_name": i.CPUModel, "virtualization": i.Virtualization,
		"arch": i.Arch, "cpu_cores": i.CPUCores, "cpu_physical_cores": i.CPUPhysicalCores,
		"os": i.OS, "kernel_version": i.Kernel, "gpu_name": "", "region": "",
		"mem_total": i.MemTotal, "swap_total": i.SwapTotal, "disk_total": i.DiskTotal,
		"weight": weight, "price": -1, "billing_cycle": 0, "auto_renewal": false, "currency": "",
		"expired_at": nil, "group": n.Group, "tags": "", "hidden": false, "traffic_limit": 0, "traffic_limit_type": "sum",
		"captain": map[string]any{"inventory": i, "traffic": n.Traffic, "info": n.Info, "storage": n.Storage},
	}
	// /api/nodes strips IP even when common:getNodes permits public addresses.
	if !publicREST {
		if i.IPv4 != "" {
			out["ipv4"] = i.IPv4
		}
		if i.IPv6 != "" {
			out["ipv6"] = i.IPv6
		}
	}
	if n.Info != nil {
		info := n.Info
		out["region"] = info.Region
		out["public_remark"] = info.Note
		if info.AutoRenewal != nil {
			out["auto_renewal"] = *info.AutoRenewal
		}
		if t, err := time.Parse("2006-01-02", info.ExpiresAt); err == nil {
			out["expired_at"] = t
		}
		if t, err := time.Parse(time.RFC3339, info.ExpiresAt); err == nil {
			out["expired_at"] = t
		}
		if info.Cost != nil {
			c := info.Cost
			if amount, err := strconv.ParseFloat(c.Amount, 64); err == nil && !math.IsInf(amount, 0) && !math.IsNaN(amount) {
				out["price"] = amount
			}
			out["currency"] = c.Currency
			days := map[string]int{"monthly": 30, "quarterly": 90, "semiannual": 180, "annual": 365}[c.Cycle]
			if c.Cycle == "custom" {
				if years, err := strconv.ParseFloat(c.Years, 64); err == nil && years > 0 && years <= 100 {
					days = int(math.Round(years * 365))
				}
			}
			out["billing_cycle"] = days
		}
	}
	if n.Traffic != nil {
		out["traffic_limit"] = n.Traffic.Limit
		mode := n.Traffic.Mode
		if mode == "both" {
			mode = "sum"
		}
		if mode != "sum" && mode != "max" && mode != "min" && mode != "up" && mode != "down" {
			mode = "sum"
		}
		out["traffic_limit_type"] = mode
	}
	return out
}

var recordKeys = map[string]string{
	"cpu": "cpu.usage", "gpu": "gpu.usage", "ram": "memory.used", "swap": "swap.used", "load": "load.average",
	"load5": "captain.load.5", "load15": "captain.load.15", "disk": "disk.used", "net_in": "net.in.rate", "net_out": "net.out.rate",
	"net_total_up": "net.total.up", "net_total_down": "net.total.down", "traffic_up": "traffic.up", "traffic_down": "traffic.down",
	"process": "process.count", "connections": "connections.tcp", "connections_udp": "connections.udp", "uptime": "captain.uptime",
}

func flatRecord(n probedata.Node, t *time.Time, values map[string]*float64, inventory bool) map[string]any {
	out := map[string]any{"client": n.UUID, "time": t, "temp": nil, "ram_total": nil, "swap_total": nil, "disk_total": nil}
	valid := map[string]bool{}
	for field, key := range recordKeys {
		out[field] = values[key]
		valid[field] = values[key] != nil
	}
	if inventory {
		out["ram_total"] = n.Inventory.MemTotal
		out["swap_total"] = n.Inventory.SwapTotal
		out["disk_total"] = n.Inventory.DiskTotal
	}
	out["captain"] = map[string]any{"valid": valid, "connections_semantics": "tcp", "received_at": t}
	return out
}

func latest(n probedata.Node, tasks []probedata.PingTask) map[string]any {
	out := flatRecord(n, n.Status.ReceivedAt, n.Status.Values, true)
	out["online"] = n.Status.Online
	// Komari latest/recent use TCP+UDP in connections; its stored records use
	// TCP alone. Retain that wire contract and name the distinction explicitly.
	out["connections"] = connectionTotal(n.Status.Values)
	out["captain"] = map[string]any{"status": n.Status, "traffic": n.Traffic, "pings": n.Pings, "storage": n.Storage, "connections_semantics": "tcp+udp", "connections_tcp": n.Status.Values["connections.tcp"], "connections_udp": n.Status.Values["connections.udp"]}
	ping := map[string]any{}
	for _, p := range n.Pings {
		id := legacyTaskID(p.TaskID, tasks)
		q := p.Quality
		v := map[string]any{"name": p.Name, "latest": p.Latency, "avg": nil, "tail": nil, "loss": p.Loss, "min": nil, "max": nil}
		if q != nil {
			v["min"] = q.Min
			v["max"] = q.Max
		}
		ping[strconv.FormatInt(id, 10)] = v
	}
	out["ping"] = ping
	return out
}

func connectionTotal(values map[string]*float64) *float64 {
	tcp, udp := values["connections.tcp"], values["connections.udp"]
	if tcp == nil || udp == nil {
		return nil
	}
	total := *tcp + *udp
	return &total
}

func report(n probedata.Node, t *time.Time, values map[string]*float64) map[string]any {
	i := n.Inventory
	return map[string]any{
		"cpu":         map[string]any{"name": i.CPUModel, "cores": i.CPUCores, "arch": i.Arch, "usage": values["cpu.usage"]},
		"ram":         map[string]any{"total": i.MemTotal, "used": values["memory.used"]},
		"swap":        map[string]any{"total": i.SwapTotal, "used": values["swap.used"]},
		"load":        map[string]any{"load1": values["load.average"], "load5": values["captain.load.5"], "load15": values["captain.load.15"]},
		"disk":        map[string]any{"total": i.DiskTotal, "used": values["disk.used"]},
		"network":     map[string]any{"up": values["net.out.rate"], "down": values["net.in.rate"], "totalUp": values["net.total.up"], "totalDown": values["net.total.down"]},
		"connections": map[string]any{"tcp": values["connections.tcp"], "udp": values["connections.udp"]},
		"uptime":      values["captain.uptime"], "process": values["process.count"], "message": "", "updated_at": t,
		"captain": map[string]any{"status": n.Status, "traffic": n.Traffic, "pings": n.Pings, "storage": n.Storage},
	}
}

func legacyTaskID(id string, tasks []probedata.PingTask) int64 {
	for _, t := range tasks {
		if t.ID == id {
			return t.LegacyID
		}
	}
	n, _ := strconv.ParseInt(id, 10, 64)
	return n
}
func publicTaskID(id string, tasks []probedata.PingTask) (string, bool) {
	for _, t := range tasks {
		if t.ID == id || strconv.FormatInt(t.LegacyID, 10) == id {
			return t.ID, true
		}
	}
	return "", false
}
func taskDTO(t probedata.PingTask, weight int) map[string]any {
	clients := t.EntityIDs
	if clients == nil {
		clients = []string{}
	}
	return map[string]any{"id": t.LegacyID, "weight": weight, "name": t.Name, "clients": clients, "default_on": true, "type": t.Type, "interval": t.Interval, "captain": map[string]any{"id": t.ID, "series": t.Series, "family": t.Family}}
}
func tasksDTO(tasks []probedata.PingTask) []map[string]any {
	out := make([]map[string]any, 0, len(tasks))
	for i, t := range tasks {
		out = append(out, taskDTO(t, i))
	}
	return out
}

func (h *Handler) settings(rSettings probedata.Settings, extra map[string]any) map[string]any {
	out := map[string]any{
		"sitename": rSettings.Title, "description": rSettings.Description, "private_site": rSettings.Visibility != "public",
		"theme": "default", "theme_settings": map[string]any{}, "custom_head": "", "custom_body": "",
		"logo": rSettings.Logo, "favicon": rSettings.Favicon, "disable_password_login": true, "oauth_enable": false, "oauth_provider": "",
		"cors_origin_check_enabled": true, "record_enabled": false,
		"ping_record_preserve_time": 0, "record_preserve_time": 0, "realtime_interval": rSettings.BeatSeconds,
		"captain": map[string]any{"compatibility": "komari-1.6.1-public-read", "settings": rSettings, "identity_scope": "probe-read", "counter_semantics": "interface-lifetime", "period_traffic": "captain.traffic"},
	}
	// The callback cannot turn arbitrary global settings into public data.
	for _, key := range []string{"theme", "theme_settings", "theme_version", "theme_name"} {
		if v, ok := extra[key]; ok {
			out[key] = v
		}
	}
	return out
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func nodeByID(s probedata.Snapshot, id string) (probedata.Node, bool) {
	for _, n := range s.Nodes {
		if n.UUID == id {
			return n, true
		}
	}
	return probedata.Node{}, false
}
func cleanStrings(values ...[]string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, list := range values {
		for _, v := range list {
			v = strings.TrimSpace(v)
			if v != "" && !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		}
	}
	return out
}
