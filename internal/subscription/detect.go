package subscription

import "strings"

// Renderers by client name; "uri" is the fallback.
var renderers = map[string]Renderer{
	"clash":   Clash{},
	"singbox": SingBox{},
	"surge":   Surge{},
	"uri":     URIList{},
}

// Aliases users may pass in ?client=.
var aliases = map[string]string{
	"mihomo": "clash", "clashmeta": "clash", "clash-meta": "clash", "stash": "clash",
	"sing-box": "singbox", "sfa": "singbox", "sfi": "singbox",
	"shadowrocket": "uri", "v2rayn": "uri", "v2rayng": "uri", "base64": "uri",
}

// Pick chooses a renderer from an explicit client name or the User-Agent.
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
	case strings.Contains(ua, "clash"), strings.Contains(ua, "mihomo"), strings.Contains(ua, "stash"):
		return renderers["clash"]
	case strings.Contains(ua, "sing-box"), strings.Contains(ua, "sfa"), strings.Contains(ua, "sfi"):
		return renderers["singbox"]
	case strings.Contains(ua, "surge"):
		return renderers["surge"]
	}
	return renderers["uri"]
}
