package subscription

import (
	"fmt"
	"strings"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

// Surge renders a Surge 5 proxy list. Surge speaks ss, vmess, trojan,
// hysteria2 and tuic; other protocols are skipped.
type Surge struct{}

func (Surge) Name() string        { return "surge" }
func (Surge) ContentType() string { return "text/plain; charset=utf-8" }

func (s Surge) Render(lines []Line, acct Account) ([]byte, error) {
	return s.RenderWith(lines, acct, "")
}

func (Surge) RenderWith(lines []Line, _ Account, tpl string) ([]byte, error) {
	body, names := iniLines(lines, surgeLine)
	return applyINI(tpl, "surge", body, names), nil
}

// iniLines renders each line with fn, skipping the ones it cannot express.
func iniLines(lines []Line, fn func(Line) string) (string, []string) {
	var b strings.Builder
	names := []string{}
	for _, l := range lines {
		if s := fn(l); s != "" {
			b.WriteString(s + "\n")
			names = append(names, l.Name)
		}
	}
	return strings.TrimRight(b.String(), "\n"), names
}

func surgeLine(l Line) string {
	ib := l.Inbound
	base := fmt.Sprintf("%s = %%s, %s, %d", l.Name, l.Host, l.Port)
	var parts []string
	switch ib.Protocol {
	case spec.Shadowsocks:
		if ss2022KeyLen(ib.Cipher) > 0 {
			return "" // Surge does not support SS2022 multi-user keys
		}
		parts = append(parts, "encrypt-method="+ib.Cipher, "password="+l.Password, "udp-relay=true")
		return fmt.Sprintf(base, "ss") + ", " + strings.Join(parts, ", ")
	case spec.VMess:
		parts = append(parts, "username="+l.UUID)
		if hasTLS(l) {
			parts = append(parts, "tls=true", "sni="+serverName(l))
		}
		if transportType(l) == "ws" {
			parts = append(parts, "ws=true", "ws-path="+ib.Transport.Path)
			if ib.Transport.Host != "" {
				parts = append(parts, "ws-headers=Host:"+ib.Transport.Host)
			}
		} else if transportType(l) != "tcp" {
			return ""
		}
		return fmt.Sprintf(base, "vmess") + ", " + strings.Join(parts, ", ")
	case spec.Trojan:
		if transportType(l) != "tcp" && transportType(l) != "ws" {
			return ""
		}
		parts = append(parts, "password="+l.Password, "sni="+serverName(l))
		if transportType(l) == "ws" {
			parts = append(parts, "ws=true", "ws-path="+ib.Transport.Path)
		}
		return fmt.Sprintf(base, "trojan") + ", " + strings.Join(parts, ", ")
	case spec.Hysteria2:
		parts = append(parts, "password="+l.Password, "sni="+serverName(l))
		if ib.DownMbps > 0 {
			parts = append(parts, fmt.Sprintf("download-bandwidth=%d", ib.DownMbps))
		}
		return fmt.Sprintf(base, "hysteria2") + ", " + strings.Join(parts, ", ")
	case spec.TUIC:
		parts = append(parts, "uuid="+l.UUID, "password="+l.Password, "sni="+serverName(l), "alpn=h3", "version=5")
		return fmt.Sprintf(base, "tuic") + ", " + strings.Join(parts, ", ")
	case spec.Snell:
		parts = append(parts, "psk="+ib.SnellPSK, fmt.Sprintf("version=%d", snellVersion(ib)))
		if ib.SnellObfs != "" && ib.SnellObfs != "off" {
			parts = append(parts, "obfs="+ib.SnellObfs)
			if ib.SnellObfsHost != "" {
				parts = append(parts, "obfs-host="+ib.SnellObfsHost)
			}
		}
		return fmt.Sprintf(base, "snell") + ", " + strings.Join(parts, ", ")
	}
	return ""
}
