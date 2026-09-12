// Package certs serves Captain's own HTTPS certificates with ACME via
// certmagic. HTTP-01 (port 80 reachable) issues one certificate per host on
// demand; with a Cloudflare API token DNS-01 is used instead and a wildcard
// for the panel domain is obtained up front, covering www and every
// subscription host under it without port 80.
package certs

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/caddyserver/certmagic"
	"github.com/libdns/cloudflare"
	"go.uber.org/zap"
)

// Options configures a Manager.
type Options struct {
	Dir             string // certificate storage
	Email           string // ACME account contact
	Domain          string // panel host; with DNS-01 also "*.<Domain>"
	CloudflareToken string // enables DNS-01
	Staging         bool
	Log             *slog.Logger
	// Allow decides which extra hosts get on-demand certificates
	// (subscription hosts). The panel domain is always allowed.
	Allow func(ctx context.Context, host string) error
	// Issuers overrides the ACME issuer (tests).
	Issuers []certmagic.Issuer
}

// Manager owns the certmagic cache and two configs sharing it: managed
// (panel host and wildcard, issued up front and renewed on a timer) and
// onDemand (subscription hosts, issued at first handshake). certmagic defers
// every name to handshakes when OnDemand is set, hence the split.
type Manager struct {
	opts     Options
	log      *slog.Logger
	cache    *certmagic.Cache
	managed  *certmagic.Config
	onDemand *certmagic.Config
	issuer   *certmagic.ACMEIssuer
}

// New builds a manager; call Start to obtain the managed certificates.
func New(opts Options) (*Manager, error) {
	if opts.Dir == "" || opts.Domain == "" {
		return nil, errors.New("certs: dir and domain are required")
	}
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	if err := os.MkdirAll(opts.Dir, 0o750); err != nil {
		return nil, err
	}
	m := &Manager{opts: opts, log: opts.Log.With("component", "certs")}
	m.cache = certmagic.NewCache(certmagic.CacheOptions{
		GetConfigForCert: func(c certmagic.Certificate) (*certmagic.Config, error) {
			for _, n := range c.Names {
				for _, want := range m.Managed() {
					if n == want {
						return m.managed, nil
					}
				}
			}
			return m.onDemand, nil
		},
		Logger: zap.NewNop(),
	})
	base := certmagic.Config{Storage: &certmagic.FileStorage{Path: opts.Dir}, Logger: zap.NewNop(), OnEvent: m.onEvent}
	m.managed = certmagic.New(m.cache, base)
	od := base
	od.OnDemand = &certmagic.OnDemandConfig{DecisionFunc: func(ctx context.Context, name string) error {
		if opts.Allow != nil {
			return opts.Allow(ctx, name)
		}
		return errors.New("host not allowed")
	}}
	m.onDemand = certmagic.New(m.cache, od)
	if opts.Issuers != nil {
		m.managed.Issuers, m.onDemand.Issuers = opts.Issuers, opts.Issuers
		return m, nil
	}
	ca := certmagic.LetsEncryptProductionCA
	if opts.Staging {
		ca = certmagic.LetsEncryptStagingCA
	}
	ai := certmagic.ACMEIssuer{CA: ca, Email: opts.Email, Agreed: true, DisableTLSALPNChallenge: true, Logger: zap.NewNop()}
	if opts.CloudflareToken != "" {
		ai.DisableHTTPChallenge = true
		ai.DNS01Solver = &certmagic.DNS01Solver{DNSManager: certmagic.DNSManager{DNSProvider: &cloudflare.Provider{APIToken: opts.CloudflareToken}}}
	}
	m.issuer = certmagic.NewACMEIssuer(m.managed, ai)
	m.managed.Issuers = []certmagic.Issuer{m.issuer}
	m.onDemand.Issuers = []certmagic.Issuer{certmagic.NewACMEIssuer(m.onDemand, ai)}
	return m, nil
}

// DNS reports whether DNS-01 (and therefore the wildcard) is in use.
func (m *Manager) DNS() bool { return m.opts.CloudflareToken != "" }

// Managed lists the names obtained up front.
func (m *Manager) Managed() []string {
	d := strings.ToLower(m.opts.Domain)
	if m.DNS() {
		return []string{d, "*." + d}
	}
	return []string{d}
}

// Start obtains the managed certificates in the background and keeps them
// renewed. Other allowed hosts are issued on first TLS handshake.
func (m *Manager) Start(ctx context.Context) error {
	return m.managed.ManageAsync(ctx, m.Managed())
}

// Sync obtains the managed certificates before returning (tests).
func (m *Manager) Sync(ctx context.Context) error {
	return m.managed.ManageSync(ctx, m.Managed())
}

// TLSConfig serves the certificates.
func (m *Manager) TLSConfig() *tls.Config {
	// The on-demand config's lookup sees the shared cache, so the wildcard
	// and panel certificates are matched before any decision is made.
	c := m.onDemand.TLSConfig()
	c.NextProtos = append([]string{"h2", "http/1.1"}, c.NextProtos...)
	return c
}

// HTTPHandler answers HTTP-01 challenges on port 80 and passes everything
// else to next.
func (m *Manager) HTTPHandler(next http.Handler) http.Handler {
	if m.issuer == nil {
		return next
	}
	return m.issuer.HTTPChallengeHandler(next)
}

// Stop releases the renewal goroutines.
func (m *Manager) Stop() { m.cache.Stop() }

func (m *Manager) onEvent(_ context.Context, event string, data map[string]any) error {
	switch event {
	case "cert_obtained":
		m.log.Info("certificate obtained", "domain", data["identifier"], "renewal", data["renewal"])
	case "cert_failed":
		m.log.Error("certificate failed", "domain", data["identifier"], "err", data["error"], "renewal", data["renewal"])
	}
	return nil
}
