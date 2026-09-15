// Package subdesign turns a visual description of a subscription (proxy
// groups plus an ordered list of rule sets) into the text templates each
// client format expects, so the operator never has to write seven
// dialects by hand. The output uses the bosun template placeholders
// ({{proxies}}, {{proxy_names}} with its tag= / match= filters).
package subdesign

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Group is one policy group.
type Group struct {
	Name string `json:"name"`
	// Type: select, url-test, fallback, load-balance.
	Type string `json:"type"`
	// Members: other group names, DIRECT, REJECT, "@all" (every server),
	// "@tag:<tag>" (servers whose entry carries the tag) or "@match:<re>"
	// (servers whose name matches the case-insensitive regexp).
	Members  []string `json:"members"`
	URL      string   `json:"url,omitempty"`
	Interval int      `json:"interval,omitempty"`
}

// Rule routes one rule set to a policy (a group name, DIRECT or REJECT).
type Rule struct {
	Set    string `json:"set"`
	Policy string `json:"policy"`
}

// Design is the whole picture.
type Design struct {
	Groups []Group `json:"groups"`
	Rules  []Rule  `json:"rules"`
	// Final is the policy for traffic no rule matched.
	Final string `json:"final"`
}

// RuleSet is a catalogue entry: an ACL4SSR list the clients fetch
// themselves, or the built-in GEOIP CN match.
type RuleSet struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}

const acl = "https://raw.githubusercontent.com/ACL4SSR/ACL4SSR/master/Clash/"

// GeoIPCN is the catalogue key rendered as each client's GEOIP,CN rule.
const GeoIPCN = "geoip-cn"

// Catalogue lists the rule sets the designer offers, in a sensible order
// for a rule list (local first, blocks, services, China, catch-all).
var Catalogue = []RuleSet{
	{Key: "lan", Name: "局域网 / LAN", URL: acl + "LocalAreaNetwork.list"},
	{Key: "unban", Name: "解除封锁 / UnBan", URL: acl + "UnBan.list"},
	{Key: "ads", Name: "广告拦截 / BanAD", URL: acl + "BanAD.list"},
	{Key: "app-ads", Name: "应用净化 / BanProgramAD", URL: acl + "BanProgramAD.list"},
	{Key: "google-fcm", Name: "谷歌 FCM", URL: acl + "Ruleset/GoogleFCM.list"},
	{Key: "google-cn", Name: "谷歌中国 / GoogleCN", URL: acl + "GoogleCN.list"},
	{Key: "steam-cn", Name: "Steam 中国", URL: acl + "Ruleset/SteamCN.list"},
	{Key: "microsoft", Name: "微软服务", URL: acl + "Microsoft.list"},
	{Key: "apple", Name: "苹果服务", URL: acl + "Apple.list"},
	{Key: "telegram", Name: "电报 / Telegram", URL: acl + "Telegram.list"},
	{Key: "openai", Name: "OpenAI", URL: acl + "Ruleset/OpenAi.list"},
	{Key: "youtube", Name: "YouTube", URL: acl + "Ruleset/YouTube.list"},
	{Key: "netflix", Name: "Netflix", URL: acl + "Ruleset/Netflix.list"},
	{Key: "media", Name: "国外媒体 / ProxyMedia", URL: acl + "ProxyMedia.list"},
	{Key: "proxy-lite", Name: "常用代理 / ProxyLite", URL: acl + "ProxyLite.list"},
	{Key: "gfw", Name: "GFW 列表 / ProxyGFWlist", URL: acl + "ProxyGFWlist.list"},
	{Key: "china-domain", Name: "国内域名 / ChinaDomain", URL: acl + "ChinaDomain.list"},
	{Key: "china-ip", Name: "国内 IP / ChinaCompanyIp", URL: acl + "ChinaCompanyIp.list"},
	{Key: GeoIPCN, Name: "国内 IP（GEOIP CN）"},
}

func ruleSet(key string) (RuleSet, bool) {
	for _, r := range Catalogue {
		if r.Key == key {
			return r, true
		}
	}
	return RuleSet{}, false
}

// Formats the designer can generate.
var Formats = []string{"clash", "stash", "surge", "surfboard", "loon", "qx", "egern"}

const testURL = "http://www.gstatic.com/generate_204"

// Preset names the UI offers.
var Presets = []RuleSet{
	{Key: "basic", Name: "基础：直连国内，其余走代理"},
	{Key: "acl4ssr", Name: "ACL4SSR 标准：广告拦截 + 常用服务分组"},
	{Key: "acl4ssr-regions", Name: "ACL4SSR 标准 + 地区分组（香港/台湾/日本/新加坡/美国/韩国）"},
}

