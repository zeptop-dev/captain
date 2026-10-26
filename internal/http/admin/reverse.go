package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/auth"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

type reverseInput struct {
	Listen     string `json:"listen"`
	ID         string `json:"id"`
	TransitID  int64  `json:"transit_id"`
	Name       string `json:"name"`
	Port       int    `json:"port"`
	TunnelPort int    `json:"tunnel_port"`
	IngressID  *int64 `json:"ingress_id"`
	GroupID    *int64 `json:"group_id"`
	ServerName string `json:"server_name"`
	Enabled    bool   `json:"enabled"`
	// Optional advanced user protocol configuration. Omission preserves it.
	Protocol spec.Protocol `json:"protocol,omitempty"`
	Settings *spec.Inbound `json:"settings,omitempty"`
}
type reverseView struct {
	reverseInput
	ExitID           int64  `json:"exit_id"`
	Version          int64  `json:"version"`
	UserInboundID    int64  `json:"user_inbound_id"`
	State            string `json:"state"`
	Host             string `json:"host"`
	PublicPort       int    `json:"public_port"`
	PublicTunnelPort int    `json:"public_tunnel_port"`
}

func (h *handlers) registerReverse(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/nodes/{id}/reverse-connections", h.requireAdmin(h.listReverse))
	mux.HandleFunc("PUT /api/admin/nodes/{id}/reverse-connections", h.requireAdmin(h.saveReverse))
}

func (h *handlers) listReverse(w http.ResponseWriter, r *http.Request) {
	ls, err := h.Store.ReverseLinks(r.Context(), idOf(r))
	if err != nil {
		serverErr(w, err)
		return
	}
	out := []reverseView{}
	for _, l := range ls {
		u, err := h.Store.InboundByID(r.Context(), l.UserInboundID)
		if err != nil {
			serverErr(w, err)
			return
		}
		c, err := h.Store.InboundByID(r.Context(), l.ReceiverInboundID)
		if err != nil {
			serverErr(w, err)
			return
		}
		e, err := h.Store.EntryByID(r.Context(), l.EntryID)
		if err != nil {
			serverErr(w, err)
			return
		}
		cfg := u.Settings
		cfg.Reverse = nil
		v := reverseView{ExitID: l.ExitID, Version: l.Version, UserInboundID: l.UserInboundID, reverseInput: reverseInput{ID: l.ID, TransitID: l.TransitID, Name: e.Name, Port: u.Port, TunnelPort: c.Port, IngressID: u.IngressID, GroupID: u.GroupID, Enabled: u.Enabled, Protocol: u.Protocol, Settings: &cfg}, Host: e.DisplayHost, PublicPort: e.DisplayPort, State: "pending"}
		if c.Settings.TLS != nil {
			v.ServerName = c.Settings.TLS.ServerName
		}
		v.Listen = u.Listen
		ce := domain.Entry{InboundID: c.ID}
		if err := h.fillEntryDefaults(r.Context(), &ce); err == nil {
			v.PublicTunnelPort = ce.DisplayPort
		}
		if !u.Enabled {
			v.State = "disabled"
		} else {
			ready := true
			for _, nid := range []int64{l.ExitID, l.TransitID} {
				n, err := h.Store.NodeByID(r.Context(), nid)
				if err != nil {
					serverErr(w, err)
					return
				}
				if !h.Store.ReverseCapable(r.Context(), nid) {
					v.State = "upgrade"
					ready = false
					break
				}
				if n.LastSeenAt == nil || time.Since(*n.LastSeenAt) > 3*time.Minute {
					v.State = "offline"
					ready = false
					break
				}
				st, err := h.State.Cached(r.Context(), n, time.Now())
				if err != nil {
					serverErr(w, err)
					return
				}
				if n.AppliedRevision != st.Revision {
					ready = false
				}
			}
			if ready {
				v.State = "unknown"
				st, err := h.Store.NodeStatus(r.Context(), l.TransitID)
				if err == nil {
					var cs map[string]agentproto.CoreStatus
					if json.Unmarshal(st.Cores, &cs) == nil {
						c := cs["xray"]
						if c.Running {
							if connected, ok := c.Reverse[l.ID]; ok && connected != nil {
								v.State = "disconnected"
								if *connected {
									v.State = "connected"
								}
							}
						}
					}
				}
			}
		}
		out = append(out, v)
	}
	ok(w, out)
}

