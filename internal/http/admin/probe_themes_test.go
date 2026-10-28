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
