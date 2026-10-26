package http

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/auth"
)

// The reverse wizards predate explicit access policies. Their embedded input
// structs must distinguish omitted group_id from the legacy explicit-null clear.
func TestNodeAccessAuditReverseEditorsPreserveManagedAccess(t *testing.T) {
	for _, kind := range []string{"vless", "wireguard"} {
		t.Run(kind, func(t *testing.T) {
			r := newRig(t)
			ctx := context.Background()
			transit, err := r.st.NodeByID(ctx, r.nodeID)
			if err != nil {
				t.Fatal(err)
			}
			transit.PublicAddr = "198.51.100.20"
			if err := r.st.UpdateNode(ctx, transit); err != nil {
				t.Fatal(err)
			}
			exit := mustJSON[struct{ ID int64 }](t, accessAuditCall(t, r.c, "POST", "/api/admin/nodes", map[string]any{
				"Name": "Reverse access exit", "PublicAddr": "203.0.113.30",
			}, 200)).ID
			newGroup := func(name string) int64 {
				t.Helper()
				return mustJSON[struct{ ID int64 }](t, accessAuditCall(t, r.c, "POST", "/api/admin/groups", map[string]string{"Name": name}, 200)).ID
			}
			groupA, groupB := newGroup("Reverse access A"), newGroup("Reverse access B")
			input := map[string]any{
				"transit_id": r.nodeID, "name": "Reverse access original", "port": 18443,
				"tunnel_port": 51820, "server_name": "www.example.com", "enabled": true, "group_id": groupA,
			}
			path := "/api/admin/nodes/" + itoa(exit) + "/reverse-connections"
			if kind == "wireguard" {
				if err := r.st.ResetPairCode(ctx, exit, "access-wg-pair", time.Minute); err != nil {
					t.Fatal(err)
				}
				if _, err := r.st.RedeemPairCode(ctx, "access-wg-pair", auth.SHA256Hex("access-wg-exit-test-token"), "test", "v0.68.0", "linux/amd64"); err != nil {
					t.Fatal(err)
				}
				for _, id := range []int64{r.nodeID, exit} {
					cores := map[string]agentproto.CoreStatus{"xray": {Capabilities: &spec.CoreCapabilities{
						WireGuardReverse: true, PrivateAccess: true, Protocols: []spec.Protocol{spec.VLESS},
					}}}
					if err := r.st.TouchNode(ctx, id, "v0.68.0", "", spec.SystemStatus{}, cores, nil); err != nil {
						t.Fatal(err)
					}
					token := "access-wg-exit-test-token"
					if id == r.nodeID {
						token = r.agent.token
					}
					if err := r.st.SetWireGuardReport(ctx, id, auth.SHA256Hex(token), &agentproto.WireGuardReport{Available: true}); err != nil {
						t.Fatal(err)
					}
				}
				path = "/api/admin/settings/nodes/" + itoa(exit) + "/wireguard-connections"
				input["endpoint"], input["mtu"] = "198.51.100.20:51820", 1380
				accessAuditCall(t, r.c, "POST", path, input, 200)
			} else {
				accessAuditCall(t, r.c, "PUT", path, map[string]any{"expected": map[string]int64{}, "links": []any{input}}, 200)
			}
			load := func() map[string]any {
				t.Helper()
				rows := mustJSON[[]map[string]any](t, accessAuditCall(t, r.c, "GET", path, nil, 200))
				if len(rows) != 1 {
					t.Fatalf("expected one managed connection, got %d", len(rows))
				}
				return rows[0]
			}
			save := func(row map[string]any, want int) {
				t.Helper()
				if kind == "wireguard" {
					accessAuditCall(t, r.c, "PUT", path+"/"+row["id"].(string), row, want)
				} else {
					accessAuditCall(t, r.c, "PUT", path, map[string]any{
						"expected": map[string]any{row["id"].(string): row["version"]}, "links": []any{row},
					}, want)
				}
			}
			inboundID := int64(load()["user_inbound_id"].(float64))
			assertLegacyGroup := func(want *int64) {
				t.Helper()
				ib, err := r.st.InboundByID(ctx, inboundID)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(ib.GroupID, want) {
					t.Fatalf("legacy group changed: got=%v, want=%v", ib.GroupID, want)
				}
			}
			assertLegacyGroup(&groupA)

			// Old clients can still clear or change an unmanaged legacy policy,
			// but simply editing a name must not clear its original group.
			row := load()
			delete(row, "group_id")
			row["name"] = "Legacy omission retained"
			save(row, 200)
			assertLegacyGroup(&groupA)
			row = load()
			row["group_id"] = nil
			save(row, 200)
			assertLegacyGroup(nil)
			row = load()
			row["group_id"] = groupA
			save(row, 200)
			assertLegacyGroup(&groupA)

			catalog := accessAuditGet(t, r)
			accessAuditCall(t, r.c, "PUT", "/api/admin/node-access/inbounds/"+itoa(inboundID),
				accessAuditPolicy(inboundID, catalog.Revision, "groups", []int64{groupA, groupB}, true), 200)
			assertManaged := func() {
				t.Helper()
				access := accessAuditFind(t, accessAuditGet(t, r), inboundID)
				if access.Legacy || access.Policy != "groups" || !reflect.DeepEqual(access.GroupIDs, []int64{groupA, groupB}) {
					t.Fatalf("reverse editor changed explicit multi-group policy: %+v", access)
				}
				assertLegacyGroup(&groupA)
			}
			row = load()
			delete(row, "group_id")
			row["name"] = "Managed omission retained"
			save(row, 200)
			if load()["name"] != row["name"] {
				t.Fatal("omitted group_id prevented unrelated connection metadata edit")
			}
			assertManaged()
			// GET-edit-PUT remains valid when the old, unchanged group is present.
			row = load()
			row["name"] = "Managed unchanged group retained"
			save(row, 200)
			assertManaged()

			for _, attempt := range []struct {
				name  string
				value any
				want  int
			}{{"change", groupB, 409}, {"clear", nil, 409}, {"malformed", "invalid", 400}} {
				before := load()
				revision := accessAuditGet(t, r).Revision
				row := load()
				row["group_id"], row["name"] = attempt.value, "Must not commit"
				save(row, attempt.want)
				after := load()
				if after["version"] != before["version"] || after["name"] != before["name"] || accessAuditGet(t, r).Revision != revision {
					t.Fatalf("rejected %s partially committed metadata or policy", attempt.name)
				}
				assertManaged()
			}
		})
	}
}
