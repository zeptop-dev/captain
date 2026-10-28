// Package probeaccess exchanges a trusted Captain login for a host-bound,
// read-only probe session. Theme JavaScript never receives a panel session.
package probeaccess

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

const CookieName = "captain_probe_session"
const MainPath = "/api/probe/access"
const LoginPath = "/__captain/login"
const ExchangePath = "/__captain/access"

type Deps struct {
	BaseURL        string
	Settings       func(context.Context) store.ProbeSettings
	ResolveSession func(context.Context, string) (*domain.User, bool, error)
	Now            func() time.Time
}

type session struct {
	parent, host string
	expires      time.Time
}

type Access struct {
	Deps
	base     *url.URL
	mu       sync.Mutex
	grants   map[[32]byte]session
	sessions map[[32]byte]session
}

func New(d Deps) *Access {
	if d.Now == nil {
		d.Now = time.Now
	}
	u, _ := url.Parse(d.BaseURL)
	if u != nil {
		if host, err := store.NormalizeProbeOrigin(u.Hostname()); err == nil {
			if port := u.Port(); port != "" {
				u.Host = net.JoinHostPort(host, port)
			} else if strings.Contains(host, ":") {
				u.Host = "[" + host + "]"
			} else {
				u.Host = host
			}
		}
	}
	return &Access{Deps: d, base: u, grants: map[[32]byte]session{}, sessions: map[[32]byte]session{}}
}

func hostname(host string) string {
	if h, _, e := net.SplitHostPort(host); e == nil {
		host = h
	}
	name, _ := store.NormalizeProbeOrigin(strings.Trim(host, "[]"))
	return name
}

// MainHost compares hostnames, not ports: cookies are not isolated by port.
func (a *Access) MainHost(host string) bool {
	return a.base != nil && hostname(host) != "" && hostname(host) == hostname(a.base.Host)
}

// Dedicated checks current registration and rejects the main site's hostname,
// including aliases that differ only by port. A path is not origin isolation.
func (a *Access) Dedicated(ctx context.Context, host string) bool {
	if a.base == nil || hostname(a.base.Host) == "" || hostname(host) == "" || (a.base.Scheme != "http" && a.base.Scheme != "https") || hostname(host) == hostname(a.base.Host) {
		return false
	}
	for _, v := range a.Settings(ctx).Hosts {
		if hostname(v) == hostname(host) {
			return true
		}
	}
	return false
}

func (a *Access) permitted(ctx context.Context, parent string) *domain.User {
	if a.ResolveSession == nil {
		return nil
	}
	u, adminAuthenticated, err := a.ResolveSession(ctx, parent)
	if err != nil || u == nil || u.Status != "active" {
		return nil
	}
	s := a.Settings(ctx)
	if !s.PageOn() || s.Visibility == "admins" && (!u.IsStaff() || !adminAuthenticated) {
		return nil
	}
	return u
}

func (a *Access) pruneLocked(at time.Time) {
	for k, v := range a.grants {
		if !at.Before(v.expires) {
			delete(a.grants, k)
		}
	}
	for k, v := range a.sessions {
		if !at.Before(v.expires) {
			delete(a.sessions, k)
		}
	}
}

