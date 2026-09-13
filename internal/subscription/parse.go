package subscription

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

// ParseURI decodes a share link (vless, vmess, trojan, ss, hysteria2, tuic,
// anytls, socks, http) into a Line: the inverse of shareURI, used to import
// external nodes and to build landing outbounds.
func ParseURI(raw string) (Line, error) {
	raw = strings.TrimSpace(raw)
	scheme, rest, ok := strings.Cut(raw, "://")
	if !ok {
		return Line{}, errors.New("not a share link")
	}
	scheme = strings.ToLower(scheme)
	if scheme == "vmess" {
		return parseVMess(rest)
	}
	if scheme == "ss" {
		return parseSS(rest)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return Line{}, err
	}
	l := Line{Name: u.Fragment, Host: u.Hostname()}
	if l.Host == "" {
		return Line{}, errors.New("missing host")
	}
	l.Port, _ = strconv.Atoi(u.Port())
	if l.Port <= 0 {
		return Line{}, errors.New("missing port")
	}
	q := u.Query()
	user := ""
	pass, hasPass := "", false
	if u.User != nil {
		user = u.User.Username()
		pass, hasPass = u.User.Password()
	}
	ib := &l.Inbound
	switch scheme {
	case "vless":
		ib.Protocol, l.UUID = spec.VLESS, user
		ib.Flow = q.Get("flow")
		parseTLSQuery(ib, q)
		parseTransportQuery(ib, q)
	case "trojan":
		ib.Protocol, l.Password = spec.Trojan, user
		parseTLSQuery(ib, q)
		parseTransportQuery(ib, q)
		if ib.TLS == nil { // trojan is TLS by definition
			ib.TLS = &spec.TLS{Mode: spec.TLSStandard, ServerName: firstNonEmptyStr(q.Get("sni"), q.Get("peer"), l.Host)}
		}
	case "hysteria2", "hy2":
		ib.Protocol, l.Password = spec.Hysteria2, user
		ib.TLS = &spec.TLS{Mode: spec.TLSStandard, ServerName: firstNonEmptyStr(q.Get("sni"), l.Host)}
		if o := q.Get("obfs"); o != "" && o != "none" {
			ib.Obfs, ib.ObfsPassword = o, q.Get("obfs-password")
		}
	case "tuic":
		ib.Protocol, l.UUID = spec.TUIC, user
		if hasPass {
			l.Password = pass
		}
		ib.TLS = &spec.TLS{Mode: spec.TLSStandard, ServerName: firstNonEmptyStr(q.Get("sni"), l.Host)}
		ib.CongestionControl = q.Get("congestion_control")
	case "anytls":
		ib.Protocol, l.Password = spec.AnyTLS, user
		ib.TLS = &spec.TLS{Mode: spec.TLSStandard, ServerName: firstNonEmptyStr(q.Get("sni"), l.Host)}
	case "socks", "socks5":
		ib.Protocol, l.UUID, l.Password = spec.SOCKS, user, pass
	case "http", "https":
		ib.Protocol, l.UUID, l.Password = spec.HTTP, user, pass
		if scheme == "https" {
			ib.TLS = &spec.TLS{Mode: spec.TLSStandard, ServerName: firstNonEmptyStr(q.Get("sni"), l.Host)}
		}
	default:
		return Line{}, fmt.Errorf("unsupported scheme %s", scheme)
	}
	if l.Name == "" {
		l.Name = net.JoinHostPort(l.Host, strconv.Itoa(l.Port))
	}
	return l, nil
}

func parseTLSQuery(ib *spec.Inbound, q url.Values) {
	switch q.Get("security") {
	case "tls", "xtls":
		ib.TLS = &spec.TLS{Mode: spec.TLSStandard, ServerName: q.Get("sni")}
	case "reality":
		ib.TLS = &spec.TLS{Mode: spec.TLSReality, ServerName: q.Get("sni"), Reality: &spec.Reality{PublicKey: q.Get("pbk")}}
		if sid := q.Get("sid"); sid != "" {
			ib.TLS.Reality.ShortIDs = []string{sid}
		}
	}
	if ib.TLS != nil {
		if alpn := q.Get("alpn"); alpn != "" {
			ib.TLS.ALPN = strings.Split(alpn, ",")
		}
	}
}

func parseTransportQuery(ib *spec.Inbound, q url.Values) {
	t := q.Get("type")
	switch t {
	case "", "tcp", "raw":
		return
	case "ws", "httpupgrade", "xhttp", "splithttp":
		if t == "splithttp" {
			t = "xhttp"
		}
		ib.Transport = &spec.Transport{Type: t, Path: q.Get("path"), Host: q.Get("host"), Mode: q.Get("mode")}
	case "grpc":
		ib.Transport = &spec.Transport{Type: "grpc", ServiceName: q.Get("serviceName")}
	case "http", "h2":
		ib.Transport = &spec.Transport{Type: "http", Path: q.Get("path"), Host: q.Get("host")}
	}
}

