package admin

import (
	"net/http"
	"net/http/httptest"
	"net/url"

	"github.com/zeptop-dev/bosun/pkg/subscription"
	"github.com/zeptop-dev/captain/internal/service"
	"github.com/zeptop-dev/captain/internal/store"
)

// registerSubTemplates mounts the subscription template editor API.
func (h *handlers) registerSubTemplates(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/settings/sub-templates", h.requireAdmin(h.getSubTemplates))
	mux.HandleFunc("PUT /api/admin/settings/sub-templates", h.requireAdmin(h.putSubTemplates))
	mux.HandleFunc("GET /api/admin/settings/response-rules", h.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		getSetting[[]service.ResponseRule](h, w, r, service.SettingResponseRules, func(v *[]service.ResponseRule) {
			if *v == nil {
				*v = []service.ResponseRule{}
			}
		})
	}))
	mux.HandleFunc("PUT /api/admin/settings/response-rules", h.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		putSetting[[]service.ResponseRule](h, w, r, service.SettingResponseRules, service.ValidateResponseRules)
	}))
	mux.HandleFunc("POST /api/admin/settings/response-rules/test", h.requireAdmin(h.testResponseRules))
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
	if !readJSON(w, r, &in) {
		return
	}
	v := store.SubTemplates{}
	for _, name := range subscription.TemplateNames() {
		if body := in.Templates[name]; body != "" {
			v[name] = body
		}
	}
	if err := h.Store.SetSetting(r.Context(), store.SettingSubTemplates, v); err != nil {
		serverErr(w, err)
		return
	}
	h.writeSubTemplates(w, r)
}

// testResponseRules answers which rule (of the given list, or the stored
// one) a request with these headers would hit and what format it gets.
func (h *handlers) testResponseRules(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Rules   *[]service.ResponseRule `json:"rules"`
		Headers map[string]string       `json:"headers"`
		Client  string                  `json:"client"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	var rules []service.ResponseRule
	if in.Rules != nil {
		rules = *in.Rules
		if msg := service.ValidateResponseRules(r.Context(), &rules); msg != "" {
			fail(w, http.StatusBadRequest, msg)
			return
		}
	} else {
		_ = h.Store.GetSetting(r.Context(), service.SettingResponseRules, &rules)
	}
	probe := httptest.NewRequest("GET", "/sub/x?client="+url.QueryEscape(in.Client), nil)
	if in.Client == "" {
		probe.URL.RawQuery = ""
	}
	for k, v := range in.Headers {
		probe.Header.Set(k, v)
	}
	out := map[string]any{"rule": nil, "action": "serve", "format": subscription.Pick(in.Client, probe.UserAgent()).Name()}
	if rule := service.MatchResponseRule(rules, probe); rule != nil {
		out["rule"] = rule.Name
		out["action"] = rule.Action
		if rule.Action == "serve" {
			f := in.Client
			if rule.Format != "" {
				f = rule.Format
			}
			out["format"] = subscription.Pick(f, probe.UserAgent()).Name()
		} else {
			out["format"] = ""
		}
	}
	ok(w, out)
}
