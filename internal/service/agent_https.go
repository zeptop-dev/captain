package service

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/zeptop-dev/captain/internal/agenthttps"
	"github.com/zeptop-dev/captain/internal/store"
)

// AgentHTTPS connects the existing certificate store to one dedicated listener.
// Certificate parsing and TLS handshakes never query SQLite.
type AgentHTTPS struct {
	Store      *store.Store
	manager    *agenthttps.Manager
	pool       *agenthttps.Pool
	multi      bool
	defaultURL string
	mu         sync.Mutex
	running    bool
	closed     bool
	reserved   []string
}

func NewAgentHTTPS(st *store.Store, handler http.Handler, log *slog.Logger, reserved ...string) *AgentHTTPS {
	return &AgentHTTPS{Store: st, manager: agenthttps.New(handler, log), pool: agenthttps.NewPool(handler), reserved: reserved}
}

// ValidateConfig reserves the website and HTTP challenge ports even if those
// listeners have not bound yet. Re-checking on startup also covers config.yaml
// changes made after an entry was saved.
func (s *AgentHTTPS) ValidateConfig(config agenthttps.Config) error {
	clean, err := agenthttps.Normalize(config)
	if err != nil || !clean.Enabled {
		return err
	}
	_, port, _ := net.SplitHostPort(clean.Listen)
	for _, addr := range s.reserved {
		_, reserved, err := net.SplitHostPort(addr)
		n, _ := strconv.Atoi(reserved)
		if err == nil && strconv.Itoa(n) == port {
			return errors.New("Agent HTTPS must use a different port from the website and its HTTP challenge listener")
		}
	}
	return nil
}

func (s *AgentHTTPS) Run(ctx context.Context) {
	s.mu.Lock()
	if s.running || s.closed {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	defer func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Close(shutdown)
	}()
	for {
		// The lock also orders reset against the snapshot being applied. It is
		// released before waiting; no network fetch occurs inside Refresh.
		s.Store.Accounts.RLock()
		s.Refresh(ctx)
		s.Store.Accounts.RUnlock()
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// Refresh is called with Store.Accounts already held by HTTP handlers or Run.
// It must not recursively acquire that RWMutex while a reset is waiting.
func (s *AgentHTTPS) Refresh(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running || s.closed {
		return
	}
	entries, certs, loadErr := s.Store.AgentEndpointCertificates(ctx)
	if loadErr != nil {
		s.manager.Fail(loadErr)
		return
	}
	if len(entries) > 0 || s.multi {
		candidateDefault := ""
		materials := []agenthttps.EntryMaterial{}
		for _, e := range entries {
			if err := s.ValidateConfig(e.Config); err != nil {
				s.manager.Fail(err)
				return
			}
			if e.Default {
				candidateDefault = e.URL
			}
			if e.Enabled {
				c := certs[e.CertificateID]
				materials = append(materials, agenthttps.EntryMaterial{Config: e.Config, Certificate: []byte(c.CertPEM), Key: []byte(c.KeyPEM)})
			}
		}
		if err := s.pool.Apply(ctx, materials); err != nil {
			s.manager.Fail(err)
			return
		}
		s.multi = true
		s.defaultURL = candidateDefault
		return
	}
	v, cert, err := s.Store.AgentHTTPSnapshot(ctx)
	if err != nil {
		s.manager.Fail(err)
		return
	}
	if err := s.ValidateConfig(v.Config); err != nil {
		s.manager.Fail(err)
		return
	}
	var chain, key []byte
	if cert != nil {
		chain, key = []byte(cert.CertPEM), []byte(cert.KeyPEM)
	}
	_ = s.manager.Apply(ctx, v.Config, chain, key)
}

func (s *AgentHTTPS) Status() agenthttps.Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.multi {
		return s.pool.Status(s.defaultURL)
	}
	return s.manager.Status()
}
func (s *AgentHTTPS) EntryStatus(url string) agenthttps.Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.multi {
		return s.pool.Status(url)
	}
	return s.manager.Status()
}

// DeleteCertificate protects both desired intent and a retained old listener
// after a failed switch. Sharing Refresh's mutex prevents a loaded old snapshot
// from becoming active between this check and the database deletion.
func (s *AgentHTTPS) DeleteCertificate(ctx context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id > 0 && (s.manager.Status().CertificateID == id || s.pool.CertificateInUse(id)) {
		return store.ErrCertificateInUse
	}
	return s.Store.DeleteCertificate(ctx, id)
}

// Ready only confirms this process's current listener and certificate. It does
// not claim that Docker publishing, a firewall, a CDN or a node can reach it.
func (s *AgentHTTPS) Ready(v store.AgentHTTPSSettings) bool {
	status := s.Status()
	return v.Enabled && status.State == "listening" && status.ActiveURL == v.URL &&
		status.ActiveListen == v.Listen && status.CertificateID == v.CertificateID
}

func (s *AgentHTTPS) Close(ctx context.Context) error {
	s.mu.Lock()
	s.closed, s.running = true, false
	s.mu.Unlock()
	_ = s.pool.Close(ctx)
	return s.manager.Close(ctx)
}
