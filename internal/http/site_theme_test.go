package http

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/zeptop-dev/captain/internal/http/site"
)

func TestSiteThemeAdminRoundTripAndPublicProjection(t *testing.T) {
	p := newPortalRig(t, true)
	visitor := &client{t: t, srv: p.srv}
	code, raw, _ := p.admin.do("GET", "/api/admin/settings/site", nil, nil)
	if code != 200 {
		t.Fatalf("read settings: %d %s", code, raw)
	}
	settings := mustJSON[site.Settings](t, raw)
	settings.InjectHead = "<script>/* private-head-marker */</script>"
	settings.InjectBody = "<div>private-body-marker</div>"
	settings.Theme = site.Theme{Primary: "teal", Radius: "sm", Scheme: "dark", FontFamily: "Example Font, sans-serif", PortalTitle: "Example portal"}
	for _, scheme := range []string{"light", "auto", "dark"} {
		t.Run(scheme, func(t *testing.T) {
			settings.Theme.SiteScheme = scheme
			code, raw, _ := p.admin.do("PUT", "/api/admin/settings/site", settings, nil)
			if code != 200 {
				t.Fatalf("save settings: %d %s", code, raw)
			}
			// A current editor carries the returned publication revision into its
			// next save. Older editors omit the new map-selection fields entirely.
			saved := mustJSON[site.Settings](t, raw)
			settings.MapRevision = saved.MapRevision
			code, raw, headers := visitor.do("GET", "/api/site", nil, nil)
			if code != 200 || headers.Get("Cache-Control") != "no-store" {
				t.Fatalf("public settings: %d %s", code, raw)
			}
			got := mustJSON[struct {
				Theme site.Theme `json:"theme"`
			}](t, raw)
			if !reflect.DeepEqual(got.Theme, settings.Theme) {
				t.Fatalf("public theme = %+v, want %+v", got.Theme, settings.Theme)
			}
			for _, forbidden := range []string{"inject_head", "inject_body", "private-head-marker", "private-body-marker", "map_entry_ids", "map_revision"} {
				if bytes.Contains(raw, []byte(forbidden)) {
					t.Fatalf("public response exposed %q", forbidden)
				}
			}
			projected := mustJSON[map[string]any](t, raw)
			allowed := map[string]bool{"homepage_enabled": true, "name": true, "tagline": true, "description": true, "features": true, "locations": true, "hub": true, "faq": true, "links": true, "show_plans": true, "registration": true, "probe_url": true, "service_status_url": true, "stats": true, "theme": true, "map_source": true, "status_map_source": true, "status_locations": true, "status_hub": true}
			for key := range projected {
				if !allowed[key] {
					t.Fatalf("unexpected public field %q", key)
				}
			}
		})
	}
}
