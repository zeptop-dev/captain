package probe

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/zeptop-dev/captain/internal/http/komari"
	"github.com/zeptop-dev/captain/internal/http/site"
	"github.com/zeptop-dev/captain/internal/probedata"
	"github.com/zeptop-dev/captain/internal/probetheme"
	"github.com/zeptop-dev/captain/internal/store"
)

func (h *handlers) registerCompatibility() {
	if h.Data == nil || h.Access == nil {
		return
	}
	h.compat = komari.New(komari.Deps{
		Reader: h.Data, Version: h.Version,
		Gate: func(r *http.Request) bool {
			if !h.Access.Dedicated(r.Context(), r.Host) {
				return false
			}
			ok, _ := h.allowed(r, h.currentSettings(r.Context()))
			return ok
		},
		Identity: func(r *http.Request) komari.Identity {
			if h.Access.Resolve(r) != nil {
				return komari.Identity{Username: "viewer", LoggedIn: true}
			}
			return komari.Identity{}
		},
		PublicSettings: h.compatibilitySettings,
	})
}

func (h *handlers) compatibilitySettings(r *http.Request) (map[string]any, error) {
	out := map[string]any{"theme": "default", "theme_settings": map[string]any{}, "login_url": "/__captain/login"}
	if h.Themes == nil {
		return out, nil
	}
	configuration, err := h.Themes.PublicConfiguration(r.Context())
	if err != nil {
		return nil, err
	}
	if configuration.Digest == "" {
		return out, nil
	}
	short, settings := configuration.Manifest.Short, configuration.Settings
	if configuration.Manifest.Configuration.Type == "managed" {
		fields, err := configuration.Manifest.Fields()
		if err != nil {
			return nil, err
		}
		needsCatalog := false
		for _, f := range fields {
			if f.Type == "nodes" || f.Type == "pingtasks" {
				needsCatalog = true
			}
		}
		if needsCatalog {
			snapshot, err := h.Data.Snapshot(r.Context(), probedata.Scope{})
			if err != nil {
				return nil, err
			}
			filterThemeSelectors(settings, fields, snapshot)
		}
	}
	out["theme"], out["theme_settings"] = short, settings
	return out, nil
}

func filterThemeSelectors(settings map[string]any, fields []probetheme.Field, snapshot probedata.Snapshot) {
	nodes, tasks := map[string]bool{}, map[string]bool{}
	for _, n := range snapshot.Nodes {
		nodes[n.UUID] = true
	}
	for _, t := range snapshot.PingTasks {
		tasks[strconv.FormatInt(t.LegacyID, 10)] = true
	}
	for _, f := range fields {
		if f.Type != "nodes" && f.Type != "pingtasks" {
			continue
		}
		// Decode the normalized selector as JSON to accept both the concrete
		// default and saved array types without stringifying arbitrary objects.
		b, err := json.Marshal(settings[f.Key])
		if err != nil {
			delete(settings, f.Key)
			continue
		}
		if f.Type == "nodes" {
			var values []string
			_ = json.Unmarshal(b, &values)
			kept := []string{}
			for _, v := range values {
				if nodes[v] {
					kept = append(kept, v)
				}
			}
			settings[f.Key] = kept
		} else {
			var values []int64
			_ = json.Unmarshal(b, &values)
			kept := []int64{}
			for _, v := range values {
				if tasks[strconv.FormatInt(v, 10)] {
					kept = append(kept, v)
				}
			}
			settings[f.Key] = kept
		}
	}
}

func (h *handlers) themePresentation(r *http.Request, s store.ProbeSettings) (string, string) {
	title := firstNonEmpty(s.Title, h.SiteName)
	if s.FollowSiteBranding == nil || !*s.FollowSiteBranding {
		return title, ""
	}
	var v site.Settings
	if h.Store.GetSetting(r.Context(), site.SettingSite, &v) != nil {
		return title, ""
	}
	title = firstNonEmpty(v.Name, h.SiteName)
	if v.Branding != nil {
		title = firstNonEmpty(v.Branding.Title, title)
	}
	return title, v.Description
}
