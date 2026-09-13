package subscription

import (
	"fmt"
	"strings"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

// Loon renders Loon node lines after the official node reference
// (nsloon.app/docs/Node): positional fields first, then key=value options.
// Loon speaks ss, vmess, vless, trojan, hysteria2 and anytls over tcp, ws
// and http transports; grpc, httpupgrade, xhttp and the other protocols are
// skipped.
type Loon struct{}

func (Loon) Name() string        { return "loon" }
func (Loon) ContentType() string { return "text/plain; charset=utf-8" }

func (l Loon) Render(lines []Line, acct Account) ([]byte, error) {
	return l.RenderWith(lines, acct, "")
}

func (Loon) RenderWith(lines []Line, _ Account, tpl string) ([]byte, error) {
	body, names := iniLines(lines, loonLine)
	return applyINI(tpl, "loon", body, names), nil
}

func loonLine(l Line) string {
	ib := l.Inbound
	q := func(s string) string { return `"` + s + `"` }
	head := func(kind string) []string { return []string{l.Name + " = " + kind, l.Host, fmt.Sprint(l.Port)} }
	var parts []string
	switch ib.Protocol {
	case spec.Shadowsocks:
		if ss2022KeyLen(ib.Cipher) > 0 {
			return "" // Loon has no SS2022 multi-user support
		}
		parts = append(head("Shadowsocks"), ib.Cipher, q(ssPassword(l)), "fast-open=false", "udp=true")
	case spec.VMess:
		t, ok := loonTransport(l)
		if !ok {
			return ""
		}
		parts = append(head("vmess"), "aes-128-gcm", q(l.UUID))
		parts = append(parts, t...)
		parts = append(parts, "alterId=0", "over-tls="+boolStr(hasTLS(l)))
		if hasTLS(l) {
			parts = append(parts, "sni="+serverName(l), "skip-cert-verify=false")
		}
		parts = append(parts, "udp=true")
	case spec.VLESS:
		t, ok := loonTransport(l)
		if !ok {
			return ""
		}
		parts = append(head("VLESS"), q(l.UUID))
		parts = append(parts, t...)
		if ib.Flow != "" && transportType(l) == "tcp" && hasTLS(l) {
			parts = append(parts, "flow="+ib.Flow)
		}
		parts = append(parts, "over-tls="+boolStr(hasTLS(l)))
		if hasTLS(l) {
			parts = append(parts, "sni="+serverName(l), "skip-cert-verify=false")
			if isReality(l) {
				r := ib.TLS.Reality
				parts = append(parts, "public-key="+r.PublicKey)
				if len(r.ShortIDs) > 0 {
					parts = append(parts, "short-id="+r.ShortIDs[0])
				}
			}
		}
		parts = append(parts, "udp=true")
	case spec.Trojan:
		if isReality(l) || !hasTLS(l) {
			return "" // Loon trojan is TLS only, no REALITY
		}
		switch transportType(l) {
		case "tcp":
		case "ws", "http":
			parts = append(parts, "transport="+transportType(l))
			if ib.Transport.Path != "" {
				parts = append(parts, "path="+ib.Transport.Path)
			}
			if ib.Transport.Host != "" {
				parts = append(parts, "host="+ib.Transport.Host)
			}
		default:
			return ""
		}
		parts = append(append(head("trojan"), q(l.Password)), parts...)
		parts = append(parts, "sni="+serverName(l), "skip-cert-verify=false", "udp=true")
	case spec.Hysteria2:
		parts = append(head("Hysteria2"), q(l.Password), "sni="+serverName(l), "skip-cert-verify=false", "fast-open=true")
		if ib.Obfs == "salamander" && ib.ObfsPassword != "" {
			parts = append(parts, "salamander-password="+q(ib.ObfsPassword))
		}
		parts = append(parts, "udp=true")
	case spec.AnyTLS:
		parts = append(head("AnyTLS"), q(l.Password), "sni="+serverName(l), "skip-cert-verify=false", "udp=true")
	default:
		return ""
	}
	return strings.Join(parts, ",")
}

// loonTransport maps the transport onto Loon's transport=/path=/host= keys;
// false when Loon has no equivalent.
func loonTransport(l Line) ([]string, bool) {
	tr := l.Inbound.Transport
	switch transportType(l) {
	case "tcp":
		return []string{"transport=tcp"}, true
	case "ws", "http":
		out := []string{"transport=" + transportType(l)}
		if tr.Path != "" {
			out = append(out, "path="+tr.Path)
		}
		if tr.Host != "" {
			out = append(out, "host="+tr.Host)
		}
		return out, true
	}
	return nil, false
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
