package admin

import (
	"errors"
	"net/http"
	"time"

	"github.com/zeptop-dev/captain/internal/store"
)

func (h *handlers) getNodeProbePlan(w http.ResponseWriter, r *http.Request) {
	id, valid := pathID(r)
	if !valid {
		fail(w, 400, "invalid node id")
		return
	}
	out, err := h.Store.NodeProbePlanView(r.Context(), id)
	if err != nil {
		serverErr(w, err)
		return
	}
	ok(w, out)
}
func (h *handlers) putNodeProbePlan(w http.ResponseWriter, r *http.Request) {
	id, valid := pathID(r)
	if !valid {
		fail(w, 400, "invalid node id")
		return
	}
	var in store.NodeProbePlanChange
	if !readJSON(w, r, &in) {
		return
	}
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	out, err := h.Store.UpdateNodeProbePlan(r.Context(), id, in, time.Now())
	if err != nil {
		switch {
		case errors.Is(err, store.ErrProbePlanConflict):
			fail(w, 409, err.Error())
		case errors.Is(err, store.ErrProbePlan):
			fail(w, 400, err.Error())
		default:
			serverErr(w, err)
		}
		return
	}
	if h.Probe != nil {
		h.Probe.Invalidate()
	}
	ok(w, out)
}
