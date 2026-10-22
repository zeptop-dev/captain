package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/coreos/go-oidc/v3/oidc"
)

func TestDiscoveryPreservesIssuerIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, configured, discovered string
		mismatch                     bool
	}{
		{"root", "", "", false},
		{"root_slash", "/", "/", false},
		{"path", "/tenant", "/tenant", false},
		{"path_slash", "/tenant/", "/tenant/", false},
		{"missing_slash", "", "/", true},
		{"extra_slash", "/", "", true},
		{"different_tenant", "/tenant", "/other", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var srv *httptest.Server
			srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				expected := "/.well-known/openid-configuration"
				if tc.configured == "/tenant" || tc.configured == "/tenant/" {
					expected = "/tenant" + expected
				}
				if r.URL.Path != expected {
					t.Errorf("discovery path = %q, want %q", r.URL.Path, expected)
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]string{"issuer": srv.URL + tc.discovered, "jwks_uri": srv.URL + "/jwks"})
			}))
			defer srv.Close()
			h := &handlers{providers: make(map[string]*oidc.Provider)}
			issuer := srv.URL + tc.configured
			p, err := h.discover(context.Background(), issuer)
			if tc.mismatch {
				var mismatch *oidc.IssuerMismatchError
				if !errors.As(err, &mismatch) {
					t.Fatalf("expected issuer mismatch, got %v", err)
				}
				if p != nil || len(h.providers) != 0 {
					t.Fatal("mismatched issuer was accepted or cached")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if h.providers[issuer] != p {
				t.Fatal("provider was not cached under the exact issuer")
			}
		})
	}
}
