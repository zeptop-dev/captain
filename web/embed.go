// Package web embeds the built frontends. Run `make web` (or the CI) before
// `go build`; without a build the admin path serves a short notice.
package web

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

//go:embed all:admin/dist
var adminFS embed.FS

//go:embed all:portal/dist
var portalFS embed.FS

//go:embed all:site/dist
var siteFS embed.FS

//go:embed all:probe/dist
var probeFS embed.FS

// Probe serves the status page SPA; the caller mounts it at "/" of a
// dedicated host or strips its path prefix first.
func Probe() http.Handler { return spa(probeFS, "probe/dist", "/", "status page") }

// Injector returns operator HTML to add before </head> and </body>.
type Injector func(r *http.Request) (head, body string)

// Inject is consulted by every SPA index; nil injects nothing.
var Inject Injector

// Site serves the landing page at "/". When overrideDir contains an
// index.html it is served instead of the built-in page, so operators can
// drop in their own design without rebuilding.
func Site(overrideDir string) http.Handler {
	builtin := spa(siteFS, "site/dist", "/", "site")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if overrideDir != "" {
			if st, err := os.Stat(filepath.Join(overrideDir, "index.html")); err == nil && !st.IsDir() {
				http.FileServer(http.Dir(overrideDir)).ServeHTTP(w, r)
				return
			}
		}
		builtin.ServeHTTP(w, r)
	})
}

// Admin serves the admin SPA under prefix (e.g. "/admin/").
func Admin(prefix string) http.Handler { return spa(adminFS, "admin/dist", prefix, "admin console") }

// Portal serves the user portal SPA under prefix (e.g. "/portal/").
func Portal(prefix string) http.Handler { return spa(portalFS, "portal/dist", prefix, "portal") }

// spa serves a built single-page app with history fallback to index.html.
func spa(root embed.FS, dir, prefix, name string) http.Handler {
	sub, _ := fs.Sub(root, dir)
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
			http.Error(w, name+" not built: run `make web`", http.StatusNotFound)
			return
		}
		if Inject != nil {
			if head, body := Inject(r); head != "" || body != "" {
				index = bytes.Replace(index, []byte("</head>"), []byte(head+"</head>"), 1)
				index = bytes.Replace(index, []byte("</body>"), []byte(body+"</body>"), 1)
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(index))
	})
}
