package store

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

// Export actual compiler output for the paired bosun native relay test. Two
// unequal branches share a physical server but have distinct listener identities.
func TestExportCompiledPathRouteTransportFixtures(t *testing.T) {
	dir := os.Getenv("CAPTAIN_PATH_TRANSPORT_FIXTURES")
	if dir == "" {
		t.Skip("set CAPTAIN_PATH_TRANSPORT_FIXTURES for paired bosun UDP/both integration")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, protocol := range []string{"udp", "both"} {
		t.Run(protocol, func(t *testing.T) {
			ctx := context.Background()
			s, ns, ib, r := pathTransportFixture(t, protocol, "relay", []string{"relay"})
			ports := []int{}
			for range 5 {
				// Reserve both transports while compiling; the consumer rebinds only after
				// this exporter exits. No production port or service is involved.
				tcp, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { tcp.Close() })
				port := tcp.Addr().(*net.TCPAddr).Port
				udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: port})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { udp.Close() })
				ports = append(ports, port)
			}
			for _, n := range ns {
				n.PublicAddr = "127.0.0.1"
				if err := s.UpdateNode(ctx, n); err != nil {
					t.Fatal(err)
				}
			}
			// Explicit local ingress is part of the compiled intent. The reader
			// must not rewrite Listen after compilation or bind all interfaces.
			ingresses := map[int64]int64{}
			for _, node := range ns[:3] {
				ingress := &Ingress{NodeID: node.ID, Name: "Loopback fixture", BindIP: "127.0.0.1", LineIP: "127.0.0.1", EntryHost: "127.0.0.1"}
				if err := s.CreateIngress(ctx, ingress); err != nil {
					t.Fatal(err)
				}
				ingresses[node.ID] = ingress.ID
			}
			entryIngress := ingresses[r.Entry.NodeID]
			r.Entry.IngressID = &entryIngress
			for i := range r.Branches {
				branch := &r.Branches[i]
				path, err := s.ForwardPath(ctx, branch.PathID, branch.PathRevision)
				if err != nil {
					t.Fatal(err)
				}
				for j := range path.Steps {
					id := ingresses[path.Steps[j].NodeID]
					path.Steps[j].IngressID = &id
				}
				if err = s.SaveForwardPath(ctx, path); err != nil {
					t.Fatal(err)
				}
				branch.PathRevision = path.Version
			}
			ib.Port = ports[4]
			if err := s.UpdateInbound(ctx, ib); err != nil {
				t.Fatal(err)
			}
			r.Entry.Port = ports[0]
			r.Branches[0].Ports = []int{ports[1], ports[2]}
			r.Branches[1].Ports = []int{ports[3]}
			r.Branches[0].Weight = 1
			if err := s.SavePathRoute(ctx, r); err != nil {
				t.Fatal(err)
			}
			fixture := struct {
				Protocol               string
				EntryPort, LandingPort int
				Nodes                  [][]spec.Forward
				BranchPorts            [][]int
			}{protocol, ports[0], ports[4], nil, [][]int{{ports[1], ports[2]}, {ports[3]}}}
			for _, n := range ns[:3] {
				fs := []spec.Forward{}
				for _, f := range pathOwned(t, s, n.ID, r.ID) {
					if f.Listen != "127.0.0.1" {
						t.Fatalf("fixture must bind loopback: %+v", f)
					}
					fs = append(fs, f.Forward)
				}
				fixture.Nodes = append(fixture.Nodes, fs)
			}
			raw, err := json.MarshalIndent(fixture, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(dir, protocol+".json"), raw, 0600); err != nil {
				t.Fatal(err)
			}
		})
	}
}
