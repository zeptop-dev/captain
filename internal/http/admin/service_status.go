package admin

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/zeptop-dev/captain/internal/store"
)

func (h *handlers) registerServiceStatus(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/settings/service-status", h.requireAdmin(h.getServiceStatus))
	mux.HandleFunc("PUT /api/admin/settings/service-status", h.requireAdmin(h.putServiceStatus))
	mux.HandleFunc("GET /api/admin/settings/service-status/preview", h.requireAdmin(h.previewServiceStatus))
}

func (h *handlers) getServiceStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	v, err := h.Store.ServiceStatusConfig(r.Context())
	if err != nil {
		serverErr(w, err)
		return
	}
	ok(w, v)
}

func (h *handlers) putServiceStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var in struct {
		Revision       *int64                      `json:"revision"`
		PublicEnabled  *bool                       `json:"public_enabled"`
		PublicEntryIDs *[]int64                    `json:"public_entry_ids"`
		Maintenance    *[]store.ServiceMaintenance `json:"maintenance"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if dec.Decode(&in) != nil || in.Revision == nil || in.PublicEnabled == nil || in.PublicEntryIDs == nil || in.Maintenance == nil || dec.Decode(new(any)) != io.EOF {
		fail(w, 400, "revision, public_enabled, public_entry_ids and maintenance are required")
		return
	}
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	v, err := h.Store.SaveServiceStatusConfig(r.Context(), store.ServiceStatusConfig{Revision: *in.Revision, PublicEnabled: *in.PublicEnabled, PublicEntryIDs: *in.PublicEntryIDs, Maintenance: *in.Maintenance}, time.Now())
	switch {
	case errors.Is(err, store.ErrServiceStatusConflict), errors.Is(err, store.ErrServiceStatusPathConflict):
		fail(w, 409, err.Error())
	case errors.Is(err, store.ErrServiceStatusInvalid):
		fail(w, 400, store.ErrServiceStatusInvalid.Error())
	case err != nil:
		serverErr(w, err)
	default:
		ok(w, v)
	}
}

func (h *handlers) previewServiceStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if h.ServiceStatus == nil {
		fail(w, 503, "service status unavailable")
		return
	}
	v, err := h.ServiceStatus.Admin(r.Context(), time.Now())
	if err != nil {
		serverErr(w, err)
		return
	}
	ok(w, v)
}