func (h *handlers) saveReverse(w http.ResponseWriter, r *http.Request) {
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	exitID := idOf(r)
	var in struct {
		Expected map[string]int64 `json:"expected"`
		Links    []struct {
			reverseInput
			GroupIDValue json.RawMessage `json:"group_id"`
		} `json:"links"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if len(in.Links) > 32 {
		fail(w, 400, "at most 32 transits per exit")
		return
	}
	if _, err := h.Store.NodeByID(r.Context(), exitID); err != nil {
		fail(w, 404, "exit node not found")
		return
	}
	if len(in.Links) > 0 {
		if !h.requireProxyNode(w, r, exitID) {
			return
		}
		ids := []int64{exitID}
		for _, v := range in.Links {
			ids = append(ids, v.TransitID)
		}
		for _, id := range ids {
			ov, err := h.Store.NodeOverrides(r.Context(), id)
			if err != nil {
				serverErr(w, err)
				return
			}
			if err := spec.CheckReverseOverride(json.RawMessage(ov["xray"])); err != nil {
				fail(w, 409, err.Error())
				return
			}
			nr, err := h.Store.NodeRouting(r.Context(), id)
			if err != nil {
				serverErr(w, err)
				return
			}
			for _, o := range nr.Outbounds {
				if strings.HasPrefix(o.Tag, "reverse-") {
					fail(w, 409, "reverse- outbound tags are reserved for managed connections")
					return
				}
			}
		}
	}
	old, err := h.Store.ReverseLinks(r.Context(), exitID)
	if err != nil {
		serverErr(w, err)
		return
	}
	byID := map[string]store.ReverseLink{}
	for _, l := range old {
		if l.ExitID == exitID {
			byID[l.ID] = l
		}
	}
	writes := []store.ReverseWrite{}
	seen := map[int64]bool{}
	for _, v := range in.Links {
		if v.TransitID == exitID || seen[v.TransitID] {
			fail(w, 400, "choose distinct transit nodes different from the exit")
			return
		}
		seen[v.TransitID] = true
		if !h.requireProxyNode(w, r, v.TransitID) {
			return
		}
		if _, err := h.Store.NodeByID(r.Context(), v.TransitID); err != nil {
			fail(w, 400, "transit node not found")
			return
		}
		v.Name = strings.TrimSpace(v.Name)
		v.ServerName = strings.TrimSpace(v.ServerName)
		if v.Name == "" || len(v.Name) > 128 || v.ServerName == "" || strings.ContainsAny(v.ServerName, " /:\r\n") {
			fail(w, 400, "name and valid REALITY server name are required")
			return
		}
		x := store.ReverseWrite{}
		if v.ID != "" {
			l, ok := byID[v.ID]
			if !ok || l.TransitID != v.TransitID {
				fail(w, 409, "connection identity changed; reload")
				return
			}
			x.Link = l
			u, e := h.Store.InboundByID(r.Context(), l.UserInboundID)
			if e != nil {
				serverErr(w, e)
				return
			}
			x.User = *u
			c, e := h.Store.InboundByID(r.Context(), l.ReceiverInboundID)
			if e != nil {
				serverErr(w, e)
				return
			}
			x.Receiver = *c
			en, e := h.Store.EntryByID(r.Context(), l.EntryID)
			if e != nil {
				serverErr(w, e)
				return
			}
			x.Entry = *en
		} else {
			x.Link = store.ReverseLink{ID: auth.UUID(), ExitID: exitID, TransitID: v.TransitID}
			tls := func() *spec.TLS {
				return &spec.TLS{Mode: spec.TLSReality, ServerName: v.ServerName, Reality: &spec.Reality{HandshakeServer: v.ServerName, HandshakePort: 443}}
			}
			x.User = domain.Inbound{Tag: "rv-user-" + x.Link.ID, Protocol: spec.VLESS, Settings: spec.Inbound{Flow: "xtls-rprx-vision", TLS: tls()}}
			x.Receiver = domain.Inbound{Tag: "rv-control-" + x.Link.ID, Protocol: spec.VLESS, Settings: spec.Inbound{TLS: tls(), Reverse: &spec.ReverseInbound{ID: x.Link.ID, Receiver: true, UUID: auth.UUID()}}}
		}
		if v.Settings != nil {
			policy := x.User.Settings.PrivateAccess
			x.User.Settings = *v.Settings
			if x.User.Settings.PrivateAccess == nil {
				x.User.Settings.PrivateAccess = policy
			}
		}
		if v.Protocol != "" {
			x.User.Protocol = v.Protocol
		}
		x.User.Settings.Reverse = &spec.ReverseInbound{ID: x.Link.ID}
		x.User.Listen = v.Listen
		// The common SNI controls the tunnel and the default REALITY user
		// endpoint; explicit standard TLS settings keep their own certificate.
		for _, ib := range []*domain.Inbound{&x.User, &x.Receiver} {
			ib.NodeID = v.TransitID
			ib.Core = "xray"
			ib.Enabled = v.Enabled
			ib.IngressID = v.IngressID
			ib.Port = v.Port
			if ib == &x.Receiver {
				ib.Port = v.TunnelPort
			}
			if t := ib.Settings.TLS; t != nil && t.Mode == spec.TLSReality {
				t.ServerName = v.ServerName
				if t.Reality != nil {
					t.Reality.HandshakeServer = v.ServerName
				}
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
		}
		if x.User.Settings.PrivateAccess.Enabled() {
			if msg := h.checkInboundCore(r.Context(), &x.User); msg != "" {
				fail(w, 400, msg)
				return
			}
			if msg := h.checkPrivateAccess(r.Context(), v.TransitID, &x.User); msg != "" {
				fail(w, 400, msg)
				return
			}
			exitInbound := x.User
			exitInbound.NodeID = exitID
			exitInbound.ID = 0
			if msg := h.checkInboundCore(r.Context(), &exitInbound); msg != "" {
				fail(w, 400, msg)
				return
			}
			if msg := h.checkPrivateAccess(r.Context(), exitID, &exitInbound); msg != "" {
				fail(w, 400, msg)
				return
			}
		}
		if x.User.Port == x.Receiver.Port {
			fail(w, 409, "user and tunnel ports must differ")
			return
		}
		// Omission preserves membership; explicit null retains the old clear operation.
		if len(v.GroupIDValue) > 0 || x.User.ID == 0 {
			if len(v.GroupIDValue) > 0 && json.Unmarshal(v.GroupIDValue, &v.GroupID) != nil {
				fail(w, 400, "invalid group_id")
				return
			}
			x.User.GroupID = v.GroupID
		}
		x.Receiver.GroupID = nil
		x.Entry.Name = v.Name
		x.Entry.Enabled = v.Enabled
		x.Entry.InboundID = x.User.ID
		// Resolve endpoints from the prospective ingress/ports, not the old
		// inbound rows (which are updated only after all links validate).
		n, _ := h.Store.NodeByID(r.Context(), v.TransitID)
		host := n.Domain
		if host == "" {
			host = n.PublicAddr
		}
		port := v.Port
		if v.IngressID != nil {
			g, e := h.Store.IngressByID(r.Context(), *v.IngressID)
			if e != nil {
				fail(w, 400, "ingress not found")
				return
			}
			host = g.ClientHost()
			port = g.EntryPort(v.Port)
		}
		if host == "" {
			fail(w, 400, "transit requires a reachable public address or ingress")
			return
		}
		x.Entry.DisplayHost = host
		x.Entry.DisplayPort = port
		writes = append(writes, x)
	}
	if err = h.Store.SaveReverseLinks(r.Context(), exitID, in.Expected, writes); err != nil {
		if errors.Is(err, store.ErrReverseConflict) {
			fail(w, 409, err.Error())
		} else {
			fail(w, 409, fmt.Sprintf("could not save reverse connections: %v", err))
		}
		return
	}
	h.State.Invalidate()
	ok(w, map[string]bool{"ok": true})
}
