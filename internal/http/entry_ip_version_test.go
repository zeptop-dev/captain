package http

import (
	"context"
	"testing"

	"github.com/zeptop-dev/captain/internal/domain"
)

func TestEntryIPVersionAPILegacyAndValidation(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	ibs, err := r.st.InboundsByNode(ctx, r.nodeID)
	if err != nil || len(ibs) != 1 {
		t.Fatal(ibs, err)
	}
	call := func(method, path string, payload any, want int) []byte {
		t.Helper()
		return accessAuditCall(t, r.c, method, path, payload, want)
	}
	legacy := mustJSON[domain.Entry](t, call("POST", "/api/admin/entries", map[string]any{"Name": "Legacy", "InboundID": ibs[0].ID, "DisplayHost": "node.example.com", "DisplayPort": 443, "ClientExtra": map[string]any{"ip-version": "ipv4"}}, 200))
	if legacy.IPVersion != "" {
		t.Fatal("legacy request changed", legacy)
	}
	path := "/api/admin/entries/" + itoa(legacy.ID)
	call("PATCH", path, map[string]any{"IPVersion": "ipv6"}, 400)
	explicit := mustJSON[domain.Entry](t, call("PATCH", path, map[string]any{"IPVersion": "ipv6", "ClientExtra": nil}, 200))
	if explicit.IPVersion != "ipv6" {
		t.Fatal(explicit)
	}
	for _, patch := range []map[string]any{{"Name": "Renamed"}, {"IPVersion": nil}} {
		got := mustJSON[domain.Entry](t, call("PATCH", path, patch, 200))
		if got.IPVersion != "ipv6" {
			t.Fatal("omission cleared policy", got)
		}
	}
	for _, patch := range []map[string]any{{"IPVersion": "bad"}, {"DisplayHost": "192.0.2.10"}, {"ClientExtra": map[string]any{"server": "other.example.com"}}, {"IPVersion": "dual", "DisplayHost": "2001:db8::10"}} {
		call("PATCH", path, patch, 400)
	}
	got, err := r.st.EntryByID(ctx, legacy.ID)
	if err != nil || got.IPVersion != "ipv6" || got.DisplayHost != "node.example.com" {
		t.Fatal("failed request wrote data", got, err)
	}
	cleared := mustJSON[domain.Entry](t, call("PATCH", path, map[string]any{"IPVersion": "", "ClientExtra": map[string]any{"ip-version": "ipv4"}}, 200))
	if cleared.IPVersion != "" {
		t.Fatal(cleared)
	}
	batch := mustJSON[struct {
		Entries []domain.Entry `json:"entries"`
	}](t, call("POST", "/api/admin/entries/variants", map[string]any{"Name": "Choice", "InboundID": ibs[0].ID, "DisplayHost": "node.example.com", "DisplayPort": 443}, 200))
	if len(batch.Entries) != 3 {
		t.Fatal(batch)
	}
	for i, mode := range []string{"ipv4", "ipv6", "dual"} {
		if batch.Entries[i].IPVersion != mode || batch.Entries[i].InboundID != ibs[0].ID {
			t.Fatal(batch)
		}
	}
	call("POST", "/api/admin/entries/variants", map[string]any{"Name": "Bad", "InboundID": ibs[0].ID, "DisplayHost": "192.0.2.10", "DisplayPort": 443}, 400)
	all, err := r.st.ListEntries(ctx)
	if err != nil || len(all) != 4 {
		t.Fatal("invalid batch partially inserted", all, err)
	}
}

func TestEntryIPVariantsPermissionsAndAtomicAccess(t *testing.T) {
	r, ib, _, group := accessAuditFixture(t)
	path := "/api/admin/entries/variants"
	payload := map[string]any{"Name": "Variants", "InboundID": ib.ID, "DisplayHost": "node.example.com", "DisplayPort": 443}
	for _, tc := range []struct {
		c    *client
		want int
	}{
		{&client{t: t, srv: r.srv}, 401},
		{accessAuditToken(t, r, 1, "full", []string{"entries:read"}), 403},
		{accessAuditToken(t, r, 1, "read", []string{"entries:write"}), 403},
		{accessAuditToken(t, r, 1, "full", []string{"nodes:write"}), 403},
	} {
		accessAuditCall(t, tc.c, "POST", path, payload, tc.want)
	}
	_, support := accessAuditStaff(t, r, domain.RoleSupport)
	accessAuditCall(t, support, "POST", path, payload, 403)
	_, operator := accessAuditStaff(t, r, domain.RoleOperator)
	accessAuditCall(t, operator, "POST", path, map[string]any{"Name": "Forbidden line", "LineID": 1}, 403)
	writer := accessAuditToken(t, r, 1, "full", []string{"entries:write"})
	accessAuditCall(t, writer, "POST", path, payload, 200)
	payload["Access"] = accessAuditPolicy(ib.ID, accessAuditGet(t, r).Revision, "groups", []int64{group}, true)
	accessAuditCall(t, writer, "POST", path, payload, 403)
	accessAuditCall(t, operator, "POST", path, payload, 403)
	both := accessAuditToken(t, r, 1, "full", []string{"entries:write", "node-access:write"})
	accessAuditCall(t, both, "POST", path, payload, 200)
	// Reusing an access snapshot cannot create another partial batch.
	accessAuditCall(t, both, "POST", path, payload, 409)
}

func TestEntryIPVersionAPIRejectsWireGuardHostnamePolicy(t *testing.T) {
	r, _, ib, e := nativeWGFixture(t)
	for _, method := range []string{"POST", "PATCH"} {
		path := "/api/admin/entries"
		if method == "PATCH" {
			path += "/" + itoa(e.ID)
		}
		accessAuditCall(t, r.c, method, path, map[string]any{"Name": "WG IPv4", "InboundID": ib.ID, "DisplayHost": "wg.example.com", "DisplayPort": 51820, "IPVersion": "ipv4"}, 400)
	}
	accessAuditCall(t, r.c, "POST", "/api/admin/entries/variants", map[string]any{"Name": "WG", "InboundID": ib.ID, "DisplayHost": "wg.example.com", "DisplayPort": 51820}, 400)
	saved, err := r.st.EntryByID(context.Background(), e.ID)
	if err != nil || saved.IPVersion != "" {
		t.Fatal("failed request changed WG entry", saved, err)
	}
}
