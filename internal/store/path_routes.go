package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

type PathBranch struct {
	ID           string `json:"id"`
	PathID       int64  `json:"path_id"`
	PathRevision int64  `json:"path_revision"`
	Weight       int    `json:"weight"`
	Ports        []int  `json:"ports"`
}
type PathRoute struct {
	Health            *PathRouteHealth `json:"health,omitempty"`
	ID                int64            `json:"id"`
	Version           int64            `json:"version"`
	Name              string           `json:"name"`
	Entry             LineMember       `json:"entry"`
	TargetInboundID   int64            `json:"target_inbound_id"`
	TargetAddressMode string           `json:"target_address_mode"`
	Strategy          string           `json:"strategy"`
	Branches          []PathBranch     `json:"branches"`
	PreviewRevision   string           `json:"preview_revision,omitempty"`
	ChangeID          string           `json:"change_id,omitempty"`
}
type PathRoutePreview struct {
	Revision     string           `json:"revision"`
	Draft        PathRoute        `json:"draft"`
	Changes      []LineNodeChange `json:"changes"`
	Dependencies []LineDependency `json:"dependencies"`
	Edges        []LineEdge       `json:"edges"`
	Uses         []LineUse        `json:"uses"`
	Endpoints    []LineEndpoint   `json:"endpoints"`
}

