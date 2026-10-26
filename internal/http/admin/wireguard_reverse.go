package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/auth"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

type wireGuardInput struct {
	Version          int64               `json:"version"`
	TransitID        int64               `json:"transit_id"`
	Name             string              `json:"name"`
	Port             int                 `json:"port"`
	TunnelPort       int                 `json:"tunnel_port"`
	Endpoint         string              `json:"endpoint"`
	MTU              int                 `json:"mtu"`
	ServerName       string              `json:"server_name"`
	GroupID          *int64              `json:"group_id"`
	PrivateAccess    *spec.PrivateAccess `json:"private_access"`
	Enabled          bool                `json:"enabled"`
	DomainPreference *string             `json:"domain_preference,omitempty"`
	AutoForwarding   *bool               `json:"auto_forwarding,omitempty"`
}

func (h *handlers) registerWireGuard(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/settings/nodes/{id}/wireguard-readiness", h.requireAdmin(h.wireGuardReadiness))
	mux.HandleFunc("POST /api/admin/settings/nodes/{id}/wireguard-setup", h.requireAdmin(h.wireGuardSetup))
	const path = "/api/admin/settings/nodes/{id}/wireguard-connections"
	mux.HandleFunc("GET "+path, h.requireAdmin(h.listWireGuard))
	mux.HandleFunc("POST "+path, h.requireAdmin(h.saveWireGuard))
	mux.HandleFunc("PUT "+path+"/{link}", h.requireAdmin(h.saveWireGuard))
	mux.HandleFunc("DELETE "+path+"/{link}", h.requireAdmin(h.deleteWireGuard))
}
func (h *handlers) listWireGuard(w http.ResponseWriter, r *http.Request) {
	links, err := h.Store.WireGuardLinks(r.Context(), idOf(r))
	if err != nil {
		serverErr(w, err)
		return
	}
	out := []any{}
	for _, link := range links {
		ib, err := h.Store.InboundByID(r.Context(), link.UserInboundID)
		if err != nil {
			serverErr(w, err)
			return
		}
		entry, err := h.Store.EntryByID(r.Context(), link.EntryID)
		if err != nil {
			serverErr(w, err)
			return
		}
		in := wireGuardInput{Version: link.Version, TransitID: link.TransitID, Name: entry.Name, Port: ib.Port, TunnelPort: link.ListenPort, Endpoint: link.Endpoint, MTU: link.MTU, GroupID: ib.GroupID, PrivateAccess: ib.Settings.PrivateAccess, Enabled: ib.Enabled, DomainPreference: &link.DomainPreference, AutoForwarding: &link.AutoForwarding}
		if ib.Settings.TLS != nil {
			in.ServerName = ib.Settings.TLS.ServerName
		}
		state := "pending"
		forwarding := h.wireGuardForwarding(r, link)
		if !ib.Enabled {
			state = "disabled"
		} else if !h.Store.WireGuardCapable(r.Context(), link.TransitID, true) || !h.Store.WireGuardCapable(r.Context(), link.ExitID, false) || (link.DomainPreference != "" && !h.Store.WireGuardDomainPreferenceCapable(r.Context(), link.TransitID)) || (link.AutoForwarding && !h.Store.WireGuardForwardingCapable(r.Context(), link.ExitID)) {
			state = "unavailable"
		} else if forwarding != nil && (forwarding.State == "error" || forwarding.State == "unsupported") {
			state = "blocked"
		} else {
			rep, at, err := h.Store.WireGuardReport(r.Context(), link.TransitID)
			node, nerr := h.Store.NodeByID(r.Context(), link.TransitID)
			if err == nil && nerr == nil && rep != nil && rep.Applied && time.Since(at) < 3*time.Minute {
				desired, derr := h.State.Cached(r.Context(), node, time.Now())
				if derr == nil && node.AppliedRevision == desired.Revision {
					for _, status := range rep.Links {
						if status.Slot == link.Slot {
							state = "disconnected"
							if status.Connected && status.Error == "" && h.wireGuardPeerApplied(r, link.ExitID, link.AutoForwarding) {
								state = "connected"
							}
						}
					}
				}
			}
		}
		out = append(out, struct {
			wireGuardInput
			ID            string                                `json:"id"`
			ExitID        int64                                 `json:"exit_id"`
			Slot          int                                   `json:"slot"`
			UserInboundID int64                                 `json:"user_inbound_id"`
			State         string                                `json:"state"`
			Forwarding    *agentproto.WireGuardForwardingStatus `json:"forwarding,omitempty"`
		}{in, link.ID, link.ExitID, link.Slot, ib.ID, state, forwarding})
	}
	ok(w, out)
}
func (h *handlers) saveWireGuard(w http.ResponseWriter, r *http.Request) {
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	var in struct {
		wireGuardInput
		GroupIDValue json.RawMessage `json:"group_id"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if len(in.GroupIDValue) > 0 && json.Unmarshal(in.GroupIDValue, &in.GroupID) != nil {
		fail(w, 400, "invalid group_id")
		return
	}
	exitID := idOf(r)
	if !h.requireProxyNode(w, r, exitID) || !h.requireProxyNode(w, r, in.TransitID) {
		return
	}
	if exitID == in.TransitID {
		fail(w, 400, "transit and exit must differ")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	in.ServerName = strings.TrimSpace(in.ServerName)
	if in.Name == "" || !validNodeName(in.Name) || in.ServerName == "" || strings.ContainsAny(in.ServerName, " /:\r\n") {
		fail(w, 400, "name and valid REALITY server name are required")
		return
	}
	ep, err := netip.ParseAddrPort(in.Endpoint)
	if err != nil || ep.Port() == 0 || !ep.Addr().IsGlobalUnicast() || ep.Addr().Is4In6() || ep.Addr().Zone() != "" {
		fail(w, 400, "endpoint must be a literal transit IP and UDP port")
		return
	}
	if in.MTU < 1280 || in.MTU > 1420 || in.TunnelPort < 1 || in.TunnelPort > 65535 || in.Port == in.TunnelPort {
		fail(w, 400, "valid distinct user/tunnel ports and MTU 1280-1420 are required")
		return
	}
	links, err := h.Store.WireGuardLinks(r.Context(), exitID)
	if err != nil {
		serverErr(w, err)
		return
	}
	write := store.WireGuardWrite{}
	id := r.PathValue("link")
	if id != "" {
		found := false
		for _, link := range links {
			if link.ID == id && link.ExitID == exitID && link.TransitID == in.TransitID {
				write.Link = link
				found = true
				break
			}
		}
		if !found {
			fail(w, 404, "connection not found")
			return
		}
		if write.Link.Version != in.Version {
			fail(w, 409, "connection changed; reload before saving")
			return
		}
		ib, err := h.Store.InboundByID(r.Context(), write.Link.UserInboundID)
		if err != nil {
			serverErr(w, err)
			return
		}
		if ib.Settings.TLS == nil || ib.Settings.TLS.Reality == nil {
			fail(w, 409, "managed inbound is missing REALITY settings")
			return
		}
		write.User = *ib
		entry, err := h.Store.EntryByID(r.Context(), write.Link.EntryID)
		if err != nil {
			serverErr(w, err)
			return
		}
		write.Entry = *entry
	} else {
		if in.Version != 0 {
			fail(w, 400, "new connection must not have a version")
			return
		}
		write.Link = store.WireGuardLink{ID: auth.UUID(), ExitID: exitID, TransitID: in.TransitID}
		write.User = domain.Inbound{Tag: "wg-user-" + write.Link.ID, Protocol: spec.VLESS, Settings: spec.Inbound{Flow: "xtls-rprx-vision", TLS: &spec.TLS{Mode: spec.TLSReality, Reality: &spec.Reality{HandshakePort: 443}}}}
	}
	if in.DomainPreference != nil {
		switch *in.DomainPreference {
		case "", "ipv4", "ipv6":
			write.Link.DomainPreference = *in.DomainPreference
		default:
			fail(w, 400, "invalid WireGuard domain preference")
			return
		}
	}
	if in.AutoForwarding != nil {
		write.Link.AutoForwarding = *in.AutoForwarding
	}
	if in.Enabled {
		for _, side := range []struct {
			id      int64
			transit bool
			label   string
		}{{in.TransitID, true, "A"}, {exitID, false, "B"}} {
			state, err := h.Store.WireGuardReadiness(r.Context(), side.id, side.transit)
			if err != nil {
				serverErr(w, err)
				return
			}
			if !state.Ready {
				missing := []string{}
				for _, c := range state.Checks {
					if !c.OK {
						missing = append(missing, c.ID)
					}
				}
				fail(w, 409, "reverse WireGuard node "+side.label+" ("+state.Name+") prerequisites not met: "+strings.Join(missing, ", "))
				return
			}
		}
	}
	if in.Enabled && write.Link.AutoForwarding && !h.Store.WireGuardForwardingCapable(r.Context(), exitID) {
		fail(w, 409, "exit needs a fresh automatic WireGuard forwarding capability report; update bosun on B")
		return
	}
	if in.Enabled && write.Link.DomainPreference != "" && !h.Store.WireGuardDomainPreferenceCapable(r.Context(), in.TransitID) {
		fail(w, 409, "transit needs a fresh WireGuard domain-preference capability report; update bosun on A")
		return
	}
	for _, nid := range []int64{exitID, in.TransitID} {
		node, err := h.Store.NodeByID(r.Context(), nid)
		if err != nil {
			serverErr(w, err)
			return
		}
		if node.EgressByIngress {
			fail(w, 409, "WireGuard cannot combine with per-ingress source egress")
			return
		}
		ov, err := h.Store.NodeOverrides(r.Context(), nid)
		if err != nil {
			serverErr(w, err)
			return
		}
		var patch map[string]any
		if raw := ov["xray"]; raw != "" {
			if json.Unmarshal([]byte(raw), &patch) != nil {
				fail(w, 400, "invalid Xray override")
				return
			}
		}
		for _, key := range []string{"routing", "outbounds", "inbounds", "policy", "dns"} {
			if _, exists := patch[key]; exists {
				fail(w, 409, "remove conflicting Xray override before creating WireGuard")
				return
			}
		}
		routing, err := h.Store.NodeRouting(r.Context(), nid)
		if err != nil {
			serverErr(w, err)
			return
		}
		for _, out := range routing.Outbounds {
			if strings.HasPrefix(out.Tag, "wireguard-") {
				fail(w, 409, "wireguard- outbound tags are reserved")
				return
			}
		}
	}
	ib := &write.User
	ib.NodeID = in.TransitID
	ib.Core = "xray"
	ib.Port = in.Port
	ib.Enabled = in.Enabled
	// Omission preserves legacy membership when editing a managed connection.
	if len(in.GroupIDValue) > 0 || ib.ID == 0 {
		ib.GroupID = in.GroupID
	}
	ib.Settings.TLS.ServerName = in.ServerName
	ib.Settings.TLS.Reality.HandshakeServer = in.ServerName
	if in.PrivateAccess != nil {
		ib.Settings.PrivateAccess = in.PrivateAccess
	}
	if ib.Settings.PrivateAccess != nil && ib.Settings.PrivateAccess.Mode == "internal" {
		fail(w, 400, "WireGuard requires explicit private destination rules")
		return
	}
	fillInboundSecrets(ib)
	if msg := checkInboundFields(ib); msg != "" {
		fail(w, 400, msg)
		return
	}
	if msg := h.checkIngress(r, ib); msg != "" {
		fail(w, 400, msg)
		return
	}
	if msg := h.checkPortConflict(r.Context(), ib); msg != "" {
		fail(w, 409, msg)
		return
	}
	tunnel := domain.Inbound{ID: ib.ID, NodeID: in.TransitID, Protocol: spec.WireGuard, Port: in.TunnelPort, Enabled: in.Enabled}
	if msg := h.checkIngress(r, &tunnel); msg != "" {
		fail(w, 400, msg)
		return
	}
	if msg := h.checkPortConflict(r.Context(), &tunnel); msg != "" {
		fail(w, 409, msg)
		return
	}
	if msg := h.checkPrivateAccess(r.Context(), in.TransitID, ib); msg != "" {
		fail(w, 400, msg)
		return
	}
	transit, err := h.Store.NodeByID(r.Context(), in.TransitID)
	if err != nil {
		serverErr(w, err)
		return
	}
	host := transit.Domain
	if host == "" {
		host = transit.PublicAddr
	}
	if host == "" {
		fail(w, 400, "transit needs a public subscription address")
		return
	}
	write.Link.Endpoint = in.Endpoint
	write.Link.ListenPort = in.TunnelPort
	write.Link.MTU = in.MTU
	write.Entry.Name = in.Name
	write.Entry.DisplayHost = host
	write.Entry.DisplayPort = in.Port
	write.Entry.Enabled = in.Enabled
	if err := h.Store.SaveWireGuardLink(r.Context(), &write); err != nil {
		if errors.Is(err, store.ErrReverseConflict) {
			fail(w, 409, err.Error())
		} else {
			fail(w, 409, "could not save WireGuard connection")
		}
		return
	}
	h.State.Invalidate()
	ok(w, map[string]any{"id": write.Link.ID, "version": write.Link.Version})
}
func (h *handlers) deleteWireGuard(w http.ResponseWriter, r *http.Request) {
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	var in struct {
		Version int64 `json:"version"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if err := h.Store.DeleteWireGuardLink(r.Context(), idOf(r), r.PathValue("link"), in.Version); err != nil {
		if errors.Is(err, store.ErrLineInUse) {
			fail(w, 409, err.Error())
			return
		}
		if entryClientChainError(w, err) {
			return
		}
		if errors.Is(err, store.ErrReverseConflict) {
			fail(w, 409, err.Error())
		} else {
			serverErr(w, err)
		}
		return
	}
	h.State.Invalidate()
	ok(w, map[string]bool{"ok": true})
}

// A recent handshake alone may belong to the previous private-access policy.
func (h *handlers) wireGuardPeerApplied(r *http.Request, id int64, automatic bool) bool {
	rep, at, err := h.Store.WireGuardReport(r.Context(), id)
	if err != nil || rep == nil || !rep.Applied || time.Since(at) < 0 || time.Since(at) > 3*time.Minute {
		return false
	}
	if automatic {
		forwarding := h.wireGuardForwarding(r, store.WireGuardLink{ExitID: id, AutoForwarding: true})
		if forwarding == nil || (forwarding.State != "synced" && forwarding.State != "external" && forwarding.State != "not_required") {
			return false
		}
	}
	n, err := h.Store.NodeByID(r.Context(), id)
	if err != nil {
		return false
	}
	desired, err := h.State.Cached(r.Context(), n, time.Now())
	return err == nil && n.AppliedRevision == desired.Revision
}

func (h *handlers) wireGuardForwarding(r *http.Request, link store.WireGuardLink) *agentproto.WireGuardForwardingStatus {
	if !link.AutoForwarding {
		return &agentproto.WireGuardForwardingStatus{State: "manual"}
	}
	rep, at, err := h.Store.WireGuardReport(r.Context(), link.ExitID)
	if err != nil || rep == nil || rep.Forwarding == nil || time.Since(at) < 0 || time.Since(at) > 3*time.Minute {
		return nil
	}
	status := *rep.Forwarding
	age := time.Since(time.Unix(status.CheckedAt, 0))
	if status.CheckedAt <= 0 || age < 0 || age > 3*time.Minute {
		return nil
	}
	switch status.State {
	case "synced", "external", "not_required", "manual", "unsupported", "error":
	default:
		return nil
	}
	if status.Backend != "ufw" && status.Backend != "firewalld" {
		status.Backend = ""
	}
	if len(status.Reason) > 64 || strings.IndexFunc(status.Reason, func(r rune) bool { return (r < 'a' || r > 'z') && r != '_' }) >= 0 {
		status.Reason = "forwarding_check_failed"
	}
	return &status
}
