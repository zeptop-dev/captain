package admin

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/zeptop-dev/captain/internal/store"
)

// Caller holds Topology across this check and the mutation. Shared by direct
// edits, staged plan creation and compensation of a historical route intent.
func (h *handlers) requirePathHealthMutation(w http.ResponseWriter, r *http.Request, before, next *store.PathRoute) bool {
	changed, err := store.PathRouteHealthChanged(before, next)
	if err != nil {
		fail(w, http.StatusBadRequest, "invalid path health policy")
		return false
	}
	if !changed {
		return true
	}
	if !userFrom(r).IsAdmin() {
		fail(w, http.StatusForbidden, "path health write permission required")
		return false
	}
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer"))
	if !strings.HasPrefix(token, "cap_") {
		return true
	}
	id := int64(0)
	if before != nil {
		id = before.ID
	} else if next != nil {
		id = next.ID
	}
	u, scope, err := h.Store.UserByAPIToken(r.Context(), token)
	allowed := err == nil && u != nil && u.IsAdmin() && u.Status == "active" && store.TokenAllowsRequest(scope, http.MethodPut, "/api/admin/path-routes/"+strconv.FormatInt(id, 10)+"/health", "PUT /api/admin/path-routes/{id}/health")
	if !allowed {
		fail(w, http.StatusForbidden, "path health write permission required")
	}
	return allowed
}
