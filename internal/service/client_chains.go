package service

import (
	"encoding/json"
	"maps"
	"strings"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/bosun/pkg/subscription"
	"github.com/zeptop-dev/captain/internal/store"
	"gopkg.in/yaml.v3"
)

// PublishedLine is internal presentation metadata. It is never merged into a
// client configuration or sent to an agent.
type PublishedLine struct {
	subscription.Line
	EntryID       int64
	DialerEntryID int64
	// CategoryID is catalogue metadata, never a subscription tag or access group.
	CategoryID int64
}

// AvailablePublications is used for the portal's format-neutral catalogue.
// A client chain has no standalone share URI, but remains visible here when
// the user can access every required node.
func AvailablePublications(lines []PublishedLine) []PublishedLine {
	byID := map[int64]PublishedLine{}
	for _, l := range lines {
		if l.EntryID != 0 {
			byID[l.EntryID] = l
		}
	}
	out := make([]PublishedLine, 0, len(lines))
	for _, l := range lines {
		current, seen, valid := l, map[int64]bool{}, true
		for depth := 1; current.DialerEntryID != 0; depth++ {
			if depth >= store.MaxClientChainDepth || seen[current.EntryID] {
				valid = false
				break
			}
			seen[current.EntryID] = true
			next, ok := byID[current.DialerEntryID]
			if !ok {
				valid = false
				break
			}
			current = next
		}
		if valid {
			out = append(out, l)
		}
	}
	return out
}

// ClientLines resolves references only within the already authorized lines.
// Missing, hidden, unsupported or ambiguous dependencies remove the dependent
// node; they never turn it into an ordinary direct node.
func ClientLines(lines []PublishedLine, format string, templates ...string) ([]subscription.Line, error) {
	supportedFormat := format == "clash" || format == "stash" || format == "singbox"
	hasChain := false
	for _, l := range lines {
		hasChain = hasChain || l.DialerEntryID != 0
	}
	plain := make([]subscription.Line, 0, len(lines))
	for _, l := range lines {
		if supportedFormat || l.DialerEntryID == 0 {
			plain = append(plain, l.Line)
		}
	}
	if !supportedFormat || !hasChain {
		return plain, nil
	}

	// Use the existing renderer as the protocol/transport capability source.
	// This avoids maintaining a second protocol matrix in Captain. The built-in
	// template also reveals collisions with selector and ShadowTLS carrier names.
	template := ""
	if len(templates) > 0 {
		template = templates[0]
	}
	data, err := subscription.Pick(format, "").RenderWith(plain, subscription.Account{}, template)
	if err != nil {
		return nil, err
	}
	counts, err := renderedClientNames(format, data)
	if err != nil {
		return nil, err
	}
	capabilities := map[int64]bool{}
	checked := map[int64]bool{}
	var probeErr error
	supports := func(l PublishedLine) bool {
		if checked[l.EntryID] {
			return capabilities[l.EntryID]
		}
		checked[l.EntryID] = true
		probe := l.Line
		probe.Name, probe.Extra = "captain-chain-capability", nil
		data, err := subscription.Pick(format, "").Render([]subscription.Line{probe}, subscription.Account{})
		if err != nil {
			probeErr = err
			return false
		}
		names, err := renderedClientNames(format, data)
		if err != nil {
			probeErr = err
			return false
		}
		capabilities[l.EntryID] = names[probe.Name] == 1
		return capabilities[l.EntryID]
	}
	byID := map[int64]PublishedLine{}
	for _, l := range lines {
		if l.EntryID != 0 {
			byID[l.EntryID] = l
		}
	}
	var valid func(PublishedLine, map[int64]bool, int) bool
	valid = func(l PublishedLine, seen map[int64]bool, depth int) bool {
		if depth > store.MaxClientChainDepth || seen[l.EntryID] || counts[l.Name] != 1 || !supports(l) {
			return false
		}
		// Client decoders may accept case/underscore aliases. A raw type can
		// even replace this proxy with a direct outbound that ignores dialers.
		for field, value := range l.Extra {
			canonical := strings.ToLower(strings.ReplaceAll(field, "_", "-"))
			switch canonical {
			case "type", "server", "port", "server-port", "udp":
				return false
			case "name", "tag":
				if value != l.Name {
					return false
				}
			case "dialer-proxy", "detour":
				if l.DialerEntryID == 0 && value != nil {
					return false
				}
			}
		}
		if format != "singbox" {
			switch strings.ToUpper(l.Name) {
			case "DIRECT", "REJECT", "REJECT-DROP", "PASS", "PASS-RULE", "COMPATIBLE", "GLOBAL":
				return false
			}
		}
		if l.DialerEntryID == 0 {
			return true
		}
		if format != "singbox" && l.Inbound.Protocol == spec.HTTP {
			// HTTP outbounds ignore udp:true in mihomo. Selecting one skips
			// matching UDP rules and can fall through to DIRECT. Keep ordinary
			// HTTP proxies compatible, but do not publish this managed path.
			return false
		}
		if format == "singbox" && l.Inbound.ShadowTLS != nil {
			// The current shared renderer owns the ShadowTLS carrier detour.
			// Until it can chain that carrier, omit rather than overwrite it.
			return false
		}
		front, ok := byID[l.DialerEntryID]
		if !ok {
			return false
		}
		seen[l.EntryID] = true
		return valid(front, seen, depth+1)
	}
	out := make([]subscription.Line, 0, len(lines))
	for _, l := range lines {
		if l.DialerEntryID == 0 {
			out = append(out, l.Line)
			continue
		}
		if !valid(l, map[int64]bool{}, 1) {
			continue
		}
		line := l.Line
		line.Extra = maps.Clone(line.Extra)
		if line.Extra == nil {
			line.Extra = map[string]any{}
		}
		for field := range line.Extra {
			canonical := strings.ToLower(strings.ReplaceAll(field, "_", "-"))
			if canonical == "dialer-proxy" || canonical == "detour" || format == "stash" && canonical == "interface-name" {
				delete(line.Extra, field)
			}
		}
		key := "dialer-proxy"
		if format == "singbox" {
			key = "detour"
		}
		line.Extra[key] = byID[l.DialerEntryID].Name
		out = append(out, line)
	}
	if probeErr != nil {
		return nil, probeErr
	}
	return out, nil
}

func renderedClientNames(format string, data []byte) (map[string]int, error) {
	counts := map[string]int{}
	if format == "singbox" {
		var doc struct {
			Outbounds []map[string]any `json:"outbounds"`
		}
		if err := json.Unmarshal(data, &doc); err != nil {
			return nil, err
		}
		for _, p := range doc.Outbounds {
			if name, ok := p["tag"].(string); ok {
				counts[name]++
			}
		}
	} else {
		var doc struct {
			Proxies []map[string]any `yaml:"proxies"`
			Groups  []map[string]any `yaml:"proxy-groups"`
		}
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return nil, err
		}
		for _, p := range append(doc.Proxies, doc.Groups...) {
			if name, ok := p["name"].(string); ok {
				counts[name]++
			}
		}
	}
	return counts, nil
}
