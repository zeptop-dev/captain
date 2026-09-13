package subscription

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

func TestParseRoundTrip(t *testing.T) {
	for _, l := range sample() {
		if l.Inbound.Protocol == spec.Mieru {
			continue
		}
		uri := shareURI(l)
		got, err := ParseURI(uri)
		if err != nil {
			t.Fatalf("%s: %v (%s)", l.Name, err, uri)
		}
		if got.Name != l.Name || got.Host != l.Host || got.Port != l.Port || got.Inbound.Protocol != l.Inbound.Protocol {
			t.Fatalf("%s: got %+v", l.Name, got)
		}
		if l.Inbound.TLS != nil && (got.Inbound.TLS == nil || got.Inbound.TLS.Mode != l.Inbound.TLS.Mode || got.Inbound.TLS.ServerName != l.Inbound.TLS.ServerName) {
			t.Fatalf("%s: tls %+v", l.Name, got.Inbound.TLS)
		}
		if isReality(l) && (got.Inbound.TLS.Reality.PublicKey != "PUB" || got.Inbound.TLS.Reality.ShortIDs[0] != "0123") {
			t.Fatalf("reality: %+v", got.Inbound.TLS.Reality)
		}
		if tr := l.Inbound.Transport; tr != nil && (got.Inbound.Transport == nil || got.Inbound.Transport.Type != tr.Type || got.Inbound.Transport.Path != tr.Path) {
			t.Fatalf("%s: transport %+v", l.Name, got.Inbound.Transport)
		}
	}
}

func TestParseForeignForms(t *testing.T) {
	// Legacy fully-base64 ss link and a plain-userinfo SIP002 link.
	ss1, err := ParseURI("ss://" + base64.StdEncoding.EncodeToString([]byte("aes-256-gcm:secret@1.2.3.4:8388")) + "#legacy")
	if err != nil || ss1.Inbound.Cipher != "aes-256-gcm" || ss1.Password != "secret" || ss1.Host != "1.2.3.4" || ss1.Name != "legacy" {
		t.Fatalf("legacy ss: %+v %v", ss1, err)
	}
	ss2, err := ParseURI("ss://chacha20-ietf-poly1305:p%40ss@host.test:443?plugin=obfs#plain")
	if err != nil || ss2.Password != "p@ss" || ss2.Port != 443 {
		t.Fatalf("sip002 ss: %+v %v", ss2, err)
	}
	// vmess with numeric port and h2.
	vm := base64.StdEncoding.EncodeToString([]byte(`{"v":"2","ps":"vm","add":"vm.test","port":443,"id":"uuid-1","net":"h2","path":"/h","host":"vm.test","tls":"tls"}`))
	l, err := ParseURI("vmess://" + vm)
	if err != nil || l.Port != 443 || l.Inbound.Transport.Type != "http" || l.Inbound.TLS == nil {
		t.Fatalf("vmess: %+v %v", l, err)
	}
	// socks with credentials becomes a Remote with username.
	sk, err := ParseURI("socks://user:pw@10.0.0.1:1080#exit")
	if err != nil || sk.Remote().Username != "user" || sk.Remote().Password != "pw" {
		t.Fatalf("socks: %+v %v", sk, err)
	}
	if _, err := ParseURI("ftp://x"); err == nil {
		t.Fatal("unsupported scheme accepted")
	}
	// Subscription body: base64 list with one bad line.
	body := base64.StdEncoding.EncodeToString([]byte(strings.Join([]string{"trojan://pw@t.test:443?sni=t.test#a", "garbage://", "ss://bad"}, "\n")))
	lines, skipped := ParseList(body)
	if len(lines) != 1 || skipped != 2 || lines[0].Name != "a" {
		t.Fatalf("list: %d skipped %d", len(lines), skipped)
	}
}
