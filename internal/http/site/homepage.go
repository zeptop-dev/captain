package site

import (
	"net/http"
	"net/url"

	"github.com/zeptop-dev/captain/internal/store"
)

// Homepage applies only at the main site's root, inside the existing probe
// and subscription-host routers. The portal decides whether to show login or
// the signed-in customer's dashboard; the redirect must never be permanent.
func Homepage(st *store.Store, page http.Handler) http.Handler {
	closed := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := "/portal/"
		// Preserve referral attribution, not arbitrary redirect/error parameters.
		if ref := r.URL.Query().Get("ref"); ref != "" {
			target += "?" + url.Values{"ref": {ref}}.Encode()
		}
		http.Redirect(w, r, target, http.StatusFound)
	})
	return selectHomepage(st, page, closed, true)
}

// HomepageAssets retains actual files used by independent public pages and
// branding. When closed, missing assets must not fall back to homepage HTML.
func HomepageAssets(st *store.Store, normal, filesOnly http.Handler) http.Handler {
	return selectHomepage(st, normal, filesOnly, false)
}

func selectHomepage(st *store.Store, enabled, disabled http.Handler, root bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var settings Settings
		if err := st.GetSetting(r.Context(), SettingSite, &settings); err != nil {
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "site settings unavailable", http.StatusInternalServerError)
			return
		}
		if root {
			w.Header().Set("Cache-Control", "no-store")
		}
		if settings.HomepageOn() {
			enabled.ServeHTTP(w, r)
		} else {
			disabled.ServeHTTP(w, r)
		}
	})
}
