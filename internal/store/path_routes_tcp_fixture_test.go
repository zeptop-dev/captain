package store

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"testing"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

// The opt-in cross-repository test feeds these actual compiler outputs into
// bosun's real TCP relay. It never modifies an agent or production server.
func TestExportCompiledPathRouteTCPFixture(t *testing.T) {
	filename := os.Getenv("CAPTAIN_PATH_ROUTE_FIXTURE")
	if filename == "" {
		t.Skip("set CAPTAIN_PATH_ROUTE_FIXTURE for the paired bosun TCP integration test")
	}
	ctx := context.Background()
	s, ns, ib, r := pathFixture(t)
	var ports []int
	for range 5 {
		ln, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { _ = ln.Close() })
		ports = append(ports, ln.Addr().(*net.TCPAddr).Port)
	}
	for _, n := range ns {
		n.PublicAddr = "127.0.0.1"
		if e := s.UpdateNode(ctx, n); e != nil {
			t.Fatal(e)
		}
	}
	// The actual compiler must own the loopback bindings. The reader consumes
	// the emitted rules unchanged instead of rewriting wildcard listeners.
	ingresses := map[int64]int64{}
	for _, node := range ns[:3] {
		ingress := &Ingress{NodeID: node.ID, Name: "Loopback fixture", BindIP: "127.0.0.1", LineIP: "127.0.0.1", EntryHost: "127.0.0.1"}
		if e := s.CreateIngress(ctx, ingress); e != nil {
			t.Fatal(e)
		}
		ingresses[node.ID] = ingress.ID
	}
	entryIngress := ingresses[r.Entry.NodeID]
	r.Entry.IngressID = &entryIngress
	for i := range r.Branches {
		branch := &r.Branches[i]
		path, e := s.ForwardPath(ctx, branch.PathID, branch.PathRevision)
		if e != nil {
			t.Fatal(e)
		}
		for j := range path.Steps {
			id := ingresses[path.Steps[j].NodeID]
			path.Steps[j].IngressID = &id
		}
		if e = s.SaveForwardPath(ctx, path); e != nil {
			t.Fatal(e)
		}
		branch.PathRevision = path.Version
	}
	ib.Port = ports[4]
	if e := s.UpdateInbound(ctx, ib); e != nil {
		t.Fatal(e)
	}
	r.Entry.Port = ports[0]
	r.Branches[0].Ports = []int{ports[1], ports[2]}
	r.Branches[1].Ports = []int{ports[3]}
	r.Branches[0].Weight = 1
	if e := s.SavePathRoute(ctx, r); e != nil {
		t.Fatal(e)
	}
	fixture := struct {
		EntryPort, LandingPort int
		Nodes                  [][]spec.Forward
		BranchPorts            [][]int
	}{ports[0], ports[4], nil, [][]int{{ports[1], ports[2]}, {ports[3]}}}
	for _, n := range ns[:3] {
		var fs []spec.Forward
		for _, f := range pathOwned(t, s, n.ID, r.ID) {
			if f.Listen != "127.0.0.1" {
				t.Fatalf("fixture must bind loopback: %+v", f)
			}
			fs = append(fs, f.Forward)
		}
		fixture.Nodes = append(fixture.Nodes, fs)
	}
	raw, e := json.MarshalIndent(fixture, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filename, raw, 0600); e != nil {
		t.Fatal(e)
	}
}
