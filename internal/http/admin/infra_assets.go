package admin

import (
	"errors"
	"github.com/zeptop-dev/captain/internal/store"
	"net/http"
	"strconv"
)

func (h *handlers) registerInfraAssets(m *http.ServeMux) {
	h.registerNodeInfrastructure(m)
	const base = "/api/admin/settings/infrastructure"
	m.HandleFunc("GET "+base, h.requireAdmin(h.listInfrastructure))
	m.HandleFunc("POST "+base+"/suppliers", h.requireAdmin(h.saveInfraSupplier))
	m.HandleFunc("PUT "+base+"/suppliers/{id}", h.requireAdmin(h.saveInfraSupplier))
	m.HandleFunc("DELETE "+base+"/suppliers/{id}", h.requireAdmin(h.deleteInfraSupplier))
	m.HandleFunc("POST "+base+"/assets", h.requireAdmin(h.saveInfraAsset))
	m.HandleFunc("PUT "+base+"/assets/{id}", h.requireAdmin(h.saveInfraAsset))
	m.HandleFunc("DELETE "+base+"/assets/{id}", h.requireAdmin(h.deleteInfraAsset))
	m.HandleFunc("POST "+base+"/assets/{id}/payments", h.requireAdmin(h.recordInfraPayment))
	m.HandleFunc("GET "+base+"/payments", h.requireAdmin(h.infraPayments))
}
func infraError(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, store.ErrNotFound):
		fail(w, 404, "asset, supplier or node not found")
	case errors.Is(e, store.ErrAssetInput):
		fail(w, 400, "invalid infrastructure fields")
	case errors.Is(e, store.ErrAssetConflict):
		fail(w, 409, "asset changed or payment key was reused; refresh before retrying")
	case errors.Is(e, store.ErrAssetHistory):
		fail(w, 409, "record has assets or payment history; archive the asset instead")
	case errors.Is(e, store.ErrInfrastructurePermission):
		fail(w, 403, e.Error())
	case errors.Is(e, store.ErrInfrastructureConversion):
		fail(w, 400, e.Error())
	case errors.Is(e, store.ErrInfrastructureConflict), errors.Is(e, store.ErrInfrastructureLinked):
		fail(w, 409, e.Error())
	default:
		serverErr(w, e)
	}
}
func (h *handlers) listInfrastructure(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sup, e := h.Store.InfraSuppliers(ctx)
	if e != nil {
		serverErr(w, e)
		return
	}
	assets, e := h.Store.InfraAssets(ctx)
	if e != nil {
		serverErr(w, e)
		return
	}
	costs, e := h.Store.InfraCosts(ctx)
	if e != nil {
		serverErr(w, e)
		return
	}
	ok(w, map[string]any{"suppliers": sup, "assets": assets, "paid_totals": costs})
}
func infraID(r *http.Request) (int64, bool) {
	if r.Method == "POST" {
		return 0, true
	}
	return pathID(r)
}
func (h *handlers) saveInfraSupplier(w http.ResponseWriter, r *http.Request) {
	var v store.InfraSupplier
	id, valid := infraID(r)
	if !valid || !decode(r, &v) {
		fail(w, 400, "invalid supplier")
		return
	}
	v.ID = id
	if e := h.Store.SaveInfraSupplier(r.Context(), &v, h.infrastructureWriteAllowed(r, 0)); e != nil {
		infraError(w, e)
		return
	}
	h.invalidateInfrastructureProbe()
	ok(w, v)
}
func (h *handlers) deleteInfraSupplier(w http.ResponseWriter, r *http.Request) {
	if e := h.Store.DeleteInfraSupplier(r.Context(), idOf(r)); e != nil {
		infraError(w, e)
		return
	}
	ok(w, map[string]bool{"ok": true})
}
func (h *handlers) saveInfraAsset(w http.ResponseWriter, r *http.Request) {
	var v store.InfraAsset
	id, valid := infraID(r)
	if !valid || !decode(r, &v) {
		fail(w, 400, "invalid asset")
		return
	}
	v.ID = id
	if e := h.Store.SaveInfraAsset(r.Context(), &v, h.infrastructureWriteAllowed(r, 0)); e != nil {
		infraError(w, e)
		return
	}
	h.invalidateInfrastructureProbe()
	ok(w, v)
}
func (h *handlers) deleteInfraAsset(w http.ResponseWriter, r *http.Request) {
	rev, e := strconv.ParseInt(r.URL.Query().Get("revision"), 10, 64)
	if e != nil || rev < 1 {
		fail(w, 400, "revision is required")
		return
	}
	if e = h.Store.DeleteInfraAsset(r.Context(), idOf(r), rev); e != nil {
		infraError(w, e)
		return
	}
	ok(w, map[string]bool{"ok": true})
}
func (h *handlers) recordInfraPayment(w http.ResponseWriter, r *http.Request) {
	var in store.InfraPaymentInput
	if !decode(r, &in) {
		fail(w, 400, "invalid payment")
		return
	}
	v, e := h.Store.RecordInfraPayment(r.Context(), idOf(r), userFrom(r).ID, in, h.infrastructureWriteAllowed(r, 0))
	if e != nil {
		infraError(w, e)
		return
	}
	h.invalidateInfrastructureProbe()
	ok(w, v)
}
func (h *handlers) infraPayments(w http.ResponseWriter, r *http.Request) {
	var id int64
	var offset int
	var e error
	if raw := r.URL.Query().Get("asset_id"); raw != "" {
		id, e = strconv.ParseInt(raw, 10, 64)
		if e != nil || id < 1 {
			fail(w, 400, "invalid asset id")
			return
		}
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		offset, e = strconv.Atoi(raw)
		if e != nil {
			fail(w, 400, "invalid offset")
			return
		}
	}
	v, total, e := h.Store.InfraPayments(r.Context(), id, offset)
	if e != nil {
		infraError(w, e)
		return
	}
	ok(w, map[string]any{"items": v, "total": total})
}
