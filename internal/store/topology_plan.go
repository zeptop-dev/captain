package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
)

type topologyPlan struct {
	base, view    *topologySnapshot
	inbounds      map[int64]*domain.Inbound
	lines         map[int64]*ForwardLine
	entries       map[int64]*domain.Entry
	explicitLines map[int64]bool
	keys          map[string]TopologyResult
	generated     map[int64]map[int64][]NodeForward
	endpoints     map[int64]map[string]LineEndpoint
	preview       *TopologyPreview
}

func topologyAssign[T any](dst *T, p *T) {
	if p != nil {
		*dst = *p
	}
}
func topologyID(p *int64) *int64 {
	if p == nil || *p == 0 {
		return nil
	}
	v := *p
	return &v
}
func (p *topologyPlan) resolve(key, kind string) (int64, error) {
	v, ok := p.keys[key]
	if !ok || v.Kind != kind {
		return 0, errors.New("temporary reference must name an operation of the correct kind")
	}
	return v.ID, nil
}
func newTopologyPlan(base *topologySnapshot) *topologyPlan {
	view := *base
	view.inbounds = map[int64]*domain.Inbound{}
	view.lines = map[int64]*ForwardLine{}
	view.entries = map[int64]*domain.Entry{}
	for id, v := range base.inbounds {
		x := topologyCopy(*v)
		view.inbounds[id] = &x
	}
	for id, v := range base.lines {
		x := topologyCopy(*v)
		view.lines[id] = &x
	}
	for id, v := range base.entries {
		x := topologyCopy(*v)
		view.entries[id] = &x
	}
	return &topologyPlan{base: base, view: &view, inbounds: map[int64]*domain.Inbound{}, lines: map[int64]*ForwardLine{}, entries: map[int64]*domain.Entry{}, explicitLines: map[int64]bool{}, keys: map[string]TopologyResult{}, generated: map[int64]map[int64][]NodeForward{}, endpoints: map[int64]map[string]LineEndpoint{}, preview: &TopologyPreview{Changes: []TopologyChange{}}}
}
func prepareTopologyTx(ctx context.Context, tx *sql.Tx, d *TopologyChangeset) (*topologyPlan, error) {
	if e := validateTopologyBindingsTx(ctx, tx, d); e != nil {
		return nil, e
	}
	base, err := loadTopologySnapshotTx(ctx, tx)
	if err != nil {
		return nil, err
	}
	p := newTopologyPlan(base)
	for i, op := range d.Operations {
		id := op.ID
		if id == 0 {
			id = -int64(i + 1)
		}
		p.keys[op.Key] = TopologyResult{op.Key, op.Kind, id}
	}
	for _, op := range d.Operations {
		if op.Kind != "inbound" {
			continue
		}
		id := p.keys[op.Key].ID
		in := op.Inbound
		if in == nil {
			return nil, ErrTopologyUnsupported
		}
		var ib domain.Inbound
		if op.ID > 0 {
			old := base.inbounds[op.ID]
			if old == nil {
				return nil, ErrNotFound
			}
			v := base.inboundView(old)
			if !v.Editable {
				return nil, ErrTopologyUnsupported
			}
			if v.Revision != op.Revision {
				return nil, ErrTopologyConflict
			}
			ib = topologyCopy(*old)
		} else {
			if in.Settings == nil {
				return nil, errors.New("save the draft before previewing creation settings")
			}
			switch in.Protocol {
			case spec.VLESS, spec.VMess, spec.Trojan, spec.Shadowsocks:
			default:
				return nil, ErrTopologyUnsupported
			}
			ib = domain.Inbound{ID: id, NodeID: in.NodeID, Protocol: in.Protocol, Core: in.Core, Enabled: true, Settings: topologyCopy(*in.Settings)}
		}
		topologyAssign(&ib.Tag, in.Tag)
		topologyAssign(&ib.Listen, in.Listen)
		topologyAssign(&ib.Port, in.Port)
		topologyAssign(&ib.Enabled, in.Enabled)
		topologyAssign(&ib.Sort, in.Sort)
		if in.IngressID != nil {
			ib.IngressID = topologyID(in.IngressID)
		}
		ib.Tag = strings.TrimSpace(ib.Tag)
		ib.Listen = strings.TrimSpace(ib.Listen)
		if ib.Settings.Reverse != nil || ib.Settings.WireGuardSlot != 0 || ib.Settings.PrivateAccess.Enabled() || (op.ID == 0 && (ib.Settings.ScopedUsers || len(ib.Settings.Users) > 0)) {
			return nil, ErrTopologyUnsupported
		}
		p.inbounds[id] = &ib
		p.view.inbounds[id] = &ib
	}
	for _, op := range d.Operations {
		if op.Kind != "line" {
			continue
		}
		id := p.keys[op.Key].ID
		if op.Line == nil || op.Line.PathRoute {
			return nil, ErrPathRouteOwned
		}
		if target := base.inbounds[op.Line.TargetInboundID]; op.InboundRef == "" && target != nil && (target.Settings.Reverse != nil || target.Settings.WireGuardSlot != 0) {
			return nil, ErrTopologyUnsupported
		}
		next := topologyCopy(*op.Line)
		next.ID = id
		next.PreviewRevision = ""
		if op.ID > 0 {
			old := base.lines[id]
			if old == nil {
				return nil, ErrNotFound
			}
			if base.routes[id] {
				return nil, ErrPathRouteOwned
			}
			if base.lineRevision(old) != op.Revision || old.Version != next.Version {
				return nil, ErrTopologyConflict
			}
			if err = checkLineRolloutWriteTx(ctx, tx, id); err != nil {
				return nil, err
			}
			preserveLineAddressModes(&next, old)
			next.Version = old.Version + 1
		} else {
			next.Version = 1
		}
		if op.InboundRef != "" {
			next.TargetInboundID, err = p.resolve(op.InboundRef, "inbound")
			if err != nil {
				return nil, err
			}
		}
		if err = normalizeLine(&next); err != nil {
			return nil, err
		}
		p.lines[id] = &next
		p.view.lines[id] = &next
		p.explicitLines[id] = true
	}
	// Inbound structural edits recompile their ordinary dependants in this same
	// intent, while path-owned landings were explicitly excluded by the catalog.
	for id, l := range base.lines {
		if p.inbounds[l.TargetInboundID] != nil && p.lines[id] == nil {
			if base.routes[id] {
				return nil, ErrPathRouteOwned
			}
			next := topologyCopy(*l)
			next.Version++
			protocol, e := LineProtocol(p.view.inbounds[l.TargetInboundID])
			if e != nil {
				return nil, e
			}
			next.Protocol = protocol
			if e = normalizeLine(&next); e != nil {
				return nil, e
			}
			p.lines[id] = &next
			p.view.lines[id] = &next
		}
	}
	for _, op := range d.Operations {
		if op.Kind != "entry" {
			continue
		}
		id := p.keys[op.Key].ID
		in := op.Entry
		if in == nil {
			return nil, ErrTopologyUnsupported
		}
		var e domain.Entry
		if op.ID > 0 {
			old := base.entries[id]
			if old == nil {
				return nil, ErrNotFound
			}
			v := base.entryView(old)
			if !v.Editable {
				return nil, ErrTopologyUnsupported
			}
			if v.Revision != op.Revision {
				return nil, ErrTopologyConflict
			}
			e = topologyCopy(*old)
		} else {
			e = domain.Entry{ID: id, Rate: 1, Enabled: true}
		}
		topologyAssign(&e.Name, in.Name)
		topologyAssign(&e.InboundID, in.InboundID)
		topologyAssign(&e.LineMemberID, in.LineMemberID)
		topologyAssign(&e.DisplayHost, in.DisplayHost)
		topologyAssign(&e.DisplayPort, in.DisplayPort)
		topologyAssign(&e.Rate, in.Rate)
		topologyAssign(&e.Sort, in.Sort)
		topologyAssign(&e.Enabled, in.Enabled)
		topologyAssign(&e.Tags, in.Tags)
		topologyAssign(&e.Region, in.Region)
		if in.LineID != nil {
			e.LineID = topologyID(in.LineID)
		}
		if in.InboundRef != "" {
			e.InboundID, err = p.resolve(in.InboundRef, "inbound")
			if err != nil {
				return nil, err
			}
		}
		if in.LineRef != "" {
			line, e2 := p.resolve(in.LineRef, "line")
			if e2 != nil {
				return nil, e2
			}
			e.LineID = &line
		}
		if in.InboundRef != "" && (in.LineRef != "" || e.LineID != nil) {
			return nil, ErrTopologyUnsupported
		}
		if op.ID == 0 && e.Rate == 0 {
			e.Rate = 1
		}
		e.Name = strings.TrimSpace(e.Name)
		e.Region = strings.ToUpper(strings.TrimSpace(e.Region))
		if e.Name == "" || len(e.Name) > 128 || !spec.PublicLabel(e.Name) || math.IsNaN(e.Rate) || math.IsInf(e.Rate, 0) || e.Rate < 0 {
			return nil, errors.New("entry name and a nonnegative rate are required")
		}
		if e.LineID == nil {
			e.LineMemberID = ""
		}
		p.entries[id] = &e
		p.view.entries[id] = &e
	}
	for _, id := range topologyKeys(p.inbounds) {
		if err = p.validateInbound(ctx, tx, p.inbounds[id]); err != nil {
			return nil, err
		}
	}
	for _, id := range topologyKeys(p.lines) {
		l := p.lines[id]
		ib := p.view.inbounds[l.TargetInboundID]
		if ib == nil {
			return nil, ErrNotFound
		}
		if ib.Settings.Reverse != nil || ib.Settings.WireGuardSlot != 0 {
			return nil, ErrTopologyUnsupported
		}
		fs, ep, e := compileOrdinaryLineModel(l, ib, func(node int64) (*lineNode, error) { return p.node(ctx, tx, node, id) })
		if e != nil {
			return nil, e
		}
		p.generated[id] = fs
		p.endpoints[id] = ep
		if !p.explicitLines[id] {
			if skip, e := allowRolloutRecompileTx(ctx, tx, l, fs, ep); e != nil {
				return nil, e
			} else if skip {
				delete(p.lines, id)
				delete(p.generated, id)
				p.view.lines[id] = p.base.lines[id]
			}
		}
	}
	// Validate all generated listeners together, including creations with only
	// temporary IDs; ordering operations cannot hide a collision.
	byNode := map[int64][]NodeForward{}
	for _, id := range topologyKeys(p.generated) {
		for node, fs := range p.generated[id] {
			for _, f := range fs {
				for _, other := range byNode[node] {
					for _, a := range lineProtocols(f.Protocol) {
						for _, b := range lineProtocols(other.Protocol) {
							if a == b && f.Port == other.Port && lineListenOverlap(f.Listen, other.Listen) {
								return nil, fmt.Errorf("server %d has conflicting draft listeners on port %d", node, f.Port)
							}
						}
					}
				}
				byNode[node] = append(byNode[node], f)
			}
		}
	}
	// Existing bound entries must also survive the final model, even when a
	// different operation moves them away from a first-group member being removed.
	for _, id := range topologyKeys(p.view.entries) {
		e := p.view.entries[id]
		if p.entries[id] != nil || (e.LineID != nil && p.lines[*e.LineID] != nil) {
			if err = p.resolveEntry(ctx, tx, e); err != nil {
				return nil, err
			}
			if p.entries[id] == nil {
				p.entries[id] = e
			}
		}
	}
	for _, op := range d.Operations {
		id := p.keys[op.Key].ID
		c := TopologyChange{Key: op.Key, Kind: op.Kind, ID: op.ID}
		switch op.Kind {
		case "inbound":
			if old := base.inbounds[id]; old != nil {
				c.Before = base.inboundView(old)
			}
			c.After = p.view.inboundView(p.inbounds[id])
		case "line":
			if old := base.lines[id]; old != nil {
				c.Before = old
			}
			c.After = p.view.lines[id]
			c.Forwards = p.lineDiff(id)
		case "entry":
			if old := base.entries[id]; old != nil {
				c.Before = base.entryView(old)
			}
			c.After = p.view.entryView(p.entries[id])
		}
		p.preview.Changes = append(p.preview.Changes, c)
	}
	for _, id := range topologyKeys(p.lines) {
		if !p.explicitLines[id] {
			p.preview.Changes = append(p.preview.Changes, TopologyChange{Key: fmt.Sprintf("dependent-line-%d", id), Kind: "line", ID: id, Before: base.lines[id], After: p.lines[id], Forwards: p.lineDiff(id)})
		}
	}
	explicitEntries := map[int64]bool{}
	for _, op := range d.Operations {
		if op.Kind == "entry" {
			explicitEntries[p.keys[op.Key].ID] = true
		}
	}
	for _, id := range topologyKeys(p.entries) {
		if !explicitEntries[id] && !reflect.DeepEqual(base.entries[id], p.entries[id]) {
			p.preview.Changes = append(p.preview.Changes, TopologyChange{Key: fmt.Sprintf("dependent-entry-%d", id), Kind: "entry", ID: id, Before: base.entryView(base.entries[id]), After: p.view.entryView(p.entries[id])})
		}
	}
	p.preview.Revision = topologyDigest([]any{d.ID, d.Version, d.Operations, base.digest, p.preview.Changes})
	return p, nil
}
func (p *topologyPlan) lineDiff(id int64) []LineNodeChange {
	out := []LineNodeChange{}
	for _, node := range topologyKeys(p.base.nodes) {
		before := []NodeForward{}
		for _, f := range p.base.forwards[node] {
			if f.LineID == id {
				before = append(before, f)
			}
		}
		a, b := expectedLineForwards(before), expectedLineForwards(p.generated[id][node])
		if !reflect.DeepEqual(a, b) {
			out = append(out, LineNodeChange{NodeID: node, Name: p.base.nodes[node].Name, Before: a, After: b})
		}
	}
	return out
}
func (p *topologyPlan) node(ctx context.Context, tx *sql.Tx, id, lineID int64) (*lineNode, error) {
	base, e := loadLineNode(ctx, tx, id, lineID)
	if e != nil {
		return nil, e
	}
	base.binds = nil
	for _, ib := range p.view.inbounds {
		if ib.NodeID == id && ib.Enabled {
			listen := ib.Listen
			if listen == "" && ib.IngressID != nil {
				if g := p.view.ingresses[*ib.IngressID]; g != nil {
					listen = g.BindIP
				}
			}
			base.binds = append(base.binds, lineInboundBinds(ib, listen)...)
		}
	}
	for _, f := range p.base.forwards[id] {
		if p.lines[f.LineID] != nil || (lineID != 0 && f.LineID == lineID) {
			continue
		}
		listen := f.Listen
		if listen == "" && f.IngressID != "" {
			gid, _ := strconv.ParseInt(f.IngressID, 10, 64)
			if g := p.view.ingresses[gid]; g != nil {
				listen = g.BindIP
			}
		}
		for _, proto := range lineProtocols(f.Protocol) {
			base.binds = append(base.binds, lineBind{proto, f.Port, listen})
		}
	}
	rows, e := tx.QueryContext(ctx, `SELECT w.listen_port FROM node_wireguard_links w JOIN inbounds i ON i.id=w.user_inbound_id WHERE w.transit_id=? AND i.enabled=1`, id)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var port int
		if e = rows.Scan(&port); e != nil {
			break
		}
		base.binds = append(base.binds, lineBind{"udp", port, ""})
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return nil, e
	}
	reservations, e := lineRolloutReservationsTx(ctx, tx, id, lineID)
	if e != nil {
		return nil, e
	}
	for _, r := range reservations {
		base.binds = append(base.binds, lineBind{"tcp", r.forward.Port, r.forward.Listen})
	}
	return base, nil
}
func (p *topologyPlan) resolveEntry(ctx context.Context, tx *sql.Tx, e *domain.Entry) error {
	if e.LineID != nil {
		if p.view.unpublished[*e.LineID] {
			return ErrLineRolloutConflict
		}
		l := p.view.lines[*e.LineID]
		if l == nil {
			return ErrNotFound
		}
		if l.PathRoute {
			return ErrPathRouteOwned
		}
		if target := p.view.inbounds[l.TargetInboundID]; target != nil && (target.Settings.Reverse != nil || target.Settings.WireGuardSlot != 0) {
			return ErrTopologyUnsupported
		}
		ep := p.endpoints[l.ID]
		if ep == nil {
			ib := p.view.inbounds[l.TargetInboundID]
			if ib == nil {
				return ErrNotFound
			}
			_, got, err := compileOrdinaryLineModel(l, ib, func(id int64) (*lineNode, error) { return p.node(ctx, tx, id, l.ID) })
			if err != nil {
				return err
			}
			ep = got
		}
		if e.LineMemberID == "" && len(ep) == 1 {
			for member := range ep {
				e.LineMemberID = member
			}
		}
		v, ok := ep[e.LineMemberID]
		if !ok || v.Host == "" {
			return errors.New("select a member of the line's first group with a public endpoint")
		}
		e.InboundID, e.DisplayHost, e.DisplayPort = l.TargetInboundID, v.Host, v.Port
	} else {
		ib := p.view.inbounds[e.InboundID]
		if ib == nil {
			return ErrNotFound
		}
		if _, err := p.node(ctx, tx, ib.NodeID, 0); err != nil {
			return err
		}
		if p.view.protectedInbound[ib.ID] || ib.Settings.Reverse != nil || ib.Settings.WireGuardSlot != 0 {
			return ErrTopologyUnsupported
		}
		defaultPort := e.DisplayPort == 0
		if defaultPort {
			e.DisplayPort = ib.Port
		}
		if ib.IngressID != nil {
			g := p.view.ingresses[*ib.IngressID]
			if g == nil {
				return ErrNotFound
			}
			if e.DisplayHost == "" {
				e.DisplayHost = g.ClientHost()
			}
			if defaultPort {
				e.DisplayPort = g.EntryPort(ib.Port)
			}
		} else if e.DisplayHost == "" {
			if sp := ib.Spec(); sp.TLS != nil && sp.TLS.Mode == spec.TLSStandard && sp.TLS.ServerName != "" {
				e.DisplayHost = sp.TLS.ServerName
			} else if n := p.view.nodes[ib.NodeID]; n != nil {
				e.DisplayHost = n.Domain
				if e.DisplayHost == "" {
					e.DisplayHost = n.PublicAddr
				}
			}
		}
	}
	if e.DisplayHost == "" || !spec.Plain(e.DisplayHost) || e.DisplayPort < 1 || e.DisplayPort > 65535 {
		return errors.New("entry requires a valid public display address and port")
	}
	return validateEntryCategory(ctx, tx, e)
}
func (s *Store) PreviewTopologyChangeset(ctx context.Context, id string, version int64) (*TopologyPreview, error) {
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	d, e := scanTopologyDraft(tx.QueryRowContext(ctx, "SELECT "+topologyCols+" FROM topology_changesets WHERE id=?", id))
	if e != nil {
		return nil, e
	}
	if d.Version != version || d.Status != "draft" {
		return nil, ErrTopologyConflict
	}
	p, e := prepareTopologyTx(ctx, tx, d)
	if e != nil {
		return nil, e
	}
	return p.preview, nil
}

