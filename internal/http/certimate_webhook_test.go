package http

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCertimateWebhookDisplayedTemplate(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	const token = "synthetic-certimate-hook"
	if err := r.st.SetSetting(ctx, "cert_webhook", map[string]string{"token": token}); err != nil {
		t.Fatal(err)
	}

	// Exercise the actual template imported by DomainsPage, with Certimate's
	// JSON-value replacement contract: decode, replace values, then encode.
	// Its current provider variables are deliberately not legacy aliases.
	template, err := os.ReadFile(filepath.Join("..", "..", "web", "admin", "src", "lib", "certimate-webhook.json"))
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]string
	if err = json.Unmarshal(template, &body); err != nil {
		t.Fatal(err)
	}
	fullchain, key := certimateChain(t)
	values := map[string]string{
		"${CERTIMATE_DEPLOYER_SUBJECTALTNAMES}": "example.com;*.example.com",
		"${CERTIMATE_DEPLOYER_CERTIFICATE}":     fullchain,
		"${CERTIMATE_DEPLOYER_PRIVATEKEY}":      key,
	}
	for field, variable := range body {
		value, ok := values[variable]
		if !ok {
			t.Fatalf("template uses an unexpected Certimate variable in %s", field)
		}
		body[field] = value
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`\n`)) {
		t.Fatal("PEM newlines were not JSON-encoded")
	}
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequest(http.MethodPost, r.srv.URL+"/api/hooks/certificate?token="+token, bytes.NewReader(encoded))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := r.srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		response, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("Certimate body rejected: status=%d read=%v", resp.StatusCode, readErr)
		}
		if bytes.Contains(response, []byte("PRIVATE KEY")) || bytes.Contains(response, []byte("BEGIN CERTIFICATE")) {
			t.Fatal("webhook response exposed PEM material")
		}
		certificates, err := r.st.ListCertificates(ctx)
		if err != nil || len(certificates) != 1 {
			t.Fatalf("renewal did not upsert one certificate: count=%d err=%v", len(certificates), err)
		}
		c := certificates[0]
		if c.Domain != "example.com" || !reflect.DeepEqual(c.Names, []string{"example.com", "*.example.com"}) || c.Renewals != attempt {
			t.Fatal("semicolon-separated SANs or renewal identity changed")
		}
		if c.CertPEM != fullchain || c.KeyPEM != key || strings.Count(c.CertPEM, "BEGIN CERTIFICATE") != 2 {
			t.Fatal("complete chain or private key was not preserved")
		}
		if !c.Covers("node.example.com") || c.Covers("node.other.example.com") {
			t.Fatal("certificate SAN coverage changed")
		}
	}
}

func certimateChain(t *testing.T) (string, string) {
	t.Helper()
	issuerKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	issuer := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Example CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "example.com"},
		DNSNames: []string{"example.com", "*.example.com"}, NotBefore: issuer.NotBefore, NotAfter: issuer.NotAfter,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, KeyUsage: x509.KeyUsageDigitalSignature}
	issuerDER, err := x509.CreateCertificate(rand.Reader, issuer, issuer, &issuerKey.PublicKey, issuerKey)
	if err != nil {
		t.Fatal(err)
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, issuer, &key.PublicKey, issuerKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	chain := append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: issuerDER})...)
	return string(chain), string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
}
