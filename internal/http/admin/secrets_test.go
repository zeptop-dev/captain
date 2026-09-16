package admin

import (
	"encoding/base64"
	"testing"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
)

func TestFillInboundSecrets(t *testing.T) {
	ss := &domain.Inbound{Protocol: spec.Shadowsocks, Settings: spec.Inbound{Cipher: "2022-blake3-aes-128-gcm"}}
	fillInboundSecrets(ss)
	if k, err := base64.StdEncoding.DecodeString(ss.Settings.ServerKey); err != nil || len(k) != 16 {
		t.Fatalf("aes-128 needs a 16-byte key, got %q", ss.Settings.ServerKey)
	}
	keep := &domain.Inbound{Protocol: spec.Shadowsocks, Settings: spec.Inbound{Cipher: "2022-blake3-aes-256-gcm", ServerKey: "given"}}
	fillInboundSecrets(keep)
	if keep.Settings.ServerKey != "given" {
		t.Fatal("an existing key must be kept")
	}
	plain := &domain.Inbound{Protocol: spec.Shadowsocks, Settings: spec.Inbound{Cipher: "aes-128-gcm"}}
	fillInboundSecrets(plain)
	if plain.Settings.ServerKey != "" {
		t.Fatal("classic ciphers take any password; nothing to generate")
	}
	wg := &domain.Inbound{Protocol: spec.WireGuard}
	fillInboundSecrets(wg)
	if wg.Settings.WGPrivateKey == "" || wg.Settings.WGPublicKey == "" {
		t.Fatal("wireguard pair expected")
	}
	derived := &domain.Inbound{Protocol: spec.WireGuard, Settings: spec.Inbound{WGPrivateKey: wg.Settings.WGPrivateKey}}
	fillInboundSecrets(derived)
	if derived.Settings.WGPublicKey != wg.Settings.WGPublicKey {
		t.Fatal("public key must derive from the given private key")
	}
	re := &domain.Inbound{Protocol: spec.VLESS, Settings: spec.Inbound{TLS: &spec.TLS{Mode: spec.TLSReality, Reality: &spec.Reality{HandshakeServer: "www.example.com"}}}}
	fillInboundSecrets(re)
	r := re.Settings.TLS.Reality
	if r.PrivateKey == "" || r.PublicKey == "" || len(r.ShortIDs) != 1 || len(r.ShortIDs[0]) != 8 {
		t.Fatalf("reality keys and a short id expected: %+v", r)
	}
	sn := &domain.Inbound{Protocol: spec.Snell}
	fillInboundSecrets(sn)
	if sn.Settings.SnellPSK == "" {
		t.Fatal("snell psk expected")
	}
}
