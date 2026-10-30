package store

import "github.com/zeptop-dev/bosun/pkg/spec"

type PathRouteHealth = spec.PathHealthPolicy

// PathRouteHealthChanged compares effective intent, including deletion. It is
// shared by direct edits, staged plans and compensation authorization. A nil
// next route deletes the policy; a non-nil route with nil Health preserves it.
func PathRouteHealthChanged(before, next *PathRoute) (bool, error) {
	old := PathRouteHealth{}
	if before != nil && before.Health != nil {
		old = *before.Health
	}
	after := PathRouteHealth{}
	if next != nil {
		after = old
		if next.Health != nil {
			after = *next.Health
		}
	}
	old, err := old.Normalized()
	if err != nil {
		return false, err
	}
	after, err = after.Normalized()
	return old != after, err
}

// PreservePathRouteHealth merges old editors without turning omission/null
// into an implicit disable. Historical intents should contain explicit policy.
func PreservePathRouteHealth(next, before *PathRoute) {
	if next.Health == nil && before != nil {
		next.Health = before.Health
	}
	if next.Health == nil {
		next.Health = &PathRouteHealth{}
	}
	copy := *next.Health
	next.Health = &copy
}

// LineExpectedPathHealth is an explicit non-secret allowlist for previews,
// change history and staged snapshots. Do not embed arbitrary spec structs.
type LineExpectedPathHealth struct {
	RouteID           int64                       `json:"route_id"`
	ScopeID           string                      `json:"scope_id"`
	Protocol          string                      `json:"protocol"`
	ServerName        string                      `json:"server_name,omitempty"`
	ALPN              []string                    `json:"alpn,omitempty"`
	AutoFailover      bool                        `json:"auto_failover"`
	IntervalSeconds   int                         `json:"interval_seconds"`
	TimeoutSeconds    int                         `json:"timeout_seconds"`
	FailureThreshold  int                         `json:"failure_threshold"`
	RecoveryThreshold int                         `json:"recovery_threshold"`
	Candidates        []LineExpectedPathCandidate `json:"candidates"`
}
type LineExpectedPathCandidate struct {
	ID     string `json:"id"`
	Target string `json:"target"`
	Digest string `json:"digest"`
}

func projectLinePathHealth(p *spec.PathHealth) *LineExpectedPathHealth {
	if p == nil {
		return nil
	}
	out := &LineExpectedPathHealth{RouteID: p.RouteID, ScopeID: p.ScopeID, Protocol: p.Protocol, ServerName: p.ServerName, ALPN: append([]string(nil), p.ALPN...), AutoFailover: p.AutoFailover, IntervalSeconds: p.IntervalSeconds, TimeoutSeconds: p.TimeoutSeconds, FailureThreshold: p.FailureThreshold, RecoveryThreshold: p.RecoveryThreshold}
	for _, c := range p.Candidates {
		out.Candidates = append(out.Candidates, LineExpectedPathCandidate{ID: c.ID, Target: c.Target, Digest: c.Digest})
	}
	return out
}

// Wire restores only reviewed fields and owns its slices, including nil policy.
func (p *LineExpectedPathHealth) Wire() *spec.PathHealth {
	if p == nil {
		return nil
	}
	out := &spec.PathHealth{RouteID: p.RouteID, ScopeID: p.ScopeID, Protocol: p.Protocol, ServerName: p.ServerName, ALPN: append([]string(nil), p.ALPN...), AutoFailover: p.AutoFailover, IntervalSeconds: p.IntervalSeconds, TimeoutSeconds: p.TimeoutSeconds, FailureThreshold: p.FailureThreshold, RecoveryThreshold: p.RecoveryThreshold}
	for _, c := range p.Candidates {
		out.Candidates = append(out.Candidates, spec.PathHealthCandidate{ID: c.ID, Target: c.Target, Digest: c.Digest})
	}
	return out
}
