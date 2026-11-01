package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

var ErrPathTransportVersion = errors.New("UDP or non-native complete paths require bosun v0.88.0 or newer on every forwarding server")

// PathTransportRequiresModernAgent is shared by compilation and delivery
// filtering of complete-path owned rules. Ordinary forwards do not use this gate.
func PathTransportRequiresModernAgent(protocol, backend string) bool {
	return protocol != "tcp" || pathWireBackend(backend) != ""
}

func validatePathBackend(backend string) error {
	switch backend {
	case "", "relay", "realm", "nft":
		return nil
	default:
		return errors.New("path backend must be relay, realm or nft")
	}
}

// The empty native backend is part of the released runtime-tag and cfg1 wire
// contract. An explicit reset to relay must compile to those same bytes.
func pathWireBackend(backend string) string {
	if backend == "relay" {
		return ""
	}
	return backend
}

// PreservePathRouteBackends keeps edits from older clients from replacing a
// chosen entry backend. Historical compensation must pass explicit relay intent
// for an old empty value instead of borrowing the current route's backend.
func PreservePathRouteBackends(next, before *PathRoute) {
	if next != nil && before != nil && next.EntryBackend == "" {
		next.EntryBackend = before.EntryBackend
	}
}

func preservePathStepBackends(next, before []PathStep) {
	old := map[string]PathStep{}
	for _, step := range before {
		old[step.ID] = step
	}
	for i := range next {
		step := &next[i]
		prior, ok := old[step.ID]
		if ok && step.ID != "" && step.Backend == "" && step.NodeID > 0 && step.NodeID == prior.NodeID && step.PathID == 0 && step.LineID == 0 && prior.PathID == 0 && prior.LineID == 0 {
			step.Backend = prior.Backend
		}
	}
}

func pathRouteMemberBackendsTx(ctx context.Context, tx *sql.Tx, p *PathRoute) (map[string]string, error) {
	out := map[string]string{p.Entry.ID: pathWireBackend(p.EntryBackend)}
	for _, branch := range p.Branches {
		path, err := pathRevisionTx(ctx, tx, branch.PathID, branch.PathRevision)
		if err != nil {
			return nil, err
		}
		for _, hop := range path.Hops {
			if err = validatePathBackend(hop.Backend); err != nil {
				return nil, err
			}
			out[pathKey(branch.ID, hop.Key)] = pathWireBackend(hop.Backend)
		}
	}
	return out, nil
}

func pathForwardListenersOverlap(a, b spec.Forward) bool {
	if a.Port != b.Port || !lineListenOverlap(a.Listen, b.Listen) {
		return false
	}
	for _, left := range lineProtocols(a.Protocol) {
		for _, right := range lineProtocols(b.Protocol) {
			if left == right {
				return true
			}
		}
	}
	return false
}
