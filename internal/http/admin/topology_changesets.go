package admin

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/zeptop-dev/captain/internal/store"
)

func registerTopologyChangesets(mux *http.ServeMux, h *handlers) {
	mux.HandleFunc("GET /api/admin/topology-changesets/catalog", h.requireAdmin(h.topologyCatalog))
	mux.HandleFunc("GET /api/admin/topology-changesets", h.requireAdmin(h.topologyList))
	mux.HandleFunc("POST /api/admin/topology-changesets", h.requireAdmin(h.topologySave))
	mux.HandleFunc("GET /api/admin/topology-changesets/{id}", h.requireAdmin(h.topologyGet))
	mux.HandleFunc("PUT /api/admin/topology-changesets/{id}", h.requireAdmin(h.topologySave))
	mux.HandleFunc("DELETE /api/admin/topology-changesets/{id}", h.requireAdmin(h.topologyDiscard))
	mux.HandleFunc("POST /api/admin/topology-changesets/{id}/preview", h.requireAdmin(h.topologyPreview))
	mux.HandleFunc("POST /api/admin/topology-changesets/{id}/commit", h.requireAdmin(h.topologyCommit))
}
func topologyDecode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		fail(w, 400, "invalid typed topology draft")
		return false
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		fail(w, 400, "invalid typed topology draft")
		return false
	}
	return true
}
func topologyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		fail(w, 404, "draft or referenced resource not found")
	case errors.Is(err, store.ErrTopologyConflict), errors.Is(err, store.ErrLineConflict), errors.Is(err, store.ErrLineRolloutConflict), errors.Is(err, store.ErrPathRouteOwned), errors.Is(err, store.ErrLineInUse):
		fail(w, 409, err.Error())
	default:
		fail(w, 400, err.Error())
	}
}

// The new permission cannot grant old object operations. In particular, lines
// have endpoint grants rather than a legacy "lines" token resource.
func (h *handlers) topologyGrants(w http.ResponseWriter, r *http.Request, ops []store.TopologyOperation, write bool, catalog bool) bool {
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer"))
	if !strings.HasPrefix(token, "cap_") {
		return true
	}
	user, policy, err := h.Store.UserByAPIToken(r.Context(), token)
	if err != nil || user == nil || !user.IsAdmin() || user.Status != "active" {
		fail(w, 403, "administrator token required")
		return false
	}
	permits := func(method, path, pattern string) bool {
		return store.TokenAllowsRequest(policy, method, path, method+" "+pattern)
	}
	if catalog {
		for _, p := range []string{"nodes", "ingresses", "lines", "entries"} {
			if !permits("GET", "/api/admin/"+p, "/api/admin/"+p) {
				fail(w, 403, "token does not permit reading the topology catalog")
				return false
			}
		}
		// Inbound lists are returned by the node detail endpoint.
		if !permits("GET", "/api/admin/nodes/1", "/api/admin/nodes/{id}") {
			fail(w, 403, "token does not permit reading inbounds")
			return false
		}
	}
	for _, op := range ops {
		method, path, pattern := "GET", "", ""
		switch op.Kind {
		case "inbound":
			path, pattern = "/api/admin/nodes/1", "/api/admin/nodes/{id}"
			if write {
				if op.ID == 0 {
					method, path, pattern = "POST", "/api/admin/nodes/1/inbounds", "/api/admin/nodes/{id}/inbounds"
				} else {
					method, path, pattern = "PATCH", "/api/admin/inbounds/"+strconv.FormatInt(op.ID, 10), "/api/admin/inbounds/{id}"
				}
			}
		case "line":
			path, pattern = "/api/admin/lines", "/api/admin/lines"
			if write {
				if op.ID == 0 {
					method = "POST"
				} else {
					method, path, pattern = "PUT", "/api/admin/lines/"+strconv.FormatInt(op.ID, 10), "/api/admin/lines/{id}"
				}
			}
		case "entry":
			path, pattern = "/api/admin/entries", "/api/admin/entries"
			if write {
				if op.ID == 0 {
					method = "POST"
				} else {
					method, path, pattern = "PATCH", "/api/admin/entries/"+strconv.FormatInt(op.ID, 10), "/api/admin/entries/{id}"
				}
			}
		default:
			fail(w, 400, "unsupported topology object")
			return false
		}
		if !permits(method, path, pattern) {
			fail(w, 403, "token does not permit one of the draft's object operations")
			return false
		}
	}
	return true
}

