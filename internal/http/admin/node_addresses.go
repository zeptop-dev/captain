package admin

import (
	"net/http"
	"time"
)

func (h *handlers) registerNodeAddresses(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/nodes/{id}/addresses", func(w http.ResponseWriter, r *http.Request) {
		out, err := h.Store.NodeAddresses(r.Context(), idOf(r))
		if err != nil {
			fail(w, 404, "node not found")
			return
		}
		ok(w, out)
	})
	mux.HandleFunc("PUT /api/admin/settings/nodes/{id}/addresses", h.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			FallbackAutoIPv4 *bool `json:"fallback_auto_ipv4"`
			FallbackAutoIPv6 *bool `json:"fallback_auto_ipv6"`
			AutoIPv4         *bool `json:"auto_ipv4"`
			AutoIPv6         *bool `json:"auto_ipv6"`
		}
		if !readJSON(w, r, &in) {
			return
		}
		h.Store.Topology.Lock()
		defer h.Store.Topology.Unlock()
		out, err := h.Store.NodeAddresses(r.Context(), idOf(r))
		if err != nil {
			fail(w, 404, "node not found")
			return
		}
		v4, v6 := out.AutoIPv4, out.AutoIPv6
		if in.AutoIPv4 != nil {
			v4 = *in.AutoIPv4
		}
		if in.AutoIPv6 != nil {
			v6 = *in.AutoIPv6
		}
		if (v4 && !out.AutoIPv4 || v6 && !out.AutoIPv6) && out.Observed.ReceivedAt < time.Now().Add(-15*time.Minute).Unix() {
			fail(w, 409, "wait for a fresh public address report from an updated bosun")
			return
		}
		if err = h.Store.SetAddressFollowPolicy(r.Context(), idOf(r), v4, v6, in.FallbackAutoIPv4, in.FallbackAutoIPv6); err != nil {
			serverErr(w, err)
			return
		}
		out, err = h.Store.NodeAddresses(r.Context(), idOf(r))
		if err != nil {
			serverErr(w, err)
			return
		}
		ok(w, out)
	}))
}
