package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProbeThemeAssetsServeBundledIcons(t *testing.T) {
	h := ProbeThemeAssets()
	for _, path := range []string{"/assets/flags/JP.svg", "/assets/logo/os-debian.svg"} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "<svg") || !strings.HasPrefix(w.Header().Get("Content-Type"), "image/svg+xml") || w.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatalf("built icon unavailable: %d, %v", w.Code, w.Header())
			}
			if strings.Contains(w.Body.String(), "<html") || w.Header().Get("ETag") == "" {
				t.Fatal("returned SPA fallback or missing asset validator")
			}
			r := httptest.NewRequest(http.MethodGet, path, nil)
			r.Header.Set("If-None-Match", w.Header().Get("ETag"))
			cached := httptest.NewRecorder()
			h.ServeHTTP(cached, r)
			if cached.Code != http.StatusNotModified {
				t.Fatal("asset validator ignored", cached.Code)
			}
			w = httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(http.MethodHead, path, nil))
			if w.Code != http.StatusOK || w.Body.Len() != 0 {
				t.Fatal("HEAD returned a body", w.Code)
			}
		})
	}
}

func TestProbeThemeAssetsUnknownRegionIsNeutral(t *testing.T) {
	h := ProbeThemeAssets()
	for _, path := range []string{"/images/flags/.svg", "/assets/flags/.svg"} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
			if w.Code != http.StatusOK || w.Body.String() != unknownRegionSVG || !strings.HasPrefix(w.Header().Get("Content-Type"), "image/svg+xml") {
				t.Fatal("unknown region did not receive the neutral globe", w.Code, w.Header())
			}
			if w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Cache-Control") != "private, no-cache" || w.Header().Get("Cross-Origin-Resource-Policy") != "same-origin" || w.Header().Get("ETag") == "" {
				t.Fatal("neutral icon lost normal asset headers", w.Header())
			}
			r := httptest.NewRequest(http.MethodGet, path, nil)
			r.Header.Set("If-None-Match", w.Header().Get("ETag"))
			cached := httptest.NewRecorder()
			h.ServeHTTP(cached, r)
			if cached.Code != http.StatusNotModified {
				t.Fatal("neutral icon cache validation failed", cached.Code)
			}
			w = httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(http.MethodHead, path, nil))
			if w.Code != http.StatusOK || w.Body.Len() != 0 {
				t.Fatal("HEAD returned a body", w.Code)
			}
			w = httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, nil))
			if w.Code != http.StatusMethodNotAllowed {
				t.Fatal("accepted write method", w.Code)
			}
		})
	}
}

func TestProbeThemeAssetsRejectNonFilesAndOtherRoutes(t *testing.T) {
	h := ProbeThemeAssets()
	for _, path := range []string{"/", "/api/nodes", "/assets", "/assets/", "/assets/flags", "/assets/flags/", "/assets/logo/", "/assets/flags/JP.svg/", "/assets/flags/../logo/os-debian.svg", "/assets/flags/%2e%2e/logo/os-debian.svg", "/assets/flags/%2fJP.svg", "/assets/flags/..%5clogo%5cos-debian.svg", "/assets/flags/../../index.html", "/assets/flags/nonexistent.svg", "/assets/logo/app.js", "/assets/earth/earth-topology.png", "/glass/images/flags/JP.svg", "/assets//flags/JP.svg", "/images/flags/JP.svg", "/images/flags/", "/images/logo/os-debian.svg", "/images/flags/.svg/", "/images/flags/unknown.svg", "/images/flags/../.svg", "/assets/flags/.svg/", "/assets/logo/.svg"} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
			if w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), "<html") {
				t.Fatal("served a non-icon path", w.Code, w.Body.String())
			}
		})
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/assets/flags/JP.svg", nil))
	if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != "GET, HEAD" {
		t.Fatal("accepted write method", w.Code)
	}
}
