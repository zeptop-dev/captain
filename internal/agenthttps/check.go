package agenthttps

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"sync/atomic"
	"time"

	"github.com/zeptop-dev/captain/internal/agentendpoint"
)

// AccessCheck is one bounded observation from the panel's network namespace.
// It cannot establish host firewall rules, Docker publishing or node reachability.
type AccessCheck struct {
	Revision    string    `json:"revision"`
	URL         string    `json:"url"`
	CheckedAt   time.Time `json:"checked_at"`
	Perspective string    `json:"perspective"`
	Status      string    `json:"status"`
	HTTPStatus  int       `json:"http_status,omitempty"`
	ElapsedMS   int64     `json:"elapsed_ms"`
}

// Checker authenticates a fresh reply from this process without disclosing a
// node token, a pairing code or a persistent instance identifier.
type Checker struct {
	key    [32]byte
	client *http.Client
}

func NewChecker() *Checker {
	c := &Checker{}
	_, _ = rand.Read(c.key[:])
	c.client = &http.Client{Transport: &http.Transport{
		// Do not forward this request through an environment-provided proxy.
		DialContext:         (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
		TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 12 * time.Second,
		MaxResponseHeaderBytes: 16 << 10, DisableKeepAlives: true,
	}, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return c
}

func (c *Checker) Proof(challenge string) string {
	b, err := hex.DecodeString(challenge)
	if err != nil || len(b) != 32 || len(challenge) != 64 {
		return ""
	}
	m := hmac.New(sha256.New, c.key[:])
	m.Write([]byte("captain-agent-health-v1\x00"))
	m.Write(b)
	return hex.EncodeToString(m.Sum(nil))
}

func (c *Checker) Check(ctx context.Context, origin string) AccessCheck {
	start := time.Now()
	out := AccessCheck{URL: origin, Perspective: "panel", Status: "unexpected_service"}
	// Callers must use saved settings; normalize again before any network I/O.
	u, err := agentendpoint.NormalizeURL(origin)
	if err != nil || u == "" {
		out.CheckedAt = time.Now().UTC()
		return out
	}
	var nonce [32]byte
	_, _ = rand.Read(nonce[:])
	challenge := hex.EncodeToString(nonce[:])
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u+"/api/agent/health?challenge="+challenge, nil)
	var handshakeFailed atomic.Bool
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{TLSHandshakeDone: func(_ tls.ConnectionState, err error) { handshakeFailed.Store(err != nil) }}))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cache-Control", "no-store")
	resp, err := c.client.Do(req)
	if err != nil {
		out.Status = accessError(err)
		if handshakeFailed.Load() && out.Status != "timeout" {
			out.Status = "tls_error"
		}
	} else {
		defer resp.Body.Close()
		out.HTTPStatus = resp.StatusCode
		switch {
		case resp.StatusCode >= 300 && resp.StatusCode < 400:
			out.Status = "redirect"
		case resp.StatusCode != http.StatusOK:
			out.Status = "http_error"
		default:
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4097))
			var health struct {
				OK      bool   `json:"ok"`
				Service string `json:"service"`
				Proof   string `json:"proof"`
			}
			if readErr != nil {
				out.Status = accessError(readErr)
			} else if len(body) <= 4096 && json.Unmarshal(body, &health) == nil && health.OK && health.Service == "captain-agent" && hmac.Equal([]byte(health.Proof), []byte(c.Proof(challenge))) {
				out.Status = "ok"
			}
		}
	}
	out.CheckedAt, out.ElapsedMS = time.Now().UTC(), time.Since(start).Milliseconds()
	return out
}

func accessError(err error) string {
	var verification *tls.CertificateVerificationError
	var record tls.RecordHeaderError
	var dns *net.DNSError
	var network net.Error
	switch {
	case errors.As(err, &verification), errors.As(err, &record):
		return "tls_error"
	case errors.As(err, &dns):
		return "dns_error"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return "timeout"
	case errors.As(err, &network) && network.Timeout():
		return "timeout"
	default:
		return "connect_error"
	}
}
