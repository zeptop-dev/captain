package admin

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/store"
)

func (h *handlers) registerNodeServiceControl(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/nodes/{id}/service-control", h.requireAdmin(h.getNodeServiceControl))
	mux.HandleFunc("PUT /api/admin/nodes/{id}/service-control", h.requireAdmin(h.putNodeServiceControl))
}
func (h *handlers) getNodeServiceControl(w http.ResponseWriter, r *http.Request) {
	id, valid := pathID(r)
	if !valid {
		fail(w, http.StatusBadRequest, "invalid node id")
		return
	}
	v, err := h.Store.NodeServiceControl(r.Context(), id, time.Now())
	if err != nil {
		serverErr(w, err)
		return
	}
	ok(w, v)
}
func (h *handlers) putNodeServiceControl(w http.ResponseWriter, r *http.Request) {
	id, valid := pathID(r)
	if !valid {
		fail(w, http.StatusBadRequest, "invalid node id")
		return
	}
	var change store.ServiceControlChange
	if !readJSON(w, r, &change) {
		return
	}
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	v, err := h.Store.UpdateNodeServiceControl(r.Context(), id, change, time.Now())
	if err != nil {
		switch {
		case errors.Is(err, store.ErrServiceControlConflict), errors.Is(err, store.ErrServiceControlExemption):
			fail(w, http.StatusConflict, err.Error())
		case errors.Is(err, store.ErrServiceControl):
			fail(w, http.StatusBadRequest, err.Error())
		default:
			serverErr(w, err)
		}
		return
	}
	ok(w, v)
}
func (h *handlers) serviceControlWriteAllowed(r *http.Request) bool {
	if !userFrom(r).IsAdmin() {
		return false
	}
	tok := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer"))
	if !strings.HasPrefix(tok, "cap_") {
		return true
	}
	u, scope, err := h.Store.UserByAPIToken(r.Context(), tok)
	return err == nil && u != nil && u.IsAdmin() && u.Status == "active" && store.TokenAllowsRequest(scope, http.MethodPut, "/api/admin/nodes/"+r.PathValue("id")+"/service-control", "PUT /api/admin/nodes/{id}/service-control")
}
