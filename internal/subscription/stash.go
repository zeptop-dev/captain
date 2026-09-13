package subscription

import (
	"fmt"
	"strings"
)

// Stash is Clash-flavoured YAML with a few key differences from mihomo
// documented at stash.wiki: `sni` instead of `servername`, Hysteria2 uses
// `auth` and `up-speed`/`down-speed`, TUIC wants an explicit version and
// alpn, mieru transports are lower-case, and there is no smux.
type Stash struct{}

func (Stash) Name() string        { return "stash" }
func (Stash) ContentType() string { return "text/yaml; charset=utf-8" }

func (s Stash) Render(lines []Line, acct Account) ([]byte, error) {
	return s.RenderWith(lines, acct, "")
}

func (Stash) RenderWith(lines []Line, _ Account, tpl string) ([]byte, error) {
	proxies, names := clashProxies(lines)
	for i, p := range proxies {
		proxies[i] = stashProxy(p.(m))
	}
	return applyYAML(tpl, "stash", proxies, names)
}

// stashProxy rewrites a mihomo proxy map into Stash's dialect.
func stashProxy(p m) m {
	if sn, ok := p["servername"]; ok {
		p["sni"] = sn
		delete(p, "servername")
	}
	delete(p, "smux")
	switch p["type"] {
	case "hysteria2":
		p["auth"] = p["password"]
		delete(p, "password")
		for _, k := range [][2]string{{"up", "up-speed"}, {"down", "down-speed"}} {
			if v, ok := p[k[0]].(string); ok {
				var n int
				if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
					p[k[1]] = n
				}
				delete(p, k[0])
			}
		}
	case "tuic":
		p["version"] = 5
		p["alpn"] = []string{"h3"}
		delete(p, "congestion-controller")
		delete(p, "udp-relay-mode")
	case "mieru":
		if t, ok := p["transport"].(string); ok {
			p["transport"] = strings.ToLower(t)
		}
	}
	return p
}
