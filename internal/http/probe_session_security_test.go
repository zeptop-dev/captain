package http

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/store"
)

func TestProbeRestrictedVisibilityRejectsBannedSessions(t *testing.T) {
	for _, tc := range []struct {
		name, visibility, accounts, login, role string
		retainedSession                         bool
	}{
		{"users_api", "users", "/api/admin/users", "/api/portal/login", "user", false},
		{"admins_api", "admins", "/api/admin/admins", "/api/admin/login", "operator", false},
		{"users_retained", "users", "/api/admin/users", "/api/portal/login", "user", true},
		{"admins_retained", "admins", "/api/admin/admins", "/api/admin/login", "operator", true},
		{"public_retained", "public", "/api/admin/admins", "/api/admin/login", "operator", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			ctx := context.Background()
			if code, b, _ := r.c.do("PUT", "/api/admin/settings/probe", map[string]any{
				"enabled": true, "path": "/status", "visibility": tc.visibility,
			}, nil); code != 200 {
				t.Fatalf("configure restricted probe: %d %s", code, b)
			}
			if tc.visibility == "public" {
				if code, b, _ := r.c.do("PATCH", "/api/admin/nodes/"+itoa(r.nodeID), map[string]any{"Name": "n1", "PublicAddr": "198.51.100.20"}, nil); code != 200 {
					t.Fatalf("set node address: %d %s", code, b)
				}
				if code, b, _ := r.c.do("PUT", "/api/admin/nodes/"+itoa(r.nodeID)+"/probe", map[string]any{"Hidden": true}, nil); code != 200 {
					t.Fatalf("hide node: %d %s", code, b)
				}
				anon := &client{t: t, srv: r.srv}
				code, b, _ := anon.do("GET", "/api/probe", nil, nil)
				view := mustJSON[map[string]any](t, b)
				if code != 200 || view["staff"] != false || len(view["nodes"].([]any)) != 0 {
					t.Fatalf("anonymous visitor saw hidden node: %d %s", code, b)
				}
			}
			code, b, _ := r.c.do("POST", tc.accounts, map[string]any{
				"Email": "revoked@example.com", "Password": "password123", "Role": tc.role,
			}, nil)
			if code != 200 {
				t.Fatalf("create account: %d %s", code, b)
			}
			visitor := &client{t: t, srv: r.srv}
			if code, b, _ := visitor.do("POST", tc.login, map[string]string{
				"Email": "revoked@example.com", "Password": "password123",
			}, nil); code != 200 {
				t.Fatalf("sign in: %d %s", code, b)
			}
			mePath := "/api/portal/me"
			if tc.role != "user" {
				mePath = "/api/admin/me"
			}
			code, b, _ = visitor.do("GET", mePath, nil, nil)
			if code != 200 {
				t.Fatalf("active account: %d %s", code, b)
			}
			account := mustJSON[map[string]any](t, b)
			for _, path := range []string{"/api/probe", "/status/"} {
				if code, b, _ := visitor.do("GET", path, nil, nil); code != 200 {
					t.Fatalf("active account cannot read %s: %d %s", path, code, b)
				}
			}
			if tc.visibility == "public" {
				_, b, _ := visitor.do("GET", "/api/probe", nil, nil)
				view := mustJSON[map[string]any](t, b)
				if view["staff"] != true || len(view["nodes"].([]any)) != 1 || view["nodes"].([]any)[0].(map[string]any)["addr"] != "198.51.100.20" {
					t.Fatalf("active staff view is missing hidden node: %s", b)
				}
			}
			id := int64(account["id"].(float64))
			if tc.retainedSession {
				// Older staff bans and internal customer status changes can leave
				// a live session row. Authorization must still check current status.
				var err error
				if tc.role == "user" {
					err = r.st.UpdateUser(ctx, id, "banned", nil, "")
				} else {
					err = r.st.UpdateStaff(ctx, id, "banned", "")
				}
				if err != nil {
					t.Fatal(err)
				}
				if _, _, err := r.st.SessionUser(ctx, visitor.cookie.Value, time.Now()); err != nil {
					t.Fatalf("fixture did not retain the session: %v", err)
				}
			} else {
				if code, b, _ := r.c.do("PATCH", tc.accounts+"/"+itoa(id), map[string]any{"Status": "banned"}, nil); code != 200 {
					t.Fatalf("ban account: %d %s", code, b)
				}
				if _, _, err := r.st.SessionUser(ctx, visitor.cookie.Value, time.Now()); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("ban did not revoke the account session: %v", err)
				}
			}
			if code, _, _ := visitor.do("GET", mePath, nil, nil); code != 401 && code != 403 {
				t.Fatalf("account API accepted banned session: %d", code)
			}
			if tc.visibility == "public" {
				code, b, _ := visitor.do("GET", "/api/probe", nil, nil)
				view := mustJSON[map[string]any](t, b)
				if code != 200 || view["staff"] != false || len(view["nodes"].([]any)) != 0 {
					t.Errorf("banned staff retained hidden-node and address access: status %d staff %v node count %d", code, view["staff"], len(view["nodes"].([]any)))
				}
				for _, suffix := range []string{"history", "storage-history", "pings", "availability"} {
					if code, _, _ := visitor.do("GET", "/api/probe/nodes/"+itoa(r.nodeID)+"/"+suffix, nil, nil); code != 404 {
						t.Errorf("banned staff retained hidden-node %s access: %d", suffix, code)
					}
				}
				return
			}
			for _, path := range []string{"/api/probe", "/status/"} {
				if code, _, _ := visitor.do("GET", path, nil, nil); code != 401 && code != 403 {
					t.Errorf("banned account can still read %s: status %d", path, code)
				}
			}
		})
	}
}
