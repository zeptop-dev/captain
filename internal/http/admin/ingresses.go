package admin

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/service"
	"github.com/zeptop-dev/captain/internal/store"
)

// Ingresses: IPLC / dedicated lines in front of a node. See store.Ingress.

func (h *handlers) registerIngresses(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/nodes/{id}/ingresses", h.requireAdmin(h.listIngresses))
	mux.HandleFunc("POST /api/admin/nodes/{id}/ingresses", h.requireAdmin(h.createIngress))
	mux.HandleFunc("PATCH /api/admin/ingresses/{id}", h.requireAdmin(h.updateIngress))
	mux.HandleFunc("DELETE /api/admin/ingresses/{id}", h.requireAdmin(h.deleteIngress))
}

func (h *handlers) listIngresses(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.IngressesByNode(r.Context(), idOf(r))
	if err != nil {
		serverErr(w, err)
		return
	}
	ok(w, list)
}

type ingressInput struct {
	Name, Kind, BindIP, LineIP, EntryHost, EntryDomain string
	PortFrom, PortTo, PortOffset                       int
	ReservedPorts                                      []int
	PortMappings                                       *[]spec.PortMapping
	RequireIngress                                     *bool
}

func (in *ingressInput) apply(g *store.Ingress) string {
	g.Name = strings.TrimSpace(in.Name)
	if g.Name == "" {
		return "name is required"
	}
	if in.Kind != "" {
		g.Kind = in.Kind
	}
	if g.Kind == "" {
		g.Kind = "mapped"
	}
	if g.Kind != "mapped" && g.Kind != "nat" && g.Kind != "iplc" {
		return "kind must be nat, iplc or mapped"
	}
	if in.PortMappings != nil {
		g.PortMappings = append([]spec.PortMapping{}, (*in.PortMappings)...)
	}
	if in.RequireIngress != nil {
		g.RequireIngress = *in.RequireIngress
	}
	g.BindIP = strings.TrimSpace(in.BindIP)
	if g.BindIP != "" && net.ParseIP(g.BindIP) == nil {
		return "bind address must be an IP on this node"
	}
	g.LineIP = strings.TrimSpace(in.LineIP)
	if g.LineIP != "" && net.ParseIP(g.LineIP) == nil {
		return "line address must be an IP"
	}
	g.EntryHost = strings.ToLower(strings.TrimSpace(in.EntryHost))
	if g.EntryHost != "" && net.ParseIP(g.EntryHost) == nil && strings.ContainsAny(g.EntryHost, " /:[]") {
		return "entry host must be a host name or IP without a port"
	}
	g.EntryDomain = strings.ToLower(strings.TrimSpace(in.EntryDomain))
	if g.EntryDomain != "" && (strings.ContainsAny(g.EntryDomain, " /:") || net.ParseIP(g.EntryDomain) != nil || !strings.Contains(g.EntryDomain, ".")) {
		return "entry domain must be a host name"
	}
	if g.EntryDomain != "" && net.ParseIP(g.EntryHost) == nil {
		return "an entry domain needs the public entry to be an IP address to point at"
	}
	if g.LineIP == "" && g.EntryHost == "" {
		return "give the line's far-end address, its public entry, or both"
	}
	g.PortFrom, g.PortTo, g.PortOffset = in.PortFrom, in.PortTo, in.PortOffset
	g.ReservedPorts = append([]int{}, in.ReservedPorts...)
	if err := g.Ports().Validate(); err != nil {
		return err.Error()
	}
	if g.Kind == "nat" && g.EntryHost == "" {
		return "NAT ingress requires a public entry address"
	}

	return ""
}

func (h *handlers) createIngress(w http.ResponseWriter, r *http.Request) {
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	var in ingressInput
	if !readJSON(w, r, &in) {
		return
	}
	if _, err := h.Store.NodeByID(r.Context(), idOf(r)); err != nil {
		fail(w, http.StatusNotFound, "node not found")
		return
	}
	g := &store.Ingress{NodeID: idOf(r)}
	if msg := in.apply(g); msg != "" {
		fail(w, http.StatusBadRequest, msg)
		return
	}
	if msg, err := h.checkIngressChange(r.Context(), g, false); err != nil {
		serverErr(w, err)
		return
	} else if msg != "" {
		fail(w, http.StatusConflict, msg)
		return
	}
	if err := h.Store.CreateIngress(r.Context(), g); err != nil {
		serverErr(w, err)
		return
	}
	ok(w, map[string]any{"ingress": g, "dns": h.DNS.EnsureMany(service.WithDNSOrigin(r.Context(), service.DNSOrigin{NodeID: g.NodeID, Source: "ingress:" + strconv.FormatInt(g.ID, 10), Actor: userFrom(r).Email}), [2]string{g.EntryDomain, g.EntryHost})})
}

func (h *handlers) updateIngress(w http.ResponseWriter, r *http.Request) {
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	var in ingressInput
	if !readJSON(w, r, &in) {
		return
	}
	g, err := h.Store.IngressByID(r.Context(), idOf(r))
	if err != nil {
		fail(w, http.StatusNotFound, "ingress not found")
		return
	}
	if msg := in.apply(g); msg != "" {
		fail(w, http.StatusBadRequest, msg)
		return
	}
	if msg, err := h.checkIngressChange(r.Context(), g, false); err != nil {
		serverErr(w, err)
		return
	} else if msg != "" {
		fail(w, http.StatusConflict, msg)
		return
	}
	if err := h.Store.UpdateIngress(r.Context(), g); err != nil {
		lineError(w, err)
		return
	}
	ok(w, map[string]any{"ingress": g, "dns": h.DNS.EnsureMany(service.WithDNSOrigin(r.Context(), service.DNSOrigin{NodeID: g.NodeID, Source: "ingress:" + strconv.FormatInt(g.ID, 10), Actor: userFrom(r).Email}), [2]string{g.EntryDomain, g.EntryHost})})
}

