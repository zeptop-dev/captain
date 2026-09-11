package subscription

import (
	"encoding/json"

	"gitlab.com/boyang-hu/bosun/pkg/spec"
)

// SingBox renders a sing-box client JSON document.
type SingBox struct{}

func (SingBox) Name() string        { return "singbox" }
func (SingBox) ContentType() string { return "application/json; charset=utf-8" }

func (SingBox) Render(lines []Line, _ Account) ([]byte, error) {
	var outbounds []any
	var names []string
	for _, l := range lines {
		o := singboxOutbound(l)
		if o == nil {
			continue
		}
		outbounds = append(outbounds, o)
		names = append(names, l.Name)
	}
	if names == nil {
		names = []string{}
	}
	all := append([]any{
		m{"type": "selector", "tag": "PROXY", "outbounds": append([]string{"AUTO"}, names...)},
		m{"type": "urltest", "tag": "AUTO", "outbounds": names, "url": "https://www.gstatic.com/generate_204", "interval": "5m"},
	}, outbounds...)
	all = append(all, m{"type": "direct", "tag": "direct"})
	doc := m{
		"log": m{"level": "info"},
		"dns": m{
			"servers": []any{
				m{"tag": "remote", "type": "https", "server": "1.1.1.1", "detour": "PROXY"},
				m{"tag": "local", "type": "https", "server": "223.5.5.5"},
			},
			"rules": []any{m{"rule_set": []string{"geosite-cn"}, "server": "local"}},
			"final": "remote",
		},
		"inbounds":  []any{m{"type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": 7890}},
		"outbounds": all,
		"route": m{
			"default_domain_resolver": "local",
			"rules": []any{
				m{"ip_is_private": true, "outbound": "direct"},
				m{"rule_set": []string{"geosite-cn", "geoip-cn"}, "outbound": "direct"},
			},
			"rule_set": []any{
				m{"tag": "geosite-cn", "type": "remote", "format": "binary", "url": "https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-cn.srs", "download_detour": "PROXY"},
				m{"tag": "geoip-cn", "type": "remote", "format": "binary", "url": "https://raw.githubusercontent.com/SagerNet/sing-geoip/rule-set/geoip-cn.srs", "download_detour": "PROXY"},
			},
			"final": "PROXY",
		},
	}
	return json.MarshalIndent(doc, "", "  ")
}

func singboxOutbound(l Line) m {
	ib := l.Inbound
	o := m{"tag": l.Name, "server": l.Host, "server_port": l.Port}
	switch ib.Protocol {
	case spec.VLESS:
		o["type"] = "vless"
		o["uuid"] = l.UUID
		if ib.Flow != "" && hasTLS(l) && transportType(l) == "tcp" {
			o["flow"] = ib.Flow
		}
	case spec.VMess:
		o["type"] = "vmess"
		o["uuid"] = l.UUID
		o["security"] = "auto"
		o["alter_id"] = 0
	case spec.Trojan:
		o["type"] = "trojan"
		o["password"] = l.Password
	case spec.Shadowsocks:
		o["type"] = "shadowsocks"
		o["method"] = ib.Cipher
		o["password"] = ssPassword(l)
	case spec.Hysteria2:
		o["type"] = "hysteria2"
		o["password"] = l.Password
		if ib.Obfs != "" {
			o["obfs"] = m{"type": ib.Obfs, "password": ib.ObfsPassword}
		}
		if ib.UpMbps > 0 {
			o["up_mbps"] = ib.UpMbps
		}
		if ib.DownMbps > 0 {
			o["down_mbps"] = ib.DownMbps
		}
	case spec.TUIC:
		o["type"] = "tuic"
		o["uuid"] = l.UUID
		o["password"] = l.Password
		if ib.CongestionControl != "" {
			o["congestion_control"] = ib.CongestionControl
		}
	case spec.AnyTLS:
		o["type"] = "anytls"
		o["password"] = l.Password
	case spec.SOCKS:
		o["type"] = "socks"
		o["username"] = l.UUID
		o["password"] = l.Password
	case spec.HTTP:
		o["type"] = "http"
		o["username"] = l.UUID
		o["password"] = l.Password
	default:
		return nil // mieru: no upstream sing-box client support
	}
	if transportType(l) == "xhttp" || transportType(l) == "http" && ib.Protocol == spec.Trojan {
		return nil
	}
	if hasTLS(l) || ib.Protocol == spec.Hysteria2 || ib.Protocol == spec.TUIC || ib.Protocol == spec.AnyTLS {
		tls := m{"enabled": true}
		if sn := serverName(l); sn != "" {
			tls["server_name"] = sn
		}
		if isReality(l) {
			r := ib.TLS.Reality
			sid := ""
			if len(r.ShortIDs) > 0 {
				sid = r.ShortIDs[0]
			}
			tls["utls"] = m{"enabled": true, "fingerprint": "chrome"}
			tls["reality"] = m{"enabled": true, "public_key": r.PublicKey, "short_id": sid}
		} else if ib.Protocol == spec.VLESS || ib.Protocol == spec.VMess || ib.Protocol == spec.Trojan {
			tls["utls"] = m{"enabled": true, "fingerprint": "chrome"}
		}
		if len(ib.TLS.ALPN) > 0 {
			tls["alpn"] = ib.TLS.ALPN
		}
		o["tls"] = tls
	}
	if tr := ib.Transport; tr != nil {
		switch tr.Type {
		case "ws":
			t := m{"type": "ws", "path": tr.Path}
			if tr.Host != "" {
				t["headers"] = m{"Host": tr.Host}
			}
			o["transport"] = t
		case "grpc":
			o["transport"] = m{"type": "grpc", "service_name": tr.ServiceName}
		case "httpupgrade":
			t := m{"type": "httpupgrade", "path": tr.Path}
			if tr.Host != "" {
				t["host"] = tr.Host
			}
			o["transport"] = t
		case "http":
			t := m{"type": "http", "path": tr.Path}
			if tr.Host != "" {
				t["host"] = []string{tr.Host}
			}
			o["transport"] = t
		}
	}
	if ib.Multiplex != nil && ib.Multiplex.Enabled {
		mx := m{"enabled": true, "protocol": "smux", "padding": ib.Multiplex.Padding}
		if ib.Multiplex.Brutal != nil {
			mx["brutal"] = m{"enabled": true, "up_mbps": ib.Multiplex.Brutal.UpMbps, "down_mbps": ib.Multiplex.Brutal.DownMbps}
		}
		o["multiplex"] = mx
	}
	return o
}
