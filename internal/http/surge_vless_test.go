package http

import (
	"context"
	"strings"
	"testing"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestSurgeVLESSSubscriptionDelivery(t *testing.T) {
	r := newRig(t)
	_, path, _ := profileSetup(t, r)
	code, b, _ := r.c.do("POST", "/api/admin/nodes/"+itoa(r.nodeID)+"/inbounds", map[string]any{"Tag": "surge-vless", "Protocol": "vless", "Port": 8443, "TLS": spec.TLS{Mode: spec.TLSStandard, ServerName: "example.com"}}, nil)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	ibs, err := r.st.InboundsByNode(context.Background(), r.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	var inboundID int64
	for _, ib := range ibs {
		if ib.Tag == "surge-vless" {
			inboundID = ib.ID
		}
	}
	if inboundID == 0 {
		t.Fatal("missing VLESS inbound")
	}
	code, b, _ = r.c.do("POST", "/api/admin/entries", map[string]any{"Name": "VLESS candidate", "InboundID": inboundID, "DisplayHost": "proxy.example.com", "DisplayPort": 8443}, nil)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	tpl := "# shared-surge-template\n[Proxy]\n{{proxies}}\n[Proxy Group]\nALL = select, {{proxy_names}}"
	if err = r.st.SetSetting(context.Background(), store.SettingSubTemplates, store.SubTemplates{"surge": tpl}); err != nil {
		t.Fatal(err)
	}
	visitor := &client{t: t, srv: r.srv}
	for _, tc := range []struct {
		client, ua string
		want       bool
	}{
		{"surge-vless", "Mozilla/5.0", true}, {"surge", "Surge Mac/6.12.0", true},
		{"surge", "Surge/5.8", false}, {"surge", "", false}, {"surge-legacy", "Surge Mac/6.12.0", false},
	} {
		code, b, headers := visitor.do("GET", path+"?client="+tc.client, nil, map[string]string{"User-Agent": tc.ua})
		if code != 200 || strings.Contains(string(b), "= vless,") != tc.want || !strings.Contains(string(b), "shared-surge-template") {
			t.Fatalf("%+v: %d %s", tc, code, b)
		}
		if headers.Get("Cache-Control") != "no-store" {
			t.Fatal("UA-dependent subscription must not be cached")
		}
	}
	if code, _, _ := visitor.do("GET", "/sub/invalid?client=surge-vless", nil, nil); code != 404 {
		t.Fatal(code)
	}
}
