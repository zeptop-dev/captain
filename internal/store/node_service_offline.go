package store

import (
	"crypto/rand"
	"fmt"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

// Capture a reviewed accounting basis, not a second customer ledger. Absolute
// boundaries are generated here so agents need not infer the panel's timezone.
func refreshOfflinePolicy(r *serviceControlRecord, p *NodeProbe, at time.Time, explicit bool) error {
	if !r.state.OfflineEnabled {
		r.state.Offline = nil
		return nil
	}
	if r.state.Policy.Action == "alert" {
		return fmt.Errorf("%w: offline protection requires an automatic pause or limit policy", ErrServiceControl)
	}
	old := r.state.Offline
	policyID := ""
	if old != nil {
		policyID = old.PolicyID
	}
	if policyID == "" {
		policyID = rand.Text()
	}
	// Preserve an old conservative anchor while a reset/restart has not yet
	// produced measured counters. Unknown traffic must never become zero.
	if !p.counters.TrafficKnown || p.counters.Epoch == "" || p.counters.Sequence == 0 || p.counters.TrafficAt <= 0 || p.PeriodStart.IsZero() || p.LimitBytes <= 0 {
		if old != nil && !explicit {
			return nil
		}
		return fmt.Errorf("%w: offline protection requires a measured traffic anchor", ErrServiceControl)
	}
	var nets []spec.NetworkResource
	for _, n := range p.counters.Networks {
		if n.Included {
			n.UpRate = nil
			n.DownRate = nil
			nets = append(nets, n)
		}
	}
	mode := p.Mode
	if mode == "" {
		mode = "sum"
	}
	if old != nil && !explicit && old.Cycle == r.state.Cycle && old.Anchor.Epoch == p.counters.Epoch && old.QuotaBytes == p.LimitBytes && old.Direction == mode && old.PeriodStart == p.PeriodStart.Unix() && at.Unix()-old.Anchor.At < 120 && old.Automatic == r.state.Automatic.Active {
		return nil
	}
	end := nodePeriodEnd(p.PeriodStart.In(time.Local), p)
	ends := make([]int64, 0, 12)
	for range 12 {
		ends = append(ends, end.Unix())
		end = nodePeriodEnd(end, p)
	}
	v := &spec.OfflineServiceControl{PolicyID: policyID, Cycle: r.state.Cycle, Action: r.state.Policy.Action, ThresholdPercent: r.state.Policy.ThresholdPercent, LimitMbps: r.state.Policy.LimitMbps, QuotaBytes: p.LimitBytes, Direction: mode, AutoResume: r.state.Policy.AutoResume, Automatic: r.state.Automatic.Active, Exempt: r.state.ExemptCycle == r.state.Cycle, ManualMode: r.state.Manual.Mode, ManualLimitMbps: r.state.Manual.LimitMbps, PeriodStart: p.PeriodStart.Unix(), PeriodEnds: ends, Anchor: spec.OfflineTrafficAnchor{Epoch: p.counters.Epoch, Sequence: p.counters.Sequence, At: p.counters.TrafficAt, UsedUp: p.UsedUp, UsedDown: p.UsedDown, Networks: nets}}
	if err := v.Validate(); err != nil {
		if old != nil && !explicit {
			return nil
		}
		return fmt.Errorf("%w: %v", ErrServiceControl, err)
	}
	r.state.Offline = v
	return nil
}
