package http

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/http/admin"
)

type accessAuditRow struct {
	InboundID int64   `json:"inbound_id"`
	Policy    string  `json:"policy"`
	GroupIDs  []int64 `json:"group_ids"`
	Revision  string  `json:"revision"`
	Legacy    bool    `json:"legacy"`
}

type accessAuditCatalog struct {
	Revision string           `json:"revision"`
	Inbounds []accessAuditRow `json:"inbounds"`
}

func accessAuditCall(t *testing.T, c *client, method, path string, body any, want int) []byte {
	t.Helper()
	code, out, _ := c.do(method, path, body, nil)
	if code != want {
		t.Fatalf("%s %s: status %d, want %d: %s", method, path, code, want, out)
	}
	return out
}

func accessAuditGet(t *testing.T, r *rig) accessAuditCatalog {
	t.Helper()
	v := mustJSON[accessAuditCatalog](t, accessAuditCall(t, r.c, "GET", "/api/admin/node-access", nil, 200))
	if v.Revision == "" {
		t.Fatal("access catalogue has no revision")
	}
	for _, row := range v.Inbounds {
		if row.InboundID <= 0 || row.Revision != v.Revision {
			t.Fatal("access row does not identify its inbound and catalogue revision", row)
		}
	}
	return v
}

func accessAuditFind(t *testing.T, v accessAuditCatalog, id int64) accessAuditRow {
	t.Helper()
	for _, row := range v.Inbounds {
		if row.InboundID == id {
			return row
		}
	}
	t.Fatalf("inbound %d missing from access catalogue", id)
	return accessAuditRow{}
}

func accessAuditFixture(t *testing.T) (*rig, *domain.Inbound, domain.Entry, int64) {
	t.Helper()
	r := newRig(t)
	ibs, err := r.st.InboundsByNode(context.Background(), r.nodeID)
	if err != nil || len(ibs) != 1 {
		t.Fatal("inbound fixture", err)
	}
	e := mustJSON[domain.Entry](t, accessAuditCall(t, r.c, "POST", "/api/admin/entries", map[string]any{
		"Name": "Private access audit node", "InboundID": ibs[0].ID,
		"DisplayHost": "node.example.com", "DisplayPort": 443,
	}, 200))
	g := mustJSON[struct {
		ID int64 `json:"id"`
	}](t, accessAuditCall(t, r.c, "POST", "/api/admin/groups", map[string]string{"Name": "Audit group"}, 200))
	if g.ID <= 0 {
		t.Fatal("group fixture missing id")
	}
	return r, ibs[0], e, g.ID
}

func accessAuditPolicy(id int64, revision, policy string, groups []int64, confirm bool) map[string]any {
	return map[string]any{"inbound_id": id, "revision": revision, "policy": policy, "group_ids": groups, "confirm_shared": confirm}
}

func accessAuditToken(t *testing.T, r *rig, owner int64, base string, scopes []string) *client {
	t.Helper()
	token, _, err := r.st.CreateAPIToken(context.Background(), owner, "Node access audit", base, nil, scopes)
	if err != nil {
		t.Fatal(err)
	}
	return &client{t: t, srv: r.srv, token: token}
}

