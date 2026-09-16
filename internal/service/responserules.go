package service

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"github.com/zeptop-dev/bosun/pkg/subscription"
)

// SettingResponseRules is the settings key of the response-rule list.
const SettingResponseRules = "response_rules"

// ResponseRule decides how /sub answers a request whose headers match: the
// format to render (instead of the User-Agent guess), extra response
// headers, a template override, or a refusal. Rules are evaluated in
// order; the first enabled rule whose conditions hold wins; no match falls
// back to the built-in User-Agent detection.
type ResponseRule struct {
	Name    string        `json:"name"`
	Enabled bool          `json:"enabled"`
	Match   []HeaderMatch `json:"match"`
	// AnyOf: one condition suffices. Default: all must hold.
	AnyOf bool `json:"any_of,omitempty"`
	// Action: "serve" (render), "block" (403), "not_found" (404),
	// "unavailable" (451), "drop" (close the connection without a reply).
	Action string `json:"action"`
	// Format is the renderer for "serve": clash, singbox, surge, … or
	// empty for the User-Agent guess.
	Format string `json:"format,omitempty"`
	// Template replaces the format's template for this rule.
	Template string `json:"template,omitempty"`
	// Headers are added to the response (e.g. profile-update-interval).
	Headers map[string]string `json:"headers,omitempty"`
}

// HeaderMatch is one condition on a request header ("client" reads the
// ?client= query parameter).
type HeaderMatch struct {
	Header string `json:"header"`
	// Op: contains, equals, prefix, regex, exists, missing. Text ops are
	// case-insensitive except regex, which is passed to Go's regexp as is.
	Op    string `json:"op"`
	Value string `json:"value,omitempty"`
}

// ResponseActions lists the accepted rule actions.
var ResponseActions = []string{"serve", "block", "not_found", "unavailable", "drop"}

var matchOps = map[string]bool{"contains": true, "equals": true, "prefix": true, "regex": true, "exists": true, "missing": true}

var (
	reMu    sync.Mutex
	reCache = map[string]*regexp.Regexp{}
)

func compiled(pattern string) (*regexp.Regexp, error) {
	reMu.Lock()
	defer reMu.Unlock()
	if re, ok := reCache[pattern]; ok {
		return re, nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	if len(reCache) > 256 {
		reCache = map[string]*regexp.Regexp{}
	}
	reCache[pattern] = re
	return re, nil
}

// Holds reports whether the condition is true for the request.
func (m HeaderMatch) Holds(r *http.Request) bool {
	var v string
	var present bool
	if strings.EqualFold(m.Header, "client") {
		v = r.URL.Query().Get("client")
		present = r.URL.Query().Has("client")
	} else {
		vals := r.Header.Values(m.Header)
		present = len(vals) > 0
		v = strings.Join(vals, ", ")
	}
	switch m.Op {
	case "exists":
		return present
	case "missing":
		return !present
	case "contains":
		return present && strings.Contains(strings.ToLower(v), strings.ToLower(m.Value))
	case "equals":
		return present && strings.EqualFold(strings.TrimSpace(v), strings.TrimSpace(m.Value))
	case "prefix":
		return present && strings.HasPrefix(strings.ToLower(v), strings.ToLower(m.Value))
	case "regex":
		re, err := compiled(m.Value)
		return err == nil && present && re.MatchString(v)
	}
	return false
}

// Matches reports whether the rule applies to the request. A rule without
// conditions matches everything (a catch-all at the end of the list).
func (rule ResponseRule) Matches(r *http.Request) bool {
	if !rule.Enabled {
		return false
	}
	if len(rule.Match) == 0 {
		return true
	}
	for _, m := range rule.Match {
		ok := m.Holds(r)
		if rule.AnyOf && ok {
			return true
		}
		if !rule.AnyOf && !ok {
			return false
		}
	}
	return !rule.AnyOf
}

// MatchResponseRule returns the first rule that applies, or nil.
func MatchResponseRule(rules []ResponseRule, r *http.Request) *ResponseRule {
	for i := range rules {
		if rules[i].Matches(r) {
			return &rules[i]
		}
	}
	return nil
}

// ValidateResponseRules normalises the list in place and returns the first
// problem as a message for the admin, or "".
func ValidateResponseRules(_ context.Context, rules *[]ResponseRule) string {
	seen := map[string]bool{}
	for i := range *rules {
		rule := &(*rules)[i]
		rule.Name = strings.TrimSpace(rule.Name)
		if rule.Name == "" {
			return fmt.Sprintf("rule %d: name is required", i+1)
		}
		if seen[rule.Name] {
			return fmt.Sprintf("rule %q: duplicate name", rule.Name)
		}
		seen[rule.Name] = true
		if rule.Action == "" {
			rule.Action = "serve"
		}
		valid := false
		for _, a := range ResponseActions {
			valid = valid || a == rule.Action
		}
		if !valid {
			return fmt.Sprintf("rule %q: unknown action %q", rule.Name, rule.Action)
		}
		rule.Format = strings.ToLower(strings.TrimSpace(rule.Format))
		if rule.Format != "" {
			name := subscription.Pick(rule.Format, "").Name()
			if name == "uri" && !strings.Contains(",uri,base64,shadowrocket,v2rayn,v2rayng,", ","+rule.Format+",") {
				return fmt.Sprintf("rule %q: unknown format %q", rule.Name, rule.Format)
			}
			rule.Format = name
		}
		for j := range rule.Match {
			m := &rule.Match[j]
			m.Header = strings.TrimSpace(m.Header)
			if m.Header == "" {
				return fmt.Sprintf("rule %q: condition %d needs a header name", rule.Name, j+1)
			}
			if m.Op == "" {
				m.Op = "contains"
			}
			if !matchOps[m.Op] {
				return fmt.Sprintf("rule %q: unknown operator %q", rule.Name, m.Op)
			}
			if m.Op == "regex" {
				if _, err := regexp.Compile(m.Value); err != nil {
					return fmt.Sprintf("rule %q: bad regex: %v", rule.Name, err)
				}
			} else if m.Op != "exists" && m.Op != "missing" && m.Value == "" {
				return fmt.Sprintf("rule %q: condition %d needs a value", rule.Name, j+1)
			}
		}
		for k := range rule.Headers {
			if strings.TrimSpace(k) == "" || strings.ContainsAny(k, " :\r\n") || strings.ContainsAny(rule.Headers[k], "\r\n") {
				return fmt.Sprintf("rule %q: bad response header %q", rule.Name, k)
			}
		}
	}
	return ""
}