// Explicit writes retain their existing response permissions. Recompilation can
// also reveal other objects: authorize those actual results before returning any
// preview, using object identity rather than caller-controlled operation keys.
func (h *handlers) topologyPreviewGrants(w http.ResponseWriter, r *http.Request, ops []store.TopologyOperation, changes []store.TopologyChange) bool {
	type object struct {
		kind string
		id   int64
	}
	explicit := make(map[object]bool, len(ops))
	for _, op := range ops {
		explicit[object{op.Kind, op.ID}] = true
	}
	var dependencies []store.TopologyOperation
	for _, change := range changes {
		if !explicit[object{change.Kind, change.ID}] {
			dependencies = append(dependencies, store.TopologyOperation{Kind: change.Kind, ID: change.ID})
		}
	}
	return h.topologyGrants(w, r, dependencies, false, false)
}

func (h *handlers) topologyCatalog(w http.ResponseWriter, r *http.Request) {
	if !h.topologyGrants(w, r, nil, false, true) {
		return
	}
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	v, e := h.Store.TopologyChangesetCatalog(r.Context())
	if e != nil {
		topologyError(w, e)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	ok(w, v)
}
func (h *handlers) topologyList(w http.ResponseWriter, r *http.Request) {
	ds, e := h.Store.ListTopologyChangesets(r.Context())
	if e != nil {
		topologyError(w, e)
		return
	}
	for _, d := range ds {
		if !h.topologyGrants(w, r, d.Operations, false, false) {
			return
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	ok(w, ds)
}
func (h *handlers) topologyRead(w http.ResponseWriter, r *http.Request, write bool) (*store.TopologyChangeset, bool) {
	d, e := h.Store.TopologyChangeset(r.Context(), r.PathValue("id"))
	if e != nil {
		topologyError(w, e)
		return nil, false
	}
	if !h.topologyGrants(w, r, d.Operations, write, false) {
		return nil, false
	}
	return d, true
}
func (h *handlers) topologyGet(w http.ResponseWriter, r *http.Request) {
	d, yes := h.topologyRead(w, r, false)
	if yes {
		w.Header().Set("Cache-Control", "no-store")
		ok(w, d)
	}
}
func (h *handlers) topologySave(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name       string                    `json:"name"`
		Version    int64                     `json:"version"`
		Operations []store.TopologyOperation `json:"operations"`
	}
	if !topologyDecode(w, r, &in) {
		return
	}
	if !h.topologyGrants(w, r, in.Operations, true, false) {
		return
	}
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	if r.PathValue("id") != "" {
		if _, yes := h.topologyRead(w, r, true); !yes {
			return
		}
	}
	d := &store.TopologyChangeset{ID: r.PathValue("id"), Version: in.Version, Name: in.Name, Operations: in.Operations}
	if e := h.Store.SaveTopologyChangeset(r.Context(), d); e != nil {
		topologyError(w, e)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	ok(w, d)
}
func (h *handlers) topologyPreview(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Version int64 `json:"version"`
	}
	if !topologyDecode(w, r, &in) {
		return
	}
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	d, yes := h.topologyRead(w, r, true)
	if !yes {
		return
	}
	p, e := h.Store.PreviewTopologyChangeset(r.Context(), r.PathValue("id"), in.Version)
	if e != nil {
		topologyError(w, e)
		return
	}
	if !h.topologyPreviewGrants(w, r, d.Operations, p.Changes) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	ok(w, p)
}
func (h *handlers) topologyCommit(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Version  int64  `json:"version"`
		Revision string `json:"preview_revision"`
	}
	if !topologyDecode(w, r, &in) {
		return
	}
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	draft, yes := h.topologyRead(w, r, true)
	if !yes {
		return
	}
	preview, e := h.Store.PreviewTopologyChangeset(r.Context(), r.PathValue("id"), in.Version)
	if e != nil {
		topologyError(w, e)
		return
	}
	if !h.topologyPreviewGrants(w, r, draft.Operations, preview.Changes) {
		return
	}
	d, e := h.Store.CommitTopologyChangeset(r.Context(), r.PathValue("id"), in.Version, in.Revision)
	if e != nil {
		topologyError(w, e)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	ok(w, d)
}
func (h *handlers) topologyDiscard(w http.ResponseWriter, r *http.Request) {
	version, e := strconv.ParseInt(r.URL.Query().Get("version"), 10, 64)
	if e != nil || version < 1 {
		fail(w, 400, "draft version required")
		return
	}
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	if _, yes := h.topologyRead(w, r, true); !yes {
		return
	}
	if e = h.Store.DiscardTopologyChangeset(r.Context(), r.PathValue("id"), version); e != nil {
		topologyError(w, e)
		return
	}
	ok(w, map[string]bool{"ok": true})
}
