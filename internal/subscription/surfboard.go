package subscription

import (
	"fmt"
	"strings"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

// Surfboard renders a Surge-style config for Surfboard (Android). Per its
// manual (manual.getsurfboard.com) it speaks http, socks5, ss, vmess and
// trojan only: no vless, hysteria2, tuic or anytls, no grpc/h2 transports.
type Surfboard struct{}

func (Surfboard) Name() string        { return "surfboard" }
func (Surfboard) ContentType() string { return "text/plain; charset=utf-8" }

func (s Surfboard) Render(lines []Line, acct Account) ([]byte, error) {
	return s.RenderWith(lines, acct, "")
}

func (Surfboard) RenderWith(lines []Line, _ Account, tpl string) ([]byte, error) {
	body, names := iniLines(lines, surfboardLine)
	return applyINI(tpl, "surfboard", body, names), nil
}

func surfboardLine(l Line) string {
	ib := l.Inbound
	base := fmt.Sprintf("%s = %%s, %s, %d", l.Name, l.Host, l.Port)
	var parts []string
	switch ib.Protocol {
	case spec.Shadowsocks:
		if ss2022KeyLen(ib.Cipher) > 0 {
			return "" // no SS2022 multi-user keys, as in Surge
		}
		parts = append(parts, "encrypt-method="+ib.Cipher, "password="+ssPassword(l), "udp-relay=true")
		return fmt.Sprintf(base, "ss") + ", " + strings.Join(parts, ", ")
	case spec.VMess:
		parts = append(parts, "username="+l.UUID, "udp-relay=true")
		switch transportType(l) {
		case "ws":
			parts = append(parts, "ws=true", "ws-path="+ib.Transport.Path)
			if ib.Transport.Host != "" {
				parts = append(parts, "ws-headers=Host:"+ib.Transport.Host)
			}
		case "tcp":
		default:
			return ""
		}
		if hasTLS(l) {
			parts = append(parts, "tls=true", "sni="+serverName(l), "skip-cert-verify=false")
		}
		parts = append(parts, "vmess-aead=true")
		return fmt.Sprintf(base, "vmess") + ", " + strings.Join(parts, ", ")
	case spec.Trojan:
		if transportType(l) != "tcp" || isReality(l) {
			return ""
		}
		parts = append(parts, "password="+l.Password, "udp-relay=true", "sni="+serverName(l), "skip-cert-verify=false")
		return fmt.Sprintf(base, "trojan") + ", " + strings.Join(parts, ", ")
	}
	return ""
}
