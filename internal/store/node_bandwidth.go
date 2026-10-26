package store

import (
	"errors"
	"math"
)

// NodeBandwidth records advertised capacity only; it never configures shaping.
// Rates use decimal megabits per second. Zero means unknown, not unlimited.
type NodeBandwidth struct {
	InboundMbps  float64 `json:"inbound_mbps"`
	OutboundMbps float64 `json:"outbound_mbps"`
	Public       bool    `json:"public"`
}

var ErrNodeBandwidth = errors.New("invalid node bandwidth: expected 0 to 1000000 Mbps")

func normalizeNodeBandwidth(info *NodeProbeInfo, old NodeProbeInfo) error {
	if info.Bandwidth == nil {
		// Omission/null from older clients must not erase the new metadata.
		info.Bandwidth = old.Bandwidth
		return nil
	}
	for _, v := range []float64{info.Bandwidth.InboundMbps, info.Bandwidth.OutboundMbps} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1000000 {
			return ErrNodeBandwidth
		}
	}
	// An explicit empty object (or both rates zero) clears the record.
	if info.Bandwidth.InboundMbps == 0 && info.Bandwidth.OutboundMbps == 0 {
		info.Bandwidth = nil
	}
	return nil
}

// PublicInfo strips private metadata before serializing a public probe response.
func (info NodeProbeInfo) PublicInfo() NodeProbeInfo {
	info.LocationSettings = nil
	info.Location = info.Location.Clone()
	if info.Bandwidth != nil && !info.Bandwidth.Public {
		info.Bandwidth = nil
	}
	return info
}
