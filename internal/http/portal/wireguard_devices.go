package portal

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/zeptop-dev/captain/internal/service"
	"github.com/zeptop-dev/captain/internal/store"
)

func (h *handlers) registerWireGuardDevices(mux *http.ServeMux) {
	const base = "/api/portal/me/wireguard-devices"
	mux.HandleFunc("GET "+base, h.requireUser(h.wireGuardDevices))
	mux.HandleFunc("POST "+base, h.requireUser(h.wireGuardDevices))
	mux.HandleFunc("PATCH "+base+"/{device}", h.requireUser(h.wireGuardDevices))
	mux.HandleFunc("DELETE "+base+"/{device}", h.requireUser(h.wireGuardDevices))
}

func (h *handlers) wireGuardDevices(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	svc := &service.Subscription{Store: h.Store}
	urlFor := func(token string) string { return h.subURL(r.Context(), token) }
	if r.Method == http.MethodGet {
		view, err := svc.WireGuardDevices(r.Context(), u, urlFor)
		if err != nil {
			fail(w, 500, "internal error")
			return
		}
		ok(w, view)
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if r.Method != http.MethodDelete {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil {
			fail(w, 400, "bad json")
			return
		}
	}
	var d *store.WireGuardDevice
	var err error
	if r.Method == http.MethodPost {
		view, e := svc.WireGuardDevices(r.Context(), u, nil)
		if e != nil {
			fail(w, 500, "internal error")
			return
		}
		if !view.Available {
			fail(w, 409, view.Reason)
			return
		}
		d, err = h.Store.CreateWireGuardDevice(r.Context(), u.ID, in.Name, view.Limit)
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
			fail(w, 500, "internal error")
		}
		return
	}
	if d == nil {
		ok(w, map[string]bool{"ok": true})
		return
	}
	ok(w, service.WireGuardDeviceView{WireGuardDevice: *d, SubscriptionURL: urlFor(d.Token)})
}