func (h *handlers) deleteIngress(w http.ResponseWriter, r *http.Request) {
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	g, err := h.Store.IngressByID(r.Context(), idOf(r))
	if err != nil {
		fail(w, http.StatusNotFound, "ingress not found")
		return
	}
	if msg, err := h.checkIngressChange(r.Context(), g, true); err != nil {
		serverErr(w, err)
		return
	} else if msg != "" {
		fail(w, http.StatusConflict, msg)
		return
	}
	if err := h.Store.DeleteIngress(r.Context(), idOf(r)); err != nil {
		serverErr(w, err)
		return
	}
	ok(w, map[string]bool{"ok": true})
}

// checkIngress is also run for direct listeners, so require_ingress cannot
// be bypassed by leaving the reference empty.
func (h *handlers) checkIngress(r *http.Request, ib *domain.Inbound) string {
	if err := ib.Spec().CheckCoreListen(""); err != nil {
		return err.Error()
	}
	gs, err := h.Store.IngressesByNode(r.Context(), ib.NodeID)
	if err != nil {
		return "could not read ingress configuration"
	}
	id := ""
	if ib.IngressID != nil {
		id = strconv.FormatInt(*ib.IngressID, 10)
	}
	if msg := checkIngressListener(gs, id, ib.Listen, ib.Port); msg != "" {
		return msg
	}
	if id != "" {
		for _, g := range gs {
			if strconv.FormatInt(g.ID, 10) == id {
				if err := ib.Spec().CheckCoreListen(g.BindIP); err != nil {
					return err.Error()
				}
				if err := g.Ports().CheckInbound(ib.Spec()); err != nil {
					return err.Error()
				}
			}
		}
	}
	return ""
}

func checkIngressListener(gs []store.Ingress, id, listen string, port int) string {
	for _, g := range gs {
		if id == "" && g.RequireIngress {
			return "this node requires an ingress for every inbound and forward"
		}
		if id != "" && strconv.FormatInt(g.ID, 10) == id {
			if err := g.Ports().Check(port); err != nil {
				return fmt.Sprintf("%s: %v", g.Name, err)
			}
			if err := spec.CheckIngressListen(g.BindIP, listen); err != nil {
				return err.Error()
			}
			return ""
		}
	}
	if id != "" {
		return "ingress not found on this node"
	}
	return ""
}

func (h *handlers) checkIngressChange(ctx context.Context, g *store.Ingress, removing bool) (string, error) {
	gs, err := h.Store.IngressesByNode(ctx, g.NodeID)
	if err != nil {
		return "", err
	}
	next := []store.Ingress{}
	for _, old := range gs {
		if old.ID != g.ID {
			next = append(next, old)
		}
	}
	if !removing {
		next = append(next, *g)
	}
	ibs, err := h.Store.AllInboundsByNode(ctx, g.NodeID)
	if err != nil {
		return "", err
	}
	for _, ib := range ibs {
		id := ""
		if ib.IngressID != nil {
			id = strconv.FormatInt(*ib.IngressID, 10)
		}
		if msg := checkIngressListener(next, id, ib.Listen, ib.Port); msg != "" {
			return "inbound " + ib.Tag + ": " + msg, nil
		}
		if ib.IngressID != nil && *ib.IngressID == g.ID && !removing {
			if err := ib.Spec().CheckCoreListen(g.BindIP); err != nil {
				return "inbound " + ib.Tag + ": " + err.Error(), nil
			}
			if err := g.Ports().CheckInbound(ib.Spec()); err != nil {
				return "inbound " + ib.Tag + ": " + err.Error(), nil
			}
		}
	}
	wgLinks, err := h.Store.WireGuardLinks(ctx, g.NodeID)
	if err != nil {
		return "", err
	}
	for _, link := range wgLinks {
		if link.TransitID == g.NodeID {
			if msg := checkIngressListener(next, "", "", link.ListenPort); msg != "" {
				return "WireGuard tunnel: " + msg, nil
			}
		}
	}
	fs, err := h.Store.NodeForwards(ctx, g.NodeID)
	if err != nil {
		return "", err
	}
	for _, f := range fs {
		effective := f.Forward
		if effective.Listen == "" {
			for _, ng := range next {
				if strconv.FormatInt(ng.ID, 10) == f.IngressID {
					effective.Listen = ng.BindIP
				}
			}
		}
		if err := effective.ValidateTargets(); err != nil {
			return "forward " + f.Tag + ": " + err.Error(), nil
		}
		if msg := checkIngressListener(next, f.IngressID, f.Listen, f.Port); msg != "" {
			return "forward " + f.Tag + ": " + msg, nil
		}
	}
	return "", nil
}

func (h *handlers) ingressListen(ctx context.Context, id *int64, listen string) string {
	if listen == "" && id != nil {
		if g, err := h.Store.IngressByID(ctx, *id); err == nil {
			return g.BindIP
		}
	}
	return listen
}
func (h *handlers) forwardIngressListen(ctx context.Context, f store.NodeForward) string {
	id, err := strconv.ParseInt(f.IngressID, 10, 64)
	if err != nil {
		return f.Listen
	}
	return h.ingressListen(ctx, &id, f.Listen)
}
