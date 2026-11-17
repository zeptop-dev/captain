package http

import (
	"context"
	"strings"
	"testing"

	"github.com/zeptop-dev/captain/internal/http/admin"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestLegacyGrantHTTPAndOmittedGroupPreservation(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	id, _ := r.user("legacy@example.com")
	g, e := r.st.CreateGroup(ctx, "Legacy")
	if e != nil {
		t.Fatal(e)
	}
	if e = r.st.UpdateUser(ctx, id, "active", &g, ""); e != nil {
		t.Fatal(e)
	}
	path := "/api/admin/users/" + itoa(id)
	status, b, _ := r.c.do("PATCH", path, map[string]any{"Status": "active", "Password": ""}, nil)
	if status != 200 {
		t.Fatal(status, string(b))
	}
	u, e := r.st.UserByID(ctx, id)
	if e != nil || u.GroupID == nil || *u.GroupID != g {
		t.Fatal("omission erased legacy grant", u, e)
	}
	status, b, _ = r.c.do("GET", path+"/legacy-grant", nil, nil)
	if status != 200 {
		t.Fatal(status, string(b))
	}
	v := mustJSON[store.LegacyUserGrant](t, b)
	status, _, _ = r.c.do("POST", path+"/legacy-grant/migrate", map[string]any{"revision": v.Revision, "confirm": true}, nil)
	if status != 409 {
		t.Fatal("uncovered cleared", status)
	}
	if _, e = r.st.DB().Exec(`UPDATE plans SET group_id=? WHERE id=(SELECT plan_id FROM subscriptions WHERE user_id=? LIMIT 1)`, g, id); e != nil {
		t.Fatal(e)
	}
	status, _, _ = r.c.do("POST", path+"/legacy-grant/migrate", map[string]any{"revision": v.Revision, "confirm": false}, nil)
	if status != 400 {
		t.Fatal(status)
	}
	status, _, _ = r.c.do("POST", path+"/legacy-grant/migrate", map[string]any{"revision": v.Revision, "confirm": true}, map[string]string{"Origin": "https://evil.example.com"})
	if status != 403 {
		t.Fatal(status)
	}
	status, b, _ = r.c.do("POST", path+"/legacy-grant/migrate", map[string]any{"revision": v.Revision, "confirm": true}, nil)
	if status != 200 {
		t.Fatal(status, string(b))
	}
	u, e = r.st.UserByID(ctx, id)
	if e != nil || u.GroupID != nil {
		t.Fatal(u, e)
	}
	status, b, _ = r.c.do("GET", path+"/legacy-grant", nil, nil)
	if status != 200 || !strings.Contains(string(b), `"group_id":null`) {
		t.Fatal(status, string(b))
	}
	// Existing explicit-null clients retain their clear behavior.
	r.st.UpdateUser(ctx, id, "active", &g, "")
	status, b, _ = r.c.do("PATCH", path, map[string]any{"Status": "active", "GroupID": nil}, nil)
	if status != 200 {
		t.Fatal(status, string(b))
	}
	u, e = r.st.UserByID(ctx, id)
	if e != nil || u.GroupID != nil {
		t.Fatal(u, e)
	}
	// Only staff sessions in the allowed roles can inspect/migrate an account.
	anon := &client{t: t, srv: r.srv}
	status, _, _ = anon.do("GET", path+"/legacy-grant", nil, nil)
	if status == 200 {
		t.Fatal(status)
	}
	staff, _ := admin.NewUser("support@example.com", "password123", "support")
	if e = r.st.CreateStaff(ctx, staff); e != nil {
		t.Fatal(e)
	}
	support := &client{t: t, srv: r.srv}
	support.do("POST", "/api/admin/login", map[string]string{"Email": staff.Email, "Password": "password123"}, nil)
	status, _, _ = support.do("GET", path+"/legacy-grant", nil, nil)
	if status != 403 {
		t.Fatal("support read", status)
	}
	status, _, _ = support.do("POST", path+"/legacy-grant/migrate", map[string]any{"confirm": true, "revision": v.Revision}, nil)
	if status != 403 {
		t.Fatal("support write", status)
	}
	// A narrow read token cannot execute the new mutation.
	token, _, e := r.st.CreateAPIToken(ctx, 1, "Read customers", "read", nil, []string{"users:read"})
	if e != nil {
		t.Fatal(e)
	}
	status, _, _ = anon.do("POST", path+"/legacy-grant/migrate", map[string]any{"confirm": true, "revision": v.Revision}, map[string]string{"Authorization": "Bearer " + token})
	if status != 403 {
		t.Fatal(status)
	}
}
