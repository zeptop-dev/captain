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

// ClientIP returns the caller's address. When the connection comes from a
// loopback or private address (a reverse proxy such as Caddy in the same
// compose network), the X-Forwarded-For / X-Real-IP header is trusted.
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip != nil && (ip.IsLoopback() || ip.IsPrivate()) {
		if v := r.Header.Get("X-Real-IP"); v != "" {
			return strings.TrimSpace(v)
		}
		if v := r.Header.Get("X-Forwarded-For"); v != "" {
			return strings.TrimSpace(strings.Split(v, ",")[0])
		}
	}
	return host
}
