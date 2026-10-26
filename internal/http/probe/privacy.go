package probe

import (
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/service"
	"github.com/zeptop-dev/captain/internal/store"
)

func hideRootDisk(h *spec.SystemStatus) {
	h.DiskUsed, h.DiskTotal = 0, 0
	v := h.Validity()
	v.Disk = false
	h.Valid = &v
}

// Apply public display choices to the payload, including history endpoints.
// Never mutate live service values: those also drive private management views.
func publicHost(h spec.SystemStatus, s store.ProbeSettings) spec.SystemStatus {
	h.Resources = nil
	current := currentCarrierSeries(s)
	pings := make([]spec.PingResult, 0, len(h.Pings))
	for _, ping := range h.Pings {
		if ping.TaskID == 0 && (!carrierSeriesMatches(current, ping.Series, ping.Family, ping.Name) || h.CarrierProbeVersion < 1) {
			continue
		}
		pings = append(pings, ping)
	}
	h.Pings = pings
	for i := range h.Pings {
		if h.Pings[i].Quality != nil {
			q := *h.Pings[i].Quality
			q.Recent = nil
			if !s.PublicShows("history") {
				q.Window = spec.ProbeWindow{}
				h.Pings[i].Loss = 0
			}
			h.Pings[i].Quality = &q
		}
	}
	v := h.Validity()
	h.Valid = &v
	if !s.PublicShows("cpu") {
		h.CPUPercent = 0
		v.CPU = false
	}
	if !s.PublicShows("memory") {
		h.MemUsed = 0
		h.MemTotal = 0
		h.SwapUsed = 0
		h.SwapTotal = 0
		v.Memory = false
		v.Swap = false
	}
	if !s.PublicShows("disk") {
		h.DiskUsed = 0
		h.DiskTotal = 0
		v.Disk = false
	}
	if !s.PublicShows("network") {
		h.NetUp = 0
		h.NetDown = 0
		h.NetTotalUp = 0
		h.NetTotalDown = 0
		v.Network = false
	}
	if !s.PublicShows("system") {
		h.Load1 = 0
		h.Load5 = 0
		h.Load15 = 0
		h.TCP = 0
		h.UDP = 0
		h.Processes = 0
		h.Uptime = 0
		h.IPv4 = false
		h.IPv6 = false
		h.Info = nil
		v.Load = false
		v.Connections = false
		v.Processes = false
		v.Uptime = false
	}
	if !s.PublicShows("latency") {
		h.Pings = nil
		h.CarrierProbeVersion = 0
	}
	return h
}

func currentCarrierSeries(s store.ProbeSettings) map[string]spec.PingTask {
	out := map[string]spec.PingTask{}
	if !s.CarrierPing || !s.PublicShows("latency") {
		return out
	}
	s.Normalize()
	for _, task := range s.CarrierProbe.Tasks() {
		out[task.Series] = task
	}
	return out
}

func carrierSeriesMatches(current map[string]spec.PingTask, series, family, name string) bool {
	task, ok := current[series]
	return ok && task.Family == family && task.Name == name
}
func publicSamples(samples []service.Sample, s store.ProbeSettings) []service.Sample {
	out := make([]service.Sample, len(samples))
	copy(out, samples)
	for i := range out {
		p := &out[i]
		if p.Valid == nil && s.PublicShows("cpu") && s.PublicShows("memory") && s.PublicShows("network") {
			continue
		}
		v := spec.SystemStatus{Valid: p.Valid}.Validity()
		p.Valid = &v
		if !s.PublicShows("cpu") {
			p.CPU = 0
			v.CPU = false
		}
		if !s.PublicShows("memory") {
			p.MemPct = 0
			v.Memory = false
		}
		if !s.PublicShows("network") {
			p.NetUp = 0
			p.NetDown = 0
			v.Network = false
		}
	}
	return out
}
func publicStats(points []store.StatPoint, s store.ProbeSettings) {
	for i := range points {
		p := &points[i]
		if !s.PublicShows("cpu") {
			p.CPU = 0
			p.Valid.CPU = false
		}
		if !s.PublicShows("memory") {
			p.MemUsed = 0
			p.MemTotal = 0
			p.SwapUsed = 0
			p.Valid.Memory = false
			p.Valid.Swap = false
		}
		if !s.PublicShows("disk") {
			p.DiskUsed = 0
			p.DiskTotal = 0
			p.Valid.Disk = false
		}
		if !s.PublicShows("network") {
			p.NetUp = 0
			p.NetDown = 0
			p.Valid.Network = false
		}
		if !s.PublicShows("system") {
			p.Load1 = 0
			p.TCP = 0
			p.UDP = 0
			p.Procs = 0
			p.Valid.Load = false
			p.Valid.Connections = false
			p.Valid.Processes = false
		}
	}
}