// Preset returns a starting design; ok is false for unknown keys.
func Preset(key string) (Design, bool) {
	auto := Group{Name: "♻️ 自动选择", Type: "url-test", Members: []string{"@all"}, URL: testURL, Interval: 300}
	switch key {
	case "basic":
		return Design{
			Groups: []Group{{Name: "🚀 节点选择", Type: "select", Members: []string{"♻️ 自动选择", "@all"}}, auto},
			Rules:  []Rule{{"lan", "DIRECT"}, {"ads", "REJECT"}, {"china-domain", "DIRECT"}, {"china-ip", "DIRECT"}, {GeoIPCN, "DIRECT"}},
			Final:  "🚀 节点选择",
		}, true
	case "acl4ssr", "acl4ssr-regions":
		pick := []string{"♻️ 自动选择", "DIRECT", "@all"}
		groups := []Group{}
		if key == "acl4ssr-regions" {
			pick = []string{"♻️ 自动选择", "🇭🇰 香港", "🇹🇼 台湾", "🇯🇵 日本", "🇸🇬 新加坡", "🇺🇸 美国", "🇰🇷 韩国", "DIRECT", "@all"}
		}
		groups = append(groups, Group{Name: "🚀 节点选择", Type: "select", Members: pick}, auto)
		if key == "acl4ssr-regions" {
			for _, r := range Regions {
				groups = append(groups, Group{Name: r.Name, Type: "url-test", Members: []string{"@match:" + r.Match}, URL: testURL, Interval: 300})
			}
		}
		groups = append(groups,
			Group{Name: "🌍 国外媒体", Type: "select", Members: []string{"🚀 节点选择", "♻️ 自动选择", "🎯 全球直连", "@all"}},
			Group{Name: "📲 电报信息", Type: "select", Members: []string{"🚀 节点选择", "🎯 全球直连", "@all"}},
			Group{Name: "🤖 AI 服务", Type: "select", Members: []string{"🚀 节点选择", "🎯 全球直连", "@all"}},
			Group{Name: "Ⓜ️ 微软服务", Type: "select", Members: []string{"🎯 全球直连", "🚀 节点选择", "@all"}},
			Group{Name: "🍎 苹果服务", Type: "select", Members: []string{"🚀 节点选择", "🎯 全球直连", "@all"}},
			Group{Name: "📢 谷歌FCM", Type: "select", Members: []string{"🚀 节点选择", "🎯 全球直连", "♻️ 自动选择", "@all"}},
			Group{Name: "🎯 全球直连", Type: "select", Members: []string{"DIRECT", "🚀 节点选择", "♻️ 自动选择"}},
			Group{Name: "🛑 全球拦截", Type: "select", Members: []string{"REJECT", "DIRECT"}},
			Group{Name: "🍃 应用净化", Type: "select", Members: []string{"REJECT", "DIRECT"}},
			Group{Name: "🐟 漏网之鱼", Type: "select", Members: []string{"🚀 节点选择", "🎯 全球直连", "♻️ 自动选择", "@all"}},
		)
		return Design{
			Groups: groups,
			Rules: []Rule{
				{"lan", "🎯 全球直连"}, {"unban", "🎯 全球直连"}, {"ads", "🛑 全球拦截"}, {"app-ads", "🍃 应用净化"},
				{"google-fcm", "📢 谷歌FCM"}, {"google-cn", "🎯 全球直连"}, {"steam-cn", "🎯 全球直连"},
				{"microsoft", "Ⓜ️ 微软服务"}, {"apple", "🍎 苹果服务"}, {"telegram", "📲 电报信息"}, {"openai", "🤖 AI 服务"},
				{"media", "🌍 国外媒体"}, {"proxy-lite", "🚀 节点选择"},
				{"china-domain", "🎯 全球直连"}, {"china-ip", "🎯 全球直连"}, {GeoIPCN, "🎯 全球直连"},
			},
			Final: "🐟 漏网之鱼",
		}, true
	}
	return Design{}, false
}

// Region is a name-match preset for the member picker.
type Region struct {
	Name  string `json:"name"`
	Match string `json:"match"`
}

// Regions are the common ones; the operator can type any regexp.
var Regions = []Region{
	{Name: "🇭🇰 香港", Match: "HK|香港|🇭🇰|Hong ?Kong"},
	{Name: "🇹🇼 台湾", Match: "TW|台湾|台灣|🇹🇼|Taiwan"},
	{Name: "🇯🇵 日本", Match: "JP|日本|🇯🇵|Japan"},
	{Name: "🇸🇬 新加坡", Match: "SG|新加坡|🇸🇬|Singapore"},
	{Name: "🇺🇸 美国", Match: "US|美国|美國|🇺🇸|United States|America"},
	{Name: "🇰🇷 韩国", Match: "KR|韩国|韓國|🇰🇷|Korea"},
}

