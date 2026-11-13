// Package agenthttps owns the optional Agent-only HTTPS listener. It does not
// issue certificates, access storage or create application/background services.
package agenthttps

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zeptop-dev/captain/internal/agentendpoint"
)

// Config selects one existing certificate and a published HTTPS origin. The
// listener port must equal the public port; deployment owns container mapping.
type Config struct {
	Enabled       bool   `json:"enabled"`
	URL           string `json:"url"`
	Listen        string `json:"listen"`
	CertificateID int64  `json:"certificate_id"`
}

// Normalize checks configuration without opening a socket or reading a key.
// Disabled configurations may be incomplete so the entry can always be stopped.
func Normalize(c Config) (Config, error) {
	if !c.Enabled {
		return c, nil
	}
	origin, err := agentendpoint.NormalizeURL(c.URL)
	if err != nil || origin == "" {
		return c, errors.New("agent HTTPS URL must be a valid HTTPS origin")
	}
	c.URL = origin
	if c.CertificateID <= 0 {
		return c, errors.New("agent HTTPS certificate is required")
	}
	u, _ := url.Parse(origin)
	port := u.Port()
	if port == "" {
		port = "443"
	}
	// Canonicalize ports so different spellings cannot accidentally trigger a
	// replacement bind for a socket which is already held by this manager.
	n, _ := strconv.Atoi(port)
	port = strconv.Itoa(n)
	if u.Port() != "" {
		c.URL = "https://" + net.JoinHostPort(u.Hostname(), port)
	}
	c.Listen = strings.TrimSpace(c.Listen)
	if c.Listen == "" {
		c.Listen = ":" + port
	}
	host, listenPort, err := net.SplitHostPort(c.Listen)
	if err != nil {
		return c, errors.New("agent HTTPS listen must be an IP address and port, or :port")
	}
	if host != "" {
		ip, err := netip.ParseAddr(host)
		if err != nil || ip.Zone() != "" {
			return c, errors.New("agent HTTPS listen host must be an IP address without a zone")
		}
		host = ip.String()
	}
	p, err := strconv.Atoi(listenPort)
	if err != nil || p != n {
		return c, errors.New("agent HTTPS listen port must match the URL port")
	}
	c.Listen = net.JoinHostPort(host, port)
	return c, nil
}

// Validate checks the actual leaf certificate and key, not stored name metadata.
// Chain trust is checked by connecting clients; this allows private PKI too.
func Validate(c Config, certPEM, keyPEM []byte) error {
	_, err := prepare(c, certPEM, keyPEM, time.Now())
	return err
}

type material struct {
	config Config
	host   string
	cert   *tls.Certificate
	tls    *tls.Config
	digest [32]byte
}

func prepare(c Config, certPEM, keyPEM []byte, now time.Time) (*material, error) {
	c, err := Normalize(c)
	if err != nil {
		return nil, err
	}
	if !c.Enabled {
		return &material{config: c}, nil
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, errors.New("agent HTTPS certificate and key are not a valid pair")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, errors.New("agent HTTPS certificate cannot be parsed")
	}
	pair.Leaf = leaf
	u, _ := url.Parse(c.URL)
	if err := leaf.VerifyHostname(u.Hostname()); err != nil {
		return nil, errors.New("agent HTTPS certificate does not cover the URL hostname")
	}
	if err := validAt(leaf, now); err != nil {
		return nil, err
	}
	if len(leaf.ExtKeyUsage) > 0 {
		server := false
		for _, usage := range leaf.ExtKeyUsage {
			server = server || usage == x509.ExtKeyUsageServerAuth || usage == x509.ExtKeyUsageAny
		}
		if !server {
			return nil, errors.New("agent HTTPS certificate does not allow server authentication")
		}
	}
	return &material{config: c, host: u.Hostname(), cert: &pair, digest: materialDigest(certPEM, keyPEM), tls: &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{pair},
		// ServeTLS configures HTTP/2 on the outer config. The selected config
		// must advertise the same protocols after GetConfigForClient runs.
		NextProtos: []string{"h2", "http/1.1"},
	}}, nil
}