func randomToken() string {
	var b [32]byte
	_, _ = rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// Resolve deliberately ignores captain_session, API tokens and identity headers.
// The original session is revalidated so logout, bans, expiry and password
// changes also revoke already-open probe connections at their next gate check.
func (a *Access) Resolve(r *http.Request) *domain.User {
	if !a.Dedicated(r.Context(), r.Host) {
		return nil
	}
	c, err := r.Cookie(CookieName)
	if err != nil {
		return nil
	}
	key := sha256.Sum256([]byte(c.Value))
	a.mu.Lock()
	v, ok := a.sessions[key]
	if ok && !a.Now().Before(v.expires) {
		delete(a.sessions, key)
		ok = false
	}
	a.mu.Unlock()
	if !ok || v.host != strings.ToLower(r.Host) {
		return nil
	}
	return a.permitted(r.Context(), v.parent)
}

func headers(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	// Form POSTs need the trusted origin for CSRF validation. no-referrer
	// makes browsers send Origin: null, including on the confirmation form.
	// Tickets stay in the POST body; only the origin is disclosed.
	w.Header().Set("Referrer-Policy", "strict-origin")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
}

// Main serves a trusted confirmation screen on the configured panel origin.
// It never accepts credentials; normal Captain login (including MFA) happens
// in a separate tab before the visitor returns to approve read-only access.
func (a *Access) Main(w http.ResponseWriter, r *http.Request) {
	headers(w)
	if a.base == nil || !strings.EqualFold(r.Host, a.base.Host) {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", 400)
		return
	}
	host := strings.ToLower(strings.TrimSpace(r.FormValue("host")))
	if strings.ContainsAny(host, "/\\?#@\r\n") || !a.Dedicated(r.Context(), host) {
		http.Error(w, "Invalid probe hostname", 400)
		return
	}
	c, err := r.Cookie("captain_session")
	if err != nil || a.permitted(r.Context(), c.Value) == nil {
		a.screen(w, "Sign in to Captain / 登录 Captain", "Sign in using the main site, then return to this page. / 请在主站完成登录，然后返回本页。", map[string]string{"Login": a.base.String() + "/portal/login", "Admin": a.base.String() + "/admin/login"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		a.screen(w, "Probe access / 探针访问", "Allow this probe to read the published monitoring data. / 允许此探针读取已公开的监控数据。", map[string]string{"Host": host, "Action": MainPath})
	case http.MethodPost:
		if !sameOrigin(r, a.base) {
			http.Error(w, "Cross-origin request refused", 403)
			return
		}
		at, token := a.Now(), randomToken()
		a.mu.Lock()
		a.pruneLocked(at)
		if len(a.grants) >= 4096 {
			a.mu.Unlock()
			http.Error(w, "Please try again later", 429)
			return
		}
		a.grants[sha256.Sum256([]byte(token))] = session{parent: c.Value, host: host, expires: at.Add(time.Minute)}
		a.mu.Unlock()
		destination := (&url.URL{Scheme: a.base.Scheme, Host: host, Path: ExchangePath}).String()
		nonce := randomToken()
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'nonce-"+nonce+"'; form-action "+a.base.Scheme+"://"+host+"; frame-ancestors 'none'; base-uri 'none'")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = exchangeTemplate.Execute(w, map[string]string{"Action": destination, "Ticket": token, "Nonce": nonce})
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Method not allowed", 405)
	}
}

func sameOrigin(r *http.Request, origin *url.URL) bool {
	src := r.Header.Get("Origin")
	if src == "" {
		src = r.Header.Get("Referer")
	}
	if src == "" {
		return r.Header.Get("Sec-Fetch-Site") != "cross-site"
	}
	u, err := url.Parse(src)
	return err == nil && u.Scheme == origin.Scheme && strings.EqualFold(u.Host, origin.Host)
}

// Serve handles only the reserved probe auth paths; callers must not pass
// unrelated application paths through this method.
func (a *Access) Serve(w http.ResponseWriter, r *http.Request) {
	headers(w)
	if !a.Dedicated(r.Context(), r.Host) {
		http.NotFound(w, r)
		return
	}
	switch r.URL.Path {
	case LoginPath:
		if r.Method != "GET" {
			http.Error(w, "Method not allowed", 405)
			return
		}
		destination := *a.base
		destination.Path = MainPath
		destination.RawQuery = url.Values{"host": {strings.ToLower(r.Host)}}.Encode()
		http.Redirect(w, r, destination.String(), http.StatusSeeOther)
	case ExchangePath:
		if r.Method != "POST" || !sameOrigin(r, a.base) {
			http.Error(w, "Invalid access exchange", 403)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid access exchange", 400)
			return
		}
		key := sha256.Sum256([]byte(r.Form.Get("ticket")))
		at := a.Now()
		a.mu.Lock()
		v, ok := a.grants[key]
		delete(a.grants, key)
		a.mu.Unlock()
		if !ok || !at.Before(v.expires) || v.host != strings.ToLower(r.Host) || a.permitted(r.Context(), v.parent) == nil {
			http.Error(w, "Access expired; sign in again", 403)
			return
		}
		token := randomToken()
		v.expires = at.Add(8 * time.Hour)
		a.mu.Lock()
		a.pruneLocked(at)
		// Reauthorizing this browser's host does not leave an unbounded set
		// of child sessions for a single panel session.
		for k, old := range a.sessions {
			if old.parent == v.parent && old.host == v.host {
				delete(a.sessions, k)
			}
		}
		if len(a.sessions) >= 8192 {
			a.mu.Unlock()
			http.Error(w, "Please try again later", 429)
			return
		}
		a.sessions[sha256.Sum256([]byte(token))] = v
		a.mu.Unlock()
		http.SetCookie(w, &http.Cookie{Name: CookieName, Value: token, Path: "/", HttpOnly: true, Secure: a.base.Scheme == "https", SameSite: http.SameSiteLaxMode, Expires: v.expires, MaxAge: 8 * 60 * 60})
		http.Redirect(w, r, "/", http.StatusSeeOther)
	case "/api/logout":
		if r.Method != "GET" && r.Method != "POST" {
			http.Error(w, "Method not allowed", 405)
			return
		}
		own := &url.URL{Scheme: a.base.Scheme, Host: r.Host}
		if !sameOrigin(r, own) {
			http.Error(w, "Cross-origin request refused", 403)
			return
		}
		if c, e := r.Cookie(CookieName); e == nil {
			a.mu.Lock()
			delete(a.sessions, sha256.Sum256([]byte(c.Value)))
			a.mu.Unlock()
		}
		http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/", HttpOnly: true, Secure: a.base.Scheme == "https", SameSite: http.SameSiteLaxMode, MaxAge: -1})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": true})
	case "/api/login":
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "error", "message": "Sign in through Captain", "login_url": LoginPath})
	default:
		http.NotFound(w, r)
	}
}

