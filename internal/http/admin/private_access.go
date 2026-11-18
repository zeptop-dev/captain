package admin

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
)

func samePrivateAccess(a, b *spec.PrivateAccess) bool { return reflect.DeepEqual(a, b) }

// Existing JSON storage keeps the policy alongside protocol settings. Validate
// against the whole node so templates/raw JSON cannot bypass conflict checks.
func (h *handlers) privateAccessNode(ctx context.Context, id int64, proposed *domain.Inbound) (*spec.Node, error) {
	nr, err := h.Store.NodeRouting(ctx, id)
	if err != nil {
		return nil, err
	}
	n := &spec.Node{Routes: nr.Routes, Outbounds: nr.Outbounds, DefaultOutbound: nr.DefaultOutbound, DNS: nr.DNS, Overrides: map[string]json.RawMessage{}}
	ibs, err := h.Store.InboundsByNode(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, ib := range ibs {
		if proposed == nil || ib.ID != proposed.ID {
			n.Inbounds = append(n.Inbounds, ib.Spec())
		}
	}
	if proposed != nil {
		n.Inbounds = append(n.Inbounds, proposed.Spec())
	}
	links, err := h.Store.ReverseLinks(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, l := range links {
		if l.ExitID == id {
			ib, e := h.Store.InboundByID(ctx, l.UserInboundID)
			if e != nil {
				return nil, e
			}
			n.ReverseClients = append(n.ReverseClients, spec.ReverseClient{ID: l.ID, PrivateAccess: ib.Settings.PrivateAccess})
		}
	}
	wgLinks, err := h.Store.WireGuardLinks(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, link := range wgLinks {
		grant := link.PrivateAccess
		if proposed != nil && proposed.ID == link.UserInboundID {
			grant = proposed.Settings.PrivateAccess
		}
		cfg, err := link.Config(id == link.ExitID, grant)
		if err != nil {
			return nil, err
		}
		n.WireGuardLinks = append(n.WireGuardLinks, cfg)
	}
	ov, err := h.Store.NodeOverrides(ctx, id)
	if err != nil {
		return nil, err
	}
	for k, v := range ov {
		n.Overrides[k] = json.RawMessage(v)
	}
	return n, nil
}

func (h *handlers) checkPrivateAccess(ctx context.Context, id int64, proposed *domain.Inbound) string {
	n, err := h.privateAccessNode(ctx, id, proposed)
	if err != nil {
		return "could not load node private access configuration"
	}
	if err = n.ValidateWireGuardLinks(); err != nil {
		return err.Error()
	}
	if err = n.ValidatePrivateAccessNode(); err != nil {
		return err.Error()
	}
	if err = h.checkPrivateRouting(ctx, id, n); err != nil {
		return err.Error()
	}
	return ""
}

func (h *handlers) checkPrivateRouting(ctx context.Context, id int64, policy *spec.Node) error {
	n, err := h.Store.NodeByID(ctx, id)
	if err != nil {
		return err
	}
	return policy.ValidatePrivateAccessRouting(h.nodeCoreCandidates(ctx, n))
}
