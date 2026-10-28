package service

import (
	"net"
	"slices"

	"github.com/zeptop-dev/captain/internal/store"
)

func dnsFamilyIPs(ips []string, family string) []string {
	out := []string{}
	for _, value := range ips {
		ip := net.ParseIP(value)
		if ip != nil && ((family == "ip4" && ip.To4() != nil) || (family == "ip6" && ip.To4() == nil)) {
			out = append(out, ip.String())
		}
	}
	return dnsIPs(out...)
}
func dnsFamilies(t store.DNSTarget) []store.DNSFamily {
	out := []store.DNSFamily{}
	for _, kind := range []string{"A", "AAAA"} {
		family := "ip4"
		if kind == "AAAA" {
			family = "ip6"
		}
		status := "unknown"
		if t.Status == "invalid" {
			status = "invalid"
		}
		if t.Mode == "conflict" {
			status = "conflict"
		}
		out = append(out, store.DNSFamily{Type: kind, Expected: dnsFamilyIPs(t.Expected, family), Status: status, Answers: []store.DNSAnswer{}})
	}
	return out
}
func dnsFamilyStatus(t store.DNSTarget, f store.DNSFamily) string {
	if t.Status == "invalid" {
		return "invalid"
	}
	if t.Mode == "conflict" {
		return "conflict"
	}
	if len(f.Answers) != 2 {
		return "unknown"
	}
	a, b := f.Answers[0], f.Answers[1]
	for _, a := range f.Answers {
		if a.Error != "" && a.Error != "not_found" {
			return "unknown"
		}
	}
	if len(f.Expected) == 0 {
		if a.Error == "not_found" && b.Error == "not_found" {
			return "unconfigured"
		}
		if a.Error != b.Error {
			return "inconsistent"
		}
		if t.Mode == "direct" {
			return "unexpected"
		}
		return "ok" // Shared/CDN responses may differ between resolvers.
	}
	return dnsTargetStatus(store.DNSTarget{Mode: t.Mode, Expected: f.Expected, Answers: f.Answers})
}
func dnsFamiliesStatus(t store.DNSTarget) string {
	if t.Status == "invalid" {
		return "invalid"
	}
	if t.Mode == "conflict" {
		return "conflict"
	}
	for _, status := range []string{"missing", "mismatch", "unknown", "inconsistent", "unexpected"} {
		for _, f := range t.Families {
			if f.Status == status {
				if status == "unexpected" {
					return "inconsistent"
				} // Preserve existing aggregate status vocabulary.
				return status
			}
		}
	}
	for _, f := range t.Families {
		if f.Status == "ok" {
			return "ok"
		}
	}
	return "missing"
}

// Preserve aggregate API fields for existing clients, without discarding the
// successful family when its sibling query failed. Family-aware readers use
// Families to distinguish these partial observations.
func combinedDNSAnswers(families []store.DNSFamily) []store.DNSAnswer {
	out := []store.DNSAnswer{}
	for i, source := range []string{"panel", "cloudflare"} {
		a := store.DNSAnswer{Source: source, Addresses: []string{}}
		missing := true
		for _, f := range families {
			if len(f.Answers) != 2 {
				continue
			}
			v := f.Answers[i]
			a.Addresses = append(a.Addresses, v.Addresses...)
			if v.Error != "not_found" {
				missing = false
			}
			if v.Error != "" && v.Error != "not_found" {
				a.Error = v.Error
			}
		}
		slices.Sort(a.Addresses)
		a.Addresses = slices.Compact(a.Addresses)
		if missing {
			a.Error = "not_found"
		}
		out = append(out, a)
	}
	return out
}
