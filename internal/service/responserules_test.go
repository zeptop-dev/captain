package service

import (
	"context"
	"net/http/httptest"
	"testing"
)

func TestResponseRules(t *testing.T) {
	rules := []ResponseRule{
		{Name: "off", Enabled: false, Match: []HeaderMatch{{Header: "User-Agent", Op: "contains", Value: "happ"}}, Action: "block"},
		{Name: "happ-json", Enabled: true, Match: []HeaderMatch{{Header: "User-Agent", Op: "contains", Value: "Happ"}, {Header: "x-hwid", Op: "exists"}}, Action: "serve", Format: "mihomo"},
		{Name: "bots", Enabled: true, AnyOf: true, Match: []HeaderMatch{{Header: "user-agent", Op: "regex", Value: `(?i)curl|wget`}, {Header: "client", Op: "equals", Value: "probe"}}, Action: "not_found"},
		{Name: "old-app", Enabled: true, Match: []HeaderMatch{{Header: "x-device-os", Op: "missing"}, {Header: "User-Agent", Op: "prefix", Value: "OldApp/"}}, Action: "unavailable"},
	}
	if msg := ValidateResponseRules(context.Background(), &rules); msg != "" {
		t.Fatal(msg)
	}
	if rules[1].Format != "clash" {
		t.Fatalf("alias not normalised: %q", rules[1].Format)
	}
	req := func(ua string, hdr map[string]string, query string) *ResponseRule {
		r := httptest.NewRequest("GET", "/sub/x"+query, nil)
		r.Header.Set("User-Agent", ua)
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		return MatchResponseRule(rules, r)
	}
	name := func(r *ResponseRule) string {
		if r == nil {
			return ""
		}
		return r.Name
	}
	cases := []struct {
		ua    string
		hdr   map[string]string
		query string
		want  string
	}{
		{"Happ/2.0", map[string]string{"x-hwid": "abc"}, "", "happ-json"},
		{"Happ/2.0", nil, "", ""},   // needs both conditions; the disabled rule never fires
		{"curl/8.0", nil, "", "bots"},
		{"clash-verge", nil, "?client=probe", "bots"},
		{"OldApp/1.0", nil, "", "old-app"},
		{"OldApp/1.0", map[string]string{"x-device-os": "ios"}, "", ""},
		{"mihomo", nil, "", ""},
	}
	for _, c := range cases {
		if got := name(req(c.ua, c.hdr, c.query)); got != c.want {
			t.Errorf("%s %v %s: got %q want %q", c.ua, c.hdr, c.query, got, c.want)
		}
	}
	bad := []ResponseRule{{Name: "x", Enabled: true, Match: []HeaderMatch{{Header: "User-Agent", Op: "regex", Value: "("}}}}
	if msg := ValidateResponseRules(context.Background(), &bad); msg == "" {
		t.Fatal("bad regex accepted")
	}
	bad = []ResponseRule{{Name: "x", Enabled: true, Action: "serve", Format: "nope"}}
	if msg := ValidateResponseRules(context.Background(), &bad); msg == "" {
		t.Fatal("unknown format accepted")
	}
	bad = []ResponseRule{{Name: "a"}, {Name: "a"}}
	if msg := ValidateResponseRules(context.Background(), &bad); msg == "" {
		t.Fatal("duplicate name accepted")
	}
}
