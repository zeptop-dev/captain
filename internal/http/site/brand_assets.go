package site

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/branding"
	"github.com/zeptop-dev/captain/internal/store"
)

func registerBrandAssets(mux *http.ServeMux, st *store.Store) {
	mux.HandleFunc("GET /brand-assets/", func(w http.ResponseWriter, r *http.Request) {
		digest, ok := strings.CutSuffix(strings.TrimPrefix(r.URL.Path, branding.AssetPrefix), ".png")
		if !ok || !branding.ValidDigest(digest) {
			http.NotFound(w, r)
			return
		}
		data, err := st.BrandAsset(r.Context(), digest)
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Set("ETag", `"`+digest+`"`)
		http.ServeContent(w, r, digest+".png", time.Time{}, bytes.NewReader(data))
	})
}
