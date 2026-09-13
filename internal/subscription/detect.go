package subscription

import "strings"

// Renderers by client name; "uri" is the fallback.
var renderers = map[string]Renderer{
	"clash":     Clash{},
	"stash":     Stash{},
	"singbox":   SingBox{},
	"surge":     Surge{},
	"surfboard": Surfboard{},
	"loon":      Loon{},
	"qx":        QuantumultX{},
	"uri":       URIList{},
}

// Aliases users may pass in ?client=.
var aliases = map[string]string{
	"mihomo": "clash", "clashmeta": "clash", "clash-meta": "clash",
	"sing-box": "singbox", "sfa": "singbox", "sfi": "singbox",
	"quantumultx": "qx", "quantumult-x": "qx", "quantumult": "qx",
	"shadowrocket": "uri", "v2rayn": "uri", "v2rayng": "uri", "base64": "uri",
}

// Pick chooses a renderer from an explicit client name or the User-Agent.
// Order matters: Stash and Surfboard identify themselves alongside the
// family they descend from, so they are matched before Clash and Surge.
func Pick(client, userAgent string) Renderer {
	if client != "" {
		c := strings.ToLower(client)
		if a, ok := aliases[c]; ok {
			c = a
		}
		if r, ok := renderers[c]; ok {
			return r
		}
	}
	ua := strings.ToLower(userAgent)
	switch {
	case strings.Contains(ua, "stash"):
		return renderers["stash"]
	case strings.Contains(ua, "clash"), strings.Contains(ua, "mihomo"):
		return renderers["clash"]
	case strings.Contains(ua, "sing-box"), strings.Contains(ua, "sfa"), strings.Contains(ua, "sfi"):
		return renderers["singbox"]
	case strings.Contains(ua, "surfboard"):
		return renderers["surfboard"]
	case strings.Contains(ua, "surge"):
		return renderers["surge"]
	case strings.Contains(ua, "loon"):
		return renderers["loon"]
	case strings.Contains(ua, "quantumult"):
		return renderers["qx"]
	}
	return renderers["uri"]
}
