package service

import (
	"context"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/zeptop-dev/captain/internal/store"
)

// AdminAllow applies the console allow-list (Settings → Security). It is
// shared by the admin API and the MCP endpoint, which are the same door:
// an API token reaches both.
type AdminAllow struct {
	Store *store.Store

	mu     sync.Mutex
	list   []string
	at     time.Time
	loaded bool
}

// Allowed reports whether ip may reach the console API. An empty list
// allows everything; a failed read keeps the previous list rather than
// opening the door for 30 seconds.
func (a *AdminAllow) Allowed(ctx context.Context, ip string) bool {
	a.mu.Lock()
	if time.Since(a.at) > 30*time.Second {
		var v store.SecuritySettings
		if err := a.Store.GetSetting(ctx, store.SettingSecurity, &v); err == nil {
			a.list, a.at, a.loaded = v.AdminAllowCIDRs, time.Now(), true
		} else if !a.loaded {
			a.list = nil // nothing cached yet: allow, and retry next call
		}
	}
	list := a.list
	a.mu.Unlock()
	if len(list) == 0 {
		return true
	}
	return CIDRsAllow(list, ip)
}

// Invalidate drops the cache (after an admin edit).
func (a *AdminAllow) Invalidate() {
	a.mu.Lock()
	a.at = time.Time{}
	a.mu.Unlock()
}

// CIDRsAllow reports whether ip is inside any entry (bare IPs allowed).
func CIDRsAllow(list []string, ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, c := range list {
		if !strings.Contains(c, "/") {
			if ip.Equal(net.ParseIP(c)) {
				return true
			}
			continue
		}
		if _, n, err := net.ParseCIDR(c); err == nil && n.Contains(ip) {
			return true
		}
	}
	return false
}
