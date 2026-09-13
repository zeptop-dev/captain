package certs

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/caddyserver/certmagic"
)

type selfIssuer struct{ key *ecdsa.PrivateKey }

func (s selfIssuer) IssuerKey() string { return "test-ca" }
func (s selfIssuer) Issue(_ context.Context, csr *x509.CertificateRequest) (*certmagic.IssuedCertificate, error) {
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: csr.DNSNames[0]},
		DNSNames: csr.DNSNames, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(90 * 24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, csr.PublicKey, s.key)
	if err != nil {
		return nil, err
	}
	return &certmagic.IssuedCertificate{Certificate: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}, nil
}

func TestWildcardAndOnDemand(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	m, err := New(Options{Dir: t.TempDir(), Domain: "Example.com", CloudflareToken: "tok", Issuers: []certmagic.Issuer{selfIssuer{key}},
		Allow: func(_ context.Context, host string) error {
			if host == "sub.other.net" {
				return nil
			}
			return errors.New("no")
		}})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()
	if got := m.Managed(); len(got) != 2 || got[0] != "example.com" || got[1] != "*.example.com" {
		t.Fatalf("managed %v", got)
	}
	if err := m.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	tc := m.TLSConfig()
	get := func(name string) (*tls.Certificate, error) {
		return tc.GetCertificate(&tls.ClientHelloInfo{ServerName: name, SupportedProtos: []string{"h2"}})
	}
	for _, name := range []string{"example.com", "www.example.com", "sub.other.net"} {
		c, err := get(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := c.Leaf.VerifyHostname(name); err != nil {
			t.Fatalf("%s served %v: %v", name, c.Leaf.DNSNames, err)
		}
	}
	if _, err := get("evil.net"); err == nil {
		t.Fatal("disallowed host got a certificate")
	}
	if tc.NextProtos[0] != "h2" {
		t.Fatalf("protos %v", tc.NextProtos)
	}
}

func TestWWWCoversApex(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	m, err := New(Options{Dir: t.TempDir(), Domain: "www.example.com", CloudflareToken: "tok", Issuers: []certmagic.Issuer{selfIssuer{key}}})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()
	if got := m.Managed(); len(got) != 2 || got[0] != "example.com" || got[1] != "*.example.com" {
		t.Fatalf("managed %v", got)
	}
	if err := m.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	tc := m.TLSConfig()
	for _, name := range []string{"www.example.com", "example.com", "sub.example.com"} {
		if _, err := tc.GetCertificate(&tls.ClientHelloInfo{ServerName: name}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestApexAndDeepHosts(t *testing.T) {
	for host, want := range map[string]string{"www.example.com": "example.com", "example.com": "example.com", "a.b.example.co.uk": "example.co.uk", "localhost": "localhost"} {
		if got := Apex(host); got != want {
			t.Errorf("Apex(%s) = %s, want %s", host, got, want)
		}
	}
	m, _ := New(Options{Dir: t.TempDir(), Domain: "my.panel.example.com", CloudflareToken: "tok", Issuers: []certmagic.Issuer{}})
	defer m.Stop()
	if got := m.Managed(); len(got) != 3 || got[2] != "my.panel.example.com" {
		t.Fatalf("deep host managed %v", got)
	}
}

func TestHTTPOnlyManagesPanelHost(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	m, err := New(Options{Dir: t.TempDir(), Domain: "panel.example.com", Issuers: []certmagic.Issuer{selfIssuer{key}}})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()
	if got := m.Managed(); len(got) != 1 || got[0] != "panel.example.com" {
		t.Fatalf("managed %v", got)
	}
}
