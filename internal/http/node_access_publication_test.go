package http

import (
	"net/url"
	"slices"
	"testing"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/service"
	"gopkg.in/yaml.v3"
)

// Exercise the public delivery boundaries, not just the store predicate: plan
// groups must yield the same subscription nodes and actual agent credentials.
func TestNodeAccessAuditPublicationAndRuntimeAgree(t *testing.T) {
	r := newRig(t)
	post := func(path string, input any) []byte {
		t.Helper()
		return accessAuditCall(t, r.c, "POST", path, input, 200)
	}
	group := func(name string) int64 {
		t.Helper()
		return mustJSON[struct{ ID int64 }](t, post("/api/admin/groups", map[string]string{"Name": name})).ID
	}
	standardGroup, premiumGroup := group("Standard"), group("Premium")
	ibs := mustJSON[struct {
		Inbounds []domain.Inbound `json:"inbounds"`
	}](t, accessAuditCall(t, r.c, "GET", "/api/admin/nodes/"+itoa(r.nodeID), nil, 200)).Inbounds
	if len(ibs) != 1 {
		t.Fatal("expected one original inbound")
	}
	ordinary := ibs[0]
	premium := mustJSON[domain.Inbound](t, post("/api/admin/nodes/"+itoa(r.nodeID)+"/inbounds", map[string]any{
		"Tag": "premium", "Protocol": "vless", "Port": 8443,
	}))
	entry := func(name, host string, ib domain.Inbound) domain.Entry {
		t.Helper()
		return mustJSON[domain.Entry](t, post("/api/admin/entries", map[string]any{
			"Name": name, "InboundID": ib.ID, "DisplayHost": host, "DisplayPort": ib.Port,
		}))
	}
	ordinaryEntry := entry("Ordinary node", "ordinary.example.com", ordinary)
	premiumEntry := entry("Premium node", "premium.example.com", premium)
	entries := map[string]domain.Entry{ordinaryEntry.Name: ordinaryEntry, premiumEntry.Name: premiumEntry}
	inboundTags := map[string]string{ordinaryEntry.Name: ordinary.Tag, premiumEntry.Name: premium.Tag}
	type customer struct {
		ID       int64  `json:"id"`
		UUID     string `json:"uuid"`
		SubToken string `json:"sub_token"`
		portal   *client
	}
	createCustomer := func(name string, groupID int64) customer {
		t.Helper()
		plan := mustJSON[domain.Plan](t, post("/api/admin/plans", map[string]any{
			"Name": name, "PriceCents": 1, "PeriodDays": 30, "GroupID": groupID,
		}))
		email := name + "@example.com"
		u := mustJSON[customer](t, post("/api/admin/users", map[string]string{"Email": email, "Password": "password123"}))
		if u.ID == 0 || u.UUID == "" || u.SubToken == "" {
			t.Fatal("customer API omitted its subscription identity")
		}
		post("/api/admin/users/"+itoa(u.ID)+"/grant", map[string]int64{"PlanID": plan.ID})
		u.portal = &client{t: t, srv: r.srv}
		accessAuditCall(t, u.portal, "POST", "/api/portal/login", map[string]string{"Email": email, "Password": "password123"}, 200)
		return u
	}
	standardUser, premiumUser := createCustomer("standard", standardGroup), createCustomer("premium", premiumGroup)
	setPolicy := func(id int64, policy string, groups []int64) {
		t.Helper()
		accessAuditCall(t, r.c, "PUT", "/api/admin/node-access/inbounds/"+itoa(id),
			accessAuditPolicy(id, accessAuditGet(t, r).Revision, policy, groups, true), 200)
	}
	setPolicy(ordinary.ID, "groups", []int64{standardGroup, premiumGroup})
	setPolicy(premium.ID, "groups", []int64{premiumGroup})
	assertNames := func(surface string, got, want []string) {
		t.Helper()
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Fatalf("%s visible nodes: got %v, want %v", surface, got, want)
		}
	}
	uuidList := func(users []spec.User) []string {
		out := make([]string, 0, len(users))
		for _, u := range users {
			out = append(out, u.UUID)
		}
		slices.Sort(out)
		return out
	}
	check := func(wantStandard, wantPremium []string) string {
		t.Helper()
		state := mustJSON[agentproto.State](t, accessAuditCall(t, r.agent, "GET", "/api/agent/state", nil, 200))
		if len(state.Node.Inbounds) != 2 {
			t.Fatal("enabled listener disappeared instead of retaining a scoped credential list")
		}
		byTag := make(map[string]spec.Inbound)
		for _, ib := range state.Node.Inbounds {
			if !ib.ScopedUsers {
				t.Fatalf("inbound %s fell back to node-wide authentication", ib.Tag)
			}
			byTag[ib.Tag] = ib
		}
		wantRuntime := map[string][]string{ordinary.Tag: {}, premium.Tag: {}}
		wantGlobal := []string{}
		for _, c := range []struct {
			user customer
			want []string
		}{{standardUser, wantStandard}, {premiumUser, wantPremium}} {
			if len(c.want) > 0 {
				wantGlobal = append(wantGlobal, c.user.UUID)
			}
			for _, name := range c.want {
				wantRuntime[inboundTags[name]] = append(wantRuntime[inboundTags[name]], c.user.UUID)
			}
			var doc struct {
				Proxies []struct {
					Name, Server, UUID string
					Port               int
				} `yaml:"proxies"`
			}
			// A subscription token works without a portal cookie and must still
			// filter every proxy through the customer's current plan permissions.
			visitor := &client{t: t, srv: r.srv}
			raw := accessAuditCall(t, visitor, "GET", "/sub/"+c.user.SubToken+"?client=clash", nil, 200)
			if err := yaml.Unmarshal(raw, &doc); err != nil {
				t.Fatal("subscription did not render valid YAML", err)
			}
			names := []string{}
			for _, proxy := range doc.Proxies {
				e, exists := entries[proxy.Name]
				if !exists || proxy.Server != e.DisplayHost || proxy.Port != e.DisplayPort || proxy.UUID != c.user.UUID || !slices.Contains(uuidList(byTag[inboundTags[proxy.Name]].Users), proxy.UUID) {
					t.Fatal("subscription endpoint or credential differs from provisioned agent access")
				}
				names = append(names, proxy.Name)
			}
			assertNames("subscription", names, c.want)
			servers := mustJSON[[]struct {
				Name, Host, URI string
				Port            int
			}](t, accessAuditCall(t, c.user.portal, "GET", "/api/portal/servers", nil, 200))
			names = []string{}
			for _, server := range servers {
				e, exists := entries[server.Name]
				uri, err := url.Parse(server.URI)
				if !exists || server.Host != e.DisplayHost || server.Port != e.DisplayPort || err != nil || uri.User == nil || uri.User.Username() != c.user.UUID {
					t.Fatal("portal returned a different node or subscriber credential")
				}
				names = append(names, server.Name)
			}
			assertNames("portal", names, c.want)
			status := mustJSON[service.ServiceStatusView](t, accessAuditCall(t, c.user.portal, "GET", "/api/portal/service-status", nil, 200))
			if status.Summary.Total != len(c.want) {
				t.Fatal("service status total includes inaccessible nodes")
			}
			names = []string{}
			for _, e := range status.Entries {
				if e.ID != entries[e.Name].ID {
					t.Fatal("service status returned a different entry identity")
				}
				names = append(names, e.Name)
			}
			assertNames("service status", names, c.want)
		}
		for tag, want := range wantRuntime {
			slices.Sort(want)
			ib, exists := byTag[tag]
			if !exists || !slices.Equal(uuidList(ib.Users), want) || !slices.Equal(uuidList(ib.EffectiveUsers(state.Users)), want) {
				t.Fatalf("inbound %s has credentials inconsistent with subscription permissions", tag)
			}
		}
		slices.Sort(wantGlobal)
		if !slices.Equal(uuidList(state.Users), wantGlobal) {
			t.Fatal("node-level fallback retained an unauthorized customer's credentials")
		}
		return state.Revision
	}
	first := check([]string{ordinaryEntry.Name}, []string{ordinaryEntry.Name, premiumEntry.Name})
	setPolicy(ordinary.ID, "none", nil)
	second := check(nil, []string{premiumEntry.Name})
	if first == second {
		t.Fatal("closing the shared inbound did not invalidate agent state")
	}
	setPolicy(premium.ID, "none", nil)
	if check(nil, nil) == second {
		t.Fatal("closing the final inbound did not invalidate agent state")
	}
}