func parseVMess(rest string) (Line, error) {
	dec, err := b64Decode(rest)
	if err != nil {
		return Line{}, errors.New("vmess: not base64 JSON")
	}
	var v struct {
		PS, Add, Port, ID, Net, Type, Host, Path, TLS, SNI, ALPN string
	}
	// Port may be a number in some generators.
	var raw map[string]any
	if err := json.Unmarshal(dec, &raw); err != nil {
		return Line{}, errors.New("vmess: bad JSON")
	}
	str := func(k string) string {
		switch x := raw[k].(type) {
		case string:
			return x
		case float64:
			return strconv.Itoa(int(x))
		}
		return ""
	}
	v.PS, v.Add, v.Port, v.ID, v.Net, v.Type, v.Host, v.Path, v.TLS, v.SNI, v.ALPN = str("ps"), str("add"), str("port"), str("id"), str("net"), str("type"), str("host"), str("path"), str("tls"), str("sni"), str("alpn")
	l := Line{Name: v.PS, Host: v.Add, UUID: v.ID}
	l.Port, _ = strconv.Atoi(v.Port)
	if l.Host == "" || l.Port <= 0 || l.UUID == "" {
		return Line{}, errors.New("vmess: missing add/port/id")
	}
	l.Inbound.Protocol = spec.VMess
	switch v.Net {
	case "ws":
		l.Inbound.Transport = &spec.Transport{Type: "ws", Path: v.Path, Host: v.Host}
	case "grpc":
		l.Inbound.Transport = &spec.Transport{Type: "grpc", ServiceName: v.Path}
	case "h2", "http":
		l.Inbound.Transport = &spec.Transport{Type: "http", Path: v.Path, Host: v.Host}
	case "httpupgrade", "xhttp":
		l.Inbound.Transport = &spec.Transport{Type: v.Net, Path: v.Path, Host: v.Host}
	}
	if v.TLS == "tls" {
		l.Inbound.TLS = &spec.TLS{Mode: spec.TLSStandard, ServerName: firstNonEmptyStr(v.SNI, v.Host)}
		if v.ALPN != "" {
			l.Inbound.TLS.ALPN = strings.Split(v.ALPN, ",")
		}
	}
	if l.Name == "" {
		l.Name = net.JoinHostPort(l.Host, v.Port)
	}
	return l, nil
}

// parseSS handles SIP002 (userinfo base64 or plain) and the legacy fully
// base64 form.
func parseSS(rest string) (Line, error) {
	frag := ""
	if i := strings.IndexByte(rest, '#'); i >= 0 {
		frag, _ = url.PathUnescape(rest[i+1:])
		rest = rest[:i]
	}
	if i := strings.IndexByte(rest, '?'); i >= 0 {
		rest = rest[:i] // plugin params are not supported
	}
	var userinfo, hostPort string
	if at := strings.LastIndexByte(rest, '@'); at >= 0 {
		userinfo, hostPort = rest[:at], rest[at+1:]
	} else {
		dec, err := b64Decode(rest)
		if err != nil {
			return Line{}, errors.New("ss: bad link")
		}
		s := string(dec)
		at := strings.LastIndexByte(s, '@')
		if at < 0 {
			return Line{}, errors.New("ss: bad link")
		}
		userinfo, hostPort = s[:at], s[at+1:]
	}
	if dec, err := b64Decode(userinfo); err == nil && strings.Contains(string(dec), ":") {
		userinfo = string(dec)
	} else if u, err := url.PathUnescape(userinfo); err == nil {
		userinfo = u
	}
	method, pass, ok := strings.Cut(userinfo, ":")
	if !ok {
		return Line{}, errors.New("ss: missing method:password")
	}
	host, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		return Line{}, errors.New("ss: bad host:port")
	}
	l := Line{Name: frag, Host: host, Password: pass}
	l.Port, _ = strconv.Atoi(port)
	l.Inbound.Protocol, l.Inbound.Cipher = spec.Shadowsocks, method
	if l.Name == "" {
		l.Name = hostPort
	}
	return l, nil
}

// ParseList decodes a subscription body: base64 or plain text, one share
// link per line. Lines that fail to parse are skipped and counted.
func ParseList(body string) (lines []Line, skipped int) {
	text := strings.TrimSpace(body)
	if dec, err := b64Decode(text); err == nil && strings.Contains(string(dec), "://") {
		text = string(dec)
	}
	for _, ln := range strings.Split(text, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") || !strings.Contains(ln, "://") {
			continue
		}
		l, err := ParseURI(ln)
		if err != nil {
			skipped++
			continue
		}
		lines = append(lines, l)
	}
	return lines, skipped
}

// Remote converts a parsed line into a bosun landing outbound.
func (l Line) Remote() *spec.Remote {
	r := &spec.Remote{Host: l.Host, Port: l.Port, UUID: l.UUID, Password: l.Password, Settings: l.Inbound}
	if l.Inbound.Protocol == spec.SOCKS || l.Inbound.Protocol == spec.HTTP {
		r.Username, r.UUID = l.UUID, ""
	}
	return r
}

func b64Decode(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(strings.ReplaceAll(s, "-", "+"), "_", "/")
	s = strings.TrimRight(s, "=")
	if pad := len(s) % 4; pad != 0 {
		s += strings.Repeat("=", 4-pad)
	}
	return base64.StdEncoding.DecodeString(s)
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
