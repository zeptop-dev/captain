package probetheme

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"html"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

// ServePage must only be reached on an isolated probe origin, after checking
// the probe's visibility policy. It deliberately never falls back to another
// package, and cannot serve Captain API, administration, or terminal routes.
func (m *Manager) ServePage(w http.ResponseWriter, r *http.Request, title, description string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	p, err := m.Active(r.Context())
	if err != nil {
		http.Error(w, "theme unavailable", http.StatusServiceUnavailable)
		return
	}
	if p == nil {
		http.NotFound(w, r)
		return
	}
	p.ServePage(w, r, title, description)
}
func (m *Manager) ServeHTTP(w http.ResponseWriter, r *http.Request) { m.ServePage(w, r, "", "") }
func (p *Package) ServePage(w http.ResponseWriter, r *http.Request, title, description string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	requestPath := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if requestPath != "" && !safePath(requestPath) {
		http.NotFound(w, r)
		return
	}
	first := strings.Split(requestPath, "/")[0]
	switch first {
	case "api", "admin", "portal", "terminal", "sub", ".well-known":
		http.NotFound(w, r)
		return
	}
	file := "dist/" + requestPath
	if strings.HasPrefix(requestPath, "themes/") {
		prefix := "themes/" + p.Manifest.Short + "/"
		if !strings.HasPrefix(requestPath, prefix) {
			http.NotFound(w, r)
			return
		}
		file = strings.TrimPrefix(requestPath, prefix)
		if file == "komari-theme.json" {
			http.NotFound(w, r)
			return
		}
	} else if requestPath == "" {
		file = "dist/index.html"
	}
	_, exists := p.files[file]
	if !exists {
		if strings.HasPrefix(requestPath, "themes/") || first == "assets" || first == "_next" || path.Ext(requestPath) != "" {
			http.NotFound(w, r)
			return
		}
		// Next static exports may have per-route HTML; ordinary Vue/React routes
		// use the documented index.html fallback instead.
		if _, ok := p.files[file+".html"]; ok {
			file += ".html"
		} else if _, ok = p.files[path.Join(file, "index.html")]; ok {
			file = path.Join(file, "index.html")
		} else {
			file = "dist/index.html"
		}
	}
	data, err := p.read(file, maxFileBytes)
	if err != nil {
		http.Error(w, "theme file unavailable", http.StatusServiceUnavailable)
		return
	}
	extension := strings.ToLower(path.Ext(file))
	contentType := mime.TypeByExtension(extension)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if extension == ".html" {
		if title != "" {
			data = bytes.ReplaceAll(data, []byte("<title>Komari Monitor</title>"), []byte("<title>"+html.EscapeString(title)+"</title>"))
		}
		data = bytes.ReplaceAll(data, []byte("A simple server monitor tool."), []byte(html.EscapeString(description)))
		// Do not inject operator custom HTML or theme administrator interfaces.
		w.Header().Set("Cache-Control", "no-store")
	} else {
		w.Header().Set("Cache-Control", "private, no-cache")
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'; object-src 'none'; base-uri 'self'; form-action 'self'")
	fileHash := sha256.Sum256(data)
	w.Header().Set("ETag", fmt.Sprintf(`"%s-%x"`, p.Digest, fileHash[:8]))
	http.ServeContent(w, r, path.Base(file), time.Time{}, bytes.NewReader(data))
}
