package http

import (
	"github.com/zeptop-dev/bosun/pkg/subdesign"
	"strings"
	"testing"
)

func TestDNSFollowDraftAndPublishedPolicy(t *testing.T) {
	r := newRig(t)
	d, _ := subdesign.Preset("basic")
	d.Groups = []subdesign.Group{{Name: "Main", Type: "select", Members: []string{"@all", "DIRECT"}}}
	d.Final = "Main"
	d.Enhancements = subdesign.Enhancements{DNSProtection: true, DNSMode: "follow"}
	root := "/api/admin/settings/sub-design"
	if code, b, _ := r.c.do("PUT", root, map[string]any{"design": d}, nil); code != 200 {
		t.Fatal(code, string(b))
	}
	code, b, _ := r.c.do("GET", root, nil, nil)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	saved := mustJSON[struct {
		Design subdesign.Design `json:"design"`
	}](t, b)
	if saved.Design.Enhancements.DNSMode != "follow" {
		t.Fatal("mode lost")
	}
	code, b, _ = r.c.do("POST", root+"/preview", map[string]any{"design": d, "format": "clash"}, nil)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	if !strings.Contains(string(b), "dns-query#Main") {
		t.Fatal(string(b))
	}
	if code, b, _ := r.c.do("POST", root+"/apply", map[string]any{"design": d, "formats": []string{"clash"}}, nil); code != 200 {
		t.Fatal(code, string(b))
	}
	d.Enhancements.DNSMode = "custom"
	d.Enhancements.DNSPolicy = "Main"
	if code, _, _ := r.c.do("PUT", root, map[string]any{"design": d}, nil); code != 400 {
		t.Fatal("custom direct accepted", code)
	}
}
