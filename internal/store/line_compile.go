package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
)

type lineNode struct {
	node      *domain.Node
	ingresses map[int64]*Ingress
	forwards  []NodeForward
	binds     []lineBind
}
type lineBind struct {
	protocol string
	port     int
	listen   string
}

func lineInboundBinds(ib *domain.Inbound, listen string) []lineBind {
	s := ib.Spec()
	switch s.Protocol {
	case spec.Hysteria2, spec.TUIC, spec.WireGuard:
		return []lineBind{{"udp", s.Port, listen}}
	case spec.Shadowsocks, spec.Snell:
		return []lineBind{{"tcp", s.Port, listen}, {"udp", s.Port, listen}}
	case spec.Mieru:
		switch strings.ToUpper(s.MieruTransport) {
		case "UDP":
			return []lineBind{{"udp", s.Port, listen}}
		case "BOTH":
			return []lineBind{{"tcp", s.Port, listen}, {"udp", s.Port + 1, listen}}
		}
	}
	return []lineBind{{"tcp", s.Port, listen}}
}
func lineProtocols(p string) []string {
	if p == "both" {
		return []string{"tcp", "udp"}
	}
	return []string{p}
}
func lineListenOverlap(a, b string) bool {
	wildcard := func(s string) bool { return s == "" || s == "0.0.0.0" || s == "::" }
	return wildcard(a) || wildcard(b) || a == b
}

func loadLineNode(ctx context.Context, tx *sql.Tx, id, lineID int64) (*lineNode, error) {
	n, err := scanNode(tx.QueryRowContext(ctx, "SELECT "+nodeCols+" FROM nodes WHERE id=?", id))
	if err != nil {
		return nil, err
	}
	if !NodeProxyAllowed(n) {
		return nil, spec.ErrMonitorOnly
	}
	var pending int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM node_jobs WHERE node_id=? AND kind='node_remove' AND (done_at IS NULL OR error='')`, id).Scan(&pending); err != nil {
		return nil, err
	}
	if pending > 0 {
		return nil, errors.New("server is being removed or has already been removed")
	}
	x := &lineNode{node: n, ingresses: map[int64]*Ingress{}}
	rows, err := tx.QueryContext(ctx, "SELECT "+ingressCols+" FROM ingresses WHERE node_id=?", id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		g, e := scanIngress(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		x.ingresses[g.ID] = g
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = tx.QueryContext(ctx, "SELECT "+inboundCols+" FROM inbounds WHERE node_id=? AND enabled=1", id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		ib, e := scanInbound(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		listen := ib.Listen
		if listen == "" && ib.IngressID != nil && x.ingresses[*ib.IngressID] != nil {
			listen = x.ingresses[*ib.IngressID].BindIP
		}
		x.binds = append(x.binds, lineInboundBinds(ib, listen)...)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT w.listen_port FROM node_wireguard_links w JOIN inbounds i ON i.id=w.user_inbound_id WHERE w.transit_id=? AND i.enabled=1`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var port int
		if err = rows.Scan(&port); err != nil {
			rows.Close()
			return nil, err
		}
		x.binds = append(x.binds, lineBind{"udp", port, ""})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	var raw string
	if err = tx.QueryRowContext(ctx, `SELECT forwards_json FROM nodes WHERE id=?`, id).Scan(&raw); err != nil {
		return nil, err
	}
	if err = json.Unmarshal([]byte(raw), &x.forwards); err != nil {
		return nil, err
	}
	for _, f := range x.forwards {
		if f.LineID == lineID {
			continue
		}
		listen := f.Listen
		if listen == "" && f.IngressID != "" {
			gid, _ := strconv.ParseInt(f.IngressID, 10, 64)
			if g := x.ingresses[gid]; g != nil {
				listen = g.BindIP
			}
		}
		for _, p := range lineProtocols(f.Protocol) {
			x.binds = append(x.binds, lineBind{p, f.Port, listen})
		}
	}
	reservations, err := lineRolloutReservationsTx(ctx, tx, id, lineID)
	if err != nil {
		return nil, err
	}
	for _, r := range reservations {
		x.binds = append(x.binds, lineBind{"tcp", r.forward.Port, r.forward.Listen})
	}
	return x, nil
}

