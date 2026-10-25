package http

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/zeptop-dev/bosun/pkg/subdesign"
	"github.com/zeptop-dev/captain/internal/domain"
)

func TestSubDesignRegionsUseRegisteredExitBeforeDecoratedNames(t *testing.T) {
	r := newRig(t)
	uid, path, profile := profileSetup(t, r)
	ctx := context.Background()
	entries, err := r.st.ListEntries(ctx)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries: %v", err)
	}
	create := func(name, region, host string) domain.Entry {
		t.Helper()
		code, body, _ := r.c.do("POST", "/api/admin/entries", map[string]any{
			"Name": name, "Region": region, "InboundID": entries[0].InboundID,
			"DisplayHost": host, "DisplayPort": 443,
		}, nil)
		if code != 200 {
			t.Fatalf("create entry: %d %s", code, body)
		}
		return mustJSON[domain.Entry](t, body)
	}
	hk := create("IPLC", "HK", "jp-ingress.example.com")
	conflicting := create("🇺🇸 US labelled exit", "HK", "node.example.com")
	create("Tokyo direct", "", "node.example.com")
	create("Unclassified exit", "", "hk-ingress.example.com")
	// Country-like prefixes and host-derived display flags must not change
	// region membership. Explicit metadata still overrides an existing flag.
	yes := true
	profile.Templates = nil
	profile.Settings.AutoFlags = &yes
	profile.Settings.RemarkPrefix = "SG Customer "
	if err := r.st.SaveSubscriptionProfile(ctx, &profile); err != nil {
		t.Fatal(err)
	}
	if err := r.st.AssignSubscriptionProfile(ctx, uid, &profile.ID); err != nil {
		t.Fatal(err)
	}
	design, _ := subdesign.Preset("basic")
	design.Enhancements.Regions = true
	if code, body, _ := r.c.do("POST", "/api/admin/settings/sub-design/apply", map[string]any{
		"design": design, "formats": []string{"clash", "stash"},
	}, nil); code != 200 {
		t.Fatalf("apply regions: %d %s", code, body)
	}
	visitor := &client{t: t, srv: r.srv}
	for _, format := range []string{"clash", "stash"} {
		t.Run(format, func(t *testing.T) {
			code, body, _ := visitor.do("GET", path+"?client="+format, nil, nil)
			if code != 200 {
				t.Fatalf("deliver: %d %s", code, body)
			}
			doc := decodeSubDesignDelivery(t, body)
			if len(doc.Proxies) != 5 {
				t.Fatalf("region grouping changed accessible nodes: %d", len(doc.Proxies))
			}
			seen := map[string]bool{}
			for _, group := range doc.Groups {
				seen[group.Name] = true
				switch group.Name {
				case "🇭🇰 香港":
					want := []string{"🇭🇰 香港 · 自动", "SG Customer 🇭🇰 IPLC", "SG Customer 🇺🇸 US labelled exit"}
					if group.Type != "select" || !slices.Equal(group.Proxies, want) {
						t.Fatalf("registered HK group: %#v", group)
					}
				case "🇭🇰 香港 · 自动":
					if group.Type != "url-test" || len(group.Proxies) != 2 {
						t.Fatalf("HK automatic candidates: %#v", group)
					}
				case "🇯🇵 日本 · 自动":
					if !slices.Equal(group.Proxies, []string{"SG Customer 🇯🇵 Tokyo direct"}) {
						t.Fatalf("name fallback included ingress or profile region: %#v", group)
					}
				case "🇸🇬 新加坡", "🇺🇸 美国", "🇹🇼 台湾", "🇰🇷 韩国":
					t.Fatalf("empty or misclassified region retained: %s", group.Name)
				}
			}
			for _, group := range []string{"🇭🇰 香港", "🇭🇰 香港 · 自动", "🇯🇵 日本", "🇯🇵 日本 · 自动"} {
				if !seen[group] {
					t.Fatalf("missing %s", group)
				}
			}
		})
	}
	if err := r.st.SetUserEntryBlocks(ctx, uid, []int64{hk.ID, conflicting.ID}); err != nil {
		t.Fatal(err)
	}
	code, body, _ := visitor.do("GET", path+"?client=clash", nil, nil)
	if code != 200 {
		t.Fatalf("delivery after access removal: %d %s", code, body)
	}
	doc := decodeSubDesignDelivery(t, body)
	if len(doc.Proxies) != 3 {
		t.Fatal("region grouping bypassed entry access controls")
	}
	for _, group := range doc.Groups {
		if strings.HasPrefix(group.Name, "🇭🇰 香港") || slices.Contains(group.Proxies, "🇭🇰 香港") {
			t.Fatalf("empty HK group or dangling parent reference after access removal: %#v", group)
		}
	}
}
