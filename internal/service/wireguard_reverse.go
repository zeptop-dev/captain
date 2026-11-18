package service

import (
	"context"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

// Both endpoints must have fresh enforcement capability. Missing support
// removes the entire managed subscriber inbound, not merely its WG routing.
func (a *AgentState) wireGuardState(ctx context.Context, nodeID int64) ([]spec.WireGuardLink, map[int64]bool, error) {
	links, err := a.Store.WireGuardLinks(ctx, nodeID)
	if err != nil {
		return nil, nil, err
	}
	configs := []spec.WireGuardLink{}
	permitted := map[int64]bool{}
	for _, link := range links {
		bindings, err := a.Store.WireGuardBindings(ctx, link.ID)
		if err != nil {
			return nil, nil, err
		}
		enabled := link.Enabled
		for _, ib := range bindings {
			if ib.ID == link.UserInboundID {
				enabled = ib.Enabled
			}
		}
		if !enabled || !a.Store.WireGuardCapable(ctx, link.ExitID, false) || !a.Store.WireGuardCapable(ctx, link.TransitID, false) {
			continue
		}

		if link.DomainPreference != "" && !a.Store.WireGuardDomainPreferenceCapable(ctx, link.TransitID) {
			continue
		}
		if link.AutoForwarding && !a.Store.WireGuardForwardingCapable(ctx, link.ExitID) {
			continue
		}
		config, err := link.Config(nodeID == link.ExitID, link.PrivateAccess)
		if err != nil {
			return nil, nil, err
		}
		configs = append(configs, config)
		if nodeID == link.TransitID {
			for _, ib := range bindings {
				if ib.Enabled && a.Store.WireGuardInboundCapable(ctx, link.TransitID, ib.Spec()) {
					permitted[ib.ID] = true
				}
			}
		}
	}
	return configs, permitted, nil
}
