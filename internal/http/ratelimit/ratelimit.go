// Package ratelimit slows down credential guessing: after a few failed
// attempts from one address, further attempts are refused for a while.
package ratelimit

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Limiter counts failures per client address.
type Limiter struct {
	Max    int           // failures allowed inside Window before locking; default 5
	Window time.Duration // default 15m
	Lock   time.Duration // how long a locked address waits; default 15m

	mu      sync.Mutex
	entries map[string]*entry
}

type entry struct {
	failures int
	first    time.Time
	lockedTo time.Time
}

// New returns a limiter with the defaults.
func New() *Limiter {
	return &Limiter{Max: 5, Window: 15 * time.Minute, Lock: 15 * time.Minute, entries: map[string]*entry{}}
}

// Allow reports whether the address may attempt a login now, and how long it
// has to wait otherwise.
func (l *Limiter) Allow(addr string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweep()
	e := l.entries[addr]
	if e == nil {
		return true, 0
	}
	if now := time.Now(); now.Before(e.lockedTo) {
		return false, e.lockedTo.Sub(now).Round(time.Second)
	}
	return true, 0
}

// Fail records a failed attempt.
func (l *Limiter) Fail(addr string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	e := l.entries[addr]
	if e == nil || now.Sub(e.first) > l.Window {
		e = &entry{first: now}
		l.entries[addr] = e
	}
	e.failures++
	if e.failures >= l.Max {
		e.lockedTo = now.Add(l.Lock)
		e.failures = 0
		e.first = now
	}
}

// Reset clears an address after a successful login.
func (l *Limiter) Reset(addr string) {
	l.mu.Lock()
	delete(l.entries, addr)
	l.mu.Unlock()
}

// sweep drops stale entries; callers hold the lock.
func (l *Limiter) sweep() {
	if len(l.entries) < 1000 {
		return
	}
	now := time.Now()
	for k, e := range l.entries {
		if now.After(e.lockedTo) && now.Sub(e.first) > l.Window {
			delete(l.entries, k)
		}
	}
}

// TrustedProxies lists the reverse proxies whose forwarding headers are
// believed (config trusted_proxies). Empty keeps the historical rule:
// any loopback or private peer (Caddy/nginx in the same compose network).
var TrustedProxies []*net.IPNet

func trusted(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if len(TrustedProxies) == 0 {
		return ip.IsLoopback() || ip.IsPrivate()
	}
	for _, n := range TrustedProxies {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// ClientIP returns the caller's address. Behind a trusted proxy the
// X-Real-IP header wins (proxies overwrite it), else X-Forwarded-For is
// read from the right, skipping trusted proxy hops; the first entry is
// whatever the client sent and is never trusted on its own.
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if trusted(net.ParseIP(host)) {
		if v := strings.TrimSpace(r.Header.Get("X-Real-IP")); v != "" && net.ParseIP(v) != nil {
			return v
		}
		if v := r.Header.Get("X-Forwarded-For"); v != "" {
			// Walk from the proxy's own entry backwards over trusted hops;
			// the first address that is not a proxy is the client. Entries
			// before that are whatever the client sent.
			parts := strings.Split(v, ",")
			for i := len(parts) - 1; i >= 0; i-- {
				hop := strings.TrimSpace(parts[i])
				ip := net.ParseIP(hop)
				if ip == nil {
					break
				}
				if !trusted(ip) {
					return hop
				}
			}
		}
	}
	return host
}