func isPathRouteTx(ctx context.Context, tx *sql.Tx, id int64) (bool, error) {
	var owned bool
	e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM forward_path_routes WHERE line_id=?)`, id).Scan(&owned)
	return owned, e
}
func pathRouteTx(ctx context.Context, tx *sql.Tx, id int64) (*PathRoute, error) {
	var raw string
	var p PathRoute
	e := tx.QueryRowContext(ctx, `SELECT r.spec_json,l.id,l.version,l.name,l.target_inbound_id,l.target_address_mode FROM forward_path_routes r JOIN forward_lines l ON l.id=r.line_id WHERE r.line_id=?`, id).Scan(&raw, &p.ID, &p.Version, &p.Name, &p.TargetInboundID, &p.TargetAddressMode)
	if e != nil {
		return nil, wrapNotFound(e)
	}
	// Line version also advances on automatic address recompiles.
	var saved PathRoute
	if e = json.Unmarshal([]byte(raw), &saved); e != nil {
		return nil, e
	}
	saved.ID, saved.Version, saved.Name, saved.TargetInboundID, saved.TargetAddressMode = p.ID, p.Version, p.Name, p.TargetInboundID, p.TargetAddressMode
	return &saved, nil
}
func (s *Store) PathRoute(ctx context.Context, id int64) (*PathRoute, error) {
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	return pathRouteTx(ctx, tx, id)
}
func (s *Store) ListPathRoutes(ctx context.Context) ([]PathRoute, error) {
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	rows, e := tx.QueryContext(ctx, `SELECT line_id FROM forward_path_routes ORDER BY line_id`)
	if e != nil {
		return nil, e
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			break
		}
		ids = append(ids, id)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return nil, e
	}
	out := []PathRoute{}
	for _, id := range ids {
		p, e := pathRouteTx(ctx, tx, id)
		if e != nil {
			return nil, e
		}
		out = append(out, *p)
	}
	return out, nil
}
func normalizePathRoute(p *PathRoute) error {
	PreservePathRouteHealth(p, nil)
	normal, err := p.Health.Normalized()
	if err != nil {
		return err
	}
	p.Health = &normal
	var e error
	p.Name, e = pathName(p.Name)
	if e != nil {
		return e
	}
	if e = validateAddressMode(p.TargetAddressMode); e != nil {
		return e
	}
	if p.Strategy == "" {
		p.Strategy = spec.BalanceFailover
	}
	if p.Strategy != spec.BalanceFailover && p.Strategy != spec.BalanceRoundRobin {
		return errors.New("choose failover or roundrobin")
	}
	if len(p.Branches) < 1 || len(p.Branches) > spec.MaxForwardHops {
		return errors.New("a route requires one to eight complete paths")
	}
	if p.Entry.ID == "" {
		p.Entry.ID = pathKey(rand.Text())
	}
	if len(p.Entry.ID) > 24 || !spec.ValidTag(p.Entry.ID) {
		return errors.New("invalid entry member ID")
	}
	if p.Entry.NodeID <= 0 || p.Entry.Port < 1 || p.Entry.Port > 65535 || p.Entry.AddressMode == AddressModeInternal {
		return errors.New("the route entry needs a public listener and explicit port")
	}
	if e = validateAddressMode(p.Entry.AddressMode); e != nil {
		return e
	}
	p.Entry.Weight = 1
	ids := map[string]bool{}
	for i := range p.Branches {
		b := &p.Branches[i]
		if b.ID == "" {
			b.ID = pathKey(rand.Text())
		}
		if len(b.ID) > 24 || !spec.ValidTag(b.ID) || ids[b.ID] {
			return errors.New("branch IDs must be unique valid tags of at most 24 characters")
		}
		ids[b.ID] = true
		if b.PathID <= 0 || b.PathRevision < 1 {
			return errors.New("every branch requires an explicit path revision")
		}
		if b.Weight == 0 {
			b.Weight = 1
		}
		if b.Weight < 1 || b.Weight > 100 {
			return errors.New("branch weight must be between one and 100")
		}
		for _, port := range b.Ports {
			if port < 1 || port > 65535 {
				return errors.New("every dedicated listener requires a port between 1 and 65535")
			}
		}
	}
	return nil
}
func pathRouteLineTx(ctx context.Context, tx *sql.Tx, p *PathRoute) (*ForwardLine, error) {
	if e := normalizePathRoute(p); e != nil {
		return nil, e
	}
	l := &ForwardLine{ID: p.ID, Version: p.Version, Name: p.Name, Protocol: "tcp", Backend: "relay", TargetInboundID: p.TargetInboundID, TargetAddressMode: p.TargetAddressMode, PathRoute: true, Groups: []LineGroup{{Strategy: spec.BalanceFailover, Members: []LineMember{p.Entry}}}}
	for _, b := range p.Branches {
		path, e := pathRevisionTx(ctx, tx, b.PathID, b.PathRevision)
		if e != nil {
			return nil, e
		}
		if len(b.Ports) != len(path.Hops) {
			return nil, errors.New("dedicated ports must match the pinned path's expanded hop count")
		}
		g := LineGroup{Strategy: spec.BalanceFailover, Members: []LineMember{}}
		for i, h := range path.Hops {
			if h.NodeID == p.Entry.NodeID {
				return nil, errors.New("a path cannot revisit its route entry server")
			}
			g.Members = append(g.Members, LineMember{ID: pathKey(b.ID, h.Key), NodeID: h.NodeID, Port: b.Ports[i], IngressID: h.IngressID, Weight: 1, AddressMode: h.AddressMode})
		}
		if len(g.Members) == 0 {
			return nil, errors.New("empty path cannot be selected")
		}
		l.Groups = append(l.Groups, g)
	}
	return l, nil
}

// Groups record membership only for composed routes. Each branch is a complete
// chain, never the ordinary compiler's Cartesian product of adjacent groups.
func compilePathRouteTx(ctx context.Context, tx *sql.Tx, l *ForwardLine, p *PathRoute) (map[int64][]NodeForward, map[string]LineEndpoint, error) {
	// During a save the line row still contains the previous landing. Compile
	// the transaction's proposed line intent, not metadata read from that row.
	next := *p
	next.Name, next.Version = l.Name, l.Version
	next.TargetInboundID, next.TargetAddressMode = l.TargetInboundID, l.TargetAddressMode
	p = &next
	logical, e := pathRouteLineTx(ctx, tx, p)
	if e != nil {
		return nil, nil, e
	}
	logical.ID = l.ID
	logical.Version = l.Version
	ib, e := scanInbound(tx.QueryRowContext(ctx, "SELECT "+inboundCols+" FROM inbounds WHERE id=?", l.TargetInboundID))
	if e != nil {
		return nil, nil, e
	}
	if ib.Settings.Reverse != nil || ib.Settings.WireGuardSlot != 0 || ib.Settings.AcceptProxyProtocol {
		return nil, nil, errors.New("complete path routes currently require an ordinary TCP landing without reverse, WireGuard or PROXY protocol")
	}
	protocol, e := LineProtocol(ib)
	if e != nil {
		return nil, nil, e
	}
	if protocol != "tcp" {
		return nil, nil, errors.New("complete path routes support TCP only")
	}
	root := *logical
	root.Groups = root.Groups[:1]
	generated, endpoints, e := compileOrdinaryLine(ctx, tx, &root)
	if e != nil {
		return nil, nil, e
	}
	entry := generated[p.Entry.NodeID][0]
	var heads []spec.ForwardTarget
	for i, b := range p.Branches {
		chain := *logical
		chain.Groups = nil
		for _, m := range logical.Groups[i+1].Members {
			chain.Groups = append(chain.Groups, LineGroup{Strategy: spec.BalanceFailover, Members: []LineMember{m}})
		}
		branch, _, e := compileOrdinaryLine(ctx, tx, &chain)
		if e != nil {
			return nil, nil, e
		}
		for id, fs := range branch {
			generated[id] = append(generated[id], fs...)
		}
		first := chain.Groups[0].Members[0]
		n, e := loadLineNode(ctx, tx, first.NodeID, l.ID)
		if e != nil {
			return nil, nil, e
		}
		var ingress *Ingress
		if first.IngressID != nil {
			ingress = n.ingresses[*first.IngressID]
		}
		host, port := lineAddress(n.node, ingress, first.Port, true)
		if first.AddressMode == AddressModeInternal {
			host, e = internalAddress(n.node.InternalAddr)
			if e != nil {
				return nil, nil, e
			}
			port = first.Port
		}
		address, e := lineHostPort(host, port)
		if e != nil {
			return nil, nil, e
		}
		heads = append(heads, spec.ForwardTarget{Target: address, Weight: b.Weight})
	}
	if len(heads) > 1 {
		n, e := loadLineNode(ctx, tx, p.Entry.NodeID, l.ID)
		if e != nil {
			return nil, nil, e
		}
		if !lineVersionOK(n.node, "v0.49.0") {
			return nil, nil, errors.New("complete path selection requires bosun v0.49.0 or newer")
		}
	}
	entry.Target, entry.Weight, entry.Balance = heads[0].Target, heads[0].Weight, p.Strategy
	entry.Targets = heads[1:]
	entry.Tag = lineRuntimeTag(l, p.Entry, entry.Forward)
	if e = entry.ValidateTargets(); e != nil {
		return nil, nil, e
	}
	generated[p.Entry.NodeID] = []NodeForward{entry}
	for id, fs := range generated {
		for i, f := range fs {
			for _, other := range fs[:i] {
				if f.Port == other.Port && lineListenOverlap(f.Listen, other.Listen) {
					return nil, nil, fmt.Errorf("server %d has overlapping dedicated TCP listeners on port %d", id, f.Port)
				}
			}
		}
	}
	l.Groups = logical.Groups
	l.PathRoute = true
	return generated, endpoints, nil
}
func pathRouteSpecWriteTx(ctx context.Context, tx *sql.Tx, p *PathRoute) error {
	safe := *p
	safe.PreviewRevision, safe.ChangeID = "", ""
	raw, e := json.Marshal(safe)
	if e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO forward_path_routes(line_id,spec_json) VALUES(?,?) ON CONFLICT(line_id) DO UPDATE SET spec_json=excluded.spec_json`, p.ID, string(raw)); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM forward_path_route_refs WHERE line_id=?`, p.ID); e != nil {
		return e
	}
	for _, b := range p.Branches {
		if _, e = tx.ExecContext(ctx, `INSERT INTO forward_path_route_refs(line_id,branch_id,path_id,revision) VALUES(?,?,?,?)`, p.ID, b.ID, b.PathID, b.PathRevision); e != nil {
			return e
		}
	}
	return nil
}
func (s *Store) SavePathRoute(ctx context.Context, p *PathRoute) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	next := *p
	next.Branches = append([]PathBranch(nil), p.Branches...)
	if next.ID > 0 {
		old, err := pathRouteTx(ctx, tx, next.ID)
		if err != nil {
			return err
		}
		PreservePathRouteHealth(&next, old)
	} else {
		PreservePathRouteHealth(&next, nil)
	}
	if e = normalizePathRoute(&next); e != nil {
		return e
	}
	if p.PreviewRevision != "" {
		preview, e := previewPathRouteTx(ctx, tx, &next)
		if e != nil {
			return e
		}
		if preview.Revision != p.PreviewRevision {
			return ErrLinePreview
		}
	}
	if next.ID == 0 {
		res, e := tx.ExecContext(ctx, `INSERT INTO forward_lines(name,version,protocol,backend,target_inbound_id,groups_json,updated_at,target_address_mode) VALUES(?,1,'tcp','relay',?,'[]',?,?)`, next.Name, next.TargetInboundID, now(), next.TargetAddressMode)
		if e != nil {
			return e
		}
		next.ID, _ = res.LastInsertId()
		next.Version = 1
	} else {
		old, e := pathRouteTx(ctx, tx, next.ID)
		if e != nil {
			return e
		}
		if old.Version != next.Version {
			return ErrLineConflict
		}
		next.Version++
	}
	l, e := pathRouteLineTx(ctx, tx, &next)
	if e != nil {
		return e
	}
	if e = pathRouteSpecWriteTx(ctx, tx, &next); e != nil {
		return e
	}
	if e = saveLineTx(ctx, tx, l, "save"); e != nil {
		return e
	}
	if e = tx.QueryRowContext(ctx, `SELECT latest_change_id FROM line_change_scopes WHERE line_id=?`, next.ID).Scan(&next.ChangeID); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	next.PreviewRevision = ""
	*p = next
	return nil
}
func (s *Store) DeletePathRoute(ctx context.Context, id, version int64) (string, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	if e = checkLineRolloutWriteTx(ctx, tx, id); e != nil {
		return "", e
	}
	p, e := pathRouteTx(ctx, tx, id)
	if e != nil {
		return "", e
	}
	if p.Version != version {
		return "", ErrLineConflict
	}
	var uses int
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM entries WHERE line_id=?`, id).Scan(&uses); e != nil {
		return "", e
	}
	if uses > 0 {
		return "", ErrLineInUse
	}
	l, e := scanLine(tx.QueryRowContext(ctx, "SELECT "+lineCols+" FROM forward_lines WHERE id=?", id))
	if e != nil {
		return "", e
	}
	before, e := lineOwnedSnapshotTx(ctx, tx, id)
	if e != nil {
		return "", e
	}
	if e = replaceLineForwardsTx(ctx, tx, id, nil); e != nil {
		return "", e
	}
	l.Version++
	if e = recordLineChangeTx(ctx, tx, l, "delete", before); e != nil {
		return "", e
	}
	var change string
	if e = tx.QueryRowContext(ctx, `SELECT latest_change_id FROM line_change_scopes WHERE line_id=?`, id).Scan(&change); e != nil {
		return "", e
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM forward_lines WHERE id=?`, id); e != nil {
		return "", e
	}
	return change, tx.Commit()
}
func (s *Store) PreviewPathRoute(ctx context.Context, p *PathRoute) (*PathRoutePreview, error) {
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	return previewPathRouteTx(ctx, tx, p)
}
func previewPathRouteTx(ctx context.Context, tx *sql.Tx, p *PathRoute) (*PathRoutePreview, error) {
	draft := *p
	draft.Branches = append([]PathBranch(nil), p.Branches...)
	draft.PreviewRevision, draft.ChangeID = "", ""
	if draft.ID > 0 {
		old, e := pathRouteTx(ctx, tx, draft.ID)
		if e != nil {
			return nil, e
		}
		if old.Version != draft.Version {
			return nil, ErrLineConflict
		}
		PreservePathRouteHealth(&draft, old)
	}
	l, e := pathRouteLineTx(ctx, tx, &draft)
	if e != nil {
		return nil, e
	}
	if l.ID == 0 {
		l.ID = -1
	}
	generated, endpoints, e := compilePathRouteTx(ctx, tx, l, &draft)
	if e != nil {
		return nil, e
	}
	if _, e = linePublishedMembersTx(ctx, tx, l.ID, endpoints); e != nil {
		return nil, e
	}
	out := &PathRoutePreview{Draft: draft, Changes: []LineNodeChange{}, Dependencies: []LineDependency{}, Edges: []LineEdge{}, Uses: []LineUse{}, Endpoints: []LineEndpoint{}}
	for _, ep := range endpoints {
		out.Endpoints = append(out.Endpoints, ep)
	}
	deps := emptyLinePreview()
	if e = pathRouteDependenciesTx(ctx, tx, l, deps); e != nil {
		return nil, e
	}
	out.Dependencies, out.Edges, out.Uses = deps.Dependencies, deps.Edges, deps.Uses
	rows, e := tx.QueryContext(ctx, `SELECT id,name,forwards_json FROM nodes ORDER BY id`)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var id int64
		var name, raw string
		if e = rows.Scan(&id, &name, &raw); e != nil {
			break
		}
		var all []NodeForward
		if e = json.Unmarshal([]byte(raw), &all); e != nil {
			break
		}
		var before []NodeForward
		for _, f := range all {
			if f.LineID == draft.ID && draft.ID > 0 {
				before = append(before, f)
			}
		}
		b, a := expectedLineForwards(before), expectedLineForwards(generated[id])
		br, _ := json.Marshal(b)
		ar, _ := json.Marshal(a)
		if string(br) != string(ar) {
			out.Changes = append(out.Changes, LineNodeChange{id, name, b, a})
		}
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return nil, e
	}
	raw, _ := json.Marshal(out)
	sum := sha256.Sum256(raw)
	out.Revision = hex.EncodeToString(sum[:])
	return out, nil
}
func pathRouteDependenciesTx(ctx context.Context, tx *sql.Tx, l *ForwardLine, p *LinePreview) error {
	var landing int64
	if e := tx.QueryRowContext(ctx, `SELECT node_id FROM inbounds WHERE id=?`, l.TargetInboundID).Scan(&landing); e != nil {
		return e
	}
	for i, g := range l.Groups {
		for j, m := range g.Members {
			var name string
			if e := tx.QueryRowContext(ctx, `SELECT name FROM nodes WHERE id=?`, m.NodeID).Scan(&name); e != nil {
				return e
			}
			p.Dependencies = append(p.Dependencies, LineDependency{m.NodeID, name, "relay", m.ID, i})
			if i == 0 {
				for _, b := range l.Groups[1:] {
					p.Edges = append(p.Edges, LineEdge{m.NodeID, b.Members[0].NodeID, "path"})
				}
			} else {
				next := landing
				if j+1 < len(g.Members) {
					next = g.Members[j+1].NodeID
				}
				p.Edges = append(p.Edges, LineEdge{m.NodeID, next, "forward"})
			}
		}
	}
	var name string
	if e := tx.QueryRowContext(ctx, `SELECT name FROM nodes WHERE id=?`, landing).Scan(&name); e != nil {
		return e
	}
	p.Dependencies = append(p.Dependencies, LineDependency{landing, name, "landing", "", len(l.Groups)})
	// The existing use walk is shared without letting it invent Cartesian edges.
	originalDeps, originalEdges := p.Dependencies, p.Edges
	plain := *l
	plain.PathRoute = false
	if e := lineUsesTx(ctx, tx, &plain, p); e != nil {
		return e
	}
	p.Dependencies, p.Edges = originalDeps, originalEdges
	return nil
}

// PathRouteAdjacency returns listener identities, not physical-node edges: two
// branches may safely have distinct listeners on the same physical server.
func PathRouteAdjacency(l *ForwardLine) map[string][]string {
	out := map[string][]string{}
	if len(l.Groups) == 0 {
		return out
	}
	root := l.Groups[0].Members[0].ID
	for _, g := range l.Groups[1:] {
		if len(g.Members) == 0 {
			continue
		}
		out[root] = append(out[root], g.Members[0].ID)
		for i, m := range g.Members {
			out[m.ID] = nil
			if i+1 < len(g.Members) {
				out[m.ID] = []string{g.Members[i+1].ID}
			}
		}
	}
	return out
}