var groupTypes = map[string]bool{"select": true, "url-test": true, "fallback": true, "load-balance": true}

// Validate checks names, types, member and policy references.
func (d *Design) Validate() error {
	if len(d.Groups) == 0 {
		return fmt.Errorf("at least one proxy group is required")
	}
	names := map[string]bool{}
	for i, g := range d.Groups {
		g.Name = strings.TrimSpace(g.Name)
		d.Groups[i].Name = g.Name
		if g.Name == "" {
			return fmt.Errorf("group %d has no name", i+1)
		}
		if g.Name == "DIRECT" || g.Name == "REJECT" || strings.ContainsAny(g.Name, ",\n\"") {
			return fmt.Errorf("group name %q is not allowed", g.Name)
		}
		if names[g.Name] {
			return fmt.Errorf("duplicate group name %q", g.Name)
		}
		names[g.Name] = true
		if !groupTypes[g.Type] {
			return fmt.Errorf("group %q: unknown type %q", g.Name, g.Type)
		}
		if len(g.Members) == 0 {
			return fmt.Errorf("group %q has no members", g.Name)
		}
	}
	for _, g := range d.Groups {
		for _, m := range g.Members {
			switch {
			case m == "@all", m == "DIRECT", m == "REJECT", names[m]:
			case strings.HasPrefix(m, "@tag:"):
				if strings.TrimSpace(m[5:]) == "" {
					return fmt.Errorf("group %q: empty tag filter", g.Name)
				}
			case strings.HasPrefix(m, "@match:"):
				if _, err := regexp.Compile("(?i)" + m[7:]); err != nil {
					return fmt.Errorf("group %q: bad pattern %q", g.Name, m[7:])
				}
			default:
				return fmt.Errorf("group %q: unknown member %q", g.Name, m)
			}
			if m == g.Name {
				return fmt.Errorf("group %q contains itself", g.Name)
			}
		}
	}
	policy := func(p string) bool { return p == "DIRECT" || p == "REJECT" || names[p] }
	for _, r := range d.Rules {
		if _, ok := ruleSet(r.Set); !ok {
			return fmt.Errorf("unknown rule set %q", r.Set)
		}
		if !policy(r.Policy) {
			return fmt.Errorf("rule %s: unknown policy %q", r.Set, r.Policy)
		}
	}
	if !policy(d.Final) {
		return fmt.Errorf("final policy %q is not a group", d.Final)
	}
	return nil
}

// Render generates the template for one format.
func (d *Design) Render(format string) (string, error) {
	if err := d.Validate(); err != nil {
		return "", err
	}
	switch format {
	case "clash", "stash":
		return d.clash(format), nil
	case "surge", "surfboard":
		return d.surge(format), nil
	case "loon":
		return d.loon(), nil
	case "qx":
		return d.qx(), nil
	case "egern":
		return d.egern(), nil
	}
	return "", fmt.Errorf("no generator for %q", format)
}

// RenderAll generates every format.
func (d *Design) RenderAll() (map[string]string, error) {
	out := map[string]string{}
	for _, f := range Formats {
		s, err := d.Render(f)
		if err != nil {
			return nil, err
		}
		out[f] = s
	}
	return out, nil
}

// member turns a designer member into the template token.
func member(m string) string {
	switch {
	case m == "@all":
		return "{{proxy_names}}"
	case strings.HasPrefix(m, "@tag:"):
		return "{{proxy_names:tag=" + m[5:] + "}}"
	case strings.HasPrefix(m, "@match:"):
		return "{{proxy_names:match=" + m[7:] + "}}"
	}
	return m
}

func (g Group) url() string {
	if g.URL != "" {
		return g.URL
	}
	return testURL
}

func (g Group) interval() int {
	if g.Interval > 0 {
		return g.Interval
	}
	return 300
}

