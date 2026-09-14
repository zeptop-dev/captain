package admin

import (
	"net/http"
	"strings"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"

	"github.com/zeptop-dev/bosun/pkg/subscription"
	"github.com/zeptop-dev/captain/internal/store"
)

func (h *handlers) registerExternal(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/external/sources", h.requireAdmin(h.listExternalSources))
	mux.HandleFunc("POST /api/admin/external/sources", h.requireAdmin(h.saveExternalSource))
	mux.HandleFunc("PATCH /api/admin/external/sources/{id}", h.requireAdmin(h.saveExternalSource))
	mux.HandleFunc("DELETE /api/admin/external/sources/{id}", h.requireAdmin(h.deleteExternalSource))
	mux.HandleFunc("POST /api/admin/external/sources/{id}/sync", h.requireAdmin(h.syncExternalSource))
	mux.HandleFunc("GET /api/admin/external/nodes", h.requireAdmin(h.listExternalNodes))
	mux.HandleFunc("POST /api/admin/external/nodes", h.requireAdmin(h.importExternalNodes))
	mux.HandleFunc("PATCH /api/admin/external/nodes/{id}", h.requireAdmin(h.updateExternalNode))
	mux.HandleFunc("DELETE /api/admin/external/nodes/{id}", h.requireAdmin(h.deleteExternalNode))
	mux.HandleFunc("POST /api/admin/external/parse", h.requireAdmin(h.parseLinks))
	mux.HandleFunc("GET /api/admin/nodes/{id}/routing", h.requireAdmin(h.getNodeRouting))
	mux.HandleFunc("PUT /api/admin/nodes/{id}/routing", h.requireAdmin(h.putNodeRouting))
	mux.HandleFunc("GET /api/admin/nodes/{id}/forwards", h.requireAdmin(h.getNodeForwards))
	mux.HandleFunc("PUT /api/admin/nodes/{id}/forwards", h.requireAdmin(h.putNodeForwards))
}

func (h *handlers) listExternalSources(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.ListExternalSources(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, list)
}

func (h *handlers) saveExternalSource(w http.ResponseWriter, r *http.Request) {
	var src store.ExternalSource
	if !decode(r, &src) || strings.TrimSpace(src.Name) == "" || !strings.HasPrefix(src.URL, "http") {
		fail(w, http.StatusBadRequest, "name and an http(s) url are required")
		return
	}
	src.ID = idOf(r)
	if err := h.Store.SaveExternalSource(r.Context(), &src); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	n, err := h.External.Sync(r.Context(), &src, time.Now())
	ok(w, map[string]any{"source": src, "synced": n, "error": errString(err)})
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (h *handlers) deleteExternalSource(w http.ResponseWriter, r *http.Request) {
	if err := h.Store.DeleteExternalSource(r.Context(), idOf(r)); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]bool{"ok": true})
}

func (h *handlers) syncExternalSource(w http.ResponseWriter, r *http.Request) {
	srcs, _ := h.Store.ListExternalSources(r.Context())
	for i := range srcs {
		if srcs[i].ID == idOf(r) {
			n, err := h.External.Sync(r.Context(), &srcs[i], time.Now())
			if err != nil {
				fail(w, http.StatusBadGateway, err.Error())
				return
			}
			ok(w, map[string]any{"synced": n})
			return
		}
	}
	fail(w, http.StatusNotFound, "source not found")
}

func (h *handlers) listExternalNodes(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.ListExternalNodes(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, list)
}

// parseLinks previews share links pasted by the admin.
func (h *handlers) parseLinks(w http.ResponseWriter, r *http.Request) {
	var in struct{ Text string }
	if !decode(r, &in) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	lines, skipped := subscription.ParseList(in.Text)
	out := make([]map[string]any, 0, len(lines))
	for _, l := range lines {
		out = append(out, map[string]any{"name": l.Name, "host": l.Host, "port": l.Port, "protocol": l.Inbound.Protocol, "uri": subscription.ShareURI(l), "remote": l.Remote()})
	}
	ok(w, map[string]any{"nodes": out, "skipped": skipped})
}

