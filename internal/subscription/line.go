// Package subscription renders a user's entries into client formats.
package subscription

import (
	"encoding/base64"
	"strings"

	"gitlab.com/boyang-hu/bosun/pkg/spec"
)

// Line is one server as the client should see it: the landing inbound's
// protocol settings with the entry's display address.
type Line struct {
	Name     string
	Host     string
	Port     int
	Inbound  spec.Inbound
	UUID     string // user identity
	Password string
}

// Account is the usage summary sent in the subscription-userinfo header.
type Account struct {
	Upload   int64
	Download int64
	Total    int64 // 0 = unlimited
	Expire   int64 // unix seconds, 0 = never
}

// Renderer turns lines into a client document.
type Renderer interface {
	Name() string
	ContentType() string
	Render(lines []Line, acct Account) ([]byte, error)
}

// Supported reports whether a renderer can express the line's protocol and
// transport; unsupported lines are skipped rather than emitted broken.
type supportFn func(Line) bool

func hasTLS(l Line) bool { return l.Inbound.TLS != nil && l.Inbound.TLS.Mode != spec.TLSNone }
func isReality(l Line) bool {
	return l.Inbound.TLS != nil && l.Inbound.TLS.Mode == spec.TLSReality && l.Inbound.TLS.Reality != nil
}
func serverName(l Line) string {
	if l.Inbound.TLS != nil {
		return l.Inbound.TLS.ServerName
	}
	return ""
}

// ss2022UserKey mirrors bosun's derivation: first n bytes of the UUID string,
// zero padded, base64. The client sends "serverKey:userKey".
func ss2022UserKey(uuid string, n int) string {
	buf := make([]byte, n)
	copy(buf, uuid)
	return base64.StdEncoding.EncodeToString(buf)
}

func ss2022KeyLen(cipher string) int {
	switch cipher {
	case "2022-blake3-aes-128-gcm":
		return 16
	case "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305":
		return 32
	}
	return 0
}

// ssPassword is what the client puts in the password field.
func ssPassword(l Line) string {
	if n := ss2022KeyLen(l.Inbound.Cipher); n > 0 {
		return l.Inbound.ServerKey + ":" + ss2022UserKey(l.UUID, n)
	}
	return l.Password
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func transportType(l Line) string { return l.Inbound.TransportType() }

func joinNonEmpty(parts []string, sep string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}
