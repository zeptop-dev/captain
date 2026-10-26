package admin

import (
	"errors"
	"io"
	"net/http"

	"github.com/zeptop-dev/captain/internal/branding"
)

func (h *handlers) uploadBrandAsset(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, branding.MaxInputBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			fail(w, http.StatusRequestEntityTooLarge, "image must be at most 2 MiB")
		} else {
			fail(w, http.StatusBadRequest, "could not read image")
		}
		return
	}
	normalized, err := branding.Normalize(data)
	if err != nil {
		fail(w, http.StatusBadRequest, "image must be PNG, JPEG or WebP, at most 2048 pixels per side and 4 MiB after normalization")
		return
	}
	digest, err := h.Store.SaveBrandAsset(r.Context(), normalized)
	if err != nil {
		serverErr(w, err)
		return
	}
	ok(w, map[string]string{"url": branding.AssetPrefix + digest + ".png"})
}
