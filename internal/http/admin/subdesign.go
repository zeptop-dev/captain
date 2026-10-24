package admin

import (
	"encoding/json"
	"net/http"

	"github.com/zeptop-dev/bosun/pkg/subdesign"
	"github.com/zeptop-dev/captain/internal/store"
)

func (h *handlers) registerSubDesign(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/settings/sub-design/claude-rules", h.requireAdmin(h.getClaudeRules))
	// Sync handles its own account locks around remote I/O.
	mux.HandleFunc("POST /api/admin/settings/sub-design/claude-rules/sync", h.syncClaudeRules)
	mux.HandleFunc("GET /api/admin/settings/sub-design", h.requireAdmin(h.getSubDesign))
	mux.HandleFunc("PUT /api/admin/settings/sub-design", h.requireAdmin(h.putSubDesign))
	mux.HandleFunc("POST /api/admin/settings/sub-design/inspect", h.requireAdmin(h.inspectSubDesign))
	mux.HandleFunc("POST /api/admin/settings/sub-design/preview", h.requireAdmin(h.previewSubDesign))
	mux.HandleFunc("POST /api/admin/settings/sub-design/apply", h.requireAdmin(h.applySubDesign))
	mux.HandleFunc("GET /api/admin/settings/sub-design/presets/{key}", h.requireAdmin(h.subDesignPreset))
	// This route owns its account locks; network I/O must not hold them.
	mux.HandleFunc("POST /api/admin/settings/sub-design/presets/sync", h.syncSubDesignPresets)
	mux.HandleFunc("POST /api/admin/settings/sub-design/presets/custom", h.requireAdmin(h.saveSubCustomPreset))
	mux.HandleFunc("DELETE /api/admin/settings/sub-design/presets/{key}", h.requireAdmin(h.deleteSubCustomPreset))
}

func (h *handlers) getSubDesign(w http.ResponseWriter, r *http.Request) {
	var d subdesign.Design
	if err := h.Store.GetSetting(r.Context(), store.SettingSubDesign, &d); err != nil {
		serverErr(w, err)
		return
	}
	if d.Groups == nil {
		d.Groups = []subdesign.Group{}
	}
	if d.Rules == nil {
		d.Rules = []subdesign.Rule{}
	}
	tags, err := h.Store.EntryTags(r.Context())
	if err != nil {
		serverErr(w, err)
		return
	}
	if tags == nil {
		tags = []string{}
	}
	library, presets, err := h.subPresetCatalogue(r.Context())
	if err != nil {
		serverErr(w, err)
		return
	}
	ok(w, map[string]any{"design": d, "catalogue": subdesign.Catalogue, "presets": presets,
		"regions": subdesign.Regions, "tags": tags, "formats": subdesign.Formats,
		"library": library, "format_support": d.Support()})
}

type subDesignRequest struct {
	Design  subdesign.Design `json:"design"`
	Format  string           `json:"format"`
	Formats *[]string        `json:"formats"`
}

func (h *handlers) decodeDesign(w http.ResponseWriter, r *http.Request) (*subDesignRequest, bool) {
	var in subDesignRequest
	if !readJSON(w, r, &in) {
		return nil, false
	}
	if err := validateSubDesign(&in.Design); err != nil {
		fail(w, 400, err.Error())
		return nil, false
	}
	return &in, true
}

func validateSubDesign(d *subdesign.Design) error {
	raw, err := json.Marshal(d)
	if err != nil {
		return err
	}
	if len(raw) > 256*1024 {
		return errSubDesignTooLarge
	}
	// Validate the derived design as well, so a conflicting overlay cannot be
	// saved successfully and fail only when the subscription is published.
	if err := d.Validate(); err != nil {
		return err
	}
	_, err = d.Effective()
	return err
}

func (h *handlers) putSubDesign(w http.ResponseWriter, r *http.Request) {
	in, good := h.decodeDesign(w, r)
	if !good {
		return
	}
	if err := h.Store.SetSetting(r.Context(), store.SettingSubDesign, &in.Design); err != nil {
		serverErr(w, err)
		return
	}
	ok(w, &in.Design)
}

func (h *handlers) inspectSubDesign(w http.ResponseWriter, r *http.Request) {
	in, good := h.decodeDesign(w, r)
	if !good {
		return
	}
	effective, err := in.Design.Effective()
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	ok(w, map[string]any{"effective_design": effective, "formats": in.Design.Support()})
}

func (h *handlers) previewSubDesign(w http.ResponseWriter, r *http.Request) {
	in, good := h.decodeDesign(w, r)
	if !good {
		return
	}
	text, err := in.Design.Render(in.Format)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	ok(w, map[string]any{"format": in.Format, "text": text, "support": in.Design.Support()})
}

func (h *handlers) applySubDesign(w http.ResponseWriter, r *http.Request) {
	in, good := h.decodeDesign(w, r)
	if !good {
		return
	}
	// Legacy clients still request every format. New clients explicitly choose
	// supported formats and see which existing templates will be retained.
	formats := subdesign.Formats
	if in.Formats != nil {
		formats = *in.Formats
	}
	if len(formats) == 0 || len(formats) > len(subdesign.Formats) {
		fail(w, 400, "select at least one supported format")
		return
	}
	all := map[string]string{}
	for _, format := range formats {
		if _, duplicate := all[format]; duplicate {
			fail(w, 400, "duplicate format")
			return
		}
		text, err := in.Design.Render(format)
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		all[format] = text
	}
	if err := h.Store.ApplySubDesign(r.Context(), &in.Design, all); err != nil {
		serverErr(w, err)
		return
	}
	h.writeSubTemplates(w, r)
}
