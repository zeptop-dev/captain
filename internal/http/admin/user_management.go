package admin

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/store"
)

func (h *handlers) registerUserManagement(mux *http.ServeMux) {
	h.registerLegacyUserGrant(mux)
	mux.HandleFunc("POST /api/admin/users/bulk/preview", h.requireAdmin(h.previewUserBulk))
	mux.HandleFunc("POST /api/admin/users/bulk/jobs/{job}", h.requireAdmin(h.executeUserBulk))
	mux.HandleFunc("GET /api/admin/users/bulk/jobs/{job}", h.requireAdmin(h.getUserBulk))
	for _, kind := range []string{"users", "nodes"} {
		mux.HandleFunc("GET /api/admin/"+kind+"/{id}/metadata", h.requireAdmin(h.getMetadata))
		mux.HandleFunc("PUT /api/admin/"+kind+"/{id}/metadata", h.requireAdmin(h.putMetadata))
	}
}
func userFilter(r *http.Request) (store.UserFilter, int, bool) {
	q := r.URL.Query()
	f := store.UserFilter{Query: q.Get("q"), Status: q.Get("status"), Access: q.Get("access"), Sort: q.Get("sort"), Desc: q.Get("direction") != "asc"}
	per := 50
	parse := func(key string, dst *int64) bool {
		if q.Get(key) == "" {
			return true
		}
		n, err := strconv.ParseInt(q.Get(key), 10, 64)
		*dst = n
		return err == nil
	}
	if !parse("plan_id", &f.PlanID) || !parse("expires_from", &f.ExpiresFrom) || !parse("expires_to", &f.ExpiresTo) {
		return f, per, false
	}
	if q.Get("group_id") != "" {
		var group int64
		if !parse("group_id", &group) {
			return f, per, false
		}
		f.GroupID = &group
	}
	if q.Get("per_page") != "" {
		n, err := strconv.Atoi(q.Get("per_page"))
		if err != nil || (n != 25 && n != 50 && n != 100) {
			return f, per, false
		}
		per = n
	}
	if d := q.Get("direction"); d != "" && d != "asc" && d != "desc" {
		return f, per, false
	}
	return f, per, f.Validate() == nil
}
func bulkError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		fail(w, 404, "bulk operation not found")
	case errors.Is(err, store.ErrBulkRequest):
		fail(w, 409, "invalid or expired bulk operation; create a new preview")
	default:
		serverErr(w, err)
	}
}
func (h *handlers) previewUserBulk(w http.ResponseWriter, r *http.Request) {
	var req store.UserBulkRequest
	if !decode(r, &req) || req.Validate() != nil {
		fail(w, 400, "select 1–200 distinct users and a valid action")
		return
	}
	job, err := h.Store.PreviewUserBulk(r.Context(), userFrom(r).ID, req, time.Now())
	if err != nil {
		bulkError(w, err)
		return
	}
	ok(w, job)
}
func (h *handlers) executeUserBulk(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Confirm bool `json:"confirm"`
	}
	if !decode(r, &req) || !req.Confirm {
		fail(w, 400, "confirm the preview before execution")
		return
	}
	job, err := h.Store.ExecuteUserBulk(r.Context(), userFrom(r).ID, r.PathValue("job"), time.Now())
	if err != nil {
		bulkError(w, err)
		return
	}
	ok(w, job)
}
func (h *handlers) getUserBulk(w http.ResponseWriter, r *http.Request) {
	job, err := h.Store.UserBulk(r.Context(), userFrom(r).ID, r.PathValue("job"))
	if err != nil {
		bulkError(w, err)
		return
	}
	ok(w, job)
}
func metadataKind(r *http.Request) string {
	return strings.Split(strings.TrimPrefix(r.URL.Path, "/api/admin/"), "/")[0]
}
func (h *handlers) getMetadata(w http.ResponseWriter, r *http.Request) {
	id, valid := pathID(r)
	if !valid {
		fail(w, 404, "not found")
		return
	}
	m, err := h.Store.Metadata(r.Context(), metadataKind(r), id)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, 404, "not found")
		return
	}
	if err != nil {
		serverErr(w, err)
		return
	}
	ok(w, m)
}
func (h *handlers) putMetadata(w http.ResponseWriter, r *http.Request) {
	id, valid := pathID(r)
	if !valid {
		fail(w, 404, "not found")
		return
	}
	var m map[string]string
	if !decode(r, &m) || store.ValidateMetadata(m) != nil {
		fail(w, 400, store.ErrMetadata.Error())
		return
	}
	err := h.Store.SetMetadata(r.Context(), metadataKind(r), id, m)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, 404, "not found")
		return
	}
	if err != nil {
		serverErr(w, err)
		return
	}
	ok(w, map[string]bool{"ok": true})
}
