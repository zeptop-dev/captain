package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"reflect"
	"strings"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
)

const AddressModeInternal = "internal"

var ErrInternalAddress = errors.New("internal forwarding address")

type addressQuery interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func validateAddressMode(mode string) error {
	switch mode {
	case "", "default", AddressModeInternal:
		return nil
	}
	return fmt.Errorf("%w: address mode must be default or internal", ErrInternalAddress)
}

func internalAddress(raw string) (string, error) {
	ip, err := netip.ParseAddr(strings.TrimSpace(raw))
	if err != nil || ip.Zone() != "" || !ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return "", fmt.Errorf("%w: configure a literal unicast server internal IP (not loopback, link-local, multicast or unspecified)", ErrInternalAddress)
	}
	ip = ip.Unmap()
	// IPv4-mapped special addresses must be checked after unmapping too.
	if !ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return "", fmt.Errorf("%w: unusable server internal IP", ErrInternalAddress)
	}
	return ip.String(), nil
}

func validateInternalListen(host, listen string) error {
	if listen == "" || listen == "::" {
		return nil
	} // existing dual-stack default
	ip, err := netip.ParseAddr(listen)
	target, e := netip.ParseAddr(host)
	if err == nil && e == nil && ((ip.Unmap() == target.Unmap()) || (ip.IsUnspecified() && ip.Unmap().Is4() == target.Unmap().Is4())) {
		return nil
	}
	return fmt.Errorf("%w: listener %q does not accept internal IP %s; adjust its bind address first", ErrInternalAddress, listen, host)
}

func internalInboundHost(n *domain.Node, ib *domain.Inbound, ingress *Ingress, protocol string) (string, error) {
	if !ib.Enabled {
		return "", fmt.Errorf("%w: target inbound must be enabled", ErrInternalAddress)
	}
	if ib.Settings.Reverse != nil && ib.Settings.Reverse.Receiver {
		return "", fmt.Errorf("%w: tunnel receiver cannot be a forwarding target", ErrInternalAddress)
	}
	host, err := internalAddress(n.InternalAddr)
	if err != nil {
		return "", fmt.Errorf("server %d: %w", n.ID, err)
	}
	listen := ib.Listen
	if listen == "" && ingress != nil {
		listen = ingress.BindIP
	}
	if listen == "" && ib.Protocol == spec.Snell {
		listen = "0.0.0.0"
	}
	if err = validateInternalListen(host, listen); err != nil {
		return "", err
	}
	for _, protocol := range lineProtocols(protocol) {
		found := false
		for _, bind := range lineInboundBinds(ib, listen) {
			if bind.protocol == protocol && bind.port == ib.Port {
				found = true
			}
		}
		if !found {
			return "", fmt.Errorf("%w: target inbound does not listen on %s port %d", ErrInternalAddress, protocol, ib.Port)
		}
	}
	return host, nil
}

func preserveLineAddressModes(next, old *ForwardLine) {
	if next.TargetAddressMode == "" {
		next.TargetAddressMode = old.TargetAddressMode
	}
	modes := map[string]string{}
	nodeModes := map[int64]string{}
	for _, g := range old.Groups {
		for _, m := range g.Members {
			modes[m.ID] = m.AddressMode
			nodeModes[m.NodeID] = m.AddressMode
		}
	}
	for i := range next.Groups {
		for j := range next.Groups[i].Members {
			m := &next.Groups[i].Members[j]
			if m.AddressMode == "" {
				mode, found := modes[m.ID]
				if !found {
					mode = nodeModes[m.NodeID]
				}
				m.AddressMode = mode
			}
		}
	}
}

func preserveForwardAddressModes(list, old []NodeForward) error {
	modes := map[string]string{}
	present := map[string]bool{}
	for _, f := range list {
		tag := strings.TrimSpace(f.Tag)
		if tag == "" {
			tag = fmt.Sprintf("fwd-%d", f.Port)
		}
		present[tag] = true
	}
	for _, f := range old {
		modes[f.Tag] = f.TargetAddressMode
	}
	for i := range list {
		f := &list[i]
		if f.TargetAddressMode != "" {
			continue
		}
		tag := strings.TrimSpace(f.Tag)
		if tag == "" {
			tag = fmt.Sprintf("fwd-%d", f.Port)
		}
		mode, known := modes[tag]
		if known {
			f.TargetAddressMode = mode
			continue
		}
		// A renamed rule is recognizable by its retained listener and inbound.
		// Adding another rule to the same inbound must still default to the old path.
		matches := 0
		ambiguous := false
		for _, prior := range old {
			if prior.TargetAddressMode != AddressModeInternal || present[prior.Tag] || prior.InboundID != f.InboundID || f.InboundID <= 0 {
				continue
			}
			if prior.Port == f.Port && prior.Listen == f.Listen && prior.IngressID == f.IngressID {
				matches++
			} else {
				ambiguous = true
			}
		}
		if matches == 1 {
			f.TargetAddressMode = AddressModeInternal
		} else if matches > 1 || ambiguous {
			return fmt.Errorf("%w: a renamed or replaced internal forward needs an explicit target_address_mode", ErrInternalAddress)
		}
	}
	return nil
}

