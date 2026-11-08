package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestIngressAddressSourceAndSubscriberBindingAPI(t *testing.T) {
	ctx := context.Background()
	h, _, admin := suggestionRig(t, "manual", "")
	n := &domain.Node{Name: "NAT", PublicAddr: "198.51.100.20"}
	if err := h.Store.CreateNode(ctx, n, "pair", time.Hour); err != nil {
		t.Fatal(err)
	}
	call := func(fn http.HandlerFunc, id int64, body any, want int) *httptest.ResponseRecorder {
		t.Helper()
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", "https://panel.example.com/api/admin/test", bytes.NewReader(raw))
		r.SetPathValue("id", strconv.FormatInt(id, 10))
		r = r.WithContext(context.WithValue(ctx, ctxKey{}, admin))
		w := httptest.NewRecorder()
		fn(w, r)
		if w.Code != want {
			t.Fatalf("want %d got %d: %s", want, w.Code, w.Body.String())
		}
		return w
	}
	body := map[string]any{"Name": "NAT", "Kind": "nat", "EntryAddressSource": "server_ipv4", "EntryHost": "203.0.113.30", "EntryDomain": "NAT.Example.Com.", "PortFrom": 443, "PortTo": 444, "PortOffset": 30000}
	w := call(h.createIngress, n.ID, body, 200)
	var result struct {
		Ingress store.Ingress `json:"ingress"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	g := result.Ingress
	if g.EntryHost != n.PublicAddr || g.EntryAddressSource != "server_ipv4" || g.EntryDomain != "nat.example.com" {
		t.Fatal(g)
	}
	delete(body, "EntryAddressSource")
	delete(body, "EntryHost")
	call(h.updateIngress, g.ID, body, 200)
	saved, _ := h.Store.IngressByID(ctx, g.ID)
	if saved.EntryAddressSource != "server_ipv4" || saved.EntryHost != n.PublicAddr {
		t.Fatal("legacy editor erased source", saved)
	}

	body["EntryHost"] = "192.0.2.10"
	call(h.updateIngress, g.ID, body, 200)
	saved, _ = h.Store.IngressByID(ctx, g.ID)
	if saved.EntryAddressSource != "custom" || saved.EntryHost != "192.0.2.10" {
		t.Fatal("legacy manual override ignored", saved)
	}
	body["EntryAddressSource"] = "server_ipv4"
	call(h.updateIngress, g.ID, body, 200)
	body["EntryAddressSource"] = "server_ipv6"
	call(h.updateIngress, g.ID, body, 400)
	body["EntryAddressSource"] = "geolocation"
	call(h.updateIngress, g.ID, body, 400)
	ib := &domain.Inbound{NodeID: n.ID, Tag: "in", Protocol: spec.VLESS, Port: 443, Enabled: true, IngressID: &g.ID}
	if err := h.Store.CreateInbound(ctx, ib); err != nil {
		t.Fatal(err)
	}
	w = call(h.createEntry, 0, map[string]any{"Name": "Inherited", "InboundID": ib.ID}, 200)
	var e domain.Entry
	_ = json.Unmarshal(w.Body.Bytes(), &e)
	if !e.FollowIngress || e.DisplayHost != "nat.example.com" || e.DisplayPort != 30443 {
		t.Fatal(e)
	}
	call(h.updateEntry, e.ID, map[string]any{"Name": "Rename only"}, 200)
	got, _ := h.Store.EntryByID(ctx, e.ID)
	if !got.FollowIngress {
		t.Fatal("partial patch lost binding")
	}
	call(h.updateEntry, e.ID, map[string]any{"DisplayHost": "override.example.com"}, 200)
	got, _ = h.Store.EntryByID(ctx, e.ID)
	if got.FollowIngress || got.DisplayHost != "override.example.com" {
		t.Fatal("manual override ignored", got)
	}
	call(h.updateEntry, e.ID, map[string]any{"FollowIngress": true}, 200)
	got, _ = h.Store.EntryByID(ctx, e.ID)
	if !got.FollowIngress || got.DisplayHost != "nat.example.com" {
		t.Fatal("explicit adoption failed", got)
	}
	w = call(h.createEntry, 0, map[string]any{"Name": "Fixed", "InboundID": ib.ID, "DisplayHost": "nat.example.com", "DisplayPort": 30443}, 200)
	var fixed domain.Entry
	_ = json.Unmarshal(w.Body.Bytes(), &fixed)
	if fixed.FollowIngress {
		t.Fatal("equal explicit address acquired binding")
	}
	body["EntryAddressSource"] = "custom"
	body["EntryHost"] = "203.0.113.30"
	body["EntryDomain"] = ""
	call(h.updateIngress, g.ID, body, 200)
	got, _ = h.Store.EntryByID(ctx, e.ID)
	fixedAfter, _ := h.Store.EntryByID(ctx, fixed.ID)
	if got.DisplayHost != "203.0.113.30" || fixedAfter.DisplayHost != fixed.DisplayHost {
		t.Fatal(got, fixedAfter)
	}
	w = call(h.createEntry, 0, map[string]any{"Name": "Fixed defaults", "InboundID": ib.ID, "FollowIngress": false}, 200)
	var noFollow domain.Entry
	_ = json.Unmarshal(w.Body.Bytes(), &noFollow)
	if noFollow.FollowIngress {
		t.Fatal("explicit false ignored")
	}
	body["EntryAddressSource"] = "server_ipv4"
	call(h.updateIngress, g.ID, body, 200)
	n.PublicAddr = ""
	err := h.Store.UpdateNode(ctx, n)
	if err == nil || !strings.Contains(err.Error(), "ipv4") {
		t.Fatal(err)
	}
}
