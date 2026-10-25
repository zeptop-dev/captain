package web

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServicePageEmbeddedAssetsWithCustomHomepage(t *testing.T) {
	var asset string
	if err := fs.WalkDir(siteFS, "site/dist/assets", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".js") && asset == "" {
			asset = strings.TrimPrefix(p, "site/dist")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if asset == "" {
		t.Fatal("build site before testing")
	}
	handler := SiteAssets(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom-Site", "yes")
		_, _ = w.Write([]byte("custom asset"))
	}))
	for _, path := range []string{asset, "/assets/operator.js", "/assets/../index.html", "/assets/nonexistent.css"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if path == asset {
			if w.Code != 200 || w.Header().Get("X-Custom-Site") != "" || strings.Contains(w.Body.String(), "<html") || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
				t.Fatal("embedded asset unavailable", w.Code, w.Header())
			}
		} else if w.Header().Get("X-Custom-Site") != "yes" {
			t.Fatal("custom-site asset intercepted", path)
		}
	}
}
