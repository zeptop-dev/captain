package http

import (
	"context"
	"testing"

	"github.com/zeptop-dev/captain/internal/domain"
)

func TestMapLocationTokenScopesAndLegacyEntryPatchAudit(t *testing.T) {
	r := newRig(t)
	c := &client{t: t, srv: r.srv}
	ctx := context.Background()
	inbounds, err := r.st.InboundsByNode(ctx, r.nodeID)
	if err != nil || len(inbounds) != 1 {
		t.Fatal(inbounds, err)
	}
	ib := inbounds[0]
	status, b, _ := r.c.do("POST", "/api/admin/entries", map[string]any{"Name": "Map node", "InboundID": ib.ID, "DisplayHost": "example.com", "DisplayPort": 443}, nil)
	if status != 200 {
		t.Fatal(status, string(b))
	}
	e := mustJSON[domain.Entry](t, b)
	cfg := domain.LocationSettings{Mode: "manual", Manual: &domain.GeoLocation{Country: "JP", Precision: "country"}}
	path := "/api/admin/entries/" + itoa(e.ID) + "/location"
	for _, tc := range []struct {
		base, grant string
		want        int
	}{
		{"full", "entries:read", 403},
		{"full", "entry-categories:write", 403},
		{"full", "nodes:write", 403},
		{"read", "entries:write", 403},
		{"full", "entries:write", 200},
		{"full", "PUT /api/admin/entries/{id}/location", 200},
	} {
		token, _, err := r.st.CreateAPIToken(ctx, 1, "map audit", tc.base, nil, []string{tc.grant})
		if err != nil {
			t.Fatal(err)
		}
		got, b, _ := c.do("PUT", path, cfg, map[string]string{"Authorization": "Bearer " + token})
		if got != tc.want {
			t.Fatal(tc, got, string(b))
		}
	}
	if status, b, _ = r.c.do("PATCH", "/api/admin/entries/"+itoa(e.ID), map[string]any{"Name": "Renamed map node"}, nil); status != 200 {
		t.Fatal(status, string(b))
	}
	settings, err := r.st.EntryLocationSettings(ctx)
	if err != nil || settings[e.ID] == nil || settings[e.ID].Mode != "manual" || settings[e.ID].Manual.Country != "JP" {
		t.Fatal(settings, err)
	}
	// A forged display field in the ordinary editor is not the separate metadata
	// operation, and cannot erase an existing manual override.
	if status, b, _ = r.c.do("PATCH", "/api/admin/entries/"+itoa(e.ID), map[string]any{"LocationSettings": domain.LocationSettings{Mode: "off"}}, nil); status != 200 {
		t.Fatal(status, string(b))
	}
	settings, err = r.st.EntryLocationSettings(ctx)
	if err != nil || settings[e.ID].Mode != "manual" {
		t.Fatal(settings, err)
	}
}
