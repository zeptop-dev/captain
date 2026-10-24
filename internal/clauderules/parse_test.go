package clauderules

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestParsePreservesMatchingSemantics(t *testing.T) {
	rules, err := Parse([]byte("# Community fixture, not copied upstream data\r\n\n Example.COM # comment\n" +
		"domain:example.com\nfull:Only.Example.com\nfull:example.com\ndomain:XN--BCHER-KVA.example\n\t\n"))
	want := []Rule{
		{Type: "domain", Value: "example.com"},
		{Type: "domain", Value: "xn--bcher-kva.example"},
		{Type: "full", Value: "example.com"},
		{Type: "full", Value: "only.example.com"},
	}
	if err != nil || !reflect.DeepEqual(rules, want) {
		t.Fatalf("got %#v, %v", rules, err)
	}
}

func TestParseRejectsWholeListOnUnsupportedOrInvalidLine(t *testing.T) {
	invalid := []string{
		"include:another-list", "keyword:example", "regexp:.*example", "unknown:example.com",
		"example.com @ads", "domain:example.com &category", "example.com@ads", "example.com&category",
		"full: example.com", "domain:", "DOMAIN:example.com", "full:example.com,Proxy", "full:example.com:443",
		"https://example.com", "example.com/path", "example.com?key=value", "*.example.com", "+.example.com",
		".example.com", "example..com", "example.com.", "-example.com", "example-.com", "example_.com",
		"localhost", "com", "192.0.2.10", "192.000.2.10", "2001:db8::1", "[2001:db8::1]",
		"例子.example", "K.example", "xn--.example", "xn--a.example", "example.123", "example.com\x00",
		"example.com\v", "example.com\x7f", "example.com\xff", strings.Repeat("a", 64) + ".example",
		strings.Repeat(strings.Repeat("a", 63)+".", 3) + strings.Repeat("a", 62),
	}
	for _, line := range invalid {
		t.Run(fmt.Sprintf("%q", line), func(t *testing.T) {
			rules, err := Parse([]byte("valid.example\n" + line + "\n"))
			if !errors.Is(err, ErrInvalidRules) || rules != nil {
				t.Fatalf("invalid line returned partial rules: %#v, %v", rules, err)
			}
		})
	}
	for _, text := range []string{"", "# only comments\n \t\r\n"} {
		if rules, err := Parse([]byte(text)); !errors.Is(err, ErrInvalidRules) || rules != nil {
			t.Fatalf("empty list accepted: %#v, %v", rules, err)
		}
	}
}

func TestParseLimits(t *testing.T) {
	body := []byte(strings.Repeat("example.com\n", maxRules))
	if rules, err := Parse(body); err != nil || len(rules) != 1 {
		t.Fatalf("valid duplicate normalization: %#v, %v", rules, err)
	}
	for _, body := range [][]byte{
		[]byte(strings.Repeat("example.com\n", maxRules+1)),
		[]byte("example.com\n#" + strings.Repeat("a", maxFileBytes)),
	} {
		if rules, err := Parse(body); !errors.Is(err, ErrTooLarge) || rules != nil {
			t.Fatalf("limit returned %#v, %v", rules, err)
		}
	}
	longest := strings.Repeat(strings.Repeat("a", 63)+".", 3) + strings.Repeat("a", 61)
	if rules, err := Parse([]byte(longest)); err != nil || len(rules) != 1 {
		t.Fatalf("253-byte domain should be accepted: %#v, %v", rules, err)
	}
}
