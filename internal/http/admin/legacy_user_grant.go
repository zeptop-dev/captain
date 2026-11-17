package admin

import (
	"errors"
	"net/http"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

func (h *handlers) registerLegacyUserGrant(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/users/{id}/legacy-grant", h.requireAdmin(h.getLegacyUserGrant))
	mux.HandleFunc("POST /api/admin/users/{id}/legacy-grant/migrate", h.requireAdmin(h.migrateLegacyUserGrant))
}
func legacyGrantReader(w http.ResponseWriter, r *http.Request) bool {
	role := userFrom(r).Role
	if role != domain.RoleAdmin && role != domain.RoleOperator {
		fail(w, 403, "your role cannot do that")
		return false
	}
	return true
}
func legacyGrantError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		fail(w, 404, "user not found")
	case errors.Is(err, store.ErrLegacyGrantConflict), errors.Is(err, store.ErrLegacyGrantNeedsPlan):
		fail(w, 409, err.Error())
	default:
		serverErr(w, err)
	}
}
func (h *handlers) getLegacyUserGrant(w http.ResponseWriter, r *http.Request) {
	if !legacyGrantReader(w, r) {
		return
	}
	id, valid := pathID(r)
	if !valid {
		fail(w, 404, "user not found")
		return
	}
	v, err := h.Store.LegacyUserGrant(r.Context(), id, time.Now())
	if err != nil {
		legacyGrantError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	ok(w, v)
}
func (h *handlers) migrateLegacyUserGrant(w http.ResponseWriter, r *http.Request) {
	if !legacyGrantReader(w, r) {
		return
	}
	id, valid := pathID(r)
	if !valid {
		fail(w, 404, "user not found")
		return
	}
	var in struct {
		Revision string `json:"revision"`
		Confirm  bool   `json:"confirm"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if !in.Confirm || in.Revision == "" {
		fail(w, 400, "confirm a current migration preview")
		return
	}
	if err := h.Store.MigrateLegacyUserGrant(r.Context(), id, in.Revision, time.Now()); err != nil {
		legacyGrantError(w, err)
		return
	}
	ok(w, map[string]bool{"ok": true})
}
