package admin

import (
	"net/http"
	"time"
)

func (h *handlers) getNodeConfiguration(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id, valid := pathID(r)
	if !valid {
		fail(w, http.StatusBadRequest, "bad id")
		return
	}
	// The server middleware holds Accounts.RLock for this request, keeping
	// reset from reusing the node ID. Do not nest a read lock: a waiting
	// reset writer would deadlock it. This endpoint never issues a delivery.
	n, err := h.Store.NodeByID(r.Context(), id)
	if err != nil {
		fail(w, http.StatusNotFound, "node not found")
		return
	}
	now := time.Now()
	state, err := h.State.Build(r.Context(), n, now)
	if err != nil {
		serverErr(w, err)
		return
	}
	history, err := h.Store.NodeConfigHistory(r.Context(), id, now)
	if err != nil {
		serverErr(w, err)
		return
	}
	status := "unknown"
	if state.ConfigRevision == "" {
		status = "unavailable"
	} else if current := history.Current; current != nil {
		switch {
		case current.ConfigRevision != state.ConfigRevision || current.StateRevision != state.Revision:
			status = "pending"
		case current.LastReceiptAt == nil:
			status = "pending"
		case !current.Fresh:
			status = "stale"
		case current.Applied:
			status = "applied"
		default:
			status = "pending"
		}
	}
	ok(w, map[string]any{"status": status, "config_revision": state.ConfigRevision, "state_revision": state.Revision, "current": history.Current, "history": history.History})
}
