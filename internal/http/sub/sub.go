// Package sub serves subscription documents at /sub/{token}.
package sub

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/http/ratelimit"
	"github.com/zeptop-dev/captain/internal/http/site"

	"github.com/zeptop-dev/bosun/pkg/subscription"
	"github.com/zeptop-dev/captain/internal/service"
	"github.com/zeptop-dev/captain/internal/store"
)

// Deps are the handler's dependencies.
type Deps struct {
	Store   *store.Store
	Log     *slog.Logger
	Service *service.Subscription
	Name    string // site name used in the download file name
}

// templateCache reads the operator's subscription templates with a short
// cache so every subscription fetch does not hit the settings table.
type templateCache struct {
	store   *store.Store
	mu      sync.Mutex
	cached  store.SubTemplates
	fetched time.Time
}

func (c *templateCache) get(ctx context.Context, format string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Since(c.fetched) >= 15*time.Second {
		var v store.SubTemplates
		_ = c.store.GetSetting(ctx, store.SettingSubTemplates, &v)
		c.cached, c.fetched = v, time.Now()
	}
	return c.cached[format]
}

// rulesCache keeps the response rules for 15 s like the templates.
type rulesCache struct {
	store   *store.Store
	mu      sync.Mutex
	cached  []service.ResponseRule
	fetched time.Time
}

func (c *rulesCache) get(ctx context.Context) []service.ResponseRule {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Since(c.fetched) >= 15*time.Second {
		var v []service.ResponseRule
		_ = c.store.GetSetting(ctx, service.SettingResponseRules, &v)
		c.cached, c.fetched = v, time.Now()
	}
	return c.cached
}

