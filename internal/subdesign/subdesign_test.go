package subdesign

import (
	"strings"
	"testing"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/bosun/pkg/subscription"
	"gopkg.in/yaml.v3"
)

func TestPresetsRenderEverywhere(t *testing.T) {
	lines := []subscription.Line{
		{Name: "🇭🇰 HK-1", Host: "203.0.113.30", Port: 443, UUID: "11111111-1111-1111-1111-111111111111", Tags: []string{"hk"},
			Inbound: spec.Inbound{Protocol: spec.Shadowsocks, Cipher: "aes-128-gcm", Port: 443}, Password: "pw"},
		{Name: "🇯🇵 JP-1", Host: "203.0.113.30", Port: 444, UUID: "11111111-1111-1111-1111-111111111111",
			Inbound: spec.Inbound{Protocol: spec.Shadowsocks, Cipher: "aes-128-gcm", Port: 444}, Password: "pw"},
	}
	for _, p := range Presets {
		d, ok := Preset(p.Key)
		if !ok {
			t.Fatalf("preset %s missing", p.Key)
		}
		all, err := d.RenderAll()
		if err != nil {
			t.Fatalf("%s: %v", p.Key, err)
		}
		for format, tpl := range all {
			r := subscription.Pick(format, "")
			if r == nil || r.Name() != format {
				t.Fatalf("no renderer for %s", format)
			}
			out, err := r.(interface {
				RenderWith([]subscription.Line, subscription.Account, string) ([]byte, error)
			}).RenderWith(lines, subscription.Account{}, tpl)
			if err != nil {
				t.Fatalf("%s/%s: %v\n%s", p.Key, format, err, tpl)
			}
			s := string(out)
			if strings.Contains(s, "{{") {
				t.Fatalf("%s/%s: placeholder left:\n%s", p.Key, format, s)
			}
			if format == "clash" || format == "stash" || format == "egern" {
				var doc map[string]any
				if err := yaml.Unmarshal(out, &doc); err != nil {
					t.Fatalf("%s/%s: bad yaml: %v", p.Key, format, err)
				}
			}
		}
		// Region groups only hold the matching servers.
		if p.Key == "acl4ssr-regions" {
			out, _ := subscription.Clash{}.RenderWith(lines, subscription.Account{}, all["clash"])
			var doc struct {
				Groups []struct {
					Name    string   `yaml:"name"`
					Proxies []string `yaml:"proxies"`
				} `yaml:"proxy-groups"`
			}
			if err := yaml.Unmarshal(out, &doc); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, g := range doc.Groups {
				if g.Name == "🇭🇰 香港" {
					found = true
					if len(g.Proxies) != 1 || g.Proxies[0] != "🇭🇰 HK-1" {
						t.Fatalf("HK group members: %v", g.Proxies)
					}
				}
			}
			if !found {
				t.Fatalf("HK group missing:\n%s", out)
			}
		}
	}
}

func TestValidate(t *testing.T) {
	d := Design{Groups: []Group{{Name: "A", Type: "select", Members: []string{"B"}}}, Final: "A"}
	if err := d.Validate(); err == nil || !strings.Contains(err.Error(), "unknown member") {
		t.Fatalf("want unknown member, got %v", err)
	}
	d = Design{Groups: []Group{{Name: "A", Type: "select", Members: []string{"@match:("}}}, Final: "A"}
	if err := d.Validate(); err == nil || !strings.Contains(err.Error(), "bad pattern") {
		t.Fatalf("want bad pattern, got %v", err)
	}
	d = Design{Groups: []Group{{Name: "A", Type: "select", Members: []string{"@all"}}}, Rules: []Rule{{"nope", "A"}}, Final: "A"}
	if err := d.Validate(); err == nil || !strings.Contains(err.Error(), "unknown rule set") {
		t.Fatalf("want unknown rule set, got %v", err)
	}
}
