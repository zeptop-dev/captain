package dns

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNameExistsDoesNotTreatMissingResultsAsAvailability(t *testing.T) {
	for _, tc := range []struct {
		body        string
		exists, bad bool
	}{
		{`{"success":true,"result":[]}`, false, false},
		{`{"success":true,"result":[{"type":"CNAME","name":"node.example.com"}]}`, true, false},
		{`{"success":true,"result":null}`, false, true},
		{`{"success":true}`, false, true},
		{`{"success":false}`, false, true},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || r.URL.Query().Get("name") != "node.example.com" || r.URL.Query().Has("type") {
				t.Error("not an exact read-only all-types query", r.URL)
			}
			_, _ = w.Write([]byte(tc.body))
		}))
		c := &Cloudflare{Base: srv.URL, Token: "fixture-token"}
		exists, err := c.NameExists(context.Background(), "zone", "node.example.com")
		srv.Close()
		if exists != tc.exists || (err != nil) != tc.bad {
			t.Fatalf("%s: exists=%v error=%v", tc.body, exists, err)
		}
	}
}
