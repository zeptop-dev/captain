package admin

import (
	"github.com/zeptop-dev/captain/internal/store"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (h *handlers) getNodeRenderedConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id, valid := pathID(r)
	if !valid {
		fail(w, 400, "invalid node id")
		return
	}
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer"))
	if strings.HasPrefix(token, "cap_") {
		user, policy, err := h.Store.UserByAPIToken(r.Context(), token)
		if err != nil || user == nil || !user.IsAdmin() || user.Status != "active" || !store.TokenAllowsRequest(policy, "GET", "/api/admin/nodes/"+strconv.FormatInt(id, 10), "GET /api/admin/nodes/{id}") {
			fail(w, 403, "node read permission required")
			return
		}
	}
	n, err := h.Store.NodeByID(r.Context(), id)
	if err != nil {
		fail(w, 404, "node not found")
		return
	}
	now := time.Now()
	state, err := h.State.Build(r.Context(), n, now)
	if err != nil {
		serverErr(w, err)
		return
	}
	history, err := h.Store.NodeRenderedConfigHistory(r.Context(), id, now)
	if err != nil {
		serverErr(w, err)
		return
	}
	status := "unknown"
	if state.ConfigRevision == "" {
		status = "unavailable"
	} else if current := history.Current; current != nil {
		switch {
		case current.ConfigRevision != state.ConfigRevision || current.StateRevision != state.Revision || current.LastReceiptAt == nil:
			status = "pending"
		case !current.Fresh:
			status = "stale"
		case !current.Applied:
			status = "pending"
		case current.MetadataStatus != "valid" || current.Evidence == nil || current.Evidence.Status != "observed":
			status = "unavailable"
		default:
			status = "observed"
		}
	}
	ok(w, map[string]any{"status": status, "config_revision": state.ConfigRevision, "state_revision": state.Revision, "current": history.Current, "history": history.History})
}
