package http

import (
	"fmt"
	"mime"
	"net/http"
	"strconv"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/http/admin"
	"github.com/zeptop-dev/captain/internal/service"
)

func (s *Server) registerClientDownloads(m *service.ClientCatalog, sessions admin.SessionStore) {
	s.mux.HandleFunc("GET /downloads/clients/{client}/{asset}/{filename}", func(w http.ResponseWriter, r *http.Request) {
		// Open the authorized immutable file while IDs and publications are stable,
		// then release Accounts before streaming a potentially large range response.
		s.store.Accounts.RLock()
		portal := false
		if c, e := r.Cookie("captain_session"); e == nil {
			if u, staff, e := sessions.Resolve(r.Context(), c.Value); e == nil && u != nil && !staff && u.Role == domain.RoleUser && u.Status == "active" {
				portal = true
			}
		}
		id, e := strconv.ParseInt(r.PathValue("asset"), 10, 64)
		if e != nil || id <= 0 {
			s.store.Accounts.RUnlock()
			http.NotFound(w, r)
			return
		}
		f, a, e := m.DownloadAuthorization(r.Context(), r.PathValue("client"), id, r.PathValue("filename"), portal)
		s.store.Accounts.RUnlock()
		if e != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": a.Name}))
		// No shared cache may outlive withdrawal or a portal-session revocation.
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("ETag", fmt.Sprintf(`"sha256-%s"`, a.SHA256))
		http.ServeContent(w, r, a.Name, a.CachedAt, f)
	})
}
