package service

import (
	"encoding/json"
	"testing"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/bosun/pkg/subscription"
	"gopkg.in/yaml.v3"
)

func chainFixture() []PublishedLine {
	return []PublishedLine{
		{EntryID: 1, Line: subscription.Line{Name: "Front renamed", Host: "front.example.com", Port: 443, UUID: "user", Inbound: spec.Inbound{Protocol: spec.VLESS}}},
		{EntryID: 2, DialerEntryID: 1, Line: subscription.Line{Name: "Home via front", Host: "exit.example.com", Port: 8388, Password: "secret", Inbound: spec.Inbound{Protocol: spec.Shadowsocks, Cipher: "aes-128-gcm"}, Extra: map[string]any{"tfo": true, "dialer-proxy": "stale-name", "detour": "stale-name"}}},
	}
}

func TestClientChainRendersCurrentNameAndKeepsLandingEndpoint(t *testing.T) {
	for _, format := range []string{"clash", "stash", "singbox"} {
		t.Run(format, func(t *testing.T) {
			input := chainFixture()
			lines, err := ClientLines(input, format)
			if err != nil || len(lines) != 2 {
				t.Fatalf("lines=%+v err=%v", lines, err)
			}
			body, err := subscription.Pick(format, "").Render(lines, subscription.Account{})
			if err != nil {
				t.Fatal(err)
			}
			var doc map[string]any
			if format == "singbox" {
				err = json.Unmarshal(body, &doc)
			} else {
				err = yaml.Unmarshal(body, &doc)
			}
			if err != nil {
				t.Fatal(err)
			}
			collection, name, port, dialer := "proxies", "name", "port", "dialer-proxy"
			if format == "singbox" {
				collection, name, port, dialer = "outbounds", "tag", "server_port", "detour"
			}
			found := false
			for _, raw := range doc[collection].([]any) {
				p := raw.(map[string]any)
				if p[name] != "Home via front" {
					continue
				}
				found = true
				if p[dialer] != "Front renamed" || p["server"] != "exit.example.com" || p["tfo"] != true {
					t.Fatalf("proxy=%v", p)
				}
				if p[port] != 8388 && p[port] != float64(8388) {
					t.Fatalf("port=%v", p[port])
				}
			}
			if !found {
				t.Fatal("chain missing from rendered document")
			}
			if input[1].Extra["dialer-proxy"] != "stale-name" {
				t.Fatal("mutated stored extra fields")
			}
		})
	}
}

func TestClientChainFailsClosedForMissingAmbiguousAndUnsupportedFront(t *testing.T) {
	for _, tc := range []struct {
		name, format string
		change       func([]PublishedLine) []PublishedLine
	}{
		{"missing", "clash", func(v []PublishedLine) []PublishedLine { return v[1:] }},
		{"duplicate", "clash", func(v []PublishedLine) []PublishedLine { return append(v, PublishedLine{Line: v[0].Line}) }},
		{"cycle", "clash", func(v []PublishedLine) []PublishedLine { v[0].DialerEntryID = 2; return v }},
		{"raw-name", "clash", func(v []PublishedLine) []PublishedLine { v[0].Extra = map[string]any{"name": "different"}; return v }},
		{"raw-dialer", "clash", func(v []PublishedLine) []PublishedLine {
			v[0].Extra = map[string]any{"dialer-proxy": "unmanaged"}
			return v
		}},
		{"unsupported", "singbox", func(v []PublishedLine) []PublishedLine { v[0].Inbound.Protocol = spec.Mieru; return v }},
		{"unsupported-name-collision", "singbox", func(v []PublishedLine) []PublishedLine {
			v[0].Inbound.Protocol = spec.Mieru
			other := v[0].Line
			other.Inbound.Protocol = spec.VLESS
			return append(v, PublishedLine{Line: other})
		}},
		{"unsupported-format", "surge", func(v []PublishedLine) []PublishedLine { return v }},
		{"standalone-uri", "uri", func(v []PublishedLine) []PublishedLine { return v }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lines, err := ClientLines(tc.change(chainFixture()), tc.format)
			if err != nil {
				t.Fatal(err)
			}
			for _, l := range lines {
				if l.Name == "Home via front" {
					t.Fatal("dependent node leaked as direct or invalid profile")
				}
			}
		})
	}
}

func TestClientChainTransitiveDependency(t *testing.T) {
	lines := chainFixture()
	lines = append(lines, PublishedLine{EntryID: 3, DialerEntryID: 2, Line: subscription.Line{Name: "Third", Host: "third.example.com", Port: 443, Inbound: spec.Inbound{Protocol: spec.VLESS}}})
	got, err := ClientLines(lines, "clash")
	if err != nil || len(got) != 3 || got[2].Extra["dialer-proxy"] != "Home via front" {
		t.Fatalf("got=%v err=%v", got, err)
	}
	got, err = ClientLines(lines[1:], "clash")
	if err != nil || len(got) != 0 {
		t.Fatalf("dependent descendants leaked: %v %v", got, err)
	}
}

