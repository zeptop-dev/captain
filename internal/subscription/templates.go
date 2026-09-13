package subscription

import (
	"embed"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Templates let the operator reshape the document around the server list.
// Placeholders:
//
//	{{proxies}}      INI formats (surge, surfboard, loon, qx): the rendered server lines
//	{{proxy_names}}  INI formats: all names comma-joined; YAML formats (clash,
//	                 stash): a list entry inside any proxy-group's "proxies"
//	                 that expands to every server name
//
// Loon and Quantumult X default to a bare node list because their remote
// subscriptions are node lists, not full configs; a full config template
// works when the user imports the URL as a configuration instead.

//go:embed templates/*.tpl
var templateFS embed.FS

// DefaultTemplate returns the built-in template for a format ("" if the
// format is not templated).
func DefaultTemplate(name string) string {
	b, err := templateFS.ReadFile("templates/" + name + ".tpl")
	if err != nil {
		return ""
	}
	return string(b)
}

// TemplateNames lists the formats that accept a custom template.
func TemplateNames() []string {
	entries, _ := templateFS.ReadDir("templates")
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, strings.TrimSuffix(e.Name(), ".tpl"))
	}
	sort.Strings(names)
	return names
}

const (
	phProxies = "{{proxies}}"
	phNames   = "{{proxy_names}}"
)

// applyINI fills an INI-like template with rendered lines and names.
func applyINI(tpl, format, lines string, names []string) []byte {
	if tpl == "" {
		tpl = DefaultTemplate(format)
	}
	out := strings.ReplaceAll(tpl, phProxies, lines)
	out = strings.ReplaceAll(out, phNames, strings.Join(names, ", "))
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return []byte(out)
}

// applyYAML fills a YAML skeleton: proxies replaces the "proxies" key and
// every "{{proxy_names}}" entry in a proxy-group's list expands in place.
func applyYAML(tpl, format string, proxies []any, names []string) ([]byte, error) {
	if tpl == "" {
		tpl = DefaultTemplate(format)
	}
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(tpl), &doc); err != nil {
		return nil, err
	}
	if doc == nil {
		doc = map[string]any{}
	}
	if proxies == nil {
		proxies = []any{}
	}
	doc["proxies"] = proxies
	if groups, ok := doc["proxy-groups"].([]any); ok {
		for _, g := range groups {
			gm, ok := g.(map[string]any)
			if !ok {
				continue
			}
			list, ok := gm["proxies"].([]any)
			if !ok {
				continue
			}
			expanded := make([]any, 0, len(list)+len(names))
			for _, item := range list {
				if s, ok := item.(string); ok && s == phNames {
					for _, n := range names {
						expanded = append(expanded, n)
					}
					continue
				}
				expanded = append(expanded, item)
			}
			gm["proxies"] = expanded
		}
	}
	return yaml.Marshal(doc)
}
