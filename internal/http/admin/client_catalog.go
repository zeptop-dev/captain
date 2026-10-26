package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"

	cc "github.com/zeptop-dev/captain/internal/clientcatalog"
	"github.com/zeptop-dev/captain/internal/store"
)

func (h *handlers) registerClientCatalog(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/settings/client-catalog", h.requireAdmin(h.getClientCatalog))
	mux.HandleFunc("PUT /api/admin/settings/client-catalog/{id}", h.requireAdmin(h.saveClientCatalog))
	mux.HandleFunc("POST /api/admin/settings/client-catalog/{id}/check", h.checkClientCatalog)
	mux.HandleFunc("POST /api/admin/settings/client-catalog/{id}/download", h.downloadClientCatalog)
	mux.HandleFunc("DELETE /api/admin/settings/client-catalog/{id}/cache/{asset}", h.requireAdmin(h.deleteClientCache))
}
func clientCatalogError(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, store.ErrNotFound):
		fail(w, 404, "not found")
	case errors.Is(e, cc.ErrBusy), errors.Is(e, cc.ErrConflict), errors.Is(e, cc.ErrReferenced):
		fail(w, 409, e.Error())
	case errors.Is(e, cc.ErrInvalid), errors.Is(e, cc.ErrMissingCache), errors.Is(e, cc.ErrAlreadyCached):
		fail(w, 400, e.Error())
	default:
		serverErr(w, e)
	}
}
func (h *handlers) getClientCatalog(w http.ResponseWriter, r *http.Request) {
	v, e := h.ClientCatalog.View(r.Context())
	if e != nil {
		serverErr(w, e)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	ok(w, v)
}
func (h *handlers) saveClientCatalog(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Revision  *int64                `json:"revision"`
		Platforms []cc.PlatformSettings `json:"platforms"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Revision == nil {
		fail(w, 400, "revision is required")
		return
	}
	v, e := h.ClientCatalog.Save(r.Context(), r.PathValue("id"), *in.Revision, in.Platforms)
	if e != nil {
		clientCatalogError(w, e)
		return
	}
	ok(w, v)
}
func (h *handlers) deleteClientCache(w http.ResponseWriter, r *http.Request) {
	id, e := strconv.ParseInt(r.PathValue("asset"), 10, 64)
	if e != nil || id <= 0 {
		fail(w, 400, "invalid asset")
		return
	}
	if e = h.ClientCatalog.DeleteCache(r.Context(), r.PathValue("id"), id); e != nil {
		clientCatalogError(w, e)
		return
	}
	ok(w, map[string]bool{"ok": true})
}

func (h *handlers) checkClientCatalog(w http.ResponseWriter, r *http.Request) {
	authorized := false
	incarnation := ""
	h.Store.Accounts.RLock()
	h.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		var e error
		incarnation, e = h.ClientCatalog.BeginCheck(r.Context(), r.PathValue("id"))
		if e != nil {
			clientCatalogError(w, e)
			return
		}
		authorized = true
	})(w, r.WithContext(context.WithValue(r.Context(), subPresetAuthOnlyKey{}, true)))
	h.Store.Accounts.RUnlock()
	if !authorized {
		return
	}
	id := r.PathValue("id")
	defer h.ClientCatalog.EndCheck(id)
	release, archived, fetchErr := h.ClientCatalog.Source.Check(r.Context(), id)
	h.Store.Accounts.Lock()
	defer h.Store.Accounts.Unlock()
	h.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		if e := h.ClientCatalog.CommitCheck(r.Context(), id, incarnation, release, archived, fetchErr); e != nil {
			clientCatalogError(w, e)
			return
		}
		c, e := h.ClientCatalog.Get(r.Context(), id)
		if e != nil {
			serverErr(w, e)
			return
		}
		ok(w, c)
	})(w, r)
}
func (h *handlers) downloadClientCatalog(w http.ResponseWriter, r *http.Request) {
	h.Store.Accounts.RLock()
	defer h.Store.Accounts.RUnlock()
	h.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			AssetID int64 `json:"asset_id"`
		}
		if !readJSON(w, r, &in) {
			return
		}
		job, run, e := h.ClientCatalog.PrepareDownload(r.Context(), r.PathValue("id"), in.AssetID)
		if e != nil {
			clientCatalogError(w, e)
			return
		}
		// Credentials live only in this request clone until the bounded task ends.
		// They are never persisted in the database, job DTO, files, or logs.
		captured := r.Clone(context.WithoutCancel(r.Context()))
		captured.Body = nil
		go run(func(commit func() error) error {
			h.Store.Accounts.Lock()
			defer h.Store.Accounts.Unlock()
			result := errors.New("download authorization expired")
			sink := httptest.NewRecorder()
			h.requireAdmin(func(_ http.ResponseWriter, _ *http.Request) { result = commit() })(sink, captured.WithContext(context.WithValue(captured.Context(), subPresetAuthOnlyKey{}, true)))
			return result
		})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(job)
	})(w, r)
}
