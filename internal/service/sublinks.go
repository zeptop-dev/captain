package service

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/zeptop-dev/captain/internal/auth"
	"github.com/zeptop-dev/captain/internal/store"
)

// SubscriptionSettings is the admin-editable list of subscription base URLs.
// Each entry is a URL prefix; "[1-9]" and "[uuid]" inside it are replaced
// per link so a wildcard DNS name can spread users over many hostnames.
type SubscriptionSettings struct {
	URLs []string `json:"urls"`
}

// SettingSubscription is the settings key.
const SettingSubscription = "subscription"

// SubLinks builds user subscription URLs from the settings, falling back to
// the panel's base URL, and knows which hosts are subscription-only.
type SubLinks struct {
	Store   *store.Store
	BaseURL string

	mu      sync.Mutex
	cached  SubscriptionSettings
	fetched time.Time
}

func (s *SubLinks) settings(ctx context.Context) SubscriptionSettings {
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Since(s.fetched) < 15*time.Second {
		return s.cached
	}
	var v SubscriptionSettings
	_ = s.Store.GetSetting(ctx, SettingSubscription, &v)
	s.cached, s.fetched = v, time.Now()
	return v
}

// Invalidate drops the cache after the settings changed.
func (s *SubLinks) Invalidate() {
	s.mu.Lock()
	s.fetched = time.Time{}
	s.mu.Unlock()
}

var (
	rangePattern = regexp.MustCompile(`\[(\d+)-(\d+)\]`)
	uuidPattern  = regexp.MustCompile(`\[uuid\]`)
)

// expand fills the placeholders of one URL prefix.
func expand(prefix string) string {
	out := rangePattern.ReplaceAllStringFunc(prefix, func(m string) string {
		p := rangePattern.FindStringSubmatch(m)
		var lo, hi int
		fmt.Sscanf(p[1], "%d", &lo)
		fmt.Sscanf(p[2], "%d", &hi)
		if lo > hi {
			lo, hi = hi, lo
		}
		return fmt.Sprintf("%d", lo+rand.IntN(hi-lo+1))
	})
	return uuidPattern.ReplaceAllStringFunc(out, func(string) string { return strings.ReplaceAll(auth.UUID(), "-", "") })
}

// URL returns a subscription link for token.
func (s *SubLinks) URL(ctx context.Context, token string) string {
	urls := s.settings(ctx).URLs
	base := strings.TrimRight(s.BaseURL, "/")
	if len(urls) > 0 {
		base = strings.TrimRight(expand(urls[rand.IntN(len(urls))]), "/")
	}
	return base + "/sub/" + token
}

// Hosts returns the hostnames of the configured subscription URLs as
// matchers: exact names, or regular expressions for names with placeholders.
func (s *SubLinks) hostMatchers(ctx context.Context) (exact map[string]bool, patterns []*regexp.Regexp) {
	exact = map[string]bool{}
	for _, raw := range s.settings(ctx).URLs {
		// url.Parse rejects "[" in hosts, so take the host by hand.
		h := hostOf(raw)
		if h == "" {
			continue
		}
		if !strings.Contains(h, "[") {
			exact[h] = true
			continue
		}
		re := regexp.QuoteMeta(h)
		re = regexp.MustCompile(`\\\[\d+-\d+\\\]`).ReplaceAllString(re, `\d+`)
		re = strings.ReplaceAll(re, `\[uuid\]`, `[0-9a-f]{32}`)
		if p, err := regexp.Compile("^" + re + "$"); err == nil {
			patterns = append(patterns, p)
		}
	}
	return exact, patterns
}

// IsSubscriptionHost reports whether host is one of the subscription-only
// hostnames (never the panel's own host).
func (s *SubLinks) IsSubscriptionHost(ctx context.Context, host string) bool {
	host = strings.ToLower(host)
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	if u, err := url.Parse(s.BaseURL); err == nil && strings.EqualFold(u.Hostname(), host) {
		return false
	}
	exact, patterns := s.hostMatchers(ctx)
	if exact[host] {
		return true
	}
	for _, p := range patterns {
		if p.MatchString(host) {
			return true
		}
	}
	return false
}

// AllowedTLSHost is the autocert host policy: the panel host plus every
// subscription host. Placeholder hosts are allowed too; Let's Encrypt rate
// limits apply per registered domain, so keep such patterns small.
func (s *SubLinks) AllowedTLSHost(ctx context.Context, host string) bool {
	if u, err := url.Parse(s.BaseURL); err == nil && strings.EqualFold(u.Hostname(), host) {
		return true
	}
	return s.IsSubscriptionHost(ctx, host)
}

// SubscriptionOnly wraps a handler so requests arriving on a subscription
// host can reach only the subscription endpoints; everything else is 404,
// which keeps the login pages off the address users hand around.
func (s *SubLinks) SubscriptionOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.IsSubscriptionHost(r.Context(), r.Host) && !strings.HasPrefix(r.URL.Path, "/sub/") && r.URL.Path != "/api/health" {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// hostOf returns the lower-cased hostname of a URL prefix that may contain
// placeholders (which url.Parse would reject as a bad IPv6 literal).
func hostOf(raw string) string {
	raw = strings.TrimSpace(raw)
	if i := strings.Index(raw, "://"); i >= 0 {
		raw = raw[i+3:]
	}
	if i := strings.IndexAny(raw, "/?#"); i >= 0 {
		raw = raw[:i]
	}
	if i := strings.LastIndexByte(raw, '@'); i >= 0 {
		raw = raw[i+1:]
	}
	// Strip a port, but not the digits inside a "[1-9]" placeholder.
	if i := strings.LastIndexByte(raw, ':'); i >= 0 && !strings.Contains(raw[i:], "]") {
		raw = raw[:i]
	}
	return strings.ToLower(raw)
}