func yq(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// rulesUsed lists the catalogue entries the rules reference, in rule order
// without repeats (for provider / remote-rule sections).
func (d *Design) rulesUsed() []RuleSet {
	seen := map[string]bool{}
	var out []RuleSet
	for _, r := range d.Rules {
		rs, _ := ruleSet(r.Set)
		if rs.URL == "" || seen[rs.Key] {
			continue
		}
		seen[rs.Key] = true
		out = append(out, rs)
	}
	return out
}

func (d *Design) clash(format string) string {
	var b strings.Builder
	b.WriteString("# Generated by the Captain subscription designer; edit the design, not this file.\n")
	if format == "clash" {
		b.WriteString("mixed-port: 7890\nallow-lan: false\n")
	}
	b.WriteString("mode: rule\nlog-level: info\nproxies: []\nproxy-groups:\n")
	for _, g := range d.Groups {
		fmt.Fprintf(&b, "  - name: %s\n    type: %s\n", yq(g.Name), g.Type)
		if g.Type != "select" {
			fmt.Fprintf(&b, "    url: %s\n    interval: %d\n", g.url(), g.interval())
			if g.Type == "url-test" {
				b.WriteString("    tolerance: 50\n")
			}
		}
		b.WriteString("    proxies:\n")
		for _, m := range g.Members {
			fmt.Fprintf(&b, "      - %s\n", yq(member(m)))
		}
	}
	if used := d.rulesUsed(); len(used) > 0 {
		b.WriteString("rule-providers:\n")
		for _, rs := range used {
			fmt.Fprintf(&b, "  %s:\n    type: http\n    behavior: classical\n", rs.Key)
			if format == "clash" {
				b.WriteString("    format: text\n")
			}
			fmt.Fprintf(&b, "    url: %s\n    path: ./ruleset/%s.list\n    interval: 86400\n", rs.URL, rs.Key)
		}
	}
	b.WriteString("rules:\n")
	for _, r := range d.Rules {
		if r.Set == GeoIPCN {
			fmt.Fprintf(&b, "  - GEOIP,CN,%s\n", r.Policy)
			continue
		}
		fmt.Fprintf(&b, "  - RULE-SET,%s,%s\n", r.Set, r.Policy)
	}
	fmt.Fprintf(&b, "  - MATCH,%s\n", d.Final)
	return b.String()
}

func iniMembers(g Group) string {
	parts := make([]string, 0, len(g.Members))
	for _, m := range g.Members {
		parts = append(parts, member(m))
	}
	return strings.Join(parts, ", ")
}

func (d *Design) surge(format string) string {
	var b strings.Builder
	b.WriteString("[General]\nloglevel = notify\n")
	if format == "surfboard" {
		b.WriteString("ipv6 = false\n")
	}
	b.WriteString("skip-proxy = localhost, *.local, 0.0.0.0/8, 10.0.0.0/8, 127.0.0.0/8, 169.254.0.0/16, 172.16.0.0/12, 192.168.0.0/16, 224.0.0.0/4\ndns-server = 223.5.5.5, 119.29.29.29\ntest-timeout = 5\n")
	if format == "surfboard" {
		b.WriteString("internet-test-url = http://bing.com\nproxy-test-url = " + testURL + "\n")
	} else {
		b.WriteString("proxy-test-url = " + testURL + "\n")
	}
	b.WriteString("\n[Proxy]\n{{proxies}}\n\n[Proxy Group]\n")
	for _, g := range d.Groups {
		fmt.Fprintf(&b, "%s = %s, %s", g.Name, g.Type, iniMembers(g))
		if g.Type != "select" {
			fmt.Fprintf(&b, ", url=%s, interval=%d", g.url(), g.interval())
			if g.Type == "url-test" {
				b.WriteString(", tolerance=50")
			}
		}
		b.WriteString("\n")
	}
	b.WriteString("\n[Rule]\n")
	for _, r := range d.Rules {
		if r.Set == GeoIPCN {
			fmt.Fprintf(&b, "GEOIP,CN,%s\n", r.Policy)
			continue
		}
		rs, _ := ruleSet(r.Set)
		fmt.Fprintf(&b, "RULE-SET,%s,%s\n", rs.URL, r.Policy)
	}
	fmt.Fprintf(&b, "FINAL,%s,dns-failed\n", d.Final)
	return b.String()
}

func (d *Design) loon() string {
	var b strings.Builder
	b.WriteString("[General]\nskip-proxy = 192.168.0.0/16, 10.0.0.0/8, 172.16.0.0/12, localhost, *.local, e.crashlytics.com\nbypass-tun = 10.0.0.0/8, 100.64.0.0/10, 127.0.0.0/8, 169.254.0.0/16, 172.16.0.0/12, 192.0.0.0/24, 192.0.2.0/24, 192.88.99.0/24, 192.168.0.0/16, 198.18.0.0/15, 198.51.100.0/24, 203.0.113.0/24, 224.0.0.0/4, 255.255.255.255/32\ndns-server = system\n\n[Proxy]\n{{proxies}}\n\n[Proxy Group]\n")
	for _, g := range d.Groups {
		fmt.Fprintf(&b, "%s = %s,%s", g.Name, g.Type, strings.ReplaceAll(iniMembers(g), ", ", ","))
		fmt.Fprintf(&b, ",url = %s,interval = %d", g.url(), g.interval())
		if g.Type == "url-test" {
			b.WriteString(",tolerance = 50")
		}
		b.WriteString("\n")
	}
	if used := d.rulesUsed(); len(used) > 0 {
		b.WriteString("\n[Remote Rule]\n")
		for _, r := range d.Rules {
			rs, _ := ruleSet(r.Set)
			if rs.URL == "" {
				continue
			}
			fmt.Fprintf(&b, "%s, policy=%s, tag=%s, enabled=true\n", rs.URL, r.Policy, rs.Key)
		}
	}
	b.WriteString("\n[Rule]\n")
	for _, r := range d.Rules {
		if r.Set == GeoIPCN {
			fmt.Fprintf(&b, "GEOIP,CN,%s\n", r.Policy)
		}
	}
	fmt.Fprintf(&b, "FINAL,%s\n", d.Final)
	return b.String()
}

func qxPolicy(p string) string {
	switch p {
	case "DIRECT":
		return "direct"
	case "REJECT":
		return "reject"
	}
	return p
}

func (d *Design) qx() string {
	var b strings.Builder
	b.WriteString("[general]\nserver_check_url=" + testURL + "\n\n[policy]\n")
	for _, g := range d.Groups {
		kind := map[string]string{"select": "static", "url-test": "url-latency-benchmark", "fallback": "available", "load-balance": "round-robin"}[g.Type]
		parts := make([]string, 0, len(g.Members))
		for _, m := range g.Members {
			parts = append(parts, qxPolicy(member(m)))
		}
		fmt.Fprintf(&b, "%s=%s, %s", kind, g.Name, strings.Join(parts, ", "))
		if g.Type == "url-test" {
			fmt.Fprintf(&b, ", check-interval=%d, tolerance=50", g.interval())
		}
		b.WriteString("\n")
	}
	b.WriteString("\n[server_local]\n{{proxies}}\n")
	if used := d.rulesUsed(); len(used) > 0 {
		b.WriteString("\n[filter_remote]\n")
		for _, r := range d.Rules {
			rs, _ := ruleSet(r.Set)
			if rs.URL == "" {
				continue
			}
			fmt.Fprintf(&b, "%s, tag=%s, force-policy=%s, update-interval=86400, opt-parser=true, enabled=true\n", rs.URL, rs.Key, qxPolicy(r.Policy))
		}
	}
	b.WriteString("\n[filter_local]\n")
	for _, r := range d.Rules {
		if r.Set == GeoIPCN {
			fmt.Fprintf(&b, "geoip, cn, %s\n", qxPolicy(r.Policy))
		}
	}
	fmt.Fprintf(&b, "final, %s\n", qxPolicy(d.Final))
	return b.String()
}

func (d *Design) egern() string {
	var b strings.Builder
	b.WriteString("# Generated by the Captain subscription designer; edit the design, not this file.\nproxies: []\npolicy_groups:\n")
	for _, g := range d.Groups {
		kind := map[string]string{"select": "select", "url-test": "auto_test", "fallback": "fallback", "load-balance": "load_balance"}[g.Type]
		fmt.Fprintf(&b, "  - %s:\n      name: %s\n      policies:\n", kind, yq(g.Name))
		for _, m := range g.Members {
			fmt.Fprintf(&b, "        - %s\n", yq(member(m)))
		}
		if g.Type != "select" {
			fmt.Fprintf(&b, "      url: %s\n      interval: %d\n", g.url(), g.interval())
			if g.Type == "url-test" {
				b.WriteString("      tolerance: 50\n")
			}
		}
	}
	b.WriteString("rules:\n")
	for _, r := range d.Rules {
		if r.Set == GeoIPCN {
			fmt.Fprintf(&b, "  - geoip:\n      match: CN\n      policy: %s\n", yq(r.Policy))
			continue
		}
		rs, _ := ruleSet(r.Set)
		fmt.Fprintf(&b, "  - rule_set:\n      match: %s\n      policy: %s\n      update_interval: 86400\n", rs.URL, yq(r.Policy))
	}
	fmt.Fprintf(&b, "  - default:\n      policy: %s\n", yq(d.Final))
	return b.String()
}

// SortedFormats is Formats in display order (stable for the UI).
func SortedFormats() []string {
	out := append([]string(nil), Formats...)
	sort.Strings(out)
	return out
}
