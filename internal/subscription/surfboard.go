package subscription

import (
	"fmt"
	"strings"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

// Surfboard renders a Surge-style config for Surfboard (Android), which
// only speaks ss, vmess, trojan and anytls.
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
		parts = append(parts, "encrypt-method="+ib.Cipher, "password="+ssPassword(l), "tfo=true", "udp-relay=true")
		return fmt.Sprintf(base, "ss") + ", " + strings.Join(parts, ", ")
	case spec.VMess:
		parts = append(parts, "username="+l.UUID, "vmess-aead=true", "tfo=true", "udp-relay=true")
		if hasTLS(l) {
			parts = append(parts, "tls=true")
			if sn := serverName(l); sn != "" {
				parts = append(parts, "sni="+sn)
			}
		}
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
		return fmt.Sprintf(base, "vmess") + ", " + strings.Join(parts, ", ")
	case spec.Trojan:
		if transportType(l) != "tcp" {
			return ""
		}
		parts = append(parts, "password="+l.Password)
		if sn := serverName(l); sn != "" {
			parts = append(parts, "sni="+sn)
		}
		parts = append(parts, "tfo=true", "udp-relay=true")
		return fmt.Sprintf(base, "trojan") + ", " + strings.Join(parts, ", ")
	case spec.AnyTLS:
		parts = append(parts, "password="+l.Password, "tfo=true", "udp-relay=true")
		if sn := serverName(l); sn != "" {
			parts = append(parts, "sni="+sn)
		}
		return fmt.Sprintf(base, "anytls") + ", " + strings.Join(parts, ", ")
	}
	return ""
}