func materialDigest(certPEM, keyPEM []byte) [32]byte {
	h := sha256.New()
	_, _ = h.Write(certPEM)
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(keyPEM)
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

func validAt(leaf *x509.Certificate, now time.Time) error {
	if now.Before(leaf.NotBefore) {
		return errors.New("agent HTTPS certificate is not valid yet")
	}
	if !now.Before(leaf.NotAfter) {
		return errors.New("agent HTTPS certificate has expired")
	}
	return nil
}

// Status describes local listener evidence only, never public reachability.
// Active fields describe the retained listener even if a new Apply failed.
type Status struct {
	State               string    `json:"state"`
	ActiveURL           string    `json:"active_url,omitempty"`
	ActiveListen        string    `json:"active_listen,omitempty"`
	CertificateID       int64     `json:"certificate_id,omitempty"`
	CertificateNotAfter time.Time `json:"certificate_not_after,omitzero"`
	LastRefresh         time.Time `json:"last_refresh,omitzero"`
	LastError           string    `json:"last_error,omitempty"`
}

type endpoint struct {
	current atomic.Pointer[material]
	closing atomic.Bool
	server  *http.Server
	cancel  context.CancelFunc
}

// Manager serializes local reconfiguration while handshakes use an immutable
// in-memory certificate. No handshake queries SQLite or invokes an issuer.
type Manager struct {
	applyMu sync.Mutex
	mu      sync.RWMutex
	handler http.Handler
	log     *slog.Logger
	active  *endpoint
	drains  map[*endpoint]struct{}
	closed  bool
	last    time.Time
	err     string
	now     func() time.Time
}

func New(handler http.Handler, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	if handler == nil {
		handler = http.NotFoundHandler()
	}
	return &Manager{handler: handler, log: logger, drains: make(map[*endpoint]struct{}), now: time.Now}
}

// Apply atomically replaces a valid certificate or starts a replacement socket
// before retiring the old listener. Errors leave the prior valid entry serving.
// Old in-flight requests receive a bounded drain period without blocking Apply.
func (m *Manager) Apply(ctx context.Context, c Config, certPEM, keyPEM []byte) error {
	m.applyMu.Lock()
	defer m.applyMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.RLock()
	closed := m.closed
	m.mu.RUnlock()
	if closed {
		return errors.New("agent HTTPS manager is closed")
	}
	normalized, err := Normalize(c)
	if err != nil {
		m.Fail(err)
		return err
	}
	m.mu.Lock()
	if m.active != nil && normalized.Enabled {
		current := m.active.current.Load()
		if current.config == normalized && current.digest == materialDigest(certPEM, keyPEM) {
			if err := validAt(current.cert.Leaf, m.now()); err != nil {
				m.err = err.Error()
				m.mu.Unlock()
				return err
			}
			m.err, m.last = "", m.now()
			m.mu.Unlock()
			return nil
		}
	}
	m.mu.Unlock()
	data, err := prepare(normalized, certPEM, keyPEM, m.now())
	if err != nil {
		m.Fail(err)
		return err
	}
	m.mu.Lock()
	old := m.active
	if !data.config.Enabled {
		m.active, m.err, m.last = nil, "", m.now()
		m.retireLocked(old)
		m.mu.Unlock()
		return nil
	}
	if old != nil && old.current.Load().config.Listen == data.config.Listen {
		old.current.Store(data)
		m.err, m.last = "", m.now()
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()
	lc := net.ListenConfig{}
	ln, err := lc.Listen(ctx, "tcp", data.config.Listen)
	if err != nil {
		err = fmt.Errorf("agent HTTPS could not listen on %s: %w", data.config.Listen, err)
		m.Fail(err)
		return err
	}
	if err := ctx.Err(); err != nil {
		_ = ln.Close()
		return err
	}
	base, cancel := context.WithCancel(context.Background())
	e := &endpoint{cancel: cancel}
	e.current.Store(data)
	e.server = &http.Server{
		Addr:              data.config.Listen,
		BaseContext:       func(net.Listener) context.Context { return base },
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          slog.NewLogLogger(m.log.Handler(), slog.LevelDebug),
		TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12, GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			current := e.current.Load()
			if e.closing.Load() {
				return nil, errors.New("agent HTTPS entry is closing")
			}
			if err := validAt(current.cert.Leaf, m.now()); err != nil {
				return nil, err
			}
			if hello.ServerName == "" {
				if _, err := netip.ParseAddr(current.host); err != nil {
					return nil, errors.New("agent HTTPS requires the configured server name")
				}
			} else if !sameHost(hello.ServerName, current.host) {
				return nil, errors.New("agent HTTPS server name does not match")
			}
			return current.tls, nil
		}},
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			current := e.current.Load()
			if e.closing.Load() || validAt(current.cert.Leaf, m.now()) != nil {
				http.Error(w, "agent HTTPS entry unavailable", http.StatusServiceUnavailable)
				return
			}
			host := r.Host
			if h, _, err := net.SplitHostPort(host); err == nil {
				host = h
			} else {
				host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
			}
			if !sameHost(host, current.host) {
				http.Error(w, "agent HTTPS host does not match", http.StatusMisdirectedRequest)
				return
			}
			m.handler.ServeHTTP(w, r)
		}),
	}
	m.mu.Lock()
	m.active, m.err, m.last = e, "", m.now()
	m.retireLocked(old)
	m.mu.Unlock()
	go func() {
		err := e.server.ServeTLS(ln, "", "")
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return
		}
		cancel()
		_ = e.server.Close()
		m.mu.Lock()
		if m.active == e {
			m.active = nil
			m.err = "agent HTTPS listener stopped unexpectedly"
		}
		m.mu.Unlock()
		m.log.Error("agent HTTPS listener stopped", "err", err)
	}()
	return nil
}

