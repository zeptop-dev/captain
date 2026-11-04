package agenthttps

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
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fixtureCert struct {
	cert, key []byte
	leaf      *x509.Certificate
}

func certificate(t *testing.T, host string, serial int64, before, after time.Time) fixtureCert {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: host}, NotBefore: before, NotAfter: after, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	if ip := net.ParseIP(host); ip != nil {
		tpl.IPAddresses = []net.IP{ip}
	} else {
		tpl.DNSNames = []string{host}
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return fixtureCert{pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), leaf}
}
func manager(t *testing.T, h http.Handler) *Manager {
	t.Helper()
	m := New(h, slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = m.Close(ctx)
	})
	return m
}

// Retain every socket in the requested batch until configurations are complete.
func configurations(t *testing.T, count int) ([]Config, func()) {
	t.Helper()
	var listeners []net.Listener
	var out []Config
	for range count {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		listeners = append(listeners, ln)
		_, port, _ := net.SplitHostPort(ln.Addr().String())
		out = append(out, Config{Enabled: true, URL: "https://agent.example.com:" + port, Listen: ln.Addr().String(), CertificateID: 1})
	}
	release := func() {
		for _, ln := range listeners {
			_ = ln.Close()
		}
	}
	t.Cleanup(release)
	return out, release
}
func transport(c Config, certs ...fixtureCert) *http.Transport {
	roots := x509.NewCertPool()
	for _, cert := range certs {
		roots.AddCert(cert.leaf)
	}
	return &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, ForceAttemptHTTP2: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", c.Listen)
	}}
}
func fetch(t *testing.T, tr *http.Transport, c Config, host string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, c.URL+"/api/agent/state", nil)
	if err != nil {
		t.Fatal(err)
	}
	if host != "" {
		req.Host = host
	}
	res, err := (&http.Client{Transport: tr, Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res, string(body)
}
func assertServing(t *testing.T, m *Manager, c Config, cert fixtureCert, serial int64) {
	t.Helper()
	tr := transport(c, cert)
	defer tr.CloseIdleConnections()
	res, body := fetch(t, tr, c, "")
	if res.StatusCode != 200 || body != "agent" {
		t.Fatalf("response %d %q", res.StatusCode, body)
	}
	if got := res.TLS.PeerCertificates[0].SerialNumber.Int64(); got != serial {
		t.Fatalf("certificate serial %d want %d", got, serial)
	}
	if s := m.Status(); s.ActiveURL != c.URL {
		t.Fatalf("wrong active URL: %+v", s)
	}
}
func TestTLSHotReloadPreservesValidListenerOnBadUpdate(t *testing.T) {
	cs, release := configurations(t, 1)
	c := cs[0]
	release()
	now := time.Now()
	first := certificate(t, "agent.example.com", 1, now.Add(-time.Hour), now.Add(time.Hour))
	second := certificate(t, "agent.example.com", 2, now.Add(-time.Hour), now.Add(2*time.Hour))
	m := manager(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "agent") }))
	if err := m.Apply(context.Background(), c, first.cert, first.key); err != nil {
		t.Fatal(err)
	}
	assertServing(t, m, c, first, 1)
	pointer := m.active.current.Load()
	if err := m.Apply(context.Background(), c, first.cert, first.key); err != nil {
		t.Fatal(err)
	}
	if pointer != m.active.current.Load() {
		t.Fatal("identical refresh reparsed certificate")
	}
	if err := m.Apply(context.Background(), c, second.cert, second.key); err != nil {
		t.Fatal(err)
	}
	assertServing(t, m, c, second, 2)
	if err := m.Apply(context.Background(), c, second.cert, first.key); err == nil {
		t.Fatal("mismatched key accepted")
	}
	if s := m.Status(); s.State != "error" || s.LastError == "" {
		t.Fatalf("failed apply reported ready: %+v", s)
	}
	assertServing(t, m, c, second, 2)
	m.Fail(errors.New("certificate temporarily unavailable"))
	assertServing(t, m, c, second, 2)
	if err := m.Apply(context.Background(), c, second.cert, second.key); err != nil {
		t.Fatal(err)
	}
	if m.Status().State != "listening" {
		t.Fatal(m.Status())
	}
}
func TestTLSHostBoundaryAndHTTP2(t *testing.T) {
	cs, release := configurations(t, 1)
	c := cs[0]
	release()
	now := time.Now()
	cert := certificate(t, "agent.example.com", 1, now.Add(-time.Hour), now.Add(time.Hour))
	m := manager(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "agent") }))
	if err := m.Apply(context.Background(), c, cert.cert, cert.key); err != nil {
		t.Fatal(err)
	}
	tr := transport(c, cert)
	defer tr.CloseIdleConnections()
	res, _ := fetch(t, tr, c, "")
	if res.ProtoMajor != 2 {
		t.Fatalf("HTTP/2 not negotiated: %s", res.Proto)
	}
	res, _ = fetch(t, tr, c, "other.example.com")
	if res.StatusCode != 421 {
		t.Fatalf("wrong Host admitted: %d", res.StatusCode)
	}
	res, _ = fetch(t, tr, c, "agent.example.com")
	if res.StatusCode != 200 {
		t.Fatalf("CDN host without port refused: %d", res.StatusCode)
	}
	for _, name := range []string{"other.example.com", ""} {
		conn, err := tls.DialWithDialer(&net.Dialer{Timeout: time.Second}, "tcp", c.Listen, &tls.Config{InsecureSkipVerify: true, ServerName: name, MinVersion: tls.VersionTLS12})
		if err == nil { // Reading forces the server alert to arrive even for TLS 1.3's asynchronous final flight.
			_, err = conn.Write([]byte("GET /api/agent/state HTTP/1.1\r\nHost: agent.example.com\r\n\r\n"))
			if err == nil {
				b := make([]byte, 1)
				_ = conn.SetReadDeadline(time.Now().Add(time.Second))
				_, err = conn.Read(b)
			}
			_ = conn.Close()
		}
		if err == nil {
			t.Fatalf("SNI %q admitted", name)
		}
	}
}
func TestIPOriginDoesNotRequireSNI(t *testing.T) {
	cs, release := configurations(t, 1)
	c := cs[0]
	release()
	u, _ := url.Parse(c.URL)
	c.URL = "https://" + net.JoinHostPort("127.0.0.1", u.Port())
	now := time.Now()
	cert := certificate(t, "127.0.0.1", 1, now.Add(-time.Hour), now.Add(time.Hour))
	m := manager(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "agent") }))
	if err := m.Apply(context.Background(), c, cert.cert, cert.key); err != nil {
		t.Fatal(err)
	}
	assertServing(t, m, c, cert, 1)
}
func TestExpiredCertificateRejectsNewAndExistingConnections(t *testing.T) {
	cs, release := configurations(t, 1)
	c := cs[0]
	release()
	now := time.Now().UTC().Truncate(time.Second)
	cert := certificate(t, "agent.example.com", 1, now.Add(-time.Hour), now.Add(time.Hour))
	var clock atomic.Int64
	clock.Store(now.UnixNano())
	m := manager(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "agent") }))
	m.now = func() time.Time { return time.Unix(0, clock.Load()) }
	if err := m.Apply(context.Background(), c, cert.cert, cert.key); err != nil {
		t.Fatal(err)
	}
	tr := transport(c, cert)
	defer tr.CloseIdleConnections()
	fetch(t, tr, c, "")
	clock.Store(now.Add(time.Hour).UnixNano())
	if s := m.Status(); s.State != "error" || !strings.Contains(s.LastError, "expired") {
		t.Fatal(s)
	}
	res, _ := fetch(t, tr, c, "")
	if res.StatusCode != 503 {
		t.Fatalf("existing connection still usable after expiry: %d", res.StatusCode)
	}
	fresh := transport(c, cert)
	defer fresh.CloseIdleConnections()
	if res, err := (&http.Client{Transport: fresh, Timeout: time.Second}).Get(c.URL); err == nil {
		res.Body.Close()
		t.Fatal("expired certificate allowed a new handshake")
	}
	if err := m.Apply(context.Background(), c, cert.cert, cert.key); err == nil {
		t.Fatal("identical expired certificate refresh succeeded")
	}
}
func TestRebindFailurePreservesOldAndDisableNeedsNoCertificate(t *testing.T) {
	cs, release := configurations(t, 2)
	now := time.Now()
	cert := certificate(t, "agent.example.com", 1, now.Add(-time.Hour), now.Add(time.Hour))
	release()
	m := manager(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "agent") }))
	if err := m.Apply(context.Background(), cs[0], cert.cert, cert.key); err != nil {
		t.Fatal(err)
	}
	busy, err := net.Listen("tcp", cs[1].Listen)
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	if err = m.Apply(context.Background(), cs[1], cert.cert, cert.key); err == nil {
		t.Fatal("occupied listener accepted")
	}
	assertServing(t, m, cs[0], cert, 1)
	busy.Close()
	if err = m.Apply(context.Background(), cs[1], cert.cert, cert.key); err != nil {
		t.Fatal(err)
	}
	assertServing(t, m, cs[1], cert, 1)
	if err = m.Apply(context.Background(), Config{URL: "invalid", CertificateID: -1}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if s := m.Status(); s.State != "disabled" || s.ActiveURL != "" {
		t.Fatal(s)
	}
}
func TestCloseCancelsLongRequestAndRefusesReopen(t *testing.T) {
	cs, release := configurations(t, 1)
	c := cs[0]
	release()
	now := time.Now()
	cert := certificate(t, "agent.example.com", 1, now.Add(-time.Hour), now.Add(time.Hour))
	entered, exited := make(chan struct{}), make(chan struct{})
	m := manager(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done(); close(exited) }))
	if err := m.Apply(context.Background(), c, cert.cert, cert.key); err != nil {
		t.Fatal(err)
	}
	tr := transport(c, cert)
	defer tr.CloseIdleConnections()
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		res, err := (&http.Client{Transport: tr, Timeout: 3 * time.Second}).Get(c.URL)
		if err == nil {
			res.Body.Close()
		}
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("request not entered")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := m.Close(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("request context not canceled")
	}
	<-requestDone
	if err := m.Apply(context.Background(), c, cert.cert, cert.key); err == nil {
		t.Fatal("closed manager reopened")
	}
}
func TestNormalizeAndCertificateValidation(t *testing.T) {
	base := Config{Enabled: true, URL: "https://agent.example.com:2096", CertificateID: 1}
	c, err := Normalize(base)
	if err != nil || c.Listen != ":2096" {
		t.Fatalf("%+v %v", c, err)
	}
	for name, edit := range map[string]func(*Config){"http": func(c *Config) { c.URL = "http://agent.example.com:2096" }, "path": func(c *Config) { c.URL += "/api" }, "no certificate": func(c *Config) { c.CertificateID = 0 }, "wrong port": func(c *Config) { c.Listen = ":2097" }, "DNS listen": func(c *Config) { c.Listen = "localhost:2096" }, "zone": func(c *Config) { c.Listen = "[fe80::1%en0]:2096" }, "zero": func(c *Config) { c.URL = "https://agent.example.com:0" }} {
		t.Run(name, func(t *testing.T) {
			c := base
			edit(&c)
			if _, err := Normalize(c); err == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
	now := time.Now()
	for _, test := range []struct {
		name, host    string
		before, after time.Time
	}{{"wrong hostname", "other.example.com", now.Add(-time.Hour), now.Add(time.Hour)}, {"expired", "agent.example.com", now.Add(-2 * time.Hour), now.Add(-time.Hour)}, {"not yet valid", "agent.example.com", now.Add(time.Hour), now.Add(2 * time.Hour)}} {
		t.Run(test.name, func(t *testing.T) {
			cert := certificate(t, test.host, 1, test.before, test.after)
			if err := Validate(base, cert.cert, cert.key); err == nil {
				t.Fatal("bad certificate accepted")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m := manager(t, nil)
	if err := m.Apply(ctx, base, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if m.Status().State != "disabled" {
		t.Fatal(m.Status())
	}
}

func TestReconfigurationDoesNotWaitForDrainingRequest(t *testing.T) {
	cs, release := configurations(t, 2)
	release()
	now := time.Now()
	cert := certificate(t, "agent.example.com", 1, now.Add(-time.Hour), now.Add(time.Hour))
	entered, finish := make(chan struct{}), make(chan struct{})
	var once atomic.Bool
	m := manager(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if once.CompareAndSwap(false, true) {
			close(entered)
			select {
			case <-finish:
			case <-r.Context().Done():
				t.Error("draining request canceled before grace period")
			}
		}
		_, _ = io.WriteString(w, "agent")
	}))
	if err := m.Apply(context.Background(), cs[0], cert.cert, cert.key); err != nil {
		t.Fatal(err)
	}
	tr := transport(cs[0], cert)
	defer tr.CloseIdleConnections()
	done := make(chan error, 1)
	go func() {
		res, err := (&http.Client{Transport: tr, Timeout: 3 * time.Second}).Get(cs[0].URL)
		if err == nil {
			body, readErr := io.ReadAll(res.Body)
			res.Body.Close()
			if readErr != nil {
				err = readErr
			}
			if string(body) != "agent" {
				err = errors.New("in-flight response lost")
			}
		}
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("request not entered")
	}
	applied := make(chan error, 1)
	go func() { applied <- m.Apply(context.Background(), cs[1], cert.cert, cert.key) }()
	select {
	case err := <-applied:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		close(finish)
		t.Fatal("Apply blocked on previous request")
	}
	assertServing(t, m, cs[1], cert, 1)
	close(finish)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestCloseDeadlineForceClosesConnection(t *testing.T) {
	cs, release := configurations(t, 1)
	release()
	now := time.Now()
	cert := certificate(t, "agent.example.com", 1, now.Add(-time.Hour), now.Add(time.Hour))
	entered, finish, exited := make(chan struct{}), make(chan struct{}), make(chan struct{})
	m := manager(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-finish; close(exited) }))
	if err := m.Apply(context.Background(), cs[0], cert.cert, cert.key); err != nil {
		t.Fatal(err)
	}
	tr := transport(cs[0], cert)
	defer tr.CloseIdleConnections()
	done := make(chan error, 1)
	go func() {
		res, err := (&http.Client{Transport: tr, Timeout: 3 * time.Second}).Get(cs[0].URL)
		if err == nil {
			res.Body.Close()
		}
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("request not entered")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := m.Close(ctx)
	close(finish)
	<-exited
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close deadline: %v", err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("connection was not forced closed")
		}
	case <-time.After(time.Second):
		t.Fatal("request socket left open")
	}
}

func TestExpiredCertificateCannotResumeTLSSession(t *testing.T) {
	cs, release := configurations(t, 1)
	c := cs[0]
	release()
	now := time.Now().UTC().Truncate(time.Second)
	cert := certificate(t, "agent.example.com", 1, now.Add(-time.Hour), now.Add(time.Hour))
	var clock atomic.Int64
	clock.Store(now.UnixNano())
	m := manager(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "agent") }))
	m.now = func() time.Time { return time.Unix(0, clock.Load()) }
	if err := m.Apply(context.Background(), c, cert.cert, cert.key); err != nil {
		t.Fatal(err)
	}
	tr := transport(c, cert)
	defer tr.CloseIdleConnections()
	tr.ForceAttemptHTTP2 = false
	tr.DisableKeepAlives = true
	tr.TLSClientConfig.MaxVersion = tls.VersionTLS12
	tr.TLSClientConfig.ClientSessionCache = tls.NewLRUClientSessionCache(1)
	first, _ := fetch(t, tr, c, "")
	if first.TLS.DidResume {
		t.Fatal("first session unexpectedly resumed")
	}
	second, _ := fetch(t, tr, c, "")
	if !second.TLS.DidResume {
		t.Fatal("fixture did not resume TLS session")
	}
	clock.Store(now.Add(time.Hour).UnixNano())
	if res, err := (&http.Client{Transport: tr, Timeout: time.Second}).Get(c.URL); err == nil {
		res.Body.Close()
		t.Fatal("expired certificate resumed a TLS session")
	}
}
