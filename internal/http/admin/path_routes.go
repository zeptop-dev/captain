package admin

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/zeptop-dev/captain/internal/store"
)

// Register this from the admin router; role and distinct resource scopes are
// checked by requireAdmin, as with ordinary managed-line configuration.
func registerPathRoutes(mux *http.ServeMux, h *handlers) {
	mux.HandleFunc("GET /api/admin/path-routes/{id}/health", h.requireAdmin(h.getPathHealth))
	mux.HandleFunc("PUT /api/admin/path-routes/{id}/health", h.requireAdmin(h.putPathHealth))
	mux.HandleFunc("GET /api/admin/forward-paths", h.requireAdmin(h.listForwardPaths))
	mux.HandleFunc("POST /api/admin/forward-paths", h.requireAdmin(h.saveForwardPath))
	mux.HandleFunc("GET /api/admin/forward-paths/{id}", h.requireAdmin(h.getForwardPath))
	mux.HandleFunc("PUT /api/admin/forward-paths/{id}", h.requireAdmin(h.saveForwardPath))
	mux.HandleFunc("DELETE /api/admin/forward-paths/{id}", h.requireAdmin(h.deleteForwardPath))
	mux.HandleFunc("GET /api/admin/forward-paths/{id}/revisions/{revision}", h.requireAdmin(h.getForwardPath))
	mux.HandleFunc("GET /api/admin/forward-paths/{id}/uses", h.requireAdmin(h.forwardPathUses))
	mux.HandleFunc("GET /api/admin/path-routes", h.requireAdmin(h.listPathRoutes))
	mux.HandleFunc("POST /api/admin/path-routes", h.requireAdmin(h.savePathRoute))
	mux.HandleFunc("POST /api/admin/path-routes/preview", h.requireAdmin(h.previewPathRoute))
	mux.HandleFunc("GET /api/admin/path-routes/{id}", h.requireAdmin(h.getPathRoute))
	mux.HandleFunc("PUT /api/admin/path-routes/{id}", h.requireAdmin(h.savePathRoute))
	mux.HandleFunc("DELETE /api/admin/path-routes/{id}", h.requireAdmin(h.deletePathRoute))
	mux.HandleFunc("POST /api/admin/path-routes/{id}/preview", h.requireAdmin(h.previewPathRoute))
}
func pathError(w http.ResponseWriter, e error) {
	if errors.Is(e, store.ErrPathConflict) || errors.Is(e, store.ErrPathInUse) || errors.Is(e, store.ErrPathRouteOwned) {
		fail(w, 409, e.Error())
		return
	}
	lineError(w, e)
}
func (h *handlers) listForwardPaths(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	v, e := h.Store.ListForwardPaths(r.Context())
	if e != nil {
		serverErr(w, e)
		return
	}
	ok(w, v)
}
func (h *handlers) getForwardPath(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	revision := int64(0)
	if raw := r.PathValue("revision"); raw != "" {
		var e error
		revision, e = strconv.ParseInt(raw, 10, 64)
		if e != nil || revision < 1 {
			fail(w, 400, "invalid revision")
			return
		}
	}
	v, e := h.Store.ForwardPath(r.Context(), idOf(r), revision)
	if e != nil {
		serverErr(w, e)
		return
	}
	ok(w, v)
}
func (h *handlers) saveForwardPath(w http.ResponseWriter, r *http.Request) {
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	var p store.ForwardPath
	if !decode(r, &p) {
		fail(w, 400, "invalid path")
		return
	}
	p.ID = idOf(r)
	if r.Method == "POST" {
		p.ID, p.Version = 0, 0
	}
	if e := h.Store.SaveForwardPath(r.Context(), &p); e != nil {
		pathError(w, e)
		return
	}
	ok(w, p)
}
func (h *handlers) deleteForwardPath(w http.ResponseWriter, r *http.Request) {
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	v, e := strconv.ParseInt(r.URL.Query().Get("version"), 10, 64)
	if e != nil || v < 1 {
		fail(w, 400, "version is required")
		return
	}
	if e = h.Store.DeleteForwardPath(r.Context(), idOf(r), v); e != nil {
		pathError(w, e)
		return
	}
	ok(w, map[string]bool{"ok": true})
}
func (h *handlers) forwardPathUses(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	v, e := h.Store.ForwardPathUses(r.Context(), idOf(r))
	if e != nil {
		serverErr(w, e)
		return
	}
	ok(w, v)
}
func (h *handlers) listPathRoutes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	v, e := h.Store.ListPathRoutes(r.Context())
	if e != nil {
		serverErr(w, e)
		return
	}
	ok(w, v)
}
func (h *handlers) getPathRoute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	v, e := h.Store.PathRoute(r.Context(), idOf(r))
	if e != nil {
		serverErr(w, e)
		return
	}
	ok(w, v)
}
func (h *handlers) savePathRoute(w http.ResponseWriter, r *http.Request) {
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	var p store.PathRoute
	if !decode(r, &p) {
		fail(w, 400, "invalid route")
		return
	}
	p.ID = idOf(r)
	if r.Method == "POST" {
		p.ID, p.Version = 0, 0
	}
	var before *store.PathRoute
	if p.ID > 0 {
		var err error
		before, err = h.Store.PathRoute(r.Context(), p.ID)
		if err != nil {
			pathError(w, err)
			return
		}
	}
	if !h.requirePathHealthMutation(w, r, before, &p) {
		return
	}
	if e := h.Store.SavePathRoute(r.Context(), &p); e != nil {
		pathError(w, e)
		return
	}
	ok(w, p)
}
func (h *handlers) deletePathRoute(w http.ResponseWriter, r *http.Request) {
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	v, e := strconv.ParseInt(r.URL.Query().Get("version"), 10, 64)
	if e != nil || v < 1 {
		fail(w, 400, "version is required")
		return
	}
	before, e := h.Store.PathRoute(r.Context(), idOf(r))
	if e != nil {
		pathError(w, e)
		return
	}
	if !h.requirePathHealthMutation(w, r, before, nil) {
		return
	}
	change, e := h.Store.DeletePathRoute(r.Context(), idOf(r), v)
	if e != nil {
		pathError(w, e)
		return
	}
	ok(w, map[string]any{"ok": true, "change_id": change})
}
func (h *handlers) previewPathRoute(w http.ResponseWriter, r *http.Request) {
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	w.Header().Set("Cache-Control", "no-store")
	var p store.PathRoute
	if !decode(r, &p) {
		fail(w, 400, "invalid route")
		return
	}
	p.ID = idOf(r)
	if p.ID == 0 {
		p.Version = 0
	}
	v, e := h.Store.PreviewPathRoute(r.Context(), &p)
	if e != nil {
		pathError(w, e)
		return
	}
	ok(w, v)
}
