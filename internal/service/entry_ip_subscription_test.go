package service

import (
	"testing"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/bosun/pkg/subscription"
)

func TestClientChainIPPolicyOmitsUnsupportedFrontAndDependents(t *testing.T) {
	input := chainFixture()
	input[0].IPVersion = "ipv6"
	for _, format := range []string{"stash", "uri"} {
		lines, err := ClientLines(input, format)
		if err != nil || len(lines) != 0 {
			t.Fatalf("%s leaked unsupported front or chain: %+v %v", format, lines, err)
		}
	}
	for _, format := range []string{"clash", "singbox"} {
		lines, err := ClientLines(input, format)
		if err != nil || len(lines) != 2 || lines[0].IPVersion != "ipv6" {
			t.Fatalf("%s lost valid front policy: %+v %v", format, lines, err)
		}
	}
	// Old stored entries can become incompatible after related changes. Never
	// claim a remotely resolved landing has a locally enforced IP policy.
	input[1].IPVersion = "ipv4"
	lines, err := ClientLines(input, "clash")
	if err != nil || len(lines) != 1 || lines[0].Name != input[0].Name {
		t.Fatalf("invalid landing leaked: %+v %v", lines, err)
	}
}

func TestClientLinesPreservesLiteralFamilyAndRejectsChangedEndpoint(t *testing.T) {
	line := PublishedLine{EntryID: 1, Line: subscription.Line{Name: "Only IPv6", Host: "2001:db8::20", Port: 443, IPVersion: "ipv6", Inbound: spec.Inbound{Protocol: spec.VLESS}}}
	for _, format := range []string{"clash", "singbox", "uri"} {
		out, err := ClientLines([]PublishedLine{line}, format)
		if err != nil || len(out) != 1 {
			t.Fatalf("literal %s: %v %v", format, out, err)
		}
		line.Host = "192.0.2.10"
		out, err = ClientLines([]PublishedLine{line}, format)
		if err != nil || len(out) != 0 {
			t.Fatalf("mismatched endpoint %s: %v %v", format, out, err)
		}
		line.Host = "2001:db8::20"
	}
}