// Register mounts the subscription route.
func Register(mux *http.ServeMux, d Deps) {
	tpls := &templateCache{store: d.Store}
	rules := &rulesCache{store: d.Store}
	serve := func(w http.ResponseWriter, r *http.Request, u *domain.User) {
		lines, acct, err := d.Service.Lines(r.Context(), u, time.Now())
		if errors.Is(err, service.ErrDisabled) {
			// Like Xboard: a banned account gets nothing, not even its usage.
			http.Error(w, "account disabled", http.StatusForbidden)
			return
		}
		if err != nil && !errors.Is(err, service.ErrNoAccess) {
			d.Log.Error("subscription", "user", u.ID, "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		// No usable plan (as opposed to a banned account, refused above)
		// renders an empty document rather than an error so clients
		// keep the subscription and see the usage header.
		req := store.SubRequest{RequestIP: clip(ratelimit.ClientIP(r), 64), UserAgent: clip(r.UserAgent(), 512)}
		var ruleHeaders map[string]string
		// Response rules: the first matching rule decides the format and
		// extra headers, or refuses the request outright.
		format, template := r.URL.Query().Get("client"), ""
		if rule := service.MatchResponseRule(rules.get(r.Context()), r); rule != nil {
			req.Rule = rule.Name
			refuse := func(status int, text string, resp string) {
				req.Response = resp
				_ = d.Store.RecordSubRequest(r.Context(), u.ID, req)
				if status == 0 {
					// "drop": close without an HTTP reply, so scanners
					// learn nothing about the link. HTTP/2 cannot be
					// hijacked, so fall back to the same 404 an unknown
					// token gets rather than a telltale 403.
					if hj, ok := w.(http.Hijacker); ok {
						if conn, _, err := hj.Hijack(); err == nil {
							_ = conn.Close()
							return
						}
					}
					status, text = http.StatusNotFound, "404 page not found"
				}
				http.Error(w, text, status)
			}
			switch rule.Action {
			case "block":
				refuse(http.StatusForbidden, "forbidden", "blocked")
				return
			case "not_found":
				refuse(http.StatusNotFound, "404 page not found", "not-found")
				return
			case "unavailable":
				refuse(http.StatusUnavailableForLegalReasons, "unavailable for legal reasons", "unavailable")
				return
			case "drop":
				refuse(0, "", "dropped")
				return
			}
			if rule.Format != "" {
				format = rule.Format
			}
			template = rule.Template
			ruleHeaders = rule.Headers
		}
		rd := subscription.Pick(format, r.UserAgent())
		req.Response = rd.Name()
		// Device limit by HWID: a client that identifies its device gets
		// counted per device; over the limit it receives an empty document
		// with the reason in headers (x-hwid-*, announce), the Happ way.
		var subs service.SubscriptionSettings
		_ = d.Store.GetSetting(r.Context(), service.SettingSubscription, &subs)
		if subs.HWID.Enabled {
			hwid := strings.TrimSpace(r.Header.Get("x-hwid"))
			switch {
			case hwid == "" && subs.HWID.Require:
				req.Response = "hwid-missing"
				_ = d.Store.RecordSubRequest(r.Context(), u.ID, req)
				http.NotFound(w, r)
				return
			case hwid != "" && !hwidRe.MatchString(hwid):
				req.Response = "hwid-invalid"
				_ = d.Store.RecordSubRequest(r.Context(), u.ID, req)
				http.Error(w, "bad x-hwid", http.StatusBadRequest)
				return
			case hwid != "":
				req.Hwid = hwid
				limit := d.Service.HWIDLimit(r.Context(), u, subs.HWID.FallbackLimit)
				allowed, n, err := d.Store.ClaimHwidDevice(r.Context(), u.ID, store.HwidDevice{Hwid: hwid, Platform: clip(r.Header.Get("x-device-os"), 64), OSVersion: clip(r.Header.Get("x-ver-os"), 64), DeviceModel: clip(r.Header.Get("x-device-model"), 128), UserAgent: clip(r.UserAgent(), 512), RequestIP: req.RequestIP}, limit, time.Now())
				if err != nil {
					d.Log.Error("hwid", "user", u.ID, "err", err)
				}
				w.Header().Set("x-hwid-active", "true")
				w.Header().Set("x-hwid-limit", strconv.Itoa(limit))
				if err == nil && !allowed {
					w.Header().Set("x-hwid-max-devices-reached", "true")
					if msg := strings.TrimSpace(subs.HWID.Announce); msg != "" {
						w.Header().Set("announce", "base64:"+base64.StdEncoding.EncodeToString([]byte(msg)))
					}
					lines = nil
					req.Response = "hwid-denied"
					d.Log.Info("hwid device limit reached", "user", u.ID, "devices", n, "limit", limit)
				}
			}
		}
		defer func() { _ = d.Store.RecordSubRequest(context.WithoutCancel(r.Context()), u.ID, req) }()
		if template == "" {
			template = tpls.get(r.Context(), rd.Name())
		}
		body, err := rd.RenderWith(lines, acct, template)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		// The profile name clients show: the admin-edited site name, else the
		// config one. Sent three ways because clients disagree on which they
		// read: an ASCII filename (no quotes, some clients keep them
		// literally), an RFC 5987 UTF-8 filename*, and Clash-style
		// profile-title (base64) which mihomo/Stash/Verge prefer.
		name := d.Name
		var ss site.Settings
		if err := d.Store.GetSetting(r.Context(), site.SettingSite, &ss); err == nil && strings.TrimSpace(ss.Name) != "" {
			name = strings.TrimSpace(ss.Name)
		}
		if name == "" {
			name = "captain"
		}
		w.Header().Set("Content-Type", rd.ContentType())
		w.Header().Set("Content-Disposition", "attachment; filename="+asciiName(name)+"; filename*=UTF-8''"+url.PathEscape(name))
		w.Header().Set("Profile-Title", "base64:"+base64.StdEncoding.EncodeToString([]byte(name)))
		w.Header().Set("Subscription-Userinfo", fmt.Sprintf("upload=%d; download=%d; total=%d; expire=%d", acct.Upload, acct.Download, acct.Total, acct.Expire))
		w.Header().Set("Profile-Update-Interval", "12")
		w.Header().Set("Cache-Control", "no-store")
		// A rule's own headers come last: an operator who sets
		// profile-update-interval or profile-title means it.
		for k, v := range ruleHeaders {
			w.Header().Set(k, v)
		}
		_, _ = w.Write(body)
	}
	mux.HandleFunc("GET /sub/{token}", func(w http.ResponseWriter, r *http.Request) {
		if !allowFetch(w, r, "tok:"+r.PathValue("token")) {
			return
		}
		u, err := d.Store.UserBySubToken(r.Context(), r.PathValue("token"))
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		serve(w, r, u)
	})
	// Short and temporary links: /s/<code>, use-counted for temp links.
	mux.HandleFunc("GET /s/{code}", func(w http.ResponseWriter, r *http.Request) {
		if !allowFetch(w, r, "code:"+r.PathValue("code")) {
			return
		}
		u, err := d.Store.UseSubLink(r.Context(), r.PathValue("code"), time.Now())
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrLinkExhausted) {
			http.Error(w, "subscription link not available", http.StatusGone)
			return
		}
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		serve(w, r, u)
	})
}

// hwidRe is what Happ and Remnawave accept as a device id.
var hwidRe = regexp.MustCompile(`^[a-zA-Z0-9=-]{10,64}$`)

// asciiName reduces a name to the token characters every client accepts
// unquoted in Content-Disposition.
func asciiName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('_')
		}
	}
	out := strings.Trim(b.String(), "_.-")
	for strings.Contains(out, "__") {
		out = strings.ReplaceAll(out, "__", "_")
	}
	if out == "" {
		return "subscription"
	}
	return out
}

// fetches limits how often one link (and one address) may be fetched:
// every fetch writes a row and may register a device, and the only thing
// a caller needs is a token somebody shared with them. Clients refresh
// hours apart, so this is far above normal use.
var fetches = &ratelimit.Limiter{Max: 60, Window: 5 * time.Minute, Lock: 5 * time.Minute}
var fetchesByIP = &ratelimit.Limiter{Max: 600, Window: 5 * time.Minute, Lock: 5 * time.Minute}

// allowFetch counts this fetch and reports whether it may proceed.
func allowFetch(w http.ResponseWriter, r *http.Request, key string) bool {
	ip := ratelimit.ClientIP(r)
	for l, k := range map[*ratelimit.Limiter]string{fetches: key, fetchesByIP: ip} {
		if ok, wait := l.Allow(k); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return false
		}
		l.Fail(k)
	}
	return true
}

// clip bounds a client-supplied string before it is stored.
func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n]
	}
	return s
}
