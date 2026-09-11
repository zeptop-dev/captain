package subscription

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"gitlab.com/boyang-hu/bosun/pkg/spec"
)

// URIList renders share links, one per line, base64-encoded as a whole.
// This is what v2rayN, Shadowrocket and most mobile apps import.
type URIList struct{}

func (URIList) Name() string        { return "uri" }
func (URIList) ContentType() string { return "text/plain; charset=utf-8" }

func (URIList) Render(lines []Line, _ Account) ([]byte, error) {
	var out []string
	for _, l := range lines {
		if u := shareURI(l); u != "" {
			out = append(out, u)
		}
	}
	return []byte(b64(strings.Join(out, "\n"))), nil
}

// ShareURI is exported for the portal's per-server copy links.
func ShareURI(l Line) string { return shareURI(l) }

func shareURI(l Line) string {
	ib := l.Inbound
	hostPort := l.Host + ":" + strconv.Itoa(l.Port)
	frag := "#" + url.PathEscape(l.Name)
	switch ib.Protocol {
	case spec.VLESS:
		q := url.Values{}
		q.Set("encryption", "none")
		if ib.Flow != "" && hasTLS(l) && transportType(l) == "tcp" {
			q.Set("flow", ib.Flow)
		}
		uriTLS(q, l)
		uriTransport(q, l)
		return "vless://" + l.UUID + "@" + hostPort + "?" + q.Encode() + frag
	case spec.VMess:
		v := m{"v": "2", "ps": l.Name, "add": l.Host, "port": strconv.Itoa(l.Port), "id": l.UUID, "aid": "0", "scy": "auto",
			"net": vmessNet(l), "type": "none", "host": "", "path": "", "tls": ""}
		if tr := ib.Transport; tr != nil {
			v["host"], v["path"] = tr.Host, tr.Path
			if tr.Type == "grpc" {
				v["path"] = tr.ServiceName
			}
		}
		if hasTLS(l) {
			v["tls"] = "tls"
			v["sni"] = serverName(l)
			v["fp"] = "chrome"
		}
		b, _ := json.Marshal(v)
		return "vmess://" + b64(string(b))
	case spec.Trojan:
		q := url.Values{}
		uriTLS(q, l)
		uriTransport(q, l)
		return "trojan://" + url.PathEscape(l.Password) + "@" + hostPort + "?" + q.Encode() + frag
	case spec.Shadowsocks:
		return "ss://" + b64(ib.Cipher+":"+ssPassword(l)) + "@" + hostPort + frag
	case spec.Hysteria2:
		q := url.Values{}
		if sn := serverName(l); sn != "" {
			q.Set("sni", sn)
		}
		if ib.Obfs != "" {
			q.Set("obfs", ib.Obfs)
			q.Set("obfs-password", ib.ObfsPassword)
		}
		return "hysteria2://" + url.PathEscape(l.Password) + "@" + hostPort + "/?" + q.Encode() + frag
	case spec.TUIC:
		q := url.Values{}
		if sn := serverName(l); sn != "" {
			q.Set("sni", sn)
		}
		if ib.CongestionControl != "" {
			q.Set("congestion_control", ib.CongestionControl)
		}
		q.Set("udp_relay_mode", "native")
		return "tuic://" + l.UUID + ":" + url.PathEscape(l.Password) + "@" + hostPort + "?" + q.Encode() + frag
	case spec.AnyTLS:
		q := url.Values{}
		if sn := serverName(l); sn != "" {
			q.Set("sni", sn)
		}
		return "anytls://" + url.PathEscape(l.Password) + "@" + hostPort + "?" + q.Encode() + frag
	case spec.Mieru:
		q := url.Values{}
		q.Set("port", strconv.Itoa(l.Port))
		proto := ib.MieruTransport
		if proto == "" {
			proto = "TCP"
		}
		q.Set("protocol", proto)
		q.Set("profile", l.Name)
		return "mierus://" + url.PathEscape(l.UUID) + ":" + url.PathEscape(l.Password) + "@" + l.Host + "?" + q.Encode()
	}
	return ""
}

func vmessNet(l Line) string {
	switch transportType(l) {
	case "ws", "grpc", "httpupgrade", "xhttp":
		return transportType(l)
	case "http":
		return "h2"
	}
	return "tcp"
}

func uriTLS(q url.Values, l Line) {
	if !hasTLS(l) {
		q.Set("security", "none")
		return
	}
	if isReality(l) {
		r := l.Inbound.TLS.Reality
		q.Set("security", "reality")
		q.Set("pbk", r.PublicKey)
		if len(r.ShortIDs) > 0 {
			q.Set("sid", r.ShortIDs[0])
		}
	} else {
		q.Set("security", "tls")
	}
	if sn := serverName(l); sn != "" {
		q.Set("sni", sn)
	}
	q.Set("fp", "chrome")
}

func uriTransport(q url.Values, l Line) {
	tr := l.Inbound.Transport
	t := transportType(l)
	switch t {
	case "tcp":
		q.Set("type", "tcp")
	case "ws", "httpupgrade", "xhttp":
		q.Set("type", t)
		if tr.Path != "" {
			q.Set("path", tr.Path)
		}
		if tr.Host != "" {
			q.Set("host", tr.Host)
		}
		if t == "xhttp" && tr.Mode != "" {
			q.Set("mode", tr.Mode)
		}
	case "grpc":
		q.Set("type", "grpc")
		q.Set("serviceName", tr.ServiceName)
	case "http":
		q.Set("type", "http")
		if tr.Path != "" {
			q.Set("path", tr.Path)
		}
		if tr.Host != "" {
			q.Set("host", tr.Host)
		}
	}
}
