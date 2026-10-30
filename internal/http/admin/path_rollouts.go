package admin

import (
	"github.com/zeptop-dev/captain/internal/store"
	"net/http"
	"time"
)

func registerPathRollouts(mux *http.ServeMux, h *handlers) {
	mux.HandleFunc("GET /api/admin/path-rollouts", h.requireAdmin(h.listLineRollouts))
	mux.HandleFunc("GET /api/admin/path-rollouts/{rollout}", h.requireAdmin(h.getLineRollout))
	mux.HandleFunc("POST /api/admin/path-rollouts/preview", h.requireAdmin(h.previewPathRollout))
	mux.HandleFunc("POST /api/admin/path-rollouts", h.requireAdmin(h.planPathRollout))
	mux.HandleFunc("POST /api/admin/path-rollouts/{rollout}/{action}", h.requireAdmin(h.actLineRollout))
}

type pathRolloutInput struct {
	Route          store.PathRoute `json:"route"`
	Remove         bool            `json:"remove"`
	TimeoutSeconds int             `json:"timeout_seconds"`
}

func decodePathRollout(w http.ResponseWriter, r *http.Request) (*pathRolloutInput, bool) {
	var in pathRolloutInput
	if !decode(r, &in) {
		fail(w, 400, "invalid path release plan")
		return nil, false
	}
	if in.TimeoutSeconds == 0 {
		in.TimeoutSeconds = 300
	}
	if in.TimeoutSeconds < 30 || in.TimeoutSeconds > 3600 {
		fail(w, 400, "timeout must be between 30 and 3600 seconds")
		return nil, false
	}
	return &in, true
}
func (h *handlers) previewPathRollout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	in, valid := decodePathRollout(w, r)
	if !valid {
		return
	}
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	v, e := h.Store.PreviewPathRollout(r.Context(), &in.Route, in.Remove, time.Duration(in.TimeoutSeconds)*time.Second)
	if e != nil {
		lineRolloutError(w, e)
		return
	}
	ok(w, v)
}
func (h *handlers) planPathRollout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	in, valid := decodePathRollout(w, r)
	if !valid {
		return
	}
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	var before *store.PathRoute
	if in.Route.ID > 0 {
		var err error
		before, err = h.Store.PathRoute(r.Context(), in.Route.ID)
		if err != nil {
			lineRolloutError(w, err)
			return
		}
	}
	next := &in.Route
	if in.Remove {
		next = nil
	}
	if !h.requirePathHealthMutation(w, r, before, next) {
		return
	}
	v, e := h.Store.PlanPathRollout(r.Context(), &in.Route, in.Remove, time.Duration(in.TimeoutSeconds)*time.Second)
	if e != nil {
		lineRolloutError(w, e)
		return
	}
	ok(w, v)
}
