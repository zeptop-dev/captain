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
	if l.entries == nil {
		// Limiters declared as literals (package-level ones) never went
		// through New.
		l.entries = map[string]*entry{}
	}
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

// ClientIP returns the caller's address. Behind a trusted proxy
// X-Forwarded-For is read from the right, skipping trusted proxy hops:
// every proxy appends the peer it saw, so the rightmost untrusted entry is
// the real client and the entries before it are whatever the client sent.
// X-Real-IP is only a fallback, because a plain `reverse_proxy` in Caddy
// (and cloudflared) passes a client-supplied X-Real-IP through untouched
// while it does rewrite X-Forwarded-For.
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if trusted(net.ParseIP(host)) {
		// Every X-Forwarded-For line, in order, so a proxy that adds its
		// own header line instead of appending is handled too.
		if v := strings.Join(r.Header.Values("X-Forwarded-For"), ","); v != "" {
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
		if v := strings.TrimSpace(r.Header.Get("X-Real-IP")); v != "" && net.ParseIP(v) != nil {
			return v
		}
	}
	return host
}
