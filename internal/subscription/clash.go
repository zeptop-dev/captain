package subscription

import (
	"fmt"

	"gopkg.in/yaml.v3"

	"gitlab.com/boyang-hu/bosun/pkg/spec"
)

// Clash renders a mihomo (Clash Meta) YAML document.
type Clash struct{}

func (Clash) Name() string        { return "clash" }
func (Clash) ContentType() string { return "text/yaml; charset=utf-8" }

type m = map[string]any

func (Clash) Render(lines []Line, _ Account) ([]byte, error) {
	var proxies []any
	var names []string
	for _, l := range lines {
		p := clashProxy(l)
		if p == nil {
			continue
		}
		proxies = append(proxies, p)
		names = append(names, l.Name)
	}
	if proxies == nil {
		proxies = []any{}
	}
	if names == nil {
		names = []string{}
	}
	doc := m{
		"mixed-port": 7890,
		"allow-lan":  false,
		"mode":       "rule",
		"log-level":  "info",
		"proxies":    proxies,
		"proxy-groups": []any{
			m{"name": "PROXY", "type": "select", "proxies": append([]string{"AUTO"}, names...)},
			m{"name": "AUTO", "type": "url-test", "url": "https://www.gstatic.com/generate_204", "interval": 300, "proxies": names},
		},
		"rules": []string{
			"GEOIP,private,DIRECT,no-resolve",
			"GEOSITE,cn,DIRECT",
			"GEOIP,cn,DIRECT",
			"MATCH,PROXY",
		},
	}
	return yaml.Marshal(doc)
}

func clashProxy(l Line) m {
	ib := l.Inbound
	p := m{"name": l.Name, "server": l.Host, "port": l.Port, "udp": true}
	switch ib.Protocol {
	case spec.VLESS:
		p["type"] = "vless"
		p["uuid"] = l.UUID
		if ib.Flow != "" && hasTLS(l) && transportType(l) == "tcp" {
			p["flow"] = ib.Flow
		}
		clashTLS(p, l)
		clashTransport(p, l)
	case spec.VMess:
		p["type"] = "vmess"
		p["uuid"] = l.UUID
		p["alterId"] = 0
		p["cipher"] = "auto"
		clashTLS(p, l)
		clashTransport(p, l)
	case spec.Trojan:
		p["type"] = "trojan"
		p["password"] = l.Password
		clashTLS(p, l)
		clashTransport(p, l)
	case spec.Shadowsocks:
		p["type"] = "ss"
		p["cipher"] = ib.Cipher
		p["password"] = ssPassword(l)
	case spec.Hysteria2:
		p["type"] = "hysteria2"
		p["password"] = l.Password
		if ib.Obfs != "" {
			p["obfs"] = ib.Obfs
			p["obfs-password"] = ib.ObfsPassword
		}
		if ib.UpMbps > 0 {
			p["up"] = fmt.Sprintf("%d Mbps", ib.UpMbps)
		}
		if ib.DownMbps > 0 {
			p["down"] = fmt.Sprintf("%d Mbps", ib.DownMbps)
		}
		if sn := serverName(l); sn != "" {
			p["sni"] = sn
		}
	case spec.TUIC:
		p["type"] = "tuic"
		p["uuid"] = l.UUID
		p["password"] = l.Password
		if ib.CongestionControl != "" {
			p["congestion-controller"] = ib.CongestionControl
		}
		p["udp-relay-mode"] = "native"
		if sn := serverName(l); sn != "" {
			p["sni"] = sn
		}
	case spec.AnyTLS:
		p["type"] = "anytls"
		p["password"] = l.Password
		p["client-fingerprint"] = "chrome"
		if sn := serverName(l); sn != "" {
			p["sni"] = sn
		}
	case spec.Mieru:
		p["type"] = "mieru"
		p["username"] = l.UUID
		p["password"] = l.Password
		p["transport"] = ib.MieruTransport
		if p["transport"] == "" {
			p["transport"] = "TCP"
		}
		delete(p, "udp")
	case spec.SOCKS:
		p["type"] = "socks5"
		p["username"] = l.UUID
		p["password"] = l.Password
	case spec.HTTP:
		p["type"] = "http"
		p["username"] = l.UUID
		p["password"] = l.Password
		if hasTLS(l) {
			p["tls"] = true
			p["sni"] = serverName(l)
		}
	default:
		return nil
	}
	if ib.Multiplex != nil && ib.Multiplex.Enabled {
		sm := m{"enabled": true, "protocol": "smux", "padding": ib.Multiplex.Padding}
		if ib.Multiplex.Brutal != nil {
			sm["brutal-opts"] = m{"enabled": true, "up": ib.Multiplex.Brutal.UpMbps, "down": ib.Multiplex.Brutal.DownMbps}
		}
		p["smux"] = sm
	}
	return p
}

func clashTLS(p m, l Line) {
	if !hasTLS(l) {
		return
	}
	p["tls"] = true
	p["client-fingerprint"] = "chrome"
	if sn := serverName(l); sn != "" {
		p["servername"] = sn
	}
	if isReality(l) {
		r := l.Inbound.TLS.Reality
		sid := ""
		if len(r.ShortIDs) > 0 {
			sid = r.ShortIDs[0]
		}
		p["reality-opts"] = m{"public-key": r.PublicKey, "short-id": sid}
	}
}

func clashTransport(p m, l Line) {
	tr := l.Inbound.Transport
	switch transportType(l) {
	case "ws":
		p["network"] = "ws"
		opts := m{"path": tr.Path}
		if tr.Host != "" {
			opts["headers"] = m{"Host": tr.Host}
		}
		p["ws-opts"] = opts
	case "grpc":
		p["network"] = "grpc"
		p["grpc-opts"] = m{"grpc-service-name": tr.ServiceName}
	case "httpupgrade":
		p["network"] = "ws"
		opts := m{"path": tr.Path, "v2ray-http-upgrade": true}
		if tr.Host != "" {
			opts["headers"] = m{"Host": tr.Host}
		}
		p["ws-opts"] = opts
	case "http":
		p["network"] = "h2"
		opts := m{"path": tr.Path}
		if tr.Host != "" {
			opts["host"] = []string{tr.Host}
		}
		p["h2-opts"] = opts
	case "xhttp":
		p["network"] = "xhttp"
		opts := m{"path": tr.Path}
		if tr.Host != "" {
			opts["host"] = tr.Host
		}
		if tr.Mode != "" {
			opts["mode"] = tr.Mode
		}
		p["xhttp-opts"] = opts
	}
}
