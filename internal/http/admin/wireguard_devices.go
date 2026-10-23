package admin

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/service"
	"github.com/zeptop-dev/captain/internal/store"
)

func (h *handlers) registerWireGuardDevices(mux *http.ServeMux) {
	const base = "/api/admin/users/{id}/wireguard-devices"
	mux.HandleFunc("GET "+base, h.requireAdmin(h.wireGuardDevices))
	mux.HandleFunc("POST "+base, h.requireAdmin(h.wireGuardDevices))
	mux.HandleFunc("PATCH "+base+"/{device}", h.requireAdmin(h.wireGuardDevices))
	mux.HandleFunc("DELETE "+base+"/{device}", h.requireAdmin(h.wireGuardDevices))
}

func (h *handlers) wireGuardDevices(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	u, okUser := h.customer(r)
	if !okUser {
		fail(w, 404, "user not found")
		return
	}
	canManage := userFrom(r).Role == domain.RoleAdmin || userFrom(r).Role == domain.RoleOperator
	svc := &service.Subscription{Store: h.Store}
	urlFor := func(token string) string { return h.subURL(r.Context(), token) }
	if !canManage {
		urlFor = nil
	}
	if r.Method == http.MethodGet {
		view, err := svc.WireGuardDevices(r.Context(), u, urlFor)
		if err != nil {
			serverErr(w, err)
			return
		}
		ok(w, view)
		return
	}
	if !canManage {
		fail(w, http.StatusForbidden, "admin or operator required")
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if r.Method != http.MethodDelete && !readJSON(w, r, &in) {
		return
	}
	var d *store.WireGuardDevice
	var err error
	if r.Method == http.MethodPost {
		view, e := svc.WireGuardDevices(r.Context(), u, nil)
		if e != nil {
			serverErr(w, e)
			return
		}
		if !view.Available {
			fail(w, http.StatusConflict, view.Reason)
			return
		}
		d, err = svc.CreateWireGuardDevice(r.Context(), u, in.Name)
	} else {
		id, e := strconv.ParseInt(r.PathValue("device"), 10, 64)
		if e != nil || id <= 0 {
			fail(w, 400, "invalid device id")
			return
		}
		if r.Method == http.MethodDelete {
			err = h.Store.DeleteWireGuardDevice(r.Context(), u.ID, id)
		} else {
			d, err = h.Store.RenameWireGuardDevice(r.Context(), u.ID, id, in.Name)
		}
	}
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			fail(w, 404, "device not found")
		case errors.Is(err, store.ErrWireGuardDeviceName):
			fail(w, 400, err.Error())
		case errors.Is(err, store.ErrWireGuardDeviceLimit), errors.Is(err, store.ErrWireGuardDevicePool):
			fail(w, 409, err.Error())
		default:
			serverErr(w, err)
		}
		return
	}
	if d == nil {
		ok(w, map[string]bool{"ok": true})
		return
	}
	ok(w, service.WireGuardDeviceView{WireGuardDevice: *d, SubscriptionURL: urlFor(d.Token)})
}
