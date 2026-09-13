package subscription

import (
	"fmt"
	"strings"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

// QuantumultX renders Quantumult X server lines (shadowsocks=host:port,...).
// The body is base64-encoded as QX's remote server subscriptions expect.
// QX speaks ss, vmess, vless and trojan over tcp/ws; hysteria2, tuic and
// the rest are skipped.
type QuantumultX struct{}

func (QuantumultX) Name() string        { return "qx" }
func (QuantumultX) ContentType() string { return "text/plain; charset=utf-8" }

func (q QuantumultX) Render(lines []Line, acct Account) ([]byte, error) {
	return q.RenderWith(lines, acct, "")
}

func (QuantumultX) RenderWith(lines []Line, _ Account, tpl string) ([]byte, error) {
	body, names := iniLines(lines, qxLine)
	return []byte(b64(string(applyINI(tpl, "qx", body, names)))), nil
}

func qxLine(l Line) string {
	ib := l.Inbound
	addr := fmt.Sprintf("%s:%d", l.Host, l.Port)
	var parts []string
	switch ib.Protocol {
	case spec.Shadowsocks:
		parts = []string{"shadowsocks=" + addr, "method=" + ib.Cipher, "password=" + ssPassword(l)}
	case spec.VMess:
		parts = []string{"vmess=" + addr, "method=aes-128-gcm", "password=" + l.UUID}
		t, ok := qxTransport(l, false)
		if !ok {
			return ""
		}
		parts = append(parts, t...)
	case spec.VLESS:
		parts = []string{"vless=" + addr, "method=none", "password=" + l.UUID}
		t, ok := qxTransport(l, false)
		if !ok {
			return ""
		}
		parts = append(parts, t...)
		if ib.Flow != "" && transportType(l) == "tcp" {
			parts = append(parts, "vless-flow="+ib.Flow)
		}
	case spec.Trojan:
		parts = []string{"trojan=" + addr, "password=" + l.Password}
		t, ok := qxTransport(l, true)
		if !ok {
			return ""
		}
		parts = append(parts, t...)
	default:
		return ""
	}
	parts = append(parts, "fast-open=true", "udp-relay=true", "tag="+l.Name)
	return strings.Join(parts, ", ")
}

// qxTransport expresses TLS and ws as QX obfs settings. Trojan carries TLS
// natively (over-tls / tls-host); vmess and vless wrap it in obfs.
func qxTransport(l Line, nativeTLS bool) ([]string, bool) {
	var out []string
	host := ""
	tt := transportType(l)
	switch tt {
	case "ws":
		if hasTLS(l) {
			out = append(out, "obfs=wss")
		} else {
			out = append(out, "obfs=ws")
		}
		if p := l.Inbound.Transport.Path; p != "" {
			out = append(out, "obfs-uri="+p)
		}
		host = l.Inbound.Transport.Host
	case "tcp":
		if hasTLS(l) {
			if nativeTLS {
				out = append(out, "over-tls=true")
			} else {
				out = append(out, "obfs=over-tls")
			}
		}
	default:
		return nil, false
	}
	if isReality(l) {
		r := l.Inbound.TLS.Reality
		if host == "" {
			host = serverName(l)
		}
		out = append(out, "reality-base64-pubkey="+r.PublicKey)
		if len(r.ShortIDs) > 0 {
			out = append(out, "reality-hex-shortid="+r.ShortIDs[0])
		}
	} else if hasTLS(l) {
		out = append(out, "tls-verification=true")
		if host == "" {
			host = serverName(l)
		}
	}
	if host != "" {
		if nativeTLS && tt != "ws" {
			out = append(out, "tls-host="+host)
		} else {
			out = append(out, "obfs-host="+host)
		}
	}
	return out, true
}
