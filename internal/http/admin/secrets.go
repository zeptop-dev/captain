package admin

import (
	"crypto/rand"
	"encoding/base64"
	"strings"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
	"golang.org/x/crypto/curve25519"
)

// fillInboundSecrets generates the keys an inbound cannot work without
// when the caller left them blank: the console's recipes, the API and
// scripts all go through here, so a Shadowsocks 2022 inbound never reaches
// a node without a server key (sing-box refuses the whole config for one
// such inbound) and REALITY / WireGuard / snell get fresh key material.
// Keys that are already set are never touched.
func fillInboundSecrets(ib *domain.Inbound) {
	s := &ib.Settings
	switch ib.Protocol {
	case spec.Shadowsocks:
		if n := ss2022KeyLen(s.Cipher); n > 0 && strings.TrimSpace(s.ServerKey) == "" {
			s.ServerKey = base64.StdEncoding.EncodeToString(randomBytes(n))
		}
	case spec.Snell:
		if strings.TrimSpace(s.SnellPSK) == "" {
			s.SnellPSK = base64.StdEncoding.EncodeToString(randomBytes(32))
		}
	case spec.WireGuard:
		if strings.TrimSpace(s.WGPrivateKey) == "" {
			priv, pub := x25519Pair()
			s.WGPrivateKey, s.WGPublicKey = base64.StdEncoding.EncodeToString(priv), base64.StdEncoding.EncodeToString(pub)
		} else if strings.TrimSpace(s.WGPublicKey) == "" {
			if priv, err := base64.StdEncoding.DecodeString(s.WGPrivateKey); err == nil && len(priv) == 32 {
				if pub, err := curve25519.X25519(priv, curve25519.Basepoint); err == nil {
					s.WGPublicKey = base64.StdEncoding.EncodeToString(pub)
				}
			}
		}
	}
	// REALITY keys are xray's format: base64url without padding.
	if s.TLS != nil && s.TLS.Mode == spec.TLSReality && s.TLS.Reality != nil {
		r := s.TLS.Reality
		if strings.TrimSpace(r.PrivateKey) == "" {
			priv, pub := x25519Pair()
			r.PrivateKey, r.PublicKey = base64.RawURLEncoding.EncodeToString(priv), base64.RawURLEncoding.EncodeToString(pub)
		} else if strings.TrimSpace(r.PublicKey) == "" {
			if priv, err := base64.RawURLEncoding.DecodeString(r.PrivateKey); err == nil && len(priv) == 32 {
				if pub, err := curve25519.X25519(priv, curve25519.Basepoint); err == nil {
					r.PublicKey = base64.RawURLEncoding.EncodeToString(pub)
				}
			}
		}
		if len(r.ShortIDs) == 0 {
			r.ShortIDs = []string{strings.ToLower(hexOf(randomBytes(4)))}
		}
	}
}

// ss2022KeyLen is the PSK length a Shadowsocks 2022 cipher needs; 0 for
// the classic AEAD ciphers, which take any password.
func ss2022KeyLen(cipher string) int {
	switch strings.ToLower(strings.TrimSpace(cipher)) {
	case "2022-blake3-aes-128-gcm":
		return 16
	case "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305":
		return 32
	}
	return 0
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // the OS random source is gone; nothing sensible to do
	}
	return b
}

func x25519Pair() (priv, pub []byte) {
	priv = randomBytes(32)
	// Clamp as WireGuard and xray do.
	priv[0] &= 248
	priv[31] &= 127
	priv[31] |= 64
	pub, _ = curve25519.X25519(priv, curve25519.Basepoint)
	return priv, pub
}

const hexDigits = "0123456789abcdef"

func hexOf(b []byte) string {
	out := make([]byte, 2*len(b))
	for i, c := range b {
		out[2*i], out[2*i+1] = hexDigits[c>>4], hexDigits[c&15]
	}
	return string(out)
}
