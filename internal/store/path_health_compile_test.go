package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
)

func pathHealthCapabilityFixture(t *testing.T, s *Store, id int64, at time.Time) {
	t.Helper()
	if _, err := s.db.Exec(`UPDATE nodes SET token_hash=?,version='v0.85.0' WHERE id=?`, configTestToken, id); err != nil {
		t.Fatal(err)
	}
	d := configDelivery(t, s, id, configSessionA, "base-config", "base-state", at)
	r := configReceipt(d, "base-config", "base-state", 1, at)
	if _, err := s.RecordNodeReport(context.Background(), id, configTestToken, agentproto.Report{Version: "v0.85.0", ConfigReceipt: r, AppliedRevision: r.StateRevision, PathHealth: &agentproto.PathHealthReport{Supported: true}}, false, at); err != nil {
		t.Fatal(err)
	}
}

func TestPathHealthCompileCapabilityWholeBranchIdentityAndPreserve(t *testing.T) {
	ctx := context.Background()
	s, ns, ib, route := pathFixture(t)
	route.Strategy = "failover"
	route.Health = &PathRouteHealth{Enabled: true, AutoFailover: true, ServerName: "example.com"}
	ib.Settings.TLS = &spec.TLS{Mode: spec.TLSStandard}
	if err := s.UpdateInbound(ctx, ib); err != nil {
		t.Fatal(err)
	}
	if err := s.SavePathRoute(ctx, route); !errors.Is(err, ErrPathHealthCapability) {
		t.Fatal("old entry accepted", err)
	}
	pathHealthCapabilityFixture(t, s, ns[0].ID, time.Now().UTC())
	preview, err := s.PreviewPathRoute(ctx, route)
	if err != nil {
		t.Fatal(err)
	}
	*route = preview.Draft
	route.PreviewRevision = preview.Revision
	if err = s.SavePathRoute(ctx, route); err != nil {
		t.Fatal(err)
	}
	entry := pathOwned(t, s, ns[0].ID, route.ID)[0]
	if entry.PathHealth == nil || entry.PathHealth.RouteID != route.ID || entry.PathHealth.ScopeID == "preview" || entry.PathHealth.Protocol != spec.PathHealthTLS || len(entry.PathHealth.Candidates) != 2 {
		t.Fatal("compiled health identity missing", entry.PathHealth)
	}
	for _, node := range ns[1:] {
		for _, f := range pathOwned(t, s, node.ID, route.ID) {
			if f.PathHealth != nil {
				t.Fatal("shadow probe rule created")
			}
		}
	}
	first := entry.PathHealth.Digest()
	// An older editor preserves the policy even after capability expiry.
	if _, err = s.db.Exec(`UPDATE path_health_capabilities SET observed_at=1,received_at=1 WHERE node_id=?`, ns[0].ID); err != nil {
		t.Fatal(err)
	}
	route.Health = nil
	route.Name = "Renamed"
	route.PreviewRevision = ""
	if err = s.SavePathRoute(ctx, route); err != nil {
		t.Fatal("same policy save needs fresh capability", err)
	}
	if got := pathOwned(t, s, ns[0].ID, route.ID)[0]; got.PathHealth.Digest() != first || got.Tag != entry.Tag {
		t.Fatal("rename changed runtime identity")
	}
	// Change the deepest branch's business port; first-hop TCP address stays.
	route.Branches[0].Ports[1] += 10
	if err = s.SavePathRoute(ctx, route); err != nil {
		t.Fatal(err)
	}
	changed := pathOwned(t, s, ns[0].ID, route.ID)[0]
	if changed.Target != entry.Target || changed.PathHealth.Candidates[0].Digest == entry.PathHealth.Candidates[0].Digest || changed.PathHealth.Candidates[1].Digest != entry.PathHealth.Candidates[1].Digest {
		t.Fatal("deep branch omitted from digest")
	}
	policy := *route.Health
	policy.TimeoutSeconds++
	route.Health = &policy
	if err = s.SavePathRoute(ctx, route); !errors.Is(err, ErrPathHealthCapability) {
		t.Fatal("changed policy uses stale capability", err)
	}
}

func TestPathHealthHandshakeSupportAndSNI(t *testing.T) {
	_, _, ib, _ := pathFixture(t)
	p := &PathRouteHealth{}
	ib.Protocol = spec.SOCKS
	if got, _, _ := pathHealthHandshake(p, ib); got != spec.PathHealthSOCKS5 {
		t.Fatal(got)
	}
	ib.Protocol = spec.HTTP
	if got, _, _ := pathHealthHandshake(p, ib); got != spec.PathHealthHTTP {
		t.Fatal(got)
	}
	ib.Settings.TLS = &spec.TLS{Mode: spec.TLSStandard}
	if got, _, _ := pathHealthHandshake(p, ib); got != spec.PathHealthUnsupported {
		t.Fatal("missing SNI invented", got)
	}
	p.ServerName = "example.com"
	if got, name, _ := pathHealthHandshake(p, ib); got != spec.PathHealthTLS || name != p.ServerName {
		t.Fatal(got, name)
	}
	ib.Settings.TLS.Mode = spec.TLSReality
	if got, _, _ := pathHealthHandshake(p, ib); got != spec.PathHealthUnsupported {
		t.Fatal("decoy proves business", got)
	}
}
