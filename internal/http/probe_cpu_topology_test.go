package http

import (
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/zeptop-dev/captain/internal/config"
	"github.com/zeptop-dev/captain/internal/store"
)

// Exercise the actual agent JSON decoders rather than constructing HostInfo in
// Go: an older shared schema would otherwise silently drop the additive fields.
func TestProbeCPUTopologySurvivesIngestAndRestart(t *testing.T) {
	for _, endpoint := range []string{"report", "beat"} {
		for _, fixture := range []struct {
			name string
			info string
		}{
			{"physical", `{"cpu_model":"Example CPU","cpu_cores":32,"cpu_physical_cores":16,"cpu_sockets":2,"virtualization_role":"none","arch":"amd64"}`},
			{"hypervisor_host", `{"cpu_model":"Example CPU","cpu_cores":64,"cpu_physical_cores":32,"cpu_sockets":2,"virtualization_role":"host","virt":"kvm"}`},
			{"guest", `{"cpu_model":"Example virtual CPU","cpu_cores":8,"virtualization_role":"guest","virt":"kvm"}`},
			{"legacy_unknown", `{"cpu_model":"Example legacy CPU","cpu_cores":8,"virt":"kvm"}`},
		} {
			t.Run(endpoint+"/"+fixture.name, func(t *testing.T) {
				r := newRig(t)
				anon := &client{t: t, srv: r.srv}
				wantInfo := mustJSON[map[string]any](t, []byte(fixture.info))
				settings := func(system bool) {
					t.Helper()
					sections := []string{"cpu", "history"}
					if system {
						sections = append(sections, "system")
					}
					if code, b, _ := r.c.do("PUT", "/api/admin/settings/probe", map[string]any{"enabled": true, "page_enabled": true, "visibility": "public", "public_sections": sections}, nil); code != 200 {
						t.Fatalf("settings: %d %s", code, b)
					}
				}
				settings(true)
				host := map[string]any{
					"info": json.RawMessage(fixture.info), "cpu_percent": 42.5,
					"load1": 3.25, "load5": 2.5, "load15": 1.75,
					"valid":     map[string]bool{"cpu": true, "load": true},
					"resources": map[string]any{"epoch": "cpu-topology", "sequence": 1, "processes": []map[string]any{{"name": "private-process", "pid": 42}}},
				}
				wantStatus := 200
				if endpoint == "beat" {
					wantStatus = 204
				}
				if code, b, _ := r.agent.do("POST", "/api/agent/"+endpoint, map[string]any{"version": "0.75.0", "host": host}, nil); code != wantStatus {
					t.Fatalf("%s: %d %s", endpoint, code, b)
				}
				get := func(c *client, path string) []byte {
					t.Helper()
					code, b, _ := c.do("GET", path, nil, nil)
					if code != 200 {
						t.Fatalf("GET %s: %d %s", path, code, b)
					}
					return b
				}
				type nodeHost struct {
					ID   int64          `json:"id"`
					Host map[string]any `json:"host"`
				}
				checkHost := func(got map[string]any, system bool) {
					t.Helper()
					if got["cpu_percent"] != 42.5 {
						t.Fatalf("CPU usage changed with topology: %v", got)
					}
					valid, _ := got["valid"].(map[string]any)
					if system {
						if !reflect.DeepEqual(got["info"], wantInfo) {
							t.Fatalf("CPU facts changed: got %v, want %v", got["info"], wantInfo)
						}
						for key, want := range map[string]float64{"load1": 3.25, "load5": 2.5, "load15": 1.75} {
							if got[key] != want {
								t.Fatalf("%s was normalized by CPU topology: got %v, want %v", key, got[key], want)
							}
						}
						if valid["load"] != true {
							t.Fatalf("valid load lost: %v", valid)
						}
					} else {
						if got["info"] != nil || valid["load"] != false {
							t.Fatalf("hidden system facts leaked: %v", got)
						}
						for _, key := range []string{"load1", "load5", "load15"} {
							if _, exposed := got[key]; exposed {
								t.Fatalf("hidden system metric %s leaked: %v", key, got)
							}
						}
					}
				}
				checkSnapshot := func(system bool) {
					t.Helper()
					doc := mustJSON[struct {
						Nodes []nodeHost `json:"nodes"`
					}](t, get(anon, "/api/probe"))
					if len(doc.Nodes) != 1 || doc.Nodes[0].ID != r.nodeID {
						t.Fatalf("unexpected public nodes: %+v", doc.Nodes)
					}
					checkHost(doc.Nodes[0].Host, system)
					if _, exposed := doc.Nodes[0].Host["resources"]; exposed {
						t.Fatal("public snapshot exposed private resource details")
					}
				}
				checkPrivate := func(live bool) {
					t.Helper()
					nodes := mustJSON[[]nodeHost](t, get(r.c, "/api/admin/monitoring"))
					if len(nodes) != 1 || nodes[0].ID != r.nodeID {
						t.Fatalf("unexpected private nodes: %+v", nodes)
					}
					checkHost(nodes[0].Host, true)
					if live {
						doc := mustJSON[struct {
							Live nodeHost `json:"live"`
						}](t, get(r.c, "/api/admin/nodes/"+itoa(r.nodeID)+"/probe"))
						checkHost(doc.Live.Host, true)
					}
				}
				checkHistory := func(system bool) {
					t.Helper()
					doc := mustJSON[struct {
						Points []map[string]any `json:"points"`
					}](t, get(anon, "/api/probe/nodes/"+itoa(r.nodeID)+"/history?range=24h"))
					if endpoint == "report" {
						if len(doc.Points) != 0 {
							t.Fatalf("minute reports unexpectedly created probe history: %v", doc.Points)
						}
						return
					}
					if len(doc.Points) != 1 || doc.Points[0]["n"] != float64(1) || doc.Points[0]["cpu"] != 42.5 {
						t.Fatalf("probe usage history changed: %v", doc.Points)
					}
					wantLoad := float64(0)
					if system {
						wantLoad = 3.25
					}
					point := doc.Points[0]
					valid, _ := point["valid"].(map[string]any)
					if point["load1"] != wantLoad || valid["load"] != system {
						t.Fatalf("load history changed with topology or ignored privacy: %v", point)
					}
				}
				checkSnapshot(true)
				checkPrivate(endpoint == "beat")
				checkHistory(true)
				settings(false)
				checkSnapshot(false)
				checkHistory(false)
				checkPrivate(endpoint == "beat") // Public masking cannot mutate live or stored facts.
				settings(true)

				// A new Store and HTTP service have no live samples or snapshot cache;
				// all host facts must now come back through the persisted JSON columns.
				r.srv.Close()
				cfg := config.Default()
				cfg.BaseURL = "http://example.com"
				r.st = store.New(r.st.DB())
				r.srv = httptest.NewServer(New(cfg, r.st, slog.Default()).Handler())
				t.Cleanup(r.srv.Close)
				r.c.srv, anon.srv = r.srv, r.srv
				checkSnapshot(true)
				checkPrivate(false)
				checkHistory(true)
				settings(false)
				checkSnapshot(false)
				checkHistory(false)
				checkPrivate(false)
			})
		}
	}
}