func (a *Access) Required(w http.ResponseWriter, r *http.Request) {
	headers(w)
	a.screen(w, "Private probe / 私有探针", "Sign in through the main site to view this probe. / 请通过主站登录后查看探针。", map[string]string{"ProbeLogin": LoginPath})
}

func (a *Access) screen(w http.ResponseWriter, title, message string, fields map[string]string) {
	fields["Title"], fields["Message"] = title, message
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
	_ = screenTemplate.Execute(w, fields)
}

var screenTemplate = template.Must(template.New("access").Parse(`<!doctype html><html><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>{{.Title}}</title><style>body{font:16px system-ui;background:#f6f6fa;color:#202333;margin:8vh auto;max-width:640px;padding:28px}main{background:white;border:1px solid #e2e3ec;border-radius:18px;padding:32px}h1{font-size:24px}p{line-height:1.7}a,button{display:inline-block;margin:8px 12px 8px 0;border:0;border-radius:9px;background:#5745e8;color:white;padding:12px 18px;text-decoration:none;font:inherit}small{display:block;color:#666;margin-top:20px}</style><main><h1>{{.Title}}</h1><p>{{.Message}}</p>{{if .Login}}<a href="{{.Login}}" target="_blank" rel="noopener">User login / 用户登录</a><a href="{{.Admin}}" target="_blank" rel="noopener">Admin login / 管理员登录</a><p><a href="">Continue / 登录后继续</a></p>{{end}}{{if .Host}}<p>{{.Host}}</p><form method="post" action="{{.Action}}"><input type="hidden" name="host" value="{{.Host}}"><button>Continue / 继续</button></form>{{end}}{{if .ProbeLogin}}<a href="{{.ProbeLogin}}">Sign in / 登录</a>{{end}}<small>Read-only monitoring access · 仅授权读取探针数据</small></main></html>`))
var exchangeTemplate = template.Must(template.New("exchange").Parse(`<!doctype html><html><meta charset="utf-8"><meta name="referrer" content="strict-origin"><title>Continue to probe</title><form id="exchange" method="post" action="{{.Action}}"><input type="hidden" name="ticket" value="{{.Ticket}}"><button>Continue / 继续</button></form><script nonce="{{.Nonce}}">document.getElementById('exchange').submit()</script></html>`))
