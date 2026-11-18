package admin

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/auth"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

func (h *handlers) bindWireGuard(w http.ResponseWriter, r *http.Request) {
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	var in struct {
		Version   int64 `json:"version"`
		InboundID int64 `json:"inbound_id"`
		Attach    bool  `json:"attach"`
		Enabled   *bool `json:"enabled,omitempty"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	links, err := h.Store.WireGuardLinks(r.Context(), idOf(r))
	if err != nil {
		serverErr(w, err)
		return
	}
	var link *store.WireGuardLink
	for i := range links {
		if links[i].ID == r.PathValue("link") && links[i].ExitID == idOf(r) {
			link = &links[i]
		}
	}
	if link == nil {
		fail(w, 404, "connection not found")
		return
	}
	ib, err := h.Store.InboundByID(r.Context(), in.InboundID)
	if err != nil {
		fail(w, 404, "inbound not found")
		return
	}
	if ib.NodeID != link.TransitID {
		fail(w, 400, "inbound must belong to the transit server")
		return
	}
	if in.Attach {
		if in.Enabled != nil {
			ib.Enabled = *in.Enabled
		}
		if ib.Enabled && !h.Store.WireGuardInboundCapable(r.Context(), link.TransitID, ib.Spec()) {
			fail(w, 409, "inbound core is not ready for WireGuard")
			return
		}
		candidate := *ib
		candidate.Settings.WireGuardSlot = link.Slot
		if msg := h.checkIngress(r, &candidate); msg != "" {
			fail(w, 400, msg)
			return
		}
		if msg := h.checkPortConflict(r.Context(), &candidate); msg != "" {
			fail(w, 409, msg)
			return
		}
		if msg := h.checkInboundCore(r.Context(), &candidate); msg != "" {
			fail(w, 400, msg)
			return
		}
		if msg := h.checkPrivateAccess(r.Context(), link.TransitID, &candidate); msg != "" {
			fail(w, 400, msg)
			return
		}
	}
	if err = h.Store.SetWireGuardBinding(r.Context(), idOf(r), link.ID, in.Version, in.InboundID, in.Attach, ib.Enabled); err != nil {
		fail(w, 409, err.Error())
		return
	}
	h.State.Invalidate()
	ok(w, map[string]bool{"ok": true})
}

// Invoked under the same topology lock as the legacy connection editor.
func (h *handlers) saveWireGuardTransport(w http.ResponseWriter, r *http.Request, in wireGuardInput) {
	exitID := idOf(r)
	if !h.requireProxyNode(w, r, exitID) || !h.requireProxyNode(w, r, in.TransitID) {
		return
	}
	if strings.TrimSpace(in.Name) == "" || !validNodeName(in.Name) || exitID == in.TransitID {
		fail(w, 400, "invalid connection name or endpoints")
		return
	}
	link := store.WireGuardLink{ID: auth.UUID(), ExitID: exitID, TransitID: in.TransitID, Name: strings.TrimSpace(in.Name), Enabled: in.Enabled, PrivateAccess: in.PrivateAccess, Endpoint: in.Endpoint, ListenPort: in.TunnelPort, MTU: in.MTU}
	if r.Method == "PUT" {
		links, err := h.Store.WireGuardLinks(r.Context(), exitID)
		if err != nil {
			serverErr(w, err)
			return
		}
		found := false
		for _, old := range links {
			if old.ID == r.PathValue("link") && old.ExitID == exitID && old.TransitID == in.TransitID && old.UserInboundID == 0 && old.Version == in.Version {
				link.ID, link.Version, link.Slot = old.ID, old.Version, old.Slot
				link.DomainPreference, link.AutoForwarding = old.DomainPreference, old.AutoForwarding
				found = true
			}
		}
		if !found {
			fail(w, 409, "connection changed or is not independent")
			return
		}
	}
	if in.DomainPreference != nil {
		link.DomainPreference = *in.DomainPreference
	}
	if in.AutoForwarding != nil {
		link.AutoForwarding = *in.AutoForwarding
	}
	if in.Enabled {
		if !h.Store.WireGuardCapable(r.Context(), exitID, false) || !h.Store.WireGuardCapable(r.Context(), in.TransitID, false) {
			fail(w, 409, "both nodes need fresh WireGuard readiness")
			return
		}
		if link.DomainPreference != "" && !h.Store.WireGuardDomainPreferenceCapable(r.Context(), in.TransitID) {
			fail(w, 409, "transit cannot enforce domain preference")
			return
		}
		if link.AutoForwarding && !h.Store.WireGuardForwardingCapable(r.Context(), exitID) {
			fail(w, 409, "exit cannot manage forwarding")
			return
		}
	}
	for _, nodeID := range []int64{exitID, in.TransitID} {
		n, err := h.Store.NodeByID(r.Context(), nodeID)
		if err != nil {
			serverErr(w, err)
			return
		}
		if n.EgressByIngress {
			fail(w, 409, "per-ingress source egress conflicts with WireGuard")
			return
		}
		state, err := h.privateAccessNode(r.Context(), nodeID, nil)
		if err != nil {
			serverErr(w, err)
			return
		}
		// Use a temporary valid key only to validate the prospective structural state.
		cfg := spec.WireGuardLink{Slot: link.Slot, Exit: nodeID == exitID, ListenPort: link.ListenPort, Endpoint: link.Endpoint, MTU: link.MTU, PrivateAccess: link.PrivateAccess, DomainPreference: link.DomainPreference}
		if cfg.Slot == 0 {
			cfg.Slot = 16383
			for _, other := range state.WireGuardLinks {
				if other.Slot == cfg.Slot {
					cfg.Slot--
				}
			}
		}
		cfg.PrivateKey = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="
		cfg.PeerKey = cfg.PrivateKey
		if cfg.Exit {
			cfg.ListenPort = 0
		} else {
			cfg.Endpoint = ""
		}
		updated := []spec.WireGuardLink{}
		for _, old := range state.WireGuardLinks {
			if old.Slot != link.Slot {
				updated = append(updated, old)
			}
		}
		state.WireGuardLinks = append(updated, cfg)
		if err = state.ValidateWireGuardLinks(); err != nil {
			fail(w, 400, err.Error())
			return
		}
		ov, err := h.Store.NodeOverrides(r.Context(), nodeID)
		if err != nil {
			serverErr(w, err)
			return
		}
		patches := map[string]json.RawMessage{}
		for k, v := range ov {
			patches[k] = json.RawMessage(v)
		}
		if err = spec.CheckWireGuardOverrides(patches); err != nil {
			fail(w, 400, err.Error())
			return
		}
	}
	// Check all effective listeners, excluding only this connection's own UDP listener.
	if msg := h.checkIngress(r, &domain.Inbound{NodeID: in.TransitID, Protocol: spec.WireGuard, Port: in.TunnelPort, Enabled: in.Enabled}); msg != "" {
		fail(w, 400, msg)
		return
	}
	if in.Enabled {
		for _, b := range h.nodeBindsExceptWG(r.Context(), in.TransitID, -1, false, link.ID) {
			if b.port == link.ListenPort && (b.proto == "udp" || b.proto == "both") {
				fail(w, 409, "WireGuard tunnel port is already in use")
				return
			}
		}
	}
	if err := h.Store.SaveWireGuardTransport(r.Context(), &link); err != nil {
		fail(w, 409, err.Error())
		return
	}
	h.State.Invalidate()
	ok(w, map[string]any{"id": link.ID, "version": link.Version})
}
