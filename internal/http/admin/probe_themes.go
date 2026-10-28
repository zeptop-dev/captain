package admin

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/zeptop-dev/captain/internal/probetheme"
	"github.com/zeptop-dev/captain/internal/store"
)

// RegisterProbeThemes mounts isolated theme package management. All mutations
// require a full administrator console session, never a reusable API token.
func RegisterProbeThemes(mux *http.ServeMux, d Deps, manager *probetheme.Manager, options func(context.Context) (probetheme.SelectorOptions, error)) {
	h := &handlers{Deps: d}
	wrap := func(next http.HandlerFunc) http.HandlerFunc {
		return h.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(strings.TrimSpace(r.Header.Get("Authorization")), "Bearer ") {
				fail(w, http.StatusForbidden, "theme management requires an administrator console session")
				return
			}
			next(w, r)
		})
	}
	base := "/api/admin/settings/probe/themes"
	mux.HandleFunc("GET "+base+"/options", wrap(func(w http.ResponseWriter, r *http.Request) {
		if options == nil {
			fail(w, http.StatusServiceUnavailable, "theme selectors unavailable")
			return
		}
		v, err := options(r.Context())
		if err != nil {
			serverErr(w, err)
			return
		}
		ok(w, v)
	}))
	mux.HandleFunc("GET "+base, wrap(func(w http.ResponseWriter, r *http.Request) {
		v, err := manager.List(r.Context())
		if err != nil {
			serverErr(w, err)
			return
		}
		ok(w, v)
	}))
	mux.HandleFunc("POST "+base, wrap(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, probetheme.MaxArchiveBytes))
		if err != nil {
			var max *http.MaxBytesError
			if errors.As(err, &max) {
				fail(w, http.StatusRequestEntityTooLarge, "theme ZIP must be at most 32 MiB")
			} else {
				fail(w, http.StatusBadRequest, "could not read theme ZIP")
			}
			return
		}
		p, err := manager.Install(r.Context(), b)
		if err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
		ok(w, p)
	}))
	mux.HandleFunc("PUT "+base+"/active", wrap(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Digest   string `json:"digest"`
			Revision int64  `json:"revision"`
		}
		if !readJSON(w, r, &in) {
			return
		}
		if in.Digest != "" {
			var s store.ProbeSettings
			if err := h.Store.GetSetting(r.Context(), store.SettingProbe, &s); err != nil {
				serverErr(w, err)
				return
			}
			if !themeDedicatedHost(s.Hosts, h.BaseURL, r.Host) {
				fail(w, http.StatusBadRequest, "save a dedicated probe domain different from the main site before activating a third-party theme")
				return
			}
		}
		if err := manager.Activate(r.Context(), in.Digest, in.Revision); err != nil {
			themeFail(w, err)
			return
		}
		ok(w, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("PUT "+base+"/{digest}/settings", wrap(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Revision int64          `json:"revision"`
			Settings map[string]any `json:"settings"`
		}
		if !readJSON(w, r, &in) {
			return
		}
		if err := manager.SaveSettings(r.Context(), r.PathValue("digest"), in.Revision, in.Settings); err != nil {
			themeFail(w, err)
			return
		}
		ok(w, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("DELETE "+base+"/{digest}", wrap(func(w http.ResponseWriter, r *http.Request) {
		if err := manager.Remove(r.Context(), r.PathValue("digest")); err != nil {
			themeFail(w, err)
			return
		}
		ok(w, map[string]bool{"ok": true})
	}))
}
func themeFail(w http.ResponseWriter, err error) {
	code := http.StatusBadRequest
	if probetheme.IsConflict(err) {
		code = http.StatusConflict
	} else if errors.Is(err, store.ErrNotFound) {
		code = http.StatusNotFound
	}
	fail(w, code, err.Error())
}
func themeDedicatedHost(hosts []string, baseURL, requestHost string) bool {
	u, _ := url.Parse(baseURL)
	base := ""
	if u != nil {
		base = strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	}
	request, _ := url.Parse("https://" + requestHost)
	current := ""
	if request != nil {
		current = strings.TrimSuffix(strings.ToLower(request.Hostname()), ".")
	}
	for _, host := range hosts {
		h, _ := url.Parse("https://" + host)
		if h == nil {
			continue
		}
		name := strings.TrimSuffix(strings.ToLower(h.Hostname()), ".")
		if name != "" && name != base && name != current {
			return true
		}
	}
	return false
}
