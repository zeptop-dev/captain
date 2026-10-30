package admin

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/store"
)

func (h *handlers) getTrafficHistory(w http.ResponseWriter, r *http.Request) {
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
	parse := func(key string, fallback int64) (int64, error) {
		if raw := r.URL.Query().Get(key); raw != "" {
			return strconv.ParseInt(raw, 10, 64)
		}
		return fallback, nil
	}
	before, err := parse("before", 0)
	if err != nil || before < 0 {
		fail(w, 400, "invalid cursor")
		return
	}
	limit, err := parse("limit", 50)
	if err != nil || limit < 1 || limit > 100 {
		fail(w, 400, "limit must be between 1 and 100")
		return
	}
	page, err := h.Store.NodeTrafficHistory(r.Context(), id, before, int(limit), time.Now())
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "node not found")
		return
	}
	if err != nil {
		serverErr(w, err)
		return
	}
	ok(w, page)
}
