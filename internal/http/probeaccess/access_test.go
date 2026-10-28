package probeaccess

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestProbeSessionIsolationReplayAndRevocation(t *testing.T) {
	at := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	active := true
	s := store.ProbeSettings{Enabled: true, Visibility: "admins", Hosts: []string{"probe.example.com", "other.example.com", "panel.example.com"}}
	a := New(Deps{BaseURL: "https://panel.example.com", Now: func() time.Time { return at }, Settings: func(context.Context) store.ProbeSettings { return s }, ResolveSession: func(_ context.Context, v string) (*domain.User, bool, error) {
		if v == "parent" && active {
			return &domain.User{ID: 1, Role: "admin", Status: "active"}, true, nil
		}
		return nil, false, nil
	}})
	request := func(method, path, host, body, origin string, cookie *http.Cookie) *http.Request {
		r := httptest.NewRequest(method, "https://"+host+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		return r
	}
	parent := &http.Cookie{Name: "captain_session", Value: "parent"}
	if a.Resolve(request("GET", "/", "probe.example.com", "", "", parent)) != nil {
		t.Fatal("theme accepted panel session")
	}
	if a.Dedicated(context.Background(), "panel.example.com:8443") {
		t.Fatal("same hostname cannot isolate theme")
	}
	for _, origin := range []string{"https://evil.example", "https://probe.example.com", "http://panel.example.com"} {
		w := httptest.NewRecorder()
		a.Main(w, request("POST", MainPath, "panel.example.com", "host=probe.example.com", origin, parent))
		if w.Code != 403 {
			t.Fatalf("cross-origin grant accepted: %s %d", origin, w.Code)
		}
	}
	grant := func() string {
		w := httptest.NewRecorder()
		a.Main(w, request("POST", MainPath, "panel.example.com", "host=probe.example.com", "https://panel.example.com", parent))
		if w.Header().Get("Referrer-Policy") != "strict-origin" || !strings.Contains(w.Body.String(), `name="referrer" content="strict-origin"`) {
			t.Fatal("bridge forms must preserve the trusted Origin without exposing the full referrer")
		}
		if w.Code != 200 {
			t.Fatalf("grant: %d %s", w.Code, w.Body.String())
		}
		m := regexp.MustCompile(`name="ticket" value="([^"]+)"`).FindStringSubmatch(w.Body.String())
		if len(m) != 2 || strings.Contains(w.Body.String(), "parent") {
			t.Fatal("missing ticket or leaked session")
		}
		return m[1]
	}
	exchange := func(ticket, host string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		a.Serve(w, request("POST", ExchangePath, host, url.Values{"ticket": {ticket}}.Encode(), "https://panel.example.com", nil))
		return w
	}
	ticket := grant()
	if w := exchange(ticket, "other.example.com"); w.Code != 403 {
		t.Fatal("cross-host exchange accepted")
	}
	if w := exchange(ticket, "probe.example.com"); w.Code != 403 {
		t.Fatal("failed exchange replay accepted")
	}
	ticket = grant()
	w := exchange(ticket, "probe.example.com")
	if w.Code != 303 {
		t.Fatalf("exchange: %d %s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing probe cookie")
	}
	cookie := cookies[0]
	if cookie.Name != CookieName || !cookie.HttpOnly || !cookie.Secure || cookie.Domain != "" || cookie.Value == "parent" {
		t.Fatalf("unsafe cookie: %#v", cookie)
	}
	if a.Resolve(request("GET", "/", "probe.example.com", "", "", cookie)) == nil {
		t.Fatal("read-only session not usable")
	}
	if a.Resolve(request("GET", "/", "other.example.com", "", "", cookie)) != nil {
		t.Fatal("child session not host bound")
	}
	if w := exchange(ticket, "probe.example.com"); w.Code != 403 {
		t.Fatal("grant replay accepted")
	}
	active = false
	if a.Resolve(request("GET", "/", "probe.example.com", "", "", cookie)) != nil {
		t.Fatal("parent revocation not honored")
	}
	active = true
	s.Hosts = nil
	if a.Resolve(request("GET", "/", "probe.example.com", "", "", cookie)) != nil {
		t.Fatal("removed host still authorized")
	}
	s.Hosts = []string{"probe.example.com"}
	at = at.Add(9 * time.Hour)
	if a.Resolve(request("GET", "/", "probe.example.com", "", "", cookie)) != nil {
		t.Fatal("expired child session accepted")
	}
}

func TestProbeAccessNeverAcceptsPasswords(t *testing.T) {
	s := store.ProbeSettings{Enabled: true, Hosts: []string{"probe.example.com"}}
	a := New(Deps{BaseURL: "https://panel.example.com", Settings: func(context.Context) store.ProbeSettings { return s }})
	r := httptest.NewRequest("POST", "https://probe.example.com/api/login", strings.NewReader(`{"username":"admin","password":"password"}`))
	w := httptest.NewRecorder()
	a.Serve(w, r)
	if w.Code != 401 || !strings.Contains(w.Body.String(), LoginPath) || len(w.Result().Cookies()) != 0 {
		t.Fatalf("password login should redirect to trusted flow: %d %s", w.Code, w.Body.String())
	}
}

func TestAdminsProbeRequiresAdminAuthenticatedSession(t *testing.T) {
	a := New(Deps{BaseURL: "https://panel.example.com", Settings: func(context.Context) store.ProbeSettings {
		return store.ProbeSettings{Enabled: true, Visibility: "admins", Hosts: []string{"probe.example.com"}}
	}, ResolveSession: func(_ context.Context, token string) (*domain.User, bool, error) {
		return &domain.User{ID: 1, Role: "admin", Status: "active"}, token == "mfa-admin", nil
	}})
	if a.permitted(context.Background(), "portal-password") != nil {
		t.Fatal("portal-only staff session bypassed administrator authentication")
	}
	if a.permitted(context.Background(), "mfa-admin") == nil {
		t.Fatal("administrator session rejected")
	}
}

func TestEquivalentMainHostCannotRunThirdPartyTheme(t *testing.T) {
	for _, tc := range []struct{ base, host string }{{"https://bücher.example", "xn--bcher-kva.example"}, {"https://[2001:0db8::1]", "[2001:db8::1]"}, {"https://panel.example.com", "PANEL.EXAMPLE.COM.:8443"}} {
		a := New(Deps{BaseURL: tc.base, Settings: func(context.Context) store.ProbeSettings {
			return store.ProbeSettings{Enabled: true, Hosts: []string{hostname(tc.host)}}
		}})
		if !a.MainHost(tc.host) || a.Dedicated(context.Background(), tc.host) {
			t.Errorf("equivalent main origin permitted: %s %s", tc.base, tc.host)
		}
	}
}

func TestCanonicalMainOriginSupportsLoginBridge(t *testing.T) {
	for _, tc := range []struct{ configured, browser string }{
		{"https://例子.example:8443", "https://xn--fsqu00a.example:8443"},
		{"https://[2001:0db8:0:0:0:0:0:1]:8443", "https://[2001:db8::1]:8443"},
		{"https://[2001:0db8:0:0:0:0:0:1]", "https://[2001:db8::1]"},
	} {
		t.Run(tc.configured, func(t *testing.T) {
			a := New(Deps{BaseURL: tc.configured, Settings: func(context.Context) store.ProbeSettings {
				return store.ProbeSettings{Enabled: true, Visibility: "admins", Hosts: []string{"probe.example.com"}}
			}, ResolveSession: func(context.Context, string) (*domain.User, bool, error) {
				return &domain.User{ID: 1, Role: "admin", Status: "active"}, true, nil
			}})
			if a.base.String() != tc.browser {
				t.Fatalf("base = %s", a.base)
			}
			r := httptest.NewRequest("POST", tc.browser+MainPath, strings.NewReader("host=probe.example.com"))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Header.Set("Origin", tc.browser)
			r.AddCookie(&http.Cookie{Name: "captain_session", Value: "parent"})
			w := httptest.NewRecorder()
			a.Main(w, r)
			if w.Code != 200 || !strings.Contains(w.Body.String(), `name="ticket"`) {
				t.Fatalf("bridge: %d %s", w.Code, w.Body.String())
			}
		})
	}
}
