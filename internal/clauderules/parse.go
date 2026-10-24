package clauderules

import (
	"bytes"
	"net/netip"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/idna"
)

// Parse accepts only the subset that can preserve exact routing semantics:
// plain/domain suffixes, full hostnames, blank lines and # comments. Includes,
// attributes, affiliations and regexp/keyword rules reject the entire list.
// The returned rules are lowercase, deduplicated and sorted by type and value.
func Parse(body []byte) ([]Rule, error) {
	if len(body) > maxFileBytes {
		return nil, ErrTooLarge
	}
	if !utf8.Valid(body) || bytes.IndexByte(body, 0) >= 0 {
		return nil, ErrInvalidRules
	}
	seen := make(map[Rule]struct{})
	var rows int
	for _, raw := range strings.Split(string(body), "\n") {
		line, _, _ := strings.Cut(raw, "#")
		line = strings.Trim(line, " \t\r")
		if line == "" {
			continue
		}
		rows++
		if rows > maxRules {
			return nil, ErrTooLarge
		}
		rule := Rule{Type: "domain", Value: line}
		if kind, value, typed := strings.Cut(line, ":"); typed {
			if kind != "domain" && kind != "full" {
				return nil, ErrInvalidRules
			}
			rule.Type, rule.Value = kind, value
		}
		for _, c := range rule.Value {
			if c > 127 {
				return nil, ErrInvalidRules
			}
		}
		rule.Value = strings.ToLower(rule.Value)
		if !validDomain(rule.Value) {
			return nil, ErrInvalidRules
		}
		seen[rule] = struct{}{}
	}
	if len(seen) == 0 {
		return nil, ErrInvalidRules
	}
	rules := make([]Rule, 0, len(seen))
	for rule := range seen {
		rules = append(rules, rule)
	}
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].Type != rules[j].Type {
			return rules[i].Type < rules[j].Type
		}
		return rules[i].Value < rules[j].Value
	})
	return rules, nil
}

func validDomain(domain string) bool {
	if len(domain) > 253 || !strings.Contains(domain, ".") {
		return false
	}
	if _, err := netip.ParseAddr(domain); err == nil {
		return false
	}
	labels := strings.Split(domain, ".")
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
		if strings.HasPrefix(label, "xn--") {
			unicode, err := idna.Lookup.ToUnicode(label)
			if err != nil {
				return false
			}
			ascii, err := idna.Lookup.ToASCII(unicode)
			if err != nil || ascii != label {
				return false
			}
		}
	}
	// Numeric final labels are not hostnames; this also rejects noncanonical
	// dotted IPv4 forms which netip intentionally does not parse.
	for _, c := range labels[len(labels)-1] {
		if c < '0' || c > '9' {
			return true
		}
	}
	return false
}
