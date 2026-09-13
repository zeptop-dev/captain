package admin

import (
	"net/http"

	"github.com/zeptop-dev/captain/internal/store"
	"github.com/zeptop-dev/captain/internal/subscription"
)

// registerSubTemplates mounts the subscription template editor API.
func (h *handlers) registerSubTemplates(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/settings/sub-templates", h.requireAdmin(h.getSubTemplates))
	mux.HandleFunc("PUT /api/admin/settings/sub-templates", h.requireAdmin(h.putSubTemplates))
}

func (h *handlers) writeSubTemplates(w http.ResponseWriter, r *http.Request) {
	var v store.SubTemplates
	_ = h.Store.GetSetting(r.Context(), store.SettingSubTemplates, &v)
	templates, defaults := map[string]string{}, map[string]string{}
	for _, name := range subscription.TemplateNames() {
		templates[name] = v[name]
		defaults[name] = subscription.DefaultTemplate(name)
	}
	ok(w, map[string]any{"templates": templates, "defaults": defaults})
}

func (h *handlers) getSubTemplates(w http.ResponseWriter, r *http.Request) { h.writeSubTemplates(w, r) }

// putSubTemplates stores overrides; unknown formats are dropped and an
// empty body means "use the default".
func (h *handlers) putSubTemplates(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Templates map[string]string `json:"templates"`
	}
	if !decode(r, &in) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	v := store.SubTemplates{}
	for _, name := range subscription.TemplateNames() {
		if body := in.Templates[name]; body != "" {
			v[name] = body
		}
	}
	if err := h.Store.SetSetting(r.Context(), store.SettingSubTemplates, v); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.writeSubTemplates(w, r)
}