func TestClientChainRejectsRawProxyReplacement(t *testing.T) {
	for _, format := range []string{"clash", "stash", "singbox"} {
		for _, key := range []string{"type", "Type", "server", "port", "server_port", "Server-Port", "udp", "UDP"} {
			for _, position := range []int{0, 1} {
				lines := chainFixture()
				value := any("direct")
				if key == "server" {
					value = "other.example.com"
				}
				if key == "port" || key == "server_port" {
					value = 1443
				}
				lines[position].Extra = map[string]any{key: value}
				got, err := ClientLines(lines, format)
				if err != nil {
					t.Fatal(err)
				}
				for _, l := range got {
					if l.Name == "Home via front" {
						t.Fatalf("%s position %d extra %s bypassed managed path", format, position, key)
					}
				}
			}
		}
	}
}

func TestClientChainCanonicalDialerAndTemplateNames(t *testing.T) {
	for _, format := range []string{"clash", "stash"} {
		for _, key := range []string{"dialer_proxy", "Dialer-Proxy", "DETOUR"} {
			lines := chainFixture()
			lines[0].Extra = map[string]any{key: "DIRECT"}
			got, err := ClientLines(lines, format)
			if err != nil || len(got) != 1 {
				t.Fatalf("alias %s leaked dependency: %+v %v", key, got, err)
			}
			lines = chainFixture()
			lines[1].Extra = map[string]any{key: "DIRECT", "Interface_Name": "en0"}
			got, err = ClientLines(lines, format)
			if err != nil || len(got) != 2 || got[1].Extra[key] != nil || got[1].Extra["dialer-proxy"] != lines[0].Name {
				t.Fatalf("alias conflict: %+v %v", got, err)
			}
			if format == "stash" && got[1].Extra["Interface_Name"] != nil {
				t.Fatal("Stash interface-name conflict")
			}
		}
		lines := chainFixture()
		template := "proxy-groups:\n  - name: '" + lines[0].Name + "'\n    type: select\n    proxies: [DIRECT]\nrules: []\n"
		got, err := ClientLines(lines, format, template)
		if err != nil || len(got) != 1 {
			t.Fatalf("actual template collision: %+v %v", got, err)
		}
		lines[0].Name = "DIRECT"
		got, err = ClientLines(lines, format)
		if err != nil || len(got) != 1 {
			t.Fatalf("reserved name was a dialer: %+v %v", got, err)
		}
	}
}

func TestClientChainHTTPDestinationCannotFallBackDirectForUDP(t *testing.T) {
	for _, format := range []string{"clash", "stash", "singbox"} {
		t.Run(format, func(t *testing.T) {
			input := chainFixture()
			input[1].Inbound.Protocol = spec.HTTP
			input = append(input,
				PublishedLine{EntryID: 3, DialerEntryID: 2, Line: subscription.Line{Name: "Third", Host: "third.example.com", Port: 443, Inbound: spec.Inbound{Protocol: spec.VLESS}}},
				PublishedLine{EntryID: 4, Line: subscription.Line{Name: "Ordinary HTTP", Host: "http.example.com", Port: 8080, Inbound: spec.Inbound{Protocol: spec.HTTP}}},
			)
			lines, err := ClientLines(input, format)
			if err != nil {
				t.Fatal(err)
			}
			body, err := subscription.Pick(format, "").Render(lines, subscription.Account{})
			if err != nil {
				t.Fatal(err)
			}
			names, err := renderedClientNames(format, body)
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"Front renamed", "Ordinary HTTP"} {
				if names[name] != 1 {
					t.Fatalf("ordinary proxy %q changed: %s", name, body)
				}
			}
			want := 0
			if format == "singbox" {
				want = 1 // Unsupported UDP returns an error; it does not skip a rule.
			}
			for _, name := range []string{"Home via front", "Third"} {
				if names[name] != want {
					t.Fatalf("unsafe HTTP chain or descendant %q: %s", name, body)
				}
			}

			// An ordinary HTTP front still carries the destination's TCP transport.
			input = chainFixture()
			input[0].Inbound.Protocol = spec.HTTP
			lines, err = ClientLines(input, format)
			if err != nil || len(lines) != 2 {
				t.Fatalf("ordinary HTTP front rejected: %+v %v", lines, err)
			}
			key := "dialer-proxy"
			if format == "singbox" {
				key = "detour"
			}
			if lines[1].Extra[key] != input[0].Name {
				t.Fatalf("HTTP front reference lost: %+v", lines[1])
			}
		})
	}
}
