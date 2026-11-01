package store

import (
	"context"
	"reflect"
	"testing"

	"github.com/zeptop-dev/bosun/pkg/configrevision"
	"github.com/zeptop-dev/bosun/pkg/spec"
)

// Fixed IDs make these outputs comparable to Captain v1.52's released compiler.
func TestPathTransportLegacyGolden(t *testing.T) {
	ctx := context.Background()
	s, ns, ib, _ := lineFixture(t)
	b := &ForwardPath{Name: "B", Steps: []PathStep{{ID: "one", NodeID: ns[1].ID}, {ID: "two", NodeID: ns[2].ID}}}
	c := &ForwardPath{Name: "C", Steps: []PathStep{{ID: "shared", NodeID: ns[2].ID}}}
	for _, p := range []*ForwardPath{b, c} {
		if err := s.SaveForwardPath(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	r := &PathRoute{Name: "Stable route", Entry: LineMember{ID: "entry", NodeID: ns[0].ID, Port: 22000}, TargetInboundID: ib.ID, Strategy: "roundrobin", Branches: []PathBranch{{ID: "branchb", PathID: b.ID, PathRevision: 1, Weight: 3, Ports: []int{22001, 22002}}, {ID: "branchc", PathID: c.ID, PathRevision: 1, Weight: 1, Ports: []int{22003}}}}
	if err := s.SavePathRoute(ctx, r); err != nil {
		t.Fatal(err)
	}
	golden := []struct {
		digest string
		tags   []string
	}{
		{"cfg1-db0e019170da53601f395396f55d24e1bd0265ee75b38d96f7bee0f98bfed735", []string{"line-1-entry-35dc392553cc"}},
		{"cfg1-334296d4168b47926a947c7a77968e38b424a5dd81a5a417937c168192f7f5a1", []string{"line-1-a2a97567ccb3f8ed-6a662b54af80"}},
		{"cfg1-75a1c64e89f7196b2df14d099e8fbfb4ccc2bf1e857807486033350fa41164e3", []string{"line-1-ca6044fff187211d-0e40b930165e", "line-1-0875330163373f12-8253e7058b40"}},
	}
	// Captured by running this fixed fixture against release commit 2235439.
	before := map[int64][]NodeForward{}
	for i, node := range ns[:3] {
		fs := pathOwned(t, s, node.ID, r.ID)
		wire := make([]spec.Forward, 0, len(fs))
		for _, f := range fs {
			wire = append(wire, f.Forward)
		}
		digest, err := configrevision.Digest(spec.Node{}, wire)
		if err != nil {
			t.Fatal(err)
		}
		tags := []string{}
		for _, f := range wire {
			tags = append(tags, f.Tag)
			if f.Backend != "" {
				t.Fatal("legacy native wire backend changed")
			}
		}
		if digest != golden[i].digest || !reflect.DeepEqual(tags, golden[i].tags) {
			t.Fatalf("released node %d wire changed: %s %+v", node.ID, digest, tags)
		}
		before[node.ID] = fs
	}
	// Explicit relay is a reset, not a distinct runtime backend or configuration.
	r.EntryBackend = "relay"
	for i := range r.Branches {
		branch := &r.Branches[i]
		path, err := s.ForwardPath(ctx, branch.PathID, branch.PathRevision)
		if err != nil {
			t.Fatal(err)
		}
		for j := range path.Steps {
			path.Steps[j].Backend = "relay"
		}
		if err = s.SaveForwardPath(ctx, path); err != nil {
			t.Fatal(err)
		}
		branch.PathRevision = path.Version
	}
	if err := s.SavePathRoute(ctx, r); err != nil {
		t.Fatal(err)
	}
	for _, node := range ns[:3] {
		if !reflect.DeepEqual(before[node.ID], pathOwned(t, s, node.ID, r.ID)) {
			t.Fatal("explicit relay reset changed released wire")
		}
	}
}
