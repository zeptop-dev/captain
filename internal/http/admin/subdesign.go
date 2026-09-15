package admin

import (
	"net/http"

	"github.com/zeptop-dev/bosun/pkg/subdesign"
	"github.com/zeptop-dev/captain/internal/store"
)

// registerSubDesign mounts the visual subscription designer API: a design
// (proxy groups + rule sets) that generates every format's template.
func (h *handlers) registerSubDesign(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/settings/sub-design", h.requireAdmin(h.getSubDesign))
	mux.HandleFunc("PUT /api/admin/settings/sub-design", h.requireAdmin(h.putSubDesign))
	mux.HandleFunc("POST /api/admin/settings/sub-design/preview", h.requireAdmin(h.previewSubDesign))
	mux.HandleFunc("POST /api/admin/settings/sub-design/apply", h.requireAdmin(h.applySubDesign))
	mux.HandleFunc("GET /api/admin/settings/sub-design/presets/{key}", h.requireAdmin(h.subDesignPreset))
}

func (h *handlers) getSubDesign(w http.ResponseWriter, r *http.Request) {
	var d subdesign.Design
	_ = h.Store.GetSetting(r.Context(), store.SettingSubDesign, &d)
	if d.Groups == nil {
		d.Groups = []subdesign.Group{}
	}
	if d.Rules == nil {
		d.Rules = []subdesign.Rule{}
	}
	tags, _ := h.Store.EntryTags(r.Context())
	if tags == nil {
		tags = []string{}
	}
	ok(w, map[string]any{"design": d, "catalogue": subdesign.Catalogue, "presets": subdesign.Presets, "regions": subdesign.Regions, "tags": tags, "formats": subdesign.Formats})
}

func (h *handlers) subDesignPreset(w http.ResponseWriter, r *http.Request) {
	d, found := subdesign.Preset(r.PathValue("key"))
	if !found {
		fail(w, http.StatusNotFound, "unknown preset")
		return
	}
	ok(w, d)
}

func (h *handlers) decodeDesign(w http.ResponseWriter, r *http.Request) (*subdesign.Design, string, bool) {
	var in struct {
		Design subdesign.Design `json:"design"`
		Format string           `json:"format"`
	}
	if !decode(r, &in) {
		fail(w, http.StatusBadRequest, "bad json")
		return nil, "", false
	}
	if err := in.Design.Validate(); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return nil, "", false
	}
	return &in.Design, in.Format, true
}

// putSubDesign stores the design without touching the templates.
func (h *handlers) putSubDesign(w http.ResponseWriter, r *http.Request) {
	d, _, good := h.decodeDesign(w, r)
	if !good {
		return
	}
	if err := h.Store.SetSetting(r.Context(), store.SettingSubDesign, d); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, d)
}

// previewSubDesign renders one format's template from the posted design.
func (h *handlers) previewSubDesign(w http.ResponseWriter, r *http.Request) {
	d, format, good := h.decodeDesign(w, r)
	if !good {
		return
	}
	text, err := d.Render(format)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	ok(w, map[string]string{"format": format, "text": text})
}

// applySubDesign stores the design and writes every generated template
// over the current ones, so the text editor and the subscriptions use them.
func (h *handlers) applySubDesign(w http.ResponseWriter, r *http.Request) {
	d, _, good := h.decodeDesign(w, r)
	if !good {
		return
	}
	all, err := d.RenderAll()
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.Store.SetSetting(r.Context(), store.SettingSubDesign, d); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	var v store.SubTemplates
	_ = h.Store.GetSetting(r.Context(), store.SettingSubTemplates, &v)
	if v == nil {
		v = store.SubTemplates{}
	}
	for format, text := range all {
		v[format] = text
	}
	if err := h.Store.SetSetting(r.Context(), store.SettingSubTemplates, v); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.writeSubTemplates(w, r)
}
