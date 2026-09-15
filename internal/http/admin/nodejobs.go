package admin

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/zeptop-dev/captain/internal/auth"
	"github.com/zeptop-dev/captain/internal/store"
)

// Node jobs: one-off tasks (REALITY target scans) the panel hands a node
// through its state. The node answers in its next report; the UI polls.

var jobKinds = map[string]bool{"reality_scan": true, "warp_register": true}

func (h *handlers) createNodeJob(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	if !okID {
		fail(w, http.StatusBadRequest, "bad id")
		return
	}
	var in struct {
		Kind   string          `json:"kind"`
		Params json.RawMessage `json:"params"`
	}
	if !decode(r, &in) || !jobKinds[in.Kind] {
		fail(w, http.StatusBadRequest, "unknown job kind")
		return
	}
	if _, err := h.Store.NodeByID(r.Context(), id); err != nil {
		fail(w, http.StatusNotFound, "node not found")
		return
	}
	jobID := auth.Token(12)
	if err := h.Store.CreateNodeJob(r.Context(), jobID, id, in.Kind, in.Params); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]string{"id": jobID})
}

func (h *handlers) getNodeJob(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	if !okID {
		fail(w, http.StatusBadRequest, "bad id")
		return
	}
	j, err := h.Store.NodeJob(r.Context(), id, r.PathValue("job"))
	if errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "job not found")
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, j)
}
