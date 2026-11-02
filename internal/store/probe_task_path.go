package store

import (
	"net"
	"slices"
	"strings"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

// AgentTask fixes the address family and measurement identity of an explicitly
// bound path. Historical tasks retain their original unbound identity.
func (t PingTask) AgentTask() spec.PingTask {
	v := spec.PingTask{ID: t.ID, Name: t.Name, Type: t.Type, Target: t.Target, IntervalSeconds: t.IntervalSeconds, SourceInterface: t.SourceInterface}
	if t.SourceInterface != "" {
		host := t.Target
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		v.Family = "ipv4"
		if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
			v.Family = "ipv6"
		}
		v.Series = spec.ProbeTaskSeries(v)
	}
	return v
}

// CurrentTaskSample prevents a former default route or a former device from
// being presented as a measurement of the currently configured bound path.
// Unregistered legacy/automatic tasks preserve the existing compatibility rules.
func CurrentTaskSample(tasks []PingTask, nodeID, id int64, name, series, family string) bool {
	for _, t := range tasks {
		if t.ID != id {
			continue
		}
		if t.SourceInterface == "" && !strings.HasPrefix(series, "task:") {
			return true
		}
		v := t.AgentTask()
		return t.Enabled && (len(t.NodeIDs) == 0 || slices.Contains(t.NodeIDs, nodeID)) && t.Name == name && v.Series == series && v.Family == family
	}
	return !strings.HasPrefix(series, "task:")
}

func FilterTaskSamples(h spec.SystemStatus, tasks []PingTask, nodeID int64) spec.SystemStatus {
	pings := make([]spec.PingResult, 0, len(h.Pings))
	for _, p := range h.Pings {
		if !CurrentTaskSample(tasks, nodeID, p.TaskID, p.Name, p.Series, p.Family) {
			continue
		}
		if strings.HasPrefix(p.Series, "task:") && h.ProbeInterfaceVersion < 1 {
			continue
		}
		pings = append(pings, p)
	}
	h.Pings = pings
	return h
}
