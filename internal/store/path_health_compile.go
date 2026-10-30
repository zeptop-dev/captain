package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
)

var ErrPathHealthCapability = errors.New("path health requires a recent capability report from bosun v0.85.0 or newer on the entry server")

func pathHealthCapabilityTx(ctx context.Context, tx *sql.Tx, id int64, at time.Time) (bool, error) {
	var supported bool
	var observed, received int64
	err := tx.QueryRowContext(ctx, `SELECT c.supported,c.observed_at,c.received_at FROM path_health_capabilities c JOIN nodes n ON n.id=c.node_id AND n.token_hash=c.token_identity JOIN node_config_heads h ON h.node_id=c.node_id AND h.token_identity=c.token_identity AND h.session=c.session JOIN node_config_sessions s ON s.node_id=h.node_id AND s.token_identity=h.token_identity AND s.session=h.session WHERE c.node_id=? AND s.retired_at IS NULL`, id).Scan(&supported, &observed, &received)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return supported && observed > 0 && received > 0 && !time.UnixMilli(observed).Before(at.Add(-3*time.Minute)) && !time.UnixMilli(observed).After(at.Add(30*time.Second)) && !time.UnixMilli(received).Before(at.Add(-3*time.Minute)), nil
}

// No TLS name is guessed from public/NAT addresses or a decoy. An explicit
// name changes only verification/SNI; dialing always uses the compiled hop.
func pathHealthHandshake(p *PathRouteHealth, ib *domain.Inbound) (protocol, name string, alpn []string) {
	s := ib.Settings
	if s.ShadowTLS != nil || s.TLS != nil && s.TLS.Mode == spec.TLSReality {
		return spec.PathHealthUnsupported, "", nil
	}
	if s.TLS != nil && s.TLS.Mode == spec.TLSStandard {
		name = p.ServerName
		if name == "" {
			name = s.TLS.ServerName
		}
		if spec.ValidPathHealthServerName(name) {
			return spec.PathHealthTLS, name, append([]string(nil), s.TLS.ALPN...)
		}
		return spec.PathHealthUnsupported, "", nil
	}
	if s.Transport != nil && s.Transport.Type != "" && s.Transport.Type != "tcp" {
		return spec.PathHealthUnsupported, "", nil
	}
	switch ib.Protocol {
	case spec.HTTP:
		return spec.PathHealthHTTP, "", nil
	case spec.SOCKS:
		return spec.PathHealthSOCKS5, "", nil
	}
	return spec.PathHealthUnsupported, "", nil
}

func compilePathHealthTx(ctx context.Context, tx *sql.Tx, l *ForwardLine, p *PathRoute, ib *domain.Inbound, entry *NodeForward, branches []map[int64][]NodeForward) error {
	if p.Health == nil || !p.Health.Enabled {
		return nil
	}
	n, err := loadLineNode(ctx, tx, p.Entry.NodeID, l.ID)
	if err != nil {
		return err
	}
	if !lineVersionOK(n.node, "v0.85.0") {
		return ErrPathHealthCapability
	}
	policy, err := p.Health.Normalized()
	if err != nil {
		return err
	}
	protocol, name, alpn := pathHealthHandshake(&policy, ib)
	scope := "preview"
	if l.ID > 0 {
		if err = tx.QueryRowContext(ctx, `SELECT id FROM line_change_scopes WHERE line_id=?`, l.ID).Scan(&scope); err != nil {
			return err
		}
	}
	health := &spec.PathHealth{RouteID: l.ID, ScopeID: scope, Protocol: protocol, ServerName: name, ALPN: alpn, AutoFailover: policy.AutoFailover, IntervalSeconds: policy.IntervalSeconds, TimeoutSeconds: policy.TimeoutSeconds, FailureThreshold: policy.FailureThreshold, RecoveryThreshold: policy.RecoveryThreshold}
	for i, b := range p.Branches {
		type member struct {
			NodeID   int64
			Forwards []LineChangeForward
		}
		members := []member{}
		for id, fs := range branches[i] {
			wire := make([]spec.Forward, 0, len(fs))
			for _, f := range fs {
				wire = append(wire, f.Forward)
			}
			members = append(members, member{NodeID: id, Forwards: lineChangeProjection(wire)})
		}
		sort.Slice(members, func(i, j int) bool { return members[i].NodeID < members[j].NodeID })
		// Include the whole actual branch, not just its first TCP address. The
		// health name and public handshake structure contain no user secrets.
		raw, _ := json.Marshal(struct {
			Scope                  string
			Branch                 PathBranch
			Members                []member
			LandingNode, LandingID int64
			LandingPort            int
			Protocol, ServerName   string
			ALPN                   []string
			Policy                 PathRouteHealth
		}{scope, b, members, ib.NodeID, ib.ID, ib.Port, protocol, name, alpn, policy})
		sum := sha256.Sum256(raw)
		health.Candidates = append(health.Candidates, spec.PathHealthCandidate{ID: b.ID, Target: entry.Hops()[i].Target, Digest: hex.EncodeToString(sum[:])})
	}
	entry.PathHealth = health
	return entry.ValidatePathHealth()
}

// Only a new/changed policy or a different entry needs a new capability proof.
// Offline peers must not turn an unchanged enabled policy into an implicit off.
func validatePathRouteHealthMutationTx(ctx context.Context, tx *sql.Tx, before, next *PathRoute) error {
	if next == nil || next.Health == nil || !next.Health.Enabled {
		return nil
	}
	changed, err := PathRouteHealthChanged(before, next)
	if err != nil {
		return err
	}
	if !changed && before != nil && before.Entry.NodeID == next.Entry.NodeID {
		return nil
	}
	n, err := loadLineNode(ctx, tx, next.Entry.NodeID, next.ID)
	if err != nil {
		return err
	}
	capable, err := pathHealthCapabilityTx(ctx, tx, next.Entry.NodeID, time.Now())
	if err != nil {
		return err
	}
	if !lineVersionOK(n.node, "v0.85.0") || !capable {
		return ErrPathHealthCapability
	}
	return nil
}
