package certs

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"sync"

	"github.com/caddyserver/certmagic"
	"github.com/libdns/cloudflare"
	"go.uber.org/zap"
)

// IssueRequest asks for one certificate covering Names, solving DNS-01
// through the Cloudflare token.
type IssueRequest struct {
	Names           []string
	Email           string
	CloudflareToken string
}

// Issuer obtains a certificate for arbitrary names on the panel's behalf.
type Issuer interface {
	Issue(ctx context.Context, req IssueRequest) (certPEM, keyPEM string, err error)
}

// ACMEIssuer issues through Let's Encrypt with certmagic's ACME client;
// one client per (email, token) pair, account keys kept under Dir.
type ACMEIssuer struct {
	Dir     string
	Staging bool

	mu      sync.Mutex
	cache   *certmagic.Cache
	clients map[string]*certmagic.ACMEIssuer
}

func (a *ACMEIssuer) client(email, token string) (*certmagic.ACMEIssuer, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	key := email + "\x00" + token
	if c, ok := a.clients[key]; ok {
		return c, nil
	}
	if err := os.MkdirAll(a.Dir, 0o750); err != nil {
		return nil, err
	}
	if a.cache == nil {
		a.cache = certmagic.NewCache(certmagic.CacheOptions{
			GetConfigForCert: func(certmagic.Certificate) (*certmagic.Config, error) {
				return certmagic.New(a.cache, certmagic.Config{}), nil
			},
			Logger: zap.NewNop(),
		})
		a.clients = map[string]*certmagic.ACMEIssuer{}
	}
	cfg := certmagic.New(a.cache, certmagic.Config{Storage: &certmagic.FileStorage{Path: a.Dir}, Logger: zap.NewNop()})
	ca := certmagic.LetsEncryptProductionCA
	if a.Staging {
		ca = certmagic.LetsEncryptStagingCA
	}
	c := certmagic.NewACMEIssuer(cfg, certmagic.ACMEIssuer{
		CA: ca, Email: email, Agreed: true, DisableHTTPChallenge: true, DisableTLSALPNChallenge: true, Logger: zap.NewNop(),
		DNS01Solver: &certmagic.DNS01Solver{DNSManager: certmagic.DNSManager{DNSProvider: &cloudflare.Provider{APIToken: token}}},
	})
	cfg.Issuers = []certmagic.Issuer{c}
	a.clients[key] = c
	return c, nil
}

// Issue generates a P-256 key and CSR for the names and completes the order.
func (a *ACMEIssuer) Issue(ctx context.Context, req IssueRequest) (string, string, error) {
	if len(req.Names) == 0 {
		return "", "", errors.New("no names")
	}
	if req.CloudflareToken == "" {
		return "", "", errors.New("a Cloudflare API token is required for DNS-01")
	}
	c, err := a.client(req.Email, req.CloudflareToken)
	if err != nil {
		return "", "", err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{DNSNames: req.Names}, key)
	if err != nil {
		return "", "", err
	}
	csr, err := x509.ParseCertificateRequest(der)
	if err != nil {
		return "", "", err
	}
	ic, err := c.Issue(ctx, csr)
	if err != nil {
		return "", "", err
	}
	kb, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", "", err
	}
	return string(ic.Certificate), string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb})), nil
}
