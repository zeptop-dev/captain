package http

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/store"
)

func routingWithRepeatedMembers() store.NodeRouting {
	return store.NodeRouting{
		Outbounds: []spec.Outbound{
			{Tag: "exit-a", Protocol: "direct"},
			{Tag: "exit-b", Protocol: "direct"},
			{Tag: "pool", Balancer: &spec.Balancer{Strategy: "random", Members: []string{"exit-a", "exit-b", "exit-a"}}},
		},
		Routes:          []spec.RouteRule{{Match: []string{"domain:before.example.com"}, Action: "direct"}},
		DefaultOutbound: "pool",
		DNS:             []string{"192.0.2.53"},
	}
}

func TestRoutingRepeatedMembersRoundTripAndUnrelatedEdits(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	legacy := routingWithRepeatedMembers()
	// Seed an already saved configuration from before the candidate-core update.
	if err := r.st.SetNodeRouting(ctx, r.nodeID, &legacy); err != nil {
		t.Fatal(err)
	}
	path := "/api/admin/nodes/" + itoa(r.nodeID) + "/routing"
	code, body, _ := r.c.do("GET", path, nil, nil)
	if code != 200 {
		t.Fatalf("read historical routing: %d %s", code, body)
	}
	got := mustJSON[store.NodeRouting](t, body)
	if !reflect.DeepEqual(got, legacy) {
		t.Fatalf("read changed historical routing: %+v", got)
	}
	got.DNS = []string{"192.0.2.54"}
	code, body, _ = r.c.do("PUT", path, got, nil)
	if code != 200 {
		t.Fatalf("DNS-only edit rejected historical members: %d %s", code, body)
	}
	want := legacy
	want.DNS = got.DNS
	if saved := mustJSON[store.NodeRouting](t, body); !reflect.DeepEqual(saved, want) {
		t.Fatalf("save changed member order, multiplicity, or unrelated routing: %+v", saved)
	}
	after, err := r.st.NodeRouting(ctx, r.nodeID)
	if err != nil || !reflect.DeepEqual(*after, want) {
		t.Fatalf("unexpected saved routing: %+v %v", after, err)
	}
	want.Routes = append(want.Routes, spec.RouteRule{Match: []string{"domain:after.example.com"}, Action: "block"})
	code, body, _ = r.c.do("PUT", path, want, nil)
	if code != 200 {
		t.Fatalf("unrelated route edit rejected historical members: %d %s", code, body)
	}
	if saved := mustJSON[store.NodeRouting](t, body); !reflect.DeepEqual(saved, want) {
		t.Fatalf("route edit changed member order, multiplicity, or unrelated routing: %+v", saved)
	}
	after, err = r.st.NodeRouting(ctx, r.nodeID)
	if err != nil || !reflect.DeepEqual(*after, want) {
		t.Fatalf("unexpected saved route addition: %+v %v", after, err)
	}
}

func TestRoutingRepeatedMembersDoNotBypassReferenceValidation(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	before := routingWithRepeatedMembers()
	if err := r.st.SetNodeRouting(ctx, r.nodeID, &before); err != nil {
		t.Fatal(err)
	}
	path := "/api/admin/nodes/" + itoa(r.nodeID) + "/routing"
	for _, invalid := range []string{"unknown-member", "duplicate-outbound-tag", "reference-loop", "unknown-default"} {
		t.Run(invalid, func(t *testing.T) {
			next := routingWithRepeatedMembers()
			switch invalid {
			case "unknown-member":
				next.Outbounds[2].Balancer.Members = []string{"exit-a", "missing", "exit-a"}
			case "duplicate-outbound-tag":
				next.Outbounds = append(next.Outbounds, spec.Outbound{Tag: "exit-a", Protocol: "direct"})
			case "reference-loop":
				next.Outbounds[0].ProxyTag = "pool"
			case "unknown-default":
				next.DefaultOutbound = "missing"
			}
			if code, body, _ := r.c.do("PUT", path, next, nil); code != 400 {
				t.Fatalf("invalid references accepted: %d %s", code, body)
			}
			after, err := r.st.NodeRouting(ctx, r.nodeID)
			if err != nil || !reflect.DeepEqual(*after, before) {
				t.Fatalf("rejected save changed historical routing: %+v %v", after, err)
			}
		})
	}
}

func TestRoutingPresetPreviewPreservesRepeatedMembers(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	current := routingWithRepeatedMembers()
	if err := r.st.SetNodeRouting(ctx, r.nodeID, &current); err != nil {
		t.Fatal(err)
	}
	route := spec.RouteRule{Match: []string{"domain:after.example.com"}, Action: "direct"}
	for _, kind := range []string{"routes", "outbounds", "repeated-members-preset"} {
		t.Run(kind, func(t *testing.T) {
			want := routingWithRepeatedMembers()
			preset := spec.ConfigPreset{Name: kind, Kind: kind}
			switch kind {
			case "routes":
				preset.Payload, _ = json.Marshal([]spec.RouteRule{route})
				want.Routes = append(want.Routes, route)
			case "outbounds":
				extra := spec.Outbound{Tag: "pool-extra", Balancer: &spec.Balancer{Members: []string{"exit-a"}}}
				preset.Payload, _ = json.Marshal([]spec.Outbound{extra})
				want.Outbounds = append(want.Outbounds, extra)
			case "repeated-members-preset":
				extra := spec.Outbound{Tag: "pool-two", Balancer: &spec.Balancer{Members: []string{"exit-b", "exit-a", "exit-b"}}}
				preset.Kind = "outbounds"
				preset.Payload, _ = json.Marshal([]spec.Outbound{extra})
				want.Outbounds = append(want.Outbounds, extra)
			}
			code, body, _ := r.c.do("POST", "/api/admin/config-presets", preset, nil)
			if code != 200 {
				t.Fatalf("save compatible preset: %d %s", code, body)
			}
			saved := mustJSON[spec.ConfigPreset](t, body)
			code, body, _ = r.c.do("POST", "/api/admin/config-presets/preview", map[string]any{"id": saved.ID, "current": current}, nil)
			if code != 200 {
				t.Fatalf("preview with historical members: %d %s", code, body)
			}
			preview := mustJSON[spec.PresetPreview](t, body)
			draft := mustJSON[store.NodeRouting](t, preview.Draft)
			if !reflect.DeepEqual(draft, want) {
				t.Fatalf("preview changed existing members or routing: %+v", draft)
			}
			after, err := r.st.NodeRouting(ctx, r.nodeID)
			if err != nil || !reflect.DeepEqual(*after, current) {
				t.Fatalf("preview modified live routing: %+v %v", after, err)
			}
		})
	}
}