// ResolveNodeForwardTargets resolves panel-only references before normal socket
// validation. The same resolver runs again inside the write transaction.
func (s *Store) ResolveNodeForwardTargets(ctx context.Context, node int64, list []NodeForward) ([]NodeForward, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	old, err := nodeForwardsTx(ctx, tx, node)
	if err != nil {
		return nil, err
	}
	if err = preserveForwardAddressModes(list, old); err != nil {
		return nil, err
	}
	if err = resolveInternalForwardTargetsTx(ctx, tx, node, list); err != nil {
		return nil, err
	}
	return list, tx.Commit()
}
func nodeForwardsTx(ctx context.Context, q addressQuery, node int64) ([]NodeForward, error) {
	var raw string
	if err := q.QueryRowContext(ctx, "SELECT forwards_json FROM nodes WHERE id=?", node).Scan(&raw); err != nil {
		return nil, wrapNotFound(err)
	}
	var list []NodeForward
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil, err
	}
	return list, nil
}
func resolveInternalForwardTargetsTx(ctx context.Context, tx *sql.Tx, source int64, list []NodeForward) error {
	for i := range list {
		f := &list[i]
		if err := validateAddressMode(f.TargetAddressMode); err != nil {
			return err
		}
		if f.TargetAddressMode != AddressModeInternal {
			continue
		}
		if f.LineID != 0 {
			return fmt.Errorf("%w: edit generated targets through their line", ErrInternalAddress)
		}
		if f.InboundID <= 0 {
			return fmt.Errorf("%w: choose a managed target inbound", ErrInternalAddress)
		}
		if len(f.Targets) > 0 {
			return fmt.Errorf("%w: standalone internal forwarding cannot have additional targets; use a managed candidate group to select each member's connection address", ErrInternalAddress)
		}
		ib, err := scanInbound(tx.QueryRowContext(ctx, "SELECT "+inboundCols+" FROM inbounds WHERE id=?", f.InboundID))
		if err != nil {
			return err
		}
		var removingExit int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM node_jobs WHERE kind='node_remove' AND (done_at IS NULL OR error='') AND node_id IN (
          SELECT exit_id FROM node_reverse_links WHERE user_inbound_id=? UNION SELECT exit_id FROM node_wireguard_links WHERE user_inbound_id=?)`, ib.ID, ib.ID).Scan(&removingExit); err != nil {
			return err
		}
		if removingExit > 0 {
			return fmt.Errorf("%w: landing reverse exit is being removed or has already been removed", ErrInternalAddress)
		}
		if ib.NodeID == source {
			return fmt.Errorf("%w: target must be another server", ErrInternalAddress)
		}
		target, err := loadLineNode(ctx, tx, ib.NodeID, 0)
		if err != nil {
			return err
		}
		var ingress *Ingress
		if ib.IngressID != nil {
			ingress = target.ingresses[*ib.IngressID]
			if ingress == nil {
				return fmt.Errorf("%w: target ingress is missing", ErrInternalAddress)
			}
		}
		protocol := f.Protocol
		if protocol == "" {
			protocol = "both"
		}
		host, err := internalInboundHost(target.node, ib, ingress, protocol)
		if err != nil {
			return err
		}
		f.Target, err = lineHostPort(host, ib.Port)
		if err != nil {
			return err
		}
		if ib.Settings.AcceptProxyProtocol && f.Backend == "nft" {
			return fmt.Errorf("%w: PROXY protocol requires relay or realm", ErrInternalAddress)
		}
		f.ProxyProtocol = ib.Settings.AcceptProxyProtocol
		// The source's listener follows its own public/mapped endpoint, never
		// the target's address family. Keep explicit local binds authoritative.
		effective := f.Forward
		var sourceIngress *Ingress
		if f.IngressID != "" {
			sourceIngress, err = scanIngress(tx.QueryRowContext(ctx, "SELECT "+ingressCols+" FROM ingresses WHERE id=? AND node_id=?", f.IngressID, source))
			if err != nil {
				return err
			}
			if effective.Listen == "" {
				effective.Listen = sourceIngress.BindIP
			}
		}
		if effective.Listen == "" && (f.Backend == "nft" || f.Backend == "realm") {
			sourceNode, err := scanNode(tx.QueryRowContext(ctx, "SELECT "+nodeCols+" FROM nodes WHERE id=?", source))
			if err != nil {
				return err
			}
			if err = setLineBackendListen(&effective, sourceNode, sourceIngress, true); err != nil {
				return fmt.Errorf("%w: %v", ErrInternalAddress, err)
			}
			f.Listen = effective.Listen
		}
		if err := effective.ValidateTargets(); err != nil {
			return fmt.Errorf("%w: %v", ErrInternalAddress, err)
		}
	}
	return nil
}

// InternalAddressUse is a panel reference, never an agent credential or a claim
// that the private route is actually reachable.
type InternalAddressUse struct {
	Kind         string `json:"kind"`
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	SourceNodeID int64  `json:"source_node_id"`
	Tag          string `json:"tag,omitempty"`
}

func (s *Store) InternalAddressUses(ctx context.Context, node int64) ([]InternalAddressUse, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := scanNode(tx.QueryRowContext(ctx, "SELECT "+nodeCols+" FROM nodes WHERE id=?", node)); err != nil {
		return nil, err
	}
	uses := []InternalAddressUse{}
	rows, err := tx.QueryContext(ctx, "SELECT "+lineCols+" FROM forward_lines ORDER BY id")
	if err != nil {
		return nil, err
	}
	var lines []*ForwardLine
	for rows.Next() {
		l, e := scanLine(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		lines = append(lines, l)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, l := range lines {
		referenced := false
		for i, g := range l.Groups {
			for _, m := range g.Members {
				if i > 0 && m.NodeID == node && m.AddressMode == AddressModeInternal {
					referenced = true
				}
			}
		}
		if l.TargetAddressMode == AddressModeInternal {
			var targetNode int64
			if err = tx.QueryRowContext(ctx, "SELECT node_id FROM inbounds WHERE id=?", l.TargetInboundID).Scan(&targetNode); err != nil {
				return nil, err
			}
			referenced = referenced || targetNode == node
		}
		if referenced {
			uses = append(uses, InternalAddressUse{Kind: "line", ID: l.ID, Name: l.Name})
		}
	}
	forwards, err := internalForwardReferences(ctx, tx, node, 0, false)
	if err != nil {
		return nil, err
	}
	uses = append(uses, forwards...)
	return uses, tx.Commit()
}
func internalForwardReferences(ctx context.Context, q addressQuery, node, inbound int64, includeReverse bool) ([]InternalAddressUse, error) {
	rows, err := q.QueryContext(ctx, "SELECT id,name,forwards_json FROM nodes ORDER BY id")
	if err != nil {
		return nil, err
	}
	type source struct {
		id       int64
		name     string
		forwards []NodeForward
	}
	var sources []source
	for rows.Next() {
		var s source
		var raw string
		if err = rows.Scan(&s.id, &s.name, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &s.forwards); err != nil {
			rows.Close()
			return nil, err
		}
		sources = append(sources, s)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := []InternalAddressUse{}
	for _, s := range sources {
		for _, f := range s.forwards {
			if f.LineID != 0 || f.TargetAddressMode != AddressModeInternal {
				continue
			}
			matches := inbound != 0 && f.InboundID == inbound
			if node != 0 {
				var n int64
				if err = q.QueryRowContext(ctx, "SELECT node_id FROM inbounds WHERE id=?", f.InboundID).Scan(&n); err != nil {
					return nil, err
				}
				matches = n == node
				if !matches && includeReverse {
					var count int
					if err = q.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM node_reverse_links WHERE user_inbound_id=? AND exit_id=?) + (SELECT COUNT(*) FROM node_wireguard_links WHERE user_inbound_id=? AND exit_id=?)`, f.InboundID, node, f.InboundID, node).Scan(&count); err != nil {
						return nil, err
					}
					matches = count > 0
				}
			}
			if matches {
				out = append(out, InternalAddressUse{Kind: "forward", ID: f.InboundID, Name: s.name, SourceNodeID: s.id, Tag: f.Tag})
			}
		}
	}
	return out, nil
}
func hasInternalForwardReference(ctx context.Context, q addressQuery, node, inbound int64) (bool, error) {
	refs, err := internalForwardReferences(ctx, q, node, inbound, true)
	return len(refs) > 0, err
}

