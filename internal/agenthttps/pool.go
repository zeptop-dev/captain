package agenthttps

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type EntryMaterial struct {
	Config           Config
	Certificate, Key []byte
}
type routeSet struct{ hosts map[string]*material }
type routeListener struct {
	requests requestGate
	cancel   context.CancelFunc
	routes   atomic.Pointer[routeSet]
	server   *http.Server
	closing  atomic.Bool
}

// Pool shares a socket between named HTTPS origins. All routes are validated
// before any current listener is changed; failed binds retain the old routes.
type Pool struct {
	mu        sync.Mutex
	handler   http.Handler
	listeners map[string]*routeListener
	statuses  map[string]Status
	lastError string
	closed    bool
}

func NewPool(handler http.Handler) *Pool {
	return &Pool{handler: handler, listeners: map[string]*routeListener{}, statuses: map[string]Status{}}
}
func hostKey(h string) string {
	if ip, err := netip.ParseAddr(h); err == nil {
		return ip.Unmap().String()
	}
	return strings.ToLower(strings.TrimSuffix(h, "."))
}
func (p *Pool) Apply(ctx context.Context, entries []EntryMaterial) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return errors.New("agent HTTPS pool closed")
	}
	groups := map[string]*routeSet{}
	statuses := map[string]Status{}
	for _, v := range entries {
		m, err := prepare(v.Config, v.Certificate, v.Key, time.Now())
		if err != nil {
			p.lastError = err.Error()
			return err
		}
		if !m.config.Enabled {
			continue
		}
		group := groups[m.config.Listen]
		if group == nil {
			group = &routeSet{hosts: map[string]*material{}}
			groups[m.config.Listen] = group
		}
		k := hostKey(m.host)
		if group.hosts[k] != nil {
			return errors.New("duplicate HTTPS host on listener")
		}
		group.hosts[k] = m
		statuses[m.config.URL] = Status{State: "listening", ActiveURL: m.config.URL, ActiveListen: m.config.Listen, CertificateID: m.config.CertificateID, CertificateNotAfter: m.cert.Leaf.NotAfter, LastRefresh: time.Now()}
	}
	created := map[string]*routeListener{}
	for addr, routes := range groups {
		if p.listeners[addr] != nil {
			continue
		}
		ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
		if err != nil {
			for _, l := range created {
				l.server.Close()
				l.cancel()
			}
			p.lastError = "Agent HTTPS could not bind a requested socket"
			return errors.New(p.lastError)
		}
		base, cancel := context.WithCancel(context.Background())
		l := &routeListener{cancel: cancel}
		l.routes.Store(routes)
		l.server = &http.Server{BaseContext: func(net.Listener) context.Context { return base }, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 16 << 10, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12, GetConfigForClient: func(h *tls.ClientHelloInfo) (*tls.Config, error) {
			if l.closing.Load() {
				return nil, errors.New("entry closing")
			}
			m := l.routes.Load().hosts[hostKey(h.ServerName)]
			if m == nil && h.ServerName == "" {
				localHost, _, _ := net.SplitHostPort(h.Conn.LocalAddr().String())
				var only *material
				count := 0
				for _, v := range l.routes.Load().hosts {
					if net.ParseIP(v.host) != nil {
						only = v
						count++
						if sameHost(v.host, localHost) {
							m = v
							break
						}
					}
				}
				if m == nil && count == 1 {
					m = only
				}
			}
			if m == nil || validAt(m.cert.Leaf, time.Now()) != nil {
				return nil, errors.New("unknown Agent HTTPS server name")
			}
			return m.tls, nil
		}}, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host := r.Host
			if h, _, err := net.SplitHostPort(host); err == nil {
				host = h
			}
			host = strings.Trim(host, "[]")
			m := l.routes.Load().hosts[hostKey(host)]
			sniOK := m != nil && r.TLS != nil && (sameHost(r.TLS.ServerName, m.host) || (r.TLS.ServerName == "" && net.ParseIP(m.host) != nil))
			if l.closing.Load() || m == nil || !sniOK || validAt(m.cert.Leaf, time.Now()) != nil {
				http.Error(w, "Agent HTTPS entry unavailable", http.StatusMisdirectedRequest)
				return
			}
			if !l.requests.begin() {
				http.Error(w, "Agent HTTPS entry closing", http.StatusServiceUnavailable)
				return
			}
			defer l.requests.done()
			p.handler.ServeHTTP(w, r)
		})}
		created[addr] = l
		go func() {
			err := l.server.ServeTLS(ln, "", "")
			if err == nil || errors.Is(err, http.ErrServerClosed) {
				return
			}
			l.cancel()
			_ = l.server.Close()
			p.mu.Lock()
			defer p.mu.Unlock()
			if p.listeners[addr] == l {
				delete(p.listeners, addr)
				p.lastError = "Agent HTTPS listener stopped unexpectedly"
			}
		}()
	}
	for addr, routes := range groups {
		l := p.listeners[addr]
		if l == nil {
			l = created[addr]
			p.listeners[addr] = l
		}
		l.routes.Store(routes)
	}
	for addr, l := range p.listeners {
		if groups[addr] != nil {
			continue
		}
		delete(p.listeners, addr)
		l.requests.close()
		l.closing.Store(true)
		go func() {
			c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = l.server.Shutdown(c)
			_ = l.server.Close()
			l.cancel()
		}()
	}
	p.statuses, p.lastError = statuses, ""
	return nil
}
func (p *Pool) Status(url string) Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	v, ok := p.statuses[url]
	if !ok {
		v.State = "disabled"
	}
	if p.lastError != "" {
		v.State = "error"
		v.LastError = p.lastError
	}
	if ok && !time.Now().Before(v.CertificateNotAfter) {
		v.State = "error"
		v.LastError = "certificate expired"
	}
	return v
}
func (p *Pool) CertificateInUse(id int64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, v := range p.statuses {
		if v.CertificateID == id {
			return true
		}
	}
	return false
}
func (p *Pool) Close(ctx context.Context) error {
	p.mu.Lock()
	p.closed = true
	listeners := p.listeners
	p.listeners = map[string]*routeListener{}
	p.statuses = map[string]Status{}
	for _, l := range listeners {
		l.requests.close()
		l.closing.Store(true)
		l.cancel()
	}
	p.mu.Unlock()
	var failures []error
	for _, l := range listeners {
		if err := l.requests.wait(ctx); err != nil {
			failures = append(failures, err)
		}
		_ = l.server.Close()
	}
	return errors.Join(failures...)
}
