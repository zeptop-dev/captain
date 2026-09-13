package admin

import (
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/zeptop-dev/captain/internal/domain"
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
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, list)
}

type ingressInput struct {
	Name, Kind, BindIP, LineIP, EntryHost, EntryDomain string
	PortFrom, PortTo, PortOffset                       int
}

func (in *ingressInput) apply(g *store.Ingress) string {
	g.Name = strings.TrimSpace(in.Name)
	if g.Name == "" {
		return "name is required"
	}
	g.Kind = "mapped"
	g.BindIP = strings.TrimSpace(in.BindIP)
	if g.BindIP != "" && net.ParseIP(g.BindIP) == nil {
		return "bind address must be an IP on this node"
	}
	g.LineIP = strings.TrimSpace(in.LineIP)
	if g.LineIP != "" && net.ParseIP(g.LineIP) == nil {
		return "line address must be an IP"
	}
	g.EntryHost = strings.ToLower(strings.TrimSpace(in.EntryHost))
	if strings.ContainsAny(g.EntryHost, " /:") {
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
	if (in.PortFrom == 0) != (in.PortTo == 0) || in.PortFrom < 0 || in.PortTo > 65535 || in.PortFrom > in.PortTo {
		return "port range must be from-to within 1-65535, or empty"
	}
	g.PortFrom, g.PortTo, g.PortOffset = in.PortFrom, in.PortTo, in.PortOffset
	return ""
}

func (h *handlers) createIngress(w http.ResponseWriter, r *http.Request) {
	var in ingressInput
	if !decode(r, &in) {
		fail(w, http.StatusBadRequest, "bad json")
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
	if err := h.Store.CreateIngress(r.Context(), g); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]any{"ingress": g, "dns": h.DNS.EnsureMany(r.Context(), [2]string{g.EntryDomain, g.EntryHost})})
}

func (h *handlers) updateIngress(w http.ResponseWriter, r *http.Request) {
	var in ingressInput
	if !decode(r, &in) {
		fail(w, http.StatusBadRequest, "bad json")
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
	if err := h.Store.UpdateIngress(r.Context(), g); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]any{"ingress": g, "dns": h.DNS.EnsureMany(r.Context(), [2]string{g.EntryDomain, g.EntryHost})})
}

func (h *handlers) deleteIngress(w http.ResponseWriter, r *http.Request) {
	if err := h.Store.DeleteIngress(r.Context(), idOf(r)); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]bool{"ok": true})
}

// checkIngress validates an inbound's ingress reference: same node, port
// inside the line's range.
func (h *handlers) checkIngress(r *http.Request, ib *domain.Inbound) string {
	if ib.IngressID == nil {
		return ""
	}
	g, err := h.Store.IngressByID(r.Context(), *ib.IngressID)
	if err != nil || g.NodeID != ib.NodeID {
		return "ingress not found on this node"
	}
	if !g.AllowsPort(ib.Port) {
		return fmt.Sprintf("port %d is outside the %s line's range %d-%d", ib.Port, g.Name, g.PortFrom, g.PortTo)
	}
	return ""
}
