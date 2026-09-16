// Package origin refuses cross-site browser writes on cookie-authenticated
// routes: a request that changes state must come from the panel's own
// origin. Non-browser clients (no Origin and no Referer) pass, as do reads;
// the session cookie's SameSite=Lax is the second layer.
package origin

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

// Allowed reports whether r may proceed. base is the panel's public URL
// (may be empty); r.Host and X-Forwarded-Host are accepted as well so a
// panel reached through a proxy or by address still works.
func Allowed(r *http.Request, base string) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	src := strings.TrimSpace(r.Header.Get("Origin"))
	if src == "" {
		src = strings.TrimSpace(r.Header.Get("Referer"))
	}
	if src == "" {
		return true
	}
	if src == "null" {
		return false
	}
	u, err := url.Parse(src)
	if err != nil || u.Host == "" {
		return false
	}
	from := hostname(u.Host)
	for _, h := range []string{r.Host, r.Header.Get("X-Forwarded-Host"), baseHost(base)} {
		if h != "" && hostname(h) == from {
			return true
		}
	}
	return false
}

func baseHost(base string) string {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil {
		return ""
	}
	return u.Host
}

// hostname lowercases and drops the port: proxies rewrite ports, and the
// site is identified by its name either way.
func hostname(h string) string {
	h = strings.TrimSpace(strings.ToLower(h))
	if i := strings.Index(h, ","); i >= 0 { // X-Forwarded-Host may be a list
		h = strings.TrimSpace(h[:i])
	}
	if host, _, err := net.SplitHostPort(h); err == nil {
		return host
	}
	return strings.Trim(h, "[]")
}
