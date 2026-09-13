package subscription

import (
	"fmt"
	"strings"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

// Loon renders Loon node lines (name=Type,host,port,...). Loon takes ss,
// vmess, vless, trojan, hysteria2 and anytls; the rest are skipped.
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
	head := func(kind string) []string { return []string{l.Name + "=" + kind, l.Host, fmt.Sprint(l.Port)} }
	var parts []string
	switch ib.Protocol {
	case spec.Shadowsocks:
		parts = append(head("Shadowsocks"), ib.Cipher, ssPassword(l), "fast-open=false", "udp=true")
	case spec.VMess:
		parts = append(head("vmess"), "auto", l.UUID, "fast-open=false", "udp=true", "alterId=0")
		if hasTLS(l) {
			parts = append(parts, "over-tls=true", "skip-cert-verify=false")
			if sn := serverName(l); sn != "" {
				parts = append(parts, "tls-name="+sn)
			}
		}
		t, ok := loonTransport(l)
		if !ok {
			return ""
		}
		parts = append(parts, t...)
	case spec.VLESS:
		parts = append(head("VLESS"), l.UUID, "alterId=0", "udp=true")
		if ib.Flow != "" && transportType(l) == "tcp" {
			parts = append(parts, "flow="+ib.Flow)
		}
		if hasTLS(l) {
			parts = append(parts, "over-tls=true", "skip-cert-verify=false")
			if sn := serverName(l); sn != "" {
				parts = append(parts, "sni="+sn)
			}
			if isReality(l) {
				r := ib.TLS.Reality
				parts = append(parts, "public-key="+r.PublicKey)
				if len(r.ShortIDs) > 0 {
					parts = append(parts, "short-id="+r.ShortIDs[0])
				}
			}
		} else {
			parts = append(parts, "over-tls=false")
		}
		t, ok := loonTransport(l)
		if !ok {
			return ""
		}
		parts = append(parts, t...)
	case spec.Trojan:
		parts = append(head("trojan"), l.Password)
		if sn := serverName(l); sn != "" {
			parts = append(parts, "tls-name="+sn)
		}
		if isReality(l) {
			r := ib.TLS.Reality
			parts = append(parts, "public-key="+r.PublicKey)
			if len(r.ShortIDs) > 0 {
				parts = append(parts, "short-id="+r.ShortIDs[0])
			}
		}
		parts = append(parts, "skip-cert-verify=false")
		t, ok := loonTransport(l)
		if !ok {
			return ""
		}
		parts = append(parts, t...)
	case spec.Hysteria2:
		parts = append(head("Hysteria2"), l.Password)
		if sn := serverName(l); sn != "" {
			parts = append(parts, "sni="+sn)
		}
		if ib.DownMbps > 0 {
			parts = append(parts, fmt.Sprintf("download-bandwidth=%d", ib.DownMbps))
		}
		parts = append(parts, "udp=true")
	case spec.AnyTLS:
		parts = append(head("anytls"), l.Password, "udp=true")
		if sn := serverName(l); sn != "" {
			parts = append(parts, "sni="+sn)
		}
	default:
		return ""
	}
	return strings.Join(parts, ",")
}

// loonTransport maps the transport onto Loon's transport=/path=/host=
// keys; false when Loon has no equivalent.
func loonTransport(l Line) ([]string, bool) {
	tr := l.Inbound.Transport
	switch transportType(l) {
	case "tcp":
		return []string{"transport=tcp"}, true
	case "ws", "httpupgrade":
		out := []string{"transport=" + transportType(l)}
		if tr.Path != "" {
			out = append(out, "path="+tr.Path)
		}
		if tr.Host != "" {
			out = append(out, "host="+tr.Host)
		}
		return out, true
	case "grpc":
		out := []string{"transport=grpc"}
		if tr.ServiceName != "" {
			out = append(out, "grpc-service-name="+tr.ServiceName)
		}
		return out, true
	case "http":
		out := []string{"transport=h2"}
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