// checkTopologyPrivateAccess validates the final node view with the same spec
// policy validators used by the ordinary inbound editor.
func (p *topologyPlan) checkTopologyPrivateAccess(ctx context.Context, tx *sql.Tx, id int64) error {
	var outs, routes, dns, overrides, def string
	if e := tx.QueryRowContext(ctx, `SELECT outbounds_json,routes_json,dns_json,overrides_json,default_outbound FROM nodes WHERE id=?`, id).Scan(&outs, &routes, &dns, &overrides, &def); e != nil {
		return e
	}
	n := spec.Node{DefaultOutbound: def, Overrides: map[string]json.RawMessage{}}
	for _, x := range []struct {
		raw string
		dst any
	}{{outs, &n.Outbounds}, {routes, &n.Routes}, {dns, &n.DNS}, {overrides, &n.Overrides}} {
		if e := json.Unmarshal([]byte(x.raw), x.dst); e != nil {
			return e
		}
	}
	for _, ib := range p.view.inbounds {
		if ib.NodeID == id && ib.Enabled {
			n.Inbounds = append(n.Inbounds, ib.Spec())
		}
	}
	rows, e := tx.QueryContext(ctx, `SELECT id,user_inbound_id FROM node_reverse_links WHERE exit_id=?`, id)
	if e != nil {
		return e
	}
	for rows.Next() {
		var rid string
		var inbound int64
		if e = rows.Scan(&rid, &inbound); e != nil {
			break
		}
		ib := p.view.inbounds[inbound]
		if ib == nil {
			e = ErrNotFound
			break
		}
		n.ReverseClients = append(n.ReverseClients, spec.ReverseClient{ID: rid, PrivateAccess: ib.Settings.PrivateAccess})
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return e
	}
	rows, e = tx.QueryContext(ctx, "SELECT "+wireGuardColumns+" FROM node_wireguard_links WHERE exit_id=? OR transit_id=?", id, id)
	if e != nil {
		return e
	}
	for rows.Next() {
		l, e2 := scanWireGuard(rows)
		if e2 != nil {
			e = e2
			break
		}
		ib := p.view.inbounds[l.UserInboundID]
		if ib == nil {
			e = ErrNotFound
			break
		}
		cfg, e2 := l.Config(id == l.ExitID, ib.Settings.PrivateAccess)
		if e2 != nil {
			e = e2
			break
		}
		n.WireGuardLinks = append(n.WireGuardLinks, cfg)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return e
	}
	if e = n.ValidateWireGuardLinks(); e != nil {
		return e
	}
	if e = n.ValidatePrivateAccessNode(); e != nil {
		return e
	}
	return n.ValidatePrivateAccessRouting(p.view.candidates[id])
}
func (p *topologyPlan) validateInbound(ctx context.Context, tx *sql.Tx, ib *domain.Inbound) error {
	if _, e := p.node(ctx, tx, ib.NodeID, 0); e != nil {
		return e
	}
	if e := ib.Spec().Validate(); e != nil {
		return e
	}
	gs := []Ingress{}
	for _, g := range p.view.ingresses {
		if g.NodeID == ib.NodeID {
			gs = append(gs, *g)
		}
	}
	if e := ValidateInboundIngress(ib, gs); e != nil {
		return e
	}
	if e := ValidateInboundCore(ib, p.view.nodes[ib.NodeID], p.view.candidates[ib.NodeID], gs); e != nil {
		return e
	}
	if e := p.checkTopologyPrivateAccess(ctx, tx, ib.NodeID); e != nil {
		return e
	}
	if ib.ID > 0 {
		if e := guardLegacyAccessTx(ctx, tx, ib); e != nil {
			return e
		}
	}
	if e := checkRolloutInboundTx(ctx, tx, ib); e != nil {
		return e
	}
	for _, other := range p.view.inbounds {
		if other.ID != ib.ID && other.NodeID == ib.NodeID && other.Tag == ib.Tag {
			return errors.New("inbound tag is already used on this server")
		}
	}
	if !ib.Enabled {
		return nil
	}
	listen := ib.Listen
	if listen == "" && ib.IngressID != nil {
		if g := p.view.ingresses[*ib.IngressID]; g != nil {
			listen = g.BindIP
		}
	}
	node, e := p.node(ctx, tx, ib.NodeID, 0)
	if e != nil {
		return e
	}
	own := lineInboundBinds(ib, listen)
	skip := map[lineBind]int{}
	for _, b := range own {
		skip[b]++
	}
	for _, b := range node.binds {
		if skip[b] > 0 {
			skip[b]--
			continue
		}
		for _, a := range own {
			if a.protocol == b.protocol && a.port == b.port && lineListenOverlap(a.listen, b.listen) {
				return fmt.Errorf("inbound listener conflicts on %s port %d", a.protocol, a.port)
			}
		}
	}
	for _, other := range p.view.inbounds {
		if other.ID != ib.ID && other.NodeID == ib.NodeID && other.Tag == ib.Tag {
			return errors.New("inbound tag is already used on this server")
		}
	}
	return nil
}
