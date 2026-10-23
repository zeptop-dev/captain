package store

import (
	"errors"
	"math"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

var ErrStaleBeat = errors.New("resource sample already recorded")

type networkCounters struct {
	RootSampleClock bool                   `json:"root_sample_clock,omitempty"`
	Filesystems     map[string]int64       `json:"filesystems,omitempty"`
	GPU             gpuCursor              `json:"gpu,omitempty"`
	Epoch           string                 `json:"epoch"`
	Sequence        uint64                 `json:"sequence"`
	Networks        []spec.NetworkResource `json:"networks"`
}

// A transiently absent root must not switch a sampler back to the legacy
// summary clock. Explicit legacy roots and different sampler epochs can still
// use their synchronous root summaries.
func rootUsesSampleClock(r *spec.Resources, previous networkCounters) bool {
	if r == nil {
		return false
	}
	for _, f := range r.Filesystems {
		if f.Mount == "/" {
			return f.ID != "" || f.SampledAt != 0
		}
	}
	return r.Epoch != "" && r.Epoch == previous.Epoch && previous.RootSampleClock
}

// A new NIC, sampler, boot or include/exclude selection starts a new baseline.
// Missing samples also break the baseline: downtime must not become a spike.
func nextNetworkCounters(old networkCounters, r *spec.Resources) (up, down int64, next networkCounters) {
	next = networkCounters{Epoch: r.Epoch, Sequence: r.Sequence, Networks: r.Networks, GPU: old.GPU}
	next.RootSampleClock = rootUsesSampleClock(r, old)
	next.Filesystems = map[string]int64{}
	for _, f := range r.Filesystems {
		if len(next.Filesystems) >= 256 {
			break
		}
		if f.ID == "" || len(f.ID) > 128 || f.SampledAt <= 0 {
			continue
		}
		previous := int64(0)
		if old.Epoch == r.Epoch && r.Epoch != "" {
			previous = old.Filesystems[f.ID]
		}
		if freshFilesystem(f, r, previous) {
			previous = f.SampledAt
		}
		next.Filesystems[f.ID] = previous
	}
	// A transiently absent mount can reappear with the same cached reading.
	// Preserve recent cursors within this sampler epoch, with a fixed bound.
	if old.Epoch == r.Epoch && r.Epoch != "" {
		for id, sampled := range old.Filesystems {
			if len(next.Filesystems) >= 256 {
				break
			}
			if _, exists := next.Filesystems[id]; exists {
				continue
			}
			if sampled > 0 && sampled <= r.At && r.At-sampled <= 90000 {
				next.Filesystems[id] = sampled
			}
		}
	}
	if freshGPU(r, old.GPU) {
		next.GPU = gpuCursor{Epoch: r.GPU.Epoch, Sequence: r.GPU.Sequence}
	}
	previous := map[string]spec.NetworkResource{}
	if old.Epoch == r.Epoch && r.Epoch != "" {
		for _, n := range old.Networks {
			previous[n.Name] = n
		}
	}
	seen := map[string]bool{}
	for _, n := range r.Networks {
		p, ok := previous[n.Name]
		if seen[n.Name] {
			continue
		}
		seen[n.Name] = true
		if !ok || !p.Included || !n.Included || n.ID == "" || p.ID != n.ID || n.Up < p.Up || n.Down < p.Down {
			continue
		}
		du, dd := n.Up-p.Up, n.Down-p.Down
		if du <= uint64(math.MaxInt64-up) {
			up += int64(du)
		}
		if dd <= uint64(math.MaxInt64-down) {
			down += int64(dd)
		}
	}
	// Rates are transient and not part of the persisted counter baseline.
	next.Networks = append([]spec.NetworkResource(nil), next.Networks...)
	for i := range next.Networks {
		next.Networks[i].UpRate = nil
		next.Networks[i].DownRate = nil
	}
	return
}

// Invalid legacy numeric fields must not enter sums, even from an inconsistent
// sender. Validity is persisted separately so a measured zero remains data.
func validHost(h spec.SystemStatus) spec.SystemStatus {
	v := h.Validity()
	if !v.CPU {
		h.CPUPercent = 0
	}
	if !v.Memory {
		h.MemUsed, h.MemTotal = 0, 0
	}
	if !v.Swap {
		h.SwapUsed = 0
	}
	if !v.Disk {
		h.DiskUsed, h.DiskTotal = 0, 0
	}
	if !v.Network {
		h.NetUp, h.NetDown = 0, 0
	}
	if !v.Load {
		h.Load1 = 0
	}
	if !v.Connections {
		h.TCP, h.UDP = 0, 0
	}
	if !v.Processes {
		h.Processes = 0
	}
	return h
}
