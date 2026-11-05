package agenthttps

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAccessCheckTLSIdentityAndResponseBounds(t *testing.T) {
	c := NewChecker()
	other := NewChecker()
	mode := "ok"
	var previous string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agent/health" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("unexpected path or credentials")
		}
		challenge := r.URL.Query().Get("challenge")
		proof := c.Proof(challenge)
		switch mode {
		case "redirect":
			http.Redirect(w, r, "/should-not-follow", 302)
			return
		case "522":
			w.WriteHeader(522)
			return
		case "other":
			proof = other.Proof(challenge)
		case "replay":
			proof = previous
		case "oversized":
			w.Write([]byte(strings.Repeat(" ", 4097)))
		case "trailing":
			defer w.Write([]byte("garbage"))
		case "timeout":
			<-r.Context().Done()
			return
		}
		previous = proof
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "service": "captain-agent", "proof": proof})
	}))
	defer srv.Close()
	if got := c.Check(context.Background(), srv.URL); got.Status != "tls_error" {
		t.Fatalf("untrusted certificate: %+v", got)
	}
	c.client.Transport = srv.Client().Transport
	for _, tc := range []struct{ mode, want string }{{"ok", "ok"}, {"replay", "unexpected_service"}, {"other", "unexpected_service"}, {"oversized", "unexpected_service"}, {"trailing", "unexpected_service"}, {"redirect", "redirect"}, {"522", "http_error"}} {
		mode = tc.mode
		got := c.Check(context.Background(), srv.URL)
		if got.Status != tc.want || got.Perspective != "panel" || got.CheckedAt.IsZero() {
			t.Fatalf("%s: %+v", mode, got)
		}
	}
	mode = "timeout"
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if got := c.Check(ctx, srv.URL); got.Status != "timeout" {
		t.Fatalf("timeout: %+v", got)
	}
	for _, challenge := range []string{"", "x", strings.Repeat("z", 64), strings.Repeat("a", 66)} {
		if c.Proof(challenge) != "" {
			t.Fatal("invalid challenge accepted")
		}
	}
}

func TestAccessCheckDoesNotUseProxyOrInvalidOrigins(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	c := NewChecker()
	if c.client.Transport.(*http.Transport).Proxy != nil {
		t.Fatal("proxy forwarding enabled")
	}
	for _, origin := range []string{"http://example.com", "https://example.com/path", "https://user:password@example.com", "https://example.com?token=secret"} {
		if got := c.Check(context.Background(), origin); got.Status != "unexpected_service" || got.HTTPStatus != 0 {
			t.Fatal(got)
		}
	}
	if got := accessError(&net.DNSError{Err: "unavailable", Name: "example.com"}); got != "dns_error" {
		t.Fatal(got)
	}
}

func TestAccessCheckTLSAlert(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	srv.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	srv.StartTLS()
	defer srv.Close()
	c := NewChecker()
	transport := srv.Client().Transport.(*http.Transport).Clone()
	transport.TLSClientConfig.MaxVersion = tls.VersionTLS12
	c.client.Transport = transport
	if got := c.Check(context.Background(), srv.URL); got.Status != "tls_error" {
		t.Fatalf("TLS alert misclassified: %+v", got)
	}
}
