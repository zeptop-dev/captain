package origin

import (
	"net/http/httptest"
	"testing"
)

func TestAllowed(t *testing.T) {
	cases := []struct {
		method, origin, referer, host, fwd, base string
		want                                     bool
	}{
		{"GET", "https://evil.example", "", "panel.example", "", "", true},
		{"POST", "", "", "panel.example", "", "", true},
		{"POST", "https://panel.example", "", "panel.example", "", "", true},
		{"POST", "https://panel.example:8443", "", "panel.example", "", "", true},
		{"POST", "https://evil.example", "", "panel.example", "", "", false},
		{"POST", "null", "", "panel.example", "", "", false},
		{"POST", "", "https://evil.example/page", "panel.example", "", "", false},
		{"POST", "", "https://panel.example/admin", "panel.example", "", "", true},
		{"POST", "https://panel.example", "", "127.0.0.1:8080", "panel.example", "", true},
		{"POST", "https://panel.example", "", "127.0.0.1:8080", "", "https://panel.example", true},
		{"POST", "https://PANEL.example", "", "panel.example", "", "", true},
		{"DELETE", "https://evil.example", "", "panel.example", "", "https://panel.example", false},
	}
	for _, c := range cases {
		r := httptest.NewRequest(c.method, "http://"+c.host+"/api/x", nil)
		r.Host = c.host
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		if c.referer != "" {
			r.Header.Set("Referer", c.referer)
		}
		if c.fwd != "" {
			r.Header.Set("X-Forwarded-Host", c.fwd)
		}
		if got := Allowed(r, c.base); got != c.want {
			t.Errorf("%s origin=%q referer=%q host=%q fwd=%q base=%q: got %v", c.method, c.origin, c.referer, c.host, c.fwd, c.base, got)
		}
	}
}