// importExternalNodes stores pasted share links as manual nodes.
func (h *handlers) importExternalNodes(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text    string
		GroupID *int64
		Rate    float64
	}
	if !decode(r, &in) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	lines, skipped := subscription.ParseList(in.Text)
	if len(lines) == 0 {
		fail(w, http.StatusBadRequest, "no share links recognised")
		return
	}
	added := 0
	for i, l := range lines {
		n := store.ExternalNode{Name: l.Name, URI: subscription.ShareURI(l), GroupID: in.GroupID, Rate: in.Rate, Sort: i, Enabled: true}
		if err := h.Store.SaveExternalNode(r.Context(), &n); err == nil {
			added++
		}
	}
	ok(w, map[string]any{"added": added, "skipped": skipped})
}

func (h *handlers) updateExternalNode(w http.ResponseWriter, r *http.Request) {
	var n store.ExternalNode
	if !decode(r, &n) || strings.TrimSpace(n.Name) == "" {
		fail(w, http.StatusBadRequest, "name is required")
		return
	}
	if _, err := subscription.ParseURI(n.URI); err != nil {
		fail(w, http.StatusBadRequest, "uri: "+err.Error())
		return
	}
	n.ID = idOf(r)
	if err := h.Store.SaveExternalNode(r.Context(), &n); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, n)
}

func (h *handlers) deleteExternalNode(w http.ResponseWriter, r *http.Request) {
	if err := h.Store.DeleteExternalNode(r.Context(), idOf(r)); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]bool{"ok": true})
}

// ---- node routing --------------------------------------------------------------------

func (h *handlers) getNodeRouting(w http.ResponseWriter, r *http.Request) {
	nr, err := h.Store.NodeRouting(r.Context(), idOf(r))
	if err != nil {
		fail(w, http.StatusNotFound, "node not found")
		return
	}
	ok(w, nr)
}

// putNodeRouting validates tags: every rule and the default must point at
// a defined outbound, chains must not loop on themselves.
func (h *handlers) putNodeRouting(w http.ResponseWriter, r *http.Request) {
	var nr store.NodeRouting
	if !decode(r, &nr) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	tags := map[string]bool{"direct": true, "block": true}
	for i := range nr.Outbounds {
		o := &nr.Outbounds[i]
		o.Tag = strings.TrimSpace(o.Tag)
		if o.Tag == "" || tags[o.Tag] {
			fail(w, http.StatusBadRequest, "every outbound needs a unique tag (not direct/block)")
			return
		}
		if o.Remote == nil && o.Protocol == "" {
			fail(w, http.StatusBadRequest, "outbound "+o.Tag+": a share link or a protocol with settings is required")
			return
		}
		if o.Remote != nil && (o.Remote.Host == "" || o.Remote.Port <= 0 || o.Remote.Settings.Protocol == "") {
			fail(w, http.StatusBadRequest, "outbound "+o.Tag+": host, port and protocol are required")
			return
		}
		if o.ProxyTag == o.Tag {
			fail(w, http.StatusBadRequest, "outbound "+o.Tag+" cannot chain through itself")
			return
		}
		tags[o.Tag] = true
	}
	for _, o := range nr.Outbounds {
		if o.ProxyTag != "" && !tags[o.ProxyTag] {
			fail(w, http.StatusBadRequest, "outbound "+o.Tag+" chains through unknown "+o.ProxyTag)
			return
		}
	}
	for i := range nr.Routes {
		rule := &nr.Routes[i]
		switch rule.Action {
		case "outbound":
			if !tags[rule.Value] || rule.Value == "direct" || rule.Value == "block" {
				fail(w, http.StatusBadRequest, "rule points at unknown outbound "+rule.Value)
				return
			}
		case "direct", "block":
			rule.Value = ""
		default:
			fail(w, http.StatusBadRequest, "rule action must be outbound, direct or block")
			return
		}
		if len(rule.Match) == 0 {
			fail(w, http.StatusBadRequest, "a rule needs at least one match (inbound:tag, domain:, ip:, protocol:, port:)")
			return
		}
	}
	if nr.DefaultOutbound != "" && (!tags[nr.DefaultOutbound] || nr.DefaultOutbound == "direct" || nr.DefaultOutbound == "block") {
		fail(w, http.StatusBadRequest, "default outbound must be one of the defined outbounds")
		return
	}
	if err := h.Store.SetNodeRouting(r.Context(), idOf(r), &nr); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.getNodeRouting(w, r)
}

var _ = spec.Outbound{}
