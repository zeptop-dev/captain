package site

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/zeptop-dev/captain/internal/service"
)

func registerServiceStatus(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("GET /api/site/service-status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if d.ServiceStatus == nil {
			http.NotFound(w, r)
			return
		}
		v, err := d.ServiceStatus.Public(r.Context(), time.Now())
		if errors.Is(err, service.ErrServiceStatusPrivate) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			slog.ErrorContext(r.Context(), "service status", "err", err)
			http.Error(w, "internal error", 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	})
}
