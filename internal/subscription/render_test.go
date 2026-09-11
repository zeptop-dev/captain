package subscription

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"gitlab.com/boyang-hu/bosun/pkg/spec"
)

func sample() []Line {
	reality := &spec.TLS{Mode: spec.TLSReality, ServerName: "www.apple.com", Reality: &spec.Reality{PublicKey: "PUB", ShortIDs: []string{"0123"}}}
	tls := &spec.TLS{Mode: spec.TLSStandard, ServerName: "node1.test"}
	uuid := "11111111-1111-1111-1111-111111111111"
	mk := func(name string, ib spec.Inbound) Line {
		return Line{Name: name, Host: "entry.test", Port: 443, Inbound: ib, UUID: uuid, Password: uuid}
	}
	return []Line{
		mk("reality", spec.Inbound{Protocol: spec.VLESS, Flow: "xtls-rprx-vision", TLS: reality}),
		mk("vmess-ws", spec.Inbound{Protocol: spec.VMess, TLS: tls, Transport: &spec.Transport{Type: "ws", Path: "/ws", Host: "cdn.test"}}),
		mk("trojan-grpc", spec.Inbound{Protocol: spec.Trojan, TLS: tls, Transport: &spec.Transport{Type: "grpc", ServiceName: "svc"}}),
		mk("ss2022", spec.Inbound{Protocol: spec.Shadowsocks, Cipher: "2022-blake3-aes-128-gcm", ServerKey: "c2VydmVya2V5c2VydmVya2V5"}),
		mk("hy2", spec.Inbound{Protocol: spec.Hysteria2, TLS: tls, Obfs: "salamander", ObfsPassword: "obfs", UpMbps: 100, DownMbps: 500}),
		mk("tuic", spec.Inbound{Protocol: spec.TUIC, TLS: tls, CongestionControl: "bbr"}),
		mk("anytls", spec.Inbound{Protocol: spec.AnyTLS, TLS: tls}),
		mk("mieru", spec.Inbound{Protocol: spec.Mieru, MieruTransport: "TCP"}),
		mk("xhttp", spec.Inbound{Protocol: spec.VLESS, TLS: tls, Transport: &spec.Transport{Type: "xhttp", Path: "/x", Mode: "auto"}}),
	}
}

func TestClash(t *testing.T) {
	out, err := Clash{}.Render(sample(), Account{})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(out, &doc); err != nil {
		t.Fatalf("yaml: %v\n%s", err, out)
	}
	proxies := doc["proxies"].([]any)
	if len(proxies) != 9 {
		t.Fatalf("expected 9 proxies, got %d", len(proxies))
	}
	p0 := proxies[0].(map[string]any)
	if p0["type"] != "vless" || p0["flow"] != "xtls-rprx-vision" || p0["reality-opts"].(map[string]any)["public-key"] != "PUB" || p0["servername"] != "www.apple.com" {
		t.Fatalf("reality proxy: %v", p0)
	}
	ss := proxies[3].(map[string]any)
	if !strings.HasPrefix(ss["password"].(string), "c2VydmVya2V5c2VydmVya2V5:") {
		t.Fatalf("ss2022 password: %v", ss["password"])
	}
	if m := proxies[7].(map[string]any); m["type"] != "mieru" || m["username"] != m["password"] || m["transport"] != "TCP" {
		t.Fatalf("mieru: %v", m)
	}
	if x := proxies[8].(map[string]any); x["network"] != "xhttp" {
		t.Fatalf("xhttp: %v", x)
	}
	groups := doc["proxy-groups"].([]any)
	if len(groups[0].(map[string]any)["proxies"].([]any)) != 10 {
		t.Fatalf("select group should list AUTO + 9")
	}
}

func TestSingBox(t *testing.T) {
	out, err := SingBox{}.Render(sample(), Account{})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("json: %v", err)
	}
	outs := doc["outbounds"].([]any)
	// 2 groups + 7 servers (mieru and xhttp skipped) + direct
	if len(outs) != 10 {
		t.Fatalf("outbounds: %d", len(outs))
	}
	r := outs[2].(map[string]any)
	if r["type"] != "vless" || r["tls"].(map[string]any)["reality"].(map[string]any)["public_key"] != "PUB" || r["flow"] != "xtls-rprx-vision" {
		t.Fatalf("reality: %v", r)
	}
	hy := outs[6].(map[string]any)
	if hy["type"] != "hysteria2" || hy["obfs"].(map[string]any)["password"] != "obfs" || hy["down_mbps"] != float64(500) {
		t.Fatalf("hy2: %v", hy)
	}
}

func TestURIList(t *testing.T) {
	out, err := URIList{}.Render(sample(), Account{})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(string(out))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(raw), "\n")
	if len(lines) != 9 {
		t.Fatalf("expected 9 links, got %d:\n%s", len(lines), raw)
	}
	if !strings.HasPrefix(lines[0], "vless://11111111-1111-1111-1111-111111111111@entry.test:443?") || !strings.Contains(lines[0], "security=reality") || !strings.Contains(lines[0], "pbk=PUB") || !strings.Contains(lines[0], "flow=xtls-rprx-vision") {
		t.Fatalf("vless: %s", lines[0])
	}
	if !strings.HasPrefix(lines[1], "vmess://") {
		t.Fatalf("vmess: %s", lines[1])
	}
	if !strings.HasPrefix(lines[3], "ss://") || !strings.HasPrefix(lines[4], "hysteria2://") || !strings.HasPrefix(lines[5], "tuic://") || !strings.HasPrefix(lines[7], "mierus://") {
		t.Fatalf("links: %v", lines)
	}
}

func TestSurgeAndPick(t *testing.T) {
	out, _ := Surge{}.Render(sample(), Account{})
	s := string(out)
	for _, want := range []string{"vmess-ws = vmess, entry.test, 443", "hy2 = hysteria2, entry.test, 443, password=", "tuic = tuic, entry.test, 443, uuid="} {
		if !strings.Contains(s, want) {
			t.Fatalf("surge missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "reality") || strings.Contains(s, "ss2022") || strings.Contains(s, "mieru") {
		t.Fatalf("surge must skip unsupported lines:\n%s", s)
	}
	if Pick("", "ClashMetaForAndroid/2.9").Name() != "clash" || Pick("", "SFA/1.10 (sing-box 1.10)").Name() != "singbox" ||
		Pick("", "Surge/5").Name() != "surge" || Pick("", "v2rayN/6").Name() != "uri" || Pick("shadowrocket", "Mozilla").Name() != "uri" || Pick("mihomo", "").Name() != "clash" {
		t.Fatal("pick")
	}
}
