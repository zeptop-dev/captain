package admin

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

func (h *handlers) registerNodeAccess(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/node-access", h.requireAdmin(h.nodeAccessCatalog))
	mux.HandleFunc("PUT /api/admin/node-access/inbounds/{id}", h.requireAdmin(h.putInboundAccess))
	mux.HandleFunc("PUT /api/admin/node-access/groups/{id}", h.requireAdmin(h.putGroupAccess))
}

func (h *handlers) nodeAccessCatalog(w http.ResponseWriter, r *http.Request) {
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	catalog, err := h.Store.AccessCatalog(r.Context())
	if err != nil {
		serverErr(w, err)
		return
	}
	ok(w, catalog)
}

func (h *handlers) putInboundAccess(w http.ResponseWriter, r *http.Request) {
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	id, valid := pathID(r)
	if !valid {
		fail(w, http.StatusBadRequest, "invalid inbound id")
		return
	}
	var change domain.InboundAccessChange
	if !readJSON(w, r, &change) {
		return
	}
	if change.InboundID != 0 && change.InboundID != id {
		fail(w, http.StatusBadRequest, "access draft belongs to a different inbound")
		return
	}
	change.InboundID = id
	if err := h.Store.UpdateInboundAccess(r.Context(), id, change); err != nil {
		if !nodeAccessError(w, err) {
			serverErr(w, err)
		}
		return
	}
	ok(w, map[string]bool{"ok": true})
}

func (h *handlers) putGroupAccess(w http.ResponseWriter, r *http.Request) {
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	id, valid := pathID(r)
	if !valid {
		fail(w, http.StatusBadRequest, "invalid group id")
		return
	}
	// A missing membership list must never mean "revoke everything".
	var input struct {
		InboundIDs    *[]int64 `json:"inbound_ids"`
		Revision      string   `json:"revision"`
		ConfirmShared bool     `json:"confirm_shared"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	if input.InboundIDs == nil {
		fail(w, http.StatusBadRequest, "inbound_ids is required")
		return
	}
	change := store.GroupAccessChange{InboundIDs: *input.InboundIDs, Revision: input.Revision, ConfirmShared: input.ConfirmShared}
	if err := h.Store.UpdateGroupAccess(r.Context(), id, change); err != nil {
		if !nodeAccessError(w, err) {
			serverErr(w, err)
		}
		return
	}
	ok(w, map[string]bool{"ok": true})
}

// Entry metadata and a shared listener's authentication have separate scopes.
// A token limited to editing entries cannot smuggle a listener change into it.
func (h *handlers) entryAccessAllowed(w http.ResponseWriter, r *http.Request, change *domain.InboundAccessChange) bool {
	if change == nil {
		return true
	}
	if !userFrom(r).IsAdmin() {
		fail(w, http.StatusForbidden, "only administrators can change node access")
		return false
	}
	tok := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer"))
	if strings.HasPrefix(tok, "cap_") {
		u, scope, err := h.Store.UserByAPIToken(r.Context(), tok)
		path := "/api/admin/node-access/inbounds/" + strconv.FormatInt(change.InboundID, 10)
		if err != nil || u == nil || !u.IsAdmin() || u.Status != "active" || !store.TokenAllowsRequest(scope, http.MethodPut, path, "PUT /api/admin/node-access/inbounds/{id}") {
			fail(w, http.StatusForbidden, "this token does not permit changing node access")
			return false
		}
	}
	return true
}

func nodeAccessError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, store.ErrAccessConflict):
		fail(w, http.StatusConflict, "node access changed; reload before saving")
	case errors.Is(err, store.ErrAccessConfirmationRequired):
		fail(w, http.StatusConflict, "confirm the shared node or plan impact before saving")
	case errors.Is(err, store.ErrAccessManaged):
		fail(w, http.StatusConflict, "this inbound uses explicit node access; edit its access policy instead of the legacy group")
	case errors.Is(err, store.ErrInvalidAccess):
		fail(w, http.StatusBadRequest, "invalid node access policy or membership")
	case errors.Is(err, store.ErrNotFound):
		fail(w, http.StatusNotFound, "node access target not found")
	default:
		return false
	}
	return true
}
