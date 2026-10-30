package admin

import (
	"net/http"
	"strconv"
	"time"
)

func (h *handlers) listLineChanges(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	query := r.URL.Query()
	parse := func(key string, fallback int64) (int64, error) {
		raw := query.Get(key)
		if raw == "" {
			return fallback, nil
		}
		return strconv.ParseInt(raw, 10, 64)
	}
	lineID, err := parse("line_id", 0)
	if err != nil || lineID < 0 {
		fail(w, 400, "invalid line id")
		return
	}
	before, err := parse("before", 0)
	if err != nil || before < 0 {
		fail(w, 400, "invalid cursor")
		return
	}
	limit, err := parse("limit", 20)
	if err != nil || limit < 1 || limit > 100 {
		fail(w, 400, "limit must be between 1 and 100")
		return
	}
	page, err := h.Store.ListLineChanges(r.Context(), lineID, before, int(limit))
	if err != nil {
		serverErr(w, err)
		return
	}
	ok(w, page)
}

func (h *handlers) getLineChange(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := r.PathValue("change")
	if len(id) == 0 || len(id) > 128 {
		fail(w, 400, "invalid change id")
		return
	}
	change, err := h.Store.LineChangeByID(r.Context(), id, time.Now())
	if err != nil {
		serverErr(w, err)
		return
	}
	ok(w, change)
}
