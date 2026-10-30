package store

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

func TestPathHealthPolicyPreserveAndProjection(t *testing.T) {
	old := &PathRoute{Health: &PathRouteHealth{Enabled: true, AutoFailover: true, ServerName: "example.com"}}
	next := &PathRoute{}
	PreservePathRouteHealth(next, old)
	if !next.Health.Enabled || next.Health == old.Health {
		t.Fatal("omitted policy lost or aliased")
	}
	next.Health.Enabled = false
	if !old.Health.Enabled {
		t.Fatal("old intent mutated")
	}
	PreservePathRouteHealth(next, old)
	if next.Health.Enabled {
		t.Fatal("explicit disable ignored")
	}
	p := &spec.PathHealth{RouteID: 7, ScopeID: "scope", Protocol: spec.PathHealthTLS, ServerName: "example.com", ALPN: []string{"h2"}, AutoFailover: true, IntervalSeconds: 15, TimeoutSeconds: 3, FailureThreshold: 3, RecoveryThreshold: 2, Candidates: []spec.PathHealthCandidate{{ID: "b", Target: "198.51.100.20:443", Digest: "digest"}}}
	projection := projectLinePathHealth(p)
	wire := projection.Wire()
	if !reflect.DeepEqual(p, wire) {
		t.Fatal("staged projection loses health config", p, wire)
	}
	wire.ALPN[0] = "http/1.1"
	wire.Candidates[0].Target = "203.0.113.30:443"
	if p.ALPN[0] != "h2" || projection.ALPN[0] != "h2" || p.Candidates[0].Target != projection.Candidates[0].Target {
		t.Fatal("projection aliases original")
	}
	if (*LineExpectedPathHealth)(nil).Wire() != nil {
		t.Fatal("nil policy became enabled")
	}
}

func TestPathHealthMutationScopeAndOmission(t *testing.T) {
	enabled := &PathRoute{Health: &PathRouteHealth{Enabled: true}}
	disabled := &PathRoute{Health: &PathRouteHealth{}}
	for _, tc := range []struct {
		name         string
		before, next *PathRoute
		changed      bool
	}{
		{"create default", nil, &PathRoute{}, false}, {"create enabled", nil, enabled, true},
		{"omit preserves", enabled, &PathRoute{}, false}, {"same", enabled, enabled, false},
		{"disable", enabled, disabled, true}, {"delete enabled", enabled, nil, true},
		{"delete default", disabled, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed, err := PathRouteHealthChanged(tc.before, tc.next)
			if err != nil || changed != tc.changed {
				t.Fatal(changed, err)
			}
		})
	}
	const pattern = "PUT /api/admin/path-routes/{id}/health"
	if TokenAllowsRequest(`full|["path-routes:write","line-rollouts:write"]`, "PUT", "/api/admin/path-routes/1/health", pattern) {
		t.Fatal("route scope bypasses health scope")
	}
	if !TokenAllowsRequest(`full|["path-health:write"]`, "PUT", "/api/admin/path-routes/1/health", pattern) {
		t.Fatal("health grant rejected")
	}
	if TokenAllowsRequest(`full|["path-health:read"]`, "PUT", "/api/admin/path-routes/1/health", pattern) {
		t.Fatal("read health can mutate")
	}
}

func TestPathHealthCapabilityRequiresVersionEvenWithFreshFlag(t *testing.T) {
	s, ns, _, _ := pathFixture(t)
	at := time.Now().UTC()
	pathHealthCapabilityFixture(t, s, ns[0].ID, at)
	if _, err := s.db.Exec(`UPDATE nodes SET version='v0.84.0' WHERE id=?`, ns[0].ID); err != nil {
		t.Fatal(err)
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	capable, err := pathHealthCapabilityTx(context.Background(), tx, ns[0].ID, at)
	if err != nil || capable {
		t.Fatal("old version advertised health capability", capable, err)
	}
}
