package admin

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/probetheme"
	"github.com/zeptop-dev/captain/internal/store"
)

type themeSessions struct {
	u     *domain.User
	admin bool
}

func (s themeSessions) Create(context.Context, int64, bool) (*domain.Session, error) { return nil, nil }
func (s themeSessions) Resolve(context.Context, string) (*domain.User, bool, error) {
	return s.u, s.admin, nil
}
func (s themeSessions) Delete(context.Context, string) error { return nil }
func TestThemeManagementRequiresFullConsoleSession(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "theme.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err = db.Migrate(ctx, conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	st := store.New(conn)
	admin := &domain.User{Email: "admin@example.com", PasswordHash: "test", Role: domain.RoleAdmin, Status: "active"}
	if err = st.CreateStaff(ctx, admin); err != nil {
		t.Fatal(err)
	}
	token, _, err := st.CreateAPIToken(ctx, admin.ID, "theme-test", "full", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, role, origin, auth string
		cookie, adminLogin       bool
		want                     int
	}{{"anonymous", "admin", "", "", false, true, 401}, {"console", "admin", "", "", true, true, 200}, {"portal session", "admin", "", "", true, false, 401}, {"operator", "operator", "", "", true, true, 403}, {"cross origin", "admin", "https://other.example.com", "", true, true, 403}, {"full token", "admin", "", token, false, true, 403}} {
		t.Run(tc.name, func(t *testing.T) {
			u := *admin
			u.Role = tc.role
			mux := http.NewServeMux()
			RegisterProbeThemes(mux, Deps{Store: st, Sessions: themeSessions{&u, tc.adminLogin}, BaseURL: "https://main.example.com", Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, probetheme.New(st), nil)
			method, target := "GET", "https://main.example.com/api/admin/settings/probe/themes"
			if tc.origin != "" {
				method = "PUT"
				target += "/active"
			}
			r := httptest.NewRequest(method, target, nil)
			if tc.cookie {
				r.AddCookie(&http.Cookie{Name: "captain_session", Value: "session"})
			}
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			if tc.auth != "" {
				r.Header.Set("Authorization", "Bearer "+tc.auth)
			}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}
func TestThemeActivationHostIsolation(t *testing.T) {
	for _, tc := range []struct {
		hosts []string
		want  bool
	}{{nil, false}, {[]string{"main.example.com"}, false}, {[]string{"MAIN.EXAMPLE.COM."}, false}, {[]string{"main.example.com:8443"}, false}, {[]string{"probe.example.com"}, true}, {[]string{"main.example.com", "probe.example.com"}, true}} {
		if got := themeDedicatedHost(tc.hosts, "https://main.example.com", "main.example.com"); got != tc.want {
			t.Fatal(tc.hosts, got)
		}
	}
}

func TestThemeActivationNormalizesOriginAliases(t *testing.T) {
	for _, tc := range []struct {
		name, base, request string
		hosts               []string
		want                bool
	}{
		{"IDNA main alias", "https://bücher.example", "main.example.com", []string{"xn--bcher-kva.example"}, false},
		{"Unicode main alias", "https://xn--bcher-kva.example", "main.example.com", []string{"BÜCHER.EXAMPLE."}, false},
		{"IDNA request alias", "https://main.example.com", "bücher.example:8443", []string{"xn--bcher-kva.example"}, false},
		{"compressed main IPv6", "https://[2001:0db8:0:0::1]:8443", "main.example.com", []string{"2001:db8::1"}, false},
		{"expanded main IPv6", "https://[2001:db8::1]", "main.example.com", []string{"2001:0db8:0:0::1"}, false},
		{"compressed request IPv6", "https://main.example.com", "[2001:0db8:0:0::1]:8443", []string{"2001:db8::1"}, false},
		{"bracketed request IPv6", "https://main.example.com", "[2001:db8::1]", []string{"2001:0db8:0:0::1"}, false},
		{"different IDNA origin", "https://main.example.com", "main.example.com", []string{"bücher.example"}, true},
		{"different IPv6 origin", "https://[2001:db8::1]", "[2001:db8::1]:8443", []string{"2001:db8::2"}, true},
		{"host port rejected", "https://main.example.com", "main.example.com", []string{"probe.example.com:443"}, false},
		{"host URL rejected", "https://main.example.com", "main.example.com", []string{"https://probe.example.com"}, false},
		{"missing base", "", "main.example.com", []string{"probe.example.com"}, false},
		{"invalid base", "file://main.example.com", "main.example.com", []string{"probe.example.com"}, false},
		{"invalid request", "https://main.example.com", "main.example.com/path", []string{"probe.example.com"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := themeDedicatedHost(tc.hosts, tc.base, tc.request); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
