package admin

import (
	"errors"
	"net/http"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/service"
	"github.com/zeptop-dev/captain/internal/store"
)

func (h *handlers) putEntryLocation(w http.ResponseWriter, r *http.Request) {
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	id, valid := pathID(r)
	if !valid {
		fail(w, 400, "invalid entry id")
		return
	}
	var cfg domain.LocationSettings
	if !readJSON(w, r, &cfg) {
		return
	}
	// Explicit mode avoids treating an accidentally empty request as clearing
	// an administrator's correction. auto is the explicit reset to inheritance.
	if cfg.Mode == "" {
		fail(w, 400, "location mode is required")
		return
	}
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		fail(w, 400, "invalid map location")
		return
	}
	if err := h.Store.SetEntryLocation(r.Context(), id, cfg); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			fail(w, 404, "entry not found")
			return
		}
		serverErr(w, err)
		return
	}
	e, err := h.Store.EntryByID(r.Context(), id)
	if err != nil {
		serverErr(w, err)
		return
	}
	locations, err := service.EntryMapLocations(r.Context(), h.Store, []*domain.Entry{e})
	if err != nil {
		serverErr(w, err)
		return
	}
	ok(w, map[string]any{"LocationSettings": cfg, "Location": locations[id]})
}