func sameHost(a, b string) bool {
	a, b = strings.TrimSuffix(a, "."), strings.TrimSuffix(b, ".")
	if ip, err := netip.ParseAddr(a); err == nil {
		other, err := netip.ParseAddr(b)
		return err == nil && ip.Unmap() == other.Unmap()
	}
	return strings.EqualFold(a, b)
}

// retireLocked marks a listener unavailable immediately. Shutdown waits happen
// in a separate goroutine, so a long poll cannot hold the manager's locks.
func (m *Manager) retireLocked(e *endpoint) {
	if e == nil {
		return
	}
	e.closing.Store(true)
	m.drains[e] = struct{}{}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := e.server.Shutdown(ctx); err != nil {
			_ = e.server.Close()
		}
		m.mu.Lock()
		e.cancel()
		delete(m.drains, e)
		m.mu.Unlock()
	}()
}

// Fail records a settings/certificate loading failure without discarding the
// last valid listener. Callers should pass a sanitized, non-secret error.
func (m *Manager) Fail(err error) {
	if err == nil {
		return
	}
	m.mu.Lock()
	if !m.closed {
		m.err = err.Error()
	}
	m.mu.Unlock()
}

func (m *Manager) Status() Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s := Status{State: "disabled", LastRefresh: m.last, LastError: m.err}
	if m.active != nil {
		data := m.active.current.Load()
		s.State, s.ActiveURL, s.ActiveListen = "listening", data.config.URL, data.config.Listen
		s.CertificateID, s.CertificateNotAfter = data.config.CertificateID, data.cert.Leaf.NotAfter
		if err := validAt(data.cert.Leaf, m.now()); err != nil {
			s.LastError = err.Error()
		}
	}
	if s.LastError != "" {
		s.State = "error"
	}
	return s
}

// Close permanently stops the manager. The caller controls the drain deadline;
// expiry force-closes sockets, and a later Apply cannot reopen the listener.
func (m *Manager) Close(ctx context.Context) error {
	m.applyMu.Lock()
	m.mu.Lock()
	m.closed = true
	if m.active != nil {
		m.active.closing.Store(true)
		m.active.cancel()
		m.drains[m.active] = struct{}{}
		m.active = nil
	}
	endpoints := make([]*endpoint, 0, len(m.drains))
	for e := range m.drains {
		e.cancel()
		endpoints = append(endpoints, e)
	}
	m.err = ""
	m.mu.Unlock()
	m.applyMu.Unlock()
	var wg sync.WaitGroup
	failures := make(chan error, len(endpoints))
	for _, e := range endpoints {
		wg.Go(func() {
			// Permanent stop has already cancelled request contexts. Closing the
			// connections also avoids HTTP/2's graceful GOAWAY timer consuming an
			// otherwise idle short shutdown deadline. Retirement still drains separately.
			_ = e.server.Close()
			if err := e.server.Shutdown(ctx); err != nil {
				failures <- err
				_ = e.server.Close()
			}
			m.mu.Lock()
			e.cancel()
			delete(m.drains, e)
			m.mu.Unlock()
		})
	}
	wg.Wait()
	close(failures)
	var errs []error
	for err := range failures {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}
