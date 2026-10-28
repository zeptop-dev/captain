package web

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

// ProbeThemeAssets exposes the shared icon paths expected by Komari themes.
// It reuses the already bundled Glassmorphism images and their existing licence
// notices in web/probe/glass/{LICENSE,ICON-LICENSES.json,licenses}; no upstream
// assets are downloaded or added. An original neutral globe handles the empty
// region paths used by themes without claiming a country. Mount only behind
// the isolated probe origin/page gate.
// Callers must first prefer a real asset supplied by the active theme package.
func ProbeThemeAssets() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path == "/images/flags/.svg" || r.URL.Path == "/assets/flags/.svg" {
			serveProbeThemeImage(w, r, "unknown-region.svg", []byte(unknownRegionSVG))
			return
		}
		parts := strings.Split(r.URL.Path, "/")
		if len(parts) != 4 || parts[0] != "" || parts[1] != "assets" || (parts[2] != "flags" && parts[2] != "logo") || !fs.ValidPath(parts[3]) || strings.ContainsAny(parts[3], `\%`) {
			http.NotFound(w, r)
			return
		}
		extension := strings.ToLower(path.Ext(parts[3]))
		switch extension {
		case ".svg", ".png", ".webp", ".jpg", ".jpeg", ".gif", ".avif":
		default:
			http.NotFound(w, r)
			return
		}
		data, err := fs.ReadFile(probeFS, "probe/dist/glass/images/"+parts[2]+"/"+parts[3])
		if err != nil {
			http.NotFound(w, r)
			return
		}
		serveProbeThemeImage(w, r, parts[3], data)
	})
}

func serveProbeThemeImage(w http.ResponseWriter, r *http.Request, filename string, data []byte) {
	w.Header().Set("Content-Type", mime.TypeByExtension(strings.ToLower(path.Ext(filename))))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("ETag", fmt.Sprintf(`"%x"`, sha256.Sum256(data)))
	http.ServeContent(w, r, filename, time.Time{}, bytes.NewReader(data))
}

// This geometric icon is Captain artwork, not a country or an upstream asset.
const unknownRegionSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#94a3b8" stroke-width="1.5"><title>Unknown region</title><circle cx="12" cy="12" r="9"/><ellipse cx="12" cy="12" rx="4.5" ry="9"/><path d="M3 12h18"/></svg>`
