package admin

import (
	"errors"
	"net/http"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

func entryCategoryError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, store.ErrEntryCategoryInvalid):
		fail(w, 400, err.Error())
	case errors.Is(err, store.ErrEntryCategoryConflict):
		fail(w, 409, err.Error())
	case errors.Is(err, store.ErrNotFound):
		fail(w, 404, "category not found")
	default:
		return false
	}
	return true
}

func (h *handlers) listEntryCategories(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.ListEntryCategories(r.Context())
	if err != nil {
		serverErr(w, err)
		return
	}
	ok(w, list)
}

func (h *handlers) createEntryCategory(w http.ResponseWriter, r *http.Request) {
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	var c domain.EntryCategory
	if !decode(r, &c) {
		fail(w, 400, "invalid category")
		return
	}
	if err := h.Store.CreateEntryCategory(r.Context(), &c); err != nil {
		if !entryCategoryError(w, err) {
			serverErr(w, err)
		}
		return
	}
	ok(w, c)
}

func (h *handlers) updateEntryCategory(w http.ResponseWriter, r *http.Request) {
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	id, valid := pathID(r)
	if !valid {
		fail(w, 400, "bad id")
		return
	}
	c, err := h.Store.EntryCategoryByID(r.Context(), id)
	if err != nil {
		if !entryCategoryError(w, err) {
			serverErr(w, err)
		}
		return
	}
	if !decode(r, c) {
		fail(w, 400, "invalid category")
		return
	}
	c.ID = id
	if err := h.Store.UpdateEntryCategory(r.Context(), c); err != nil {
		if !entryCategoryError(w, err) {
			serverErr(w, err)
		}
		return
	}
	ok(w, c)
}

func (h *handlers) deleteEntryCategory(w http.ResponseWriter, r *http.Request) {
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	id, valid := pathID(r)
	if !valid {
		fail(w, 400, "bad id")
		return
	}
	if err := h.Store.DeleteEntryCategory(r.Context(), id); err != nil {
		if !entryCategoryError(w, err) {
			serverErr(w, err)
		}
		return
	}
	ok(w, map[string]bool{"ok": true})
}

func (h *handlers) reorderEntryCategories(w http.ResponseWriter, r *http.Request) {
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	var in struct{ IDs []int64 }
	if !decode(r, &in) || in.IDs == nil {
		fail(w, 400, "IDs required")
		return
	}
	if err := h.Store.ReorderEntryCategories(r.Context(), in.IDs); err != nil {
		if !entryCategoryError(w, err) {
			serverErr(w, err)
		}
		return
	}
	ok(w, map[string]bool{"ok": true})
}

func (h *handlers) assignEntryCategory(w http.ResponseWriter, r *http.Request) {
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	var in struct {
		IDs        []int64
		CategoryID *int64
	}
	if !decode(r, &in) || in.CategoryID == nil {
		fail(w, 400, "CategoryID is required; use 0 for unclassified")
		return
	}
	if err := h.Store.AssignEntryCategory(r.Context(), in.IDs, *in.CategoryID); err != nil {
		if !entryCategoryError(w, err) {
			serverErr(w, err)
		}
		return
	}
	ok(w, map[string]bool{"ok": true})
}