func accessAuditStaff(t *testing.T, r *rig, role string) (*domain.User, *client) {
	t.Helper()
	u, err := admin.NewUser(role+"-access@example.com", "password123", role)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.st.CreateStaff(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	c := &client{t: t, srv: r.srv}
	accessAuditCall(t, c, "POST", "/api/admin/login", map[string]string{"Email": u.Email, "Password": "password123"}, 200)
	return u, c
}

func TestNodeAccessAuditAuthorizationAndEmbeddedScope(t *testing.T) {
	r, ib, entry, group := accessAuditFixture(t)
	base := "/api/admin/node-access"
	entryPath := "/api/admin/entries/" + itoa(entry.ID)
	policy := accessAuditPolicy(ib.ID, accessAuditGet(t, r).Revision, "groups", []int64{group}, true)
	requests := []struct {
		method, path string
		body         any
	}{
		{"GET", base, nil},
		{"PUT", base + "/inbounds/" + itoa(ib.ID), policy},
		{"PUT", base + "/groups/" + itoa(group), map[string]any{"inbound_ids": []int64{ib.ID}, "revision": policy["revision"], "confirm_shared": true}},
		{"PATCH", entryPath, map[string]any{"Name": "Forbidden rename", "Access": policy}},
		{"POST", "/api/admin/entries", map[string]any{"Name": "Forbidden entry", "InboundID": ib.ID, "DisplayHost": "node.example.com", "DisplayPort": 443, "Access": policy}},
	}
	deny := func(c *client, want int) {
		t.Helper()
		for _, req := range requests {
			out := accessAuditCall(t, c, req.method, req.path, req.body, want)
			if strings.Contains(string(out), entry.Name) || strings.Contains(string(out), "node.example.com") {
				t.Fatal("denied response exposed hidden resource metadata")
			}
		}
	}
	deny(&client{t: t, srv: r.srv}, 401)
	r.user("access-customer@example.com")
	customer := &client{t: t, srv: r.srv}
	accessAuditCall(t, customer, "POST", "/api/portal/login", map[string]string{"Email": "access-customer@example.com", "Password": "password123"}, 200)
	deny(customer, 401)
	for _, role := range []string{domain.RoleOperator, domain.RoleSupport} {
		u, c := accessAuditStaff(t, r, role)
		deny(c, 403)
		deny(accessAuditToken(t, r, u.ID, "full", []string{"*"}), 403)
	}
	for _, scopes := range [][]string{{"entries:*"}, {"nodes:*"}, {"plans:*"}, {"groups:*"}, {"entries:write", "node-access:read"}, {"PATCH /api/admin/entries/{id}"}} {
		c := accessAuditToken(t, r, 1, "full", scopes)
		want := 403
		if slices.Contains(scopes, "node-access:read") {
			want = 200
		}
		accessAuditCall(t, c, "GET", base, nil, want)
		accessAuditCall(t, c, "PUT", base+"/inbounds/"+itoa(ib.ID), policy, 403)
		accessAuditCall(t, c, "PUT", base+"/groups/"+itoa(group), map[string]any{"inbound_ids": []int64{ib.ID}, "revision": policy["revision"], "confirm_shared": true}, 403)
		accessAuditCall(t, c, "PATCH", entryPath, map[string]any{"Access": policy}, 403)
		accessAuditCall(t, c, "POST", "/api/admin/entries", map[string]any{"Name": "Forbidden token entry", "InboundID": ib.ID, "DisplayHost": "node.example.com", "DisplayPort": 443, "Access": policy}, 403)
	}
	read := accessAuditToken(t, r, 1, "read", []string{"entries:*", "node-access:*"})
	accessAuditCall(t, read, "GET", base, nil, 200)
	accessAuditCall(t, read, "PATCH", entryPath, map[string]any{"Access": policy}, 403)
	accessAuditCall(t, read, "PUT", base+"/inbounds/"+itoa(ib.ID), policy, 403)
	// An entries-only token still edits ordinary metadata without changing access.
	narrow := accessAuditToken(t, r, 1, "full", []string{"entries:write"})
	accessAuditCall(t, narrow, "PATCH", entryPath, map[string]any{"Name": "Allowed metadata rename"}, 200)
	row := accessAuditFind(t, accessAuditGet(t, r), ib.ID)
	if row.Policy != "all" || !row.Legacy {
		t.Fatal("metadata-only edit modified legacy access", row)
	}
	policy["revision"] = accessAuditGet(t, r).Revision
	both := accessAuditToken(t, r, 1, "full", []string{"entries:write", "node-access:write"})
	accessAuditCall(t, both, "PATCH", entryPath, map[string]any{"Access": policy}, 200)
	row = accessAuditFind(t, accessAuditGet(t, r), ib.ID)
	if row.Policy != "groups" || !slices.Equal(row.GroupIDs, []int64{group}) || row.Legacy {
		t.Fatal("dual-scope access edit did not apply", row)
	}
}

func TestNodeAccessAuditSharedConfirmationCASAndBulkAtomicity(t *testing.T) {
	r, ib, entry, group := accessAuditFixture(t)
	base := "/api/admin/node-access"
	before := accessAuditGet(t, r)
	accessAuditCall(t, r.c, "POST", "/api/admin/entries", map[string]any{"Name": "Second endpoint", "InboundID": ib.ID, "DisplayHost": "second.example.com", "DisplayPort": 443}, 200)
	after := accessAuditGet(t, r)
	if before.Revision == after.Revision {
		t.Fatal("adding a shared endpoint did not invalidate the access draft")
	}
	path := base + "/inbounds/" + itoa(ib.ID)
	accessAuditCall(t, r.c, "PUT", path, accessAuditPolicy(ib.ID, before.Revision, "groups", []int64{group}, true), 409)
	accessAuditCall(t, r.c, "PUT", path, accessAuditPolicy(ib.ID, after.Revision, "groups", []int64{group}, false), 409)
	if !reflect.DeepEqual(after, accessAuditGet(t, r)) {
		t.Fatal("rejected shared edit changed the access catalogue")
	}
	accessAuditCall(t, r.c, "PUT", path, accessAuditPolicy(ib.ID, after.Revision, "groups", []int64{group}, true), 200)
	grouped := accessAuditGet(t, r)
	for _, body := range []any{
		map[string]any{"inbound_ids": []int64{ib.ID, 999999}, "revision": grouped.Revision, "confirm_shared": true},
		map[string]any{"inbound_ids": []int64{-1}, "revision": grouped.Revision, "confirm_shared": true},
	} {
		accessAuditCall(t, r.c, "PUT", base+"/groups/"+itoa(group), body, 400)
		if !reflect.DeepEqual(grouped, accessAuditGet(t, r)) {
			t.Fatal("invalid whole-group edit applied a partial permission change")
		}
	}
	accessAuditCall(t, r.c, "PUT", base+"/groups/"+itoa(group), map[string]any{"inbound_ids": []int64{}, "revision": grouped.Revision, "confirm_shared": false}, 409)
	accessAuditCall(t, r.c, "PUT", base+"/groups/"+itoa(group), map[string]any{"inbound_ids": []int64{}, "revision": grouped.Revision, "confirm_shared": true}, 200)
	closed := accessAuditGet(t, r)
	if row := accessAuditFind(t, closed, ib.ID); row.Policy != "none" || len(row.GroupIDs) != 0 {
		t.Fatal("removing the last group reopened a shared inbound", row)
	}
	// Buying/editing a plan changes the impact of a group edit; old drafts cannot
	// silently continue with the former set of affected plans.
	plan := mustJSON[domain.Plan](t, accessAuditCall(t, r.c, "POST", "/api/admin/plans", map[string]any{"Name": "Permission audit plan", "PriceCents": 1, "PeriodDays": 30, "GroupID": group}, 200))
	if plan.ID == 0 || accessAuditGet(t, r).Revision == closed.Revision {
		t.Fatal("plan-to-group relationship did not invalidate the access draft")
	}
	accessAuditCall(t, r.c, "PATCH", "/api/admin/entries/"+itoa(entry.ID), map[string]any{"Name": "Stale rename", "Access": accessAuditPolicy(ib.ID, closed.Revision, "all", nil, true)}, 409)
	got, err := r.st.EntryByID(context.Background(), entry.ID)
	if err != nil || got.Name != entry.Name {
		t.Fatal("stale access edit saved entry metadata", err)
	}
}

func TestNodeAccessAuditEmbeddedValidationIsAtomicAndTargetBound(t *testing.T) {
	r, ib, entry, group := accessAuditFixture(t)
	second := mustJSON[domain.Inbound](t, accessAuditCall(t, r.c, "POST", "/api/admin/nodes/"+itoa(r.nodeID)+"/inbounds", map[string]any{"Tag": "second", "Protocol": "vless", "Port": 8443}, 200))
	path := "/api/admin/entries/" + itoa(entry.ID)
	snapshot := accessAuditGet(t, r)
	policies := []map[string]any{
		accessAuditPolicy(ib.ID, snapshot.Revision, "bogus", nil, true),
		accessAuditPolicy(ib.ID, snapshot.Revision, "groups", []int64{999999}, true),
		accessAuditPolicy(ib.ID, snapshot.Revision, "groups", []int64{}, true),
		accessAuditPolicy(ib.ID, snapshot.Revision, "none", []int64{group}, true),
		accessAuditPolicy(0, snapshot.Revision, "none", nil, true),
	}
	for _, policy := range policies {
		accessAuditCall(t, r.c, "PATCH", path, map[string]any{"Name": "Must not persist", "Access": policy}, 400)
		accessAuditCall(t, r.c, "POST", "/api/admin/entries", map[string]any{"Name": "Must not be created", "InboundID": ib.ID, "DisplayHost": "node.example.com", "DisplayPort": 443, "Access": policy}, 400)
		if !reflect.DeepEqual(snapshot, accessAuditGet(t, r)) {
			t.Fatal("invalid embedded access changed catalogue")
		}
		entries, err := r.st.ListEntries(context.Background())
		if err != nil || len(entries) != 1 || entries[0].Name != entry.Name {
			t.Fatal("invalid embedded access saved entry metadata", err)
		}
	}
	// A policy confirmed for inbound A cannot be applied to B by changing the
	// entry target in the same request, even with a fresh global revision.
	accessAuditCall(t, r.c, "PATCH", path, map[string]any{"Name": "Rebound", "InboundID": second.ID, "Access": accessAuditPolicy(ib.ID, snapshot.Revision, "none", nil, true)}, 400)
	got, err := r.st.EntryByID(context.Background(), entry.ID)
	if err != nil || got.InboundID != ib.ID || got.Name != entry.Name || !reflect.DeepEqual(snapshot, accessAuditGet(t, r)) {
		t.Fatal("mismatched access target partially changed entry or permissions", err)
	}
	// Valid combined changes take effect together, while later ordinary edits
	// retain explicit deny rather than interpreting nil legacy GroupID as public.
	accessAuditCall(t, r.c, "PATCH", path, map[string]any{"Name": "Closed endpoint", "Access": accessAuditPolicy(ib.ID, snapshot.Revision, "none", nil, true)}, 200)
	accessAuditCall(t, r.c, "PATCH", path, map[string]any{"Name": "Closed endpoint renamed"}, 200)
	row := accessAuditFind(t, accessAuditGet(t, r), ib.ID)
	if row.Policy != "none" || row.Legacy || len(row.GroupIDs) != 0 {
		t.Fatal("ordinary entry edit reopened explicit deny", row)
	}
	current := accessAuditGet(t, r)
	created := mustJSON[domain.Entry](t, accessAuditCall(t, r.c, "POST", "/api/admin/entries", map[string]any{
		"Name": "Created with restricted access", "InboundID": second.ID, "DisplayHost": "node.example.com", "DisplayPort": 8443,
		"Access": accessAuditPolicy(second.ID, current.Revision, "groups", []int64{group}, true),
	}, 200))
	row = accessAuditFind(t, accessAuditGet(t, r), second.ID)
	if created.ID <= 0 || created.InboundID != second.ID || row.Policy != "groups" || !slices.Equal(row.GroupIDs, []int64{group}) {
		t.Fatal("entry creation and permission update did not commit together", row)
	}
	// The catalogue is a public contract; its revision must remain a JSON string.
	raw := accessAuditCall(t, r.c, "GET", "/api/admin/node-access", nil, 200)
	var encoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &encoded); err != nil || len(encoded["revision"]) == 0 || encoded["revision"][0] != '"' {
		t.Fatal("access revision is not a JSON string")
	}
}
