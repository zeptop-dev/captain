// Package sub serves subscription documents at /sub/{token}.
package sub

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/zeptop-dev/captain/internal/service"
	"github.com/zeptop-dev/captain/internal/store"
	"github.com/zeptop-dev/captain/internal/subscription"
)

// Deps are the handler's dependencies.
type Deps struct {
	Store   *store.Store
	Log     *slog.Logger
	Service *service.Subscription
	Name    string // site name used in the download file name
}

// Register mounts the subscription route.
func Register(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("GET /sub/{token}", func(w http.ResponseWriter, r *http.Request) {
		u, err := d.Store.UserBySubToken(r.Context(), r.PathValue("token"))
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		lines, acct, err := d.Service.Lines(r.Context(), u, time.Now())
		if err != nil && !errors.Is(err, service.ErrNoAccess) {
			d.Log.Error("subscription", "user", u.ID, "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		// No access renders an empty document rather than an error so clients
		// keep the subscription and see the usage header.
		rd := subscription.Pick(r.URL.Query().Get("client"), r.UserAgent())
		body, err := rd.Render(lines, acct)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		name := d.Name
		if name == "" {
			name = "captain"
		}
		w.Header().Set("Content-Type", rd.ContentType())
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
		w.Header().Set("Subscription-Userinfo", fmt.Sprintf("upload=%d; download=%d; total=%d; expire=%d", acct.Upload, acct.Download, acct.Total, acct.Expire))
		w.Header().Set("Profile-Update-Interval", "12")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(body)
	})
}
