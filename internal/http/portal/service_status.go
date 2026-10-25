package portal

import (
	"net/http"
	"time"
)

func (h *handlers) serviceStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if h.ServiceStatus == nil {
		fail(w, 503, "service status unavailable")
		return
	}
	v, err := h.ServiceStatus.Portal(r.Context(), userFrom(r), time.Now())
	if err != nil {
		h.serverErr(w, "service status", err)
		return
	}
	ok(w, v)
}
