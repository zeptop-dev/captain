// Package web embeds the built frontends. Run `make web` (or the CI) before
// `go build`; without a build the admin path serves a short notice.
package web

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:embed all:admin/dist
var adminFS embed.FS

// Admin serves the admin SPA under prefix (e.g. "/admin/") with history
// fallback to index.html.
func Admin(prefix string) http.Handler {
	sub, _ := fs.Sub(adminFS, "admin/dist")
	files := http.FS(sub)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, prefix)
		if p == "" || p == "/" {
			p = "index.html"
		}
		if f, err := sub.Open(path.Clean(p)); err == nil && p != "index.html" {
			f.Close()
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/" + p
			if strings.HasPrefix(p, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			http.FileServer(files).ServeHTTP(w, r2)
			return
		}
		index, err := fs.ReadFile(sub, "index.html")
		if err != nil {
			http.Error(w, "admin console not built: run `make web`", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(index))
	})
}