func compileLine(ctx context.Context, tx *sql.Tx, l *ForwardLine) (map[int64][]NodeForward, map[string]LineEndpoint, error) {
	owned, err := isPathRouteTx(ctx, tx, l.ID)
	if err != nil {
		return nil, nil, err
	}
	if owned {
		route, err := pathRouteTx(ctx, tx, l.ID)
		if err != nil {
			return nil, nil, err
		}
		return compilePathRouteTx(ctx, tx, l, route)
	}
	return compileOrdinaryLine(ctx, tx, l)
}

func compileOrdinaryLine(ctx context.Context, tx *sql.Tx, l *ForwardLine) (map[int64][]NodeForward, map[string]LineEndpoint, error) {
	ib, err := scanInbound(tx.QueryRowContext(ctx, "SELECT "+inboundCols+" FROM inbounds WHERE id=?", l.TargetInboundID))
	if err != nil {
		return nil, nil, err
	}
	// A reverse subscriber listener lives on A, but still depends on its owned
	// exit B. Check only this listener's connection, not unrelated links on A.
	var removingExit int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM node_jobs WHERE kind='node_remove' AND (done_at IS NULL OR error='') AND node_id IN (
	SELECT exit_id FROM node_reverse_links WHERE user_inbound_id=?
	 UNION SELECT exit_id FROM node_wireguard_links WHERE user_inbound_id=?)`, ib.ID, ib.ID).Scan(&removingExit); err != nil {
		return nil, nil, err
	}
	if removingExit > 0 {
		return nil, nil, errors.New("landing reverse exit is being removed or has already been removed")
	}
	if view, ok := ctx.Value(topologyCompileContext{}).(*topologyPlan); ok {
		return compileOrdinaryLineModel(l, ib, func(id int64) (*lineNode, error) { return view.node(ctx, tx, id, l.ID) })
	}
	return compileOrdinaryLineModel(l, ib, func(id int64) (*lineNode, error) { return loadLineNode(ctx, tx, id, l.ID) })
}

// compileOrdinaryLineModel consumes an explicit consistent view; it has no database writes or reads.
func compileOrdinaryLineModel(l *ForwardLine, ib *domain.Inbound, loadNode func(int64) (*lineNode, error)) (map[int64][]NodeForward, map[string]LineEndpoint, error) {
	var err error
	if !ib.Enabled {
		return nil, nil, errors.New("line landing inbound must be enabled")
	}
	if ib.Settings.Reverse != nil && ib.Settings.Reverse.Receiver {
		return nil, nil, errors.New("a reverse tunnel receiver cannot be a line landing")
	}
	expected, err := LineProtocol(ib)
	if err != nil {
		return nil, nil, err
	}
	if l.Protocol != expected {
		return nil, nil, fmt.Errorf("line protocol must be %s for this landing inbound", expected)
	}
	if ib.Settings.AcceptProxyProtocol && l.Backend == "nft" {
		return nil, nil, errors.New("a PROXY protocol landing requires relay or realm")
	}
	targetNode, err := loadNode(ib.NodeID)
	if err != nil {
		return nil, nil, err
	}
	var targetIngress *Ingress
	if ib.IngressID != nil {
		targetIngress = targetNode.ingresses[*ib.IngressID]
		if targetIngress == nil {
			return nil, nil, errors.New("landing ingress is missing")
		}
	}
	targetHost, targetPort := lineAddress(targetNode.node, targetIngress, ib.Port, true)
	if l.TargetAddressMode == AddressModeInternal {
		targetHost, err = internalInboundHost(targetNode.node, ib, targetIngress, l.Protocol)
		if err != nil {
			return nil, nil, err
		}
		targetPort = ib.Port
	}
	target, err := lineHostPort(targetHost, targetPort)
	if err != nil {
		return nil, nil, err
	}
	nodes := map[int64]*lineNode{}
	addresses := map[string]string{}
	endpoints := map[string]LineEndpoint{}
	for i, g := range l.Groups {
		for _, m := range g.Members {
			if m.NodeID == ib.NodeID {
				return nil, nil, errors.New("landing server cannot also be a line relay")
			}
			n, err := loadNode(m.NodeID)
			if err != nil {
				return nil, nil, err
			}
			nodes[m.NodeID] = n
			var ing *Ingress
			if m.IngressID != nil {
				ing = n.ingresses[*m.IngressID]
				if ing == nil {
					return nil, nil, errors.New("member ingress does not belong to its server")
				}
				if err = ing.Ports().Check(m.Port); err != nil {
					return nil, nil, err
				}
			} else {
				for _, g := range n.ingresses {
					if g.RequireIngress {
						return nil, nil, errors.New("member server requires an ingress")
					}
				}
			}
			host, port := lineAddress(n.node, ing, m.Port, true)
			if m.AddressMode == AddressModeInternal {
				host, err = internalAddress(n.node.InternalAddr)
				if err != nil {
					return nil, nil, fmt.Errorf("server %d: %w", m.NodeID, err)
				}
				port = m.Port
				if ing != nil {
					if err = validateInternalListen(host, ing.BindIP); err != nil {
						return nil, nil, err
					}
				}
			}
			addresses[m.ID], err = lineHostPort(host, port)
			if err != nil {
				return nil, nil, err
			}
			if i == 0 {
				host, port = lineAddress(n.node, ing, m.Port, false)
				endpoints[m.ID] = LineEndpoint{m.ID, m.NodeID, host, port}
			}
		}
	}
	generated := map[int64][]NodeForward{}
	for i, g := range l.Groups {
		for _, m := range g.Members {
			n := nodes[m.NodeID]
			if l.Backend == "nft" && !lineVersionOK(n.node, "v0.62.0") {
				return nil, nil, errors.New("nft lines require bosun v0.62.0 or newer")
			}
			if l.Backend == "realm" && !lineVersionOK(n.node, "v0.49.0") {
				return nil, nil, errors.New("realm lines require bosun v0.49.0 or newer")
			}
			f := spec.Forward{Port: m.Port, Protocol: l.Protocol, Target: target}
			if l.Backend != "relay" {
				f.Backend = l.Backend
			}
			var ingress *Ingress
			if m.IngressID != nil {
				ingress = n.ingresses[*m.IngressID]
				f.IngressID = strconv.FormatInt(*m.IngressID, 10)
				f.Listen = ingress.BindIP
			}
			if m.AddressMode == AddressModeInternal && f.Listen == "" {
				f.Listen, err = internalAddress(n.node.InternalAddr)
				if err != nil {
					return nil, nil, err
				}
			}
			destinationMode := l.TargetAddressMode
			if i+1 < len(l.Groups) {
				next := l.Groups[i+1]
				f.Target = addresses[next.Members[0].ID]
				destinationMode = next.Members[0].AddressMode
				if len(next.Members) > 1 {
					if !lineVersionOK(n.node, "v0.49.0") {
						return nil, nil, errors.New("candidate groups require bosun v0.49.0 or newer on every selecting relay")
					}
					f.Balance, f.Weight = next.Strategy, next.Members[0].Weight
					for _, candidate := range next.Members[1:] {
						f.Targets = append(f.Targets, spec.ForwardTarget{Target: addresses[candidate.ID], Weight: candidate.Weight})
					}
				}
			}
			if l.Backend == "realm" && l.Protocol != "tcp" && destinationMode != AddressModeInternal {
				destination, destinationIngress, port := targetNode.node, targetIngress, ib.Port
				if i+1 < len(l.Groups) {
					// Realm lines have one next-hop member. Unlike its TCP dialer,
					// realm 2.9.6 UDP tries only the first DNS address.
					next := l.Groups[i+1].Members[0]
					destination, destinationIngress, port = nodes[next.NodeID].node, nil, next.Port
					if next.IngressID != nil {
						destinationIngress = nodes[next.NodeID].ingresses[*next.IngressID]
					}
				}
				f.Target, err = lineRealmUDPAddress(n.node, destination, destinationIngress, port)
				if err != nil {
					return nil, nil, err
				}
			}
			// Prefix exactly once. Later relays carry the header as opaque bytes.
			f.ProxyProtocol = i == 0 && ib.Settings.AcceptProxyProtocol
			if f.ProxyProtocol && !lineVersionOK(n.node, "v0.49.0") {
				return nil, nil, errors.New("PROXY protocol lines require a recent bosun relay")
			}
			if err = setLineBackendListen(&f, n.node, ingress, i == 0); err != nil {
				return nil, nil, err
			}
			// nft creates a single-family packet rule rather than a dual-stack
			// socket: an IPv6 wildcard cannot receive the preceding IPv4 hop.
			if f.Backend == "nft" && m.AddressMode == AddressModeInternal && lineLiteralFamily(f.Listen) != lineLiteralFamily(strings.TrimSpace(n.node.InternalAddr)) {
				return nil, nil, fmt.Errorf("%w: nft member listener and internal IP must use the same address family", ErrInternalAddress)
			}
			f.Tag = lineRuntimeTag(l, m, f)
			if err = f.ValidateTargets(); err != nil {
				return nil, nil, err
			}
			if !spec.ValidTag(f.Tag) {
				return nil, nil, errors.New("generated forward tag is too long")
			}
			for _, p := range lineProtocols(f.Protocol) {
				for _, b := range n.binds {
					if b.protocol == p && b.port == f.Port && lineListenOverlap(b.listen, f.Listen) {
						return nil, nil, fmt.Errorf("server %d already uses %s port %d", m.NodeID, p, f.Port)
					}
				}
			}
			generated[m.NodeID] = append(generated[m.NodeID], NodeForward{Forward: f, LineID: l.ID, LineMemberID: m.ID})
		}
	}
	return generated, endpoints, nil
}

// Use a registered common family for ordinary managed destinations instead of
// relying on realm UDP's first DNS answer. Inventory is only a preference, not
// proof of an OS route: unknown/disjoint inventories retain normal resolution.
// Explicit provider paths and shared/external DNS remain authoritative. Listen
// and public NAT ingress families say nothing about outbound reachability.
func lineRealmUDPAddress(source, destination *domain.Node, ingress *Ingress, port int) (string, error) {
	host, port := lineAddress(destination, ingress, port, true)
	if ingress == nil && !destination.DomainShared {
		switch {
		case lineLiteralFamily(source.PublicAddr) == 4 && lineLiteralFamily(destination.PublicAddr) == 4:
			host = destination.PublicAddr
		case lineLiteralFamily(source.V6Addr) == 6 && lineLiteralFamily(destination.V6Addr) == 6:
			host = destination.V6Addr
		}
	}
	return lineHostPort(host, port)
}

// Backends give an empty listen different meanings: realm binds IPv4 only,
// while nft selects the target's family. Derive only a wildcard from known
// addresses; a public/NAT address is not necessarily assigned to this host.
// Explicit local ingress binds remain authoritative, including when the
// provider translates the public endpoint's family before traffic arrives.
func setLineBackendListen(f *spec.Forward, n *domain.Node, ingress *Ingress, published bool) error {
	if f.Listen != "" || (f.Backend != "nft" && f.Backend != "realm") {
		return nil
	}
	host, _ := lineAddress(n, ingress, f.Port, true)
	family := lineLiteralFamily(host)
	entryFamily := 0
	if published {
		host, _ = lineAddress(n, ingress, f.Port, false)
		entryFamily = lineLiteralFamily(host)
	}
	if f.Backend == "realm" {
		if family == 6 || entryFamily == 6 || (ingress == nil && n.Domain != "" && lineLiteralFamily(n.V6Addr) == 6) {
			f.Listen = "::"
		}
		return nil
	}
	if family != 0 && entryFamily != 0 && family != entryFamily {
		return errors.New("nft ingress addresses use different families; set the actual local bind address")
	}
	if family == 0 {
		family = entryFamily
	}
	switch family {
	case 4:
		f.Listen = "0.0.0.0"
	case 6:
		f.Listen = "::"
	}
	// Keep unknown domains unresolved here: compilation holds a transaction.
	// An explicit wildcard also makes the node resolve nft targets in that family.
	return nil
}

func lineLiteralFamily(host string) int {
	ip := net.ParseIP(host)
	if ip == nil {
		return 0
	}
	if ip.To4() != nil {
		return 4
	}
	return 6
}

// LineProtocol is the wire transport, not the proxy payload's TCP/UDP support.
func LineProtocol(ib *domain.Inbound) (string, error) {
	if ib.Protocol == spec.Mieru && strings.EqualFold(ib.Settings.MieruTransport, "BOTH") {
		return "", errors.New("mieru BOTH needs two adjacent mapped listeners and is not supported by lines")
	}
	binds := lineInboundBinds(ib, "")
	if len(binds) > 1 {
		return "both", nil
	}
	return binds[0].protocol, nil
}