func refreshInternalForwardsForNodeTx(ctx context.Context, tx *sql.Tx, node int64) error {
	rows, err := tx.QueryContext(ctx, "SELECT id,forwards_json FROM nodes")
	if err != nil {
		return err
	}
	all := map[int64][]NodeForward{}
	for rows.Next() {
		var id int64
		var raw string
		if err = rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return err
		}
		var list []NodeForward
		if err = json.Unmarshal([]byte(raw), &list); err != nil {
			rows.Close()
			return err
		}
		all[id] = list
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for source, list := range all {
		affected := false
		for _, f := range list {
			if f.TargetAddressMode != AddressModeInternal || f.LineID != 0 {
				continue
			}
			var target int64
			if err = tx.QueryRowContext(ctx, "SELECT node_id FROM inbounds WHERE id=?", f.InboundID).Scan(&target); err != nil {
				return err
			}
			if source == node || target == node {
				affected = true
			}
		}
		if !affected {
			continue
		}
		old := append([]NodeForward(nil), list...)
		if err = resolveInternalForwardTargetsTx(ctx, tx, source, list); err != nil {
			return err
		}
		if reflect.DeepEqual(old, list) {
			continue
		}
		raw, _ := json.Marshal(list)
		if _, err = tx.ExecContext(ctx, "UPDATE nodes SET forwards_json=?,updated_at=? WHERE id=?", string(raw), now(), source); err != nil {
			return err
		}
		for i, f := range list {
			if !reflect.DeepEqual(f, old[i]) {
				if _, err = tx.ExecContext(ctx, "DELETE FROM forward_status WHERE node_id=? AND tag=?", source, f.Tag); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
