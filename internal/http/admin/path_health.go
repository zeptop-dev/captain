package admin

import (
	"net/http"
	"time"

	"github.com/zeptop-dev/captain/internal/store"
)

func (h *handlers) getPathHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	v, err := h.Store.PathHealth(r.Context(), idOf(r), time.Now())
	if err != nil {
		serverErr(w, err)
		return
	}
	ok(w, v)
}

func (h *handlers) putPathHealth(w http.ResponseWriter, r *http.Request) {
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	var input struct {
		Version int64                  `json:"version"`
		Health  *store.PathRouteHealth `json:"health"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	if input.Health == nil || input.Version < 1 {
		fail(w, http.StatusBadRequest, "version and health are required")
		return
	}
	before, err := h.Store.PathRoute(r.Context(), idOf(r))
	if err != nil {
		pathError(w, err)
		return
	}
	if before.Version != input.Version {
		pathError(w, store.ErrLineConflict)
		return
	}
	next := *before
	next.Health = input.Health
	if !h.requirePathHealthMutation(w, r, before, &next) {
		return
	}
	if err = h.Store.SavePathRoute(r.Context(), &next); err != nil {
		pathError(w, err)
		return
	}
	ok(w, next)
}
