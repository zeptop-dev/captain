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

// Register mounts the subscription route.
func Register(mux *http.ServeMux, d Deps) {
	tpls := &templateCache{store: d.Store}
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
		rd := subscription.Pick(r.URL.Query().Get("client"), r.UserAgent())
		req := store.SubRequest{RequestIP: ratelimit.ClientIP(r), UserAgent: r.UserAgent(), Response: rd.Name()}
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
				allowed, n, err := d.Store.ClaimHwidDevice(r.Context(), u.ID, store.HwidDevice{Hwid: hwid, Platform: r.Header.Get("x-device-os"), OSVersion: r.Header.Get("x-ver-os"), DeviceModel: r.Header.Get("x-device-model"), UserAgent: r.UserAgent(), RequestIP: req.RequestIP}, limit, time.Now())
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
		body, err := rd.RenderWith(lines, acct, tpls.get(r.Context(), rd.Name()))
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
		_, _ = w.Write(body)
	}
	mux.HandleFunc("GET /sub/{token}", func(w http.ResponseWriter, r *http.Request) {
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
