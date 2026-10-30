package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"sort"
)

// LinePreview compares saved and proposed desired forwards. It does not claim
// to contain bosun's rendered files or prove runtime/connection health.
type LinePreview struct {
	Revision     string           `json:"revision"`
	Draft        ForwardLine      `json:"draft"`
	Changes      []LineNodeChange `json:"changes"`
	Dependencies []LineDependency `json:"dependencies"`
	Edges        []LineEdge       `json:"edges"`
	Uses         []LineUse        `json:"uses"`
	Endpoints    []LineEndpoint   `json:"endpoints"`
}

// Explicit allowlist: never embed an inbound, user, runtime report or arbitrary
// future spec fields in an impact response.
type LineExpectedForward struct {
	PathHealth     *LineExpectedPathHealth `json:"path_health,omitempty"`
	MemberID       string                  `json:"member_id"`
	Listen         string                  `json:"listen"`
	Port           int                     `json:"port"`
	Protocol       string                  `json:"protocol"`
	Backend        string                  `json:"backend"`
	ProxyProtocol  bool                    `json:"proxy_protocol"`
	PreserveSource bool                    `json:"preserve_source"`
	Balance        string                  `json:"balance"`
	Targets        []LineExpectedTarget    `json:"targets"`
}
type LineExpectedTarget struct {
	Target string `json:"target"`
	Weight int    `json:"weight"`
}
type LineNodeChange struct {
	NodeID int64                 `json:"node_id"`
	Name   string                `json:"name"`
	Before []LineExpectedForward `json:"before"`
	After  []LineExpectedForward `json:"after"`
}
type LineDependency struct {
	NodeID   int64  `json:"node_id"`
	Name     string `json:"name"`
	Role     string `json:"role"`
	MemberID string `json:"member_id,omitempty"`
	Group    int    `json:"group"`
}
type LineEdge struct {
	From int64  `json:"from"`
	To   int64  `json:"to"`
	Kind string `json:"kind"`
}
type LineUse struct {
	EntryID  int64              `json:"entry_id"`
	Name     string             `json:"name"`
	Kind     string             `json:"kind"`
	Enabled  bool               `json:"enabled"`
	MemberID string             `json:"member_id,omitempty"`
	Before   *LineEntryEndpoint `json:"before,omitempty"`
	After    *LineEntryEndpoint `json:"after,omitempty"`
}
type LineEntryEndpoint struct {
	InboundID int64  `json:"inbound_id"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
}

// PreviewLine uses only SELECTs in one SQLite snapshot. Caller holds Topology,
// as for SaveLine. A generated member ID belongs only to the returned draft.
func (s *Store) PreviewLine(ctx context.Context, input *ForwardLine) (*LinePreview, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	return previewLineTx(ctx, tx, input)
}

// LineDependencies remains usable even when a saved line no longer compiles.
func (s *Store) LineDependencies(ctx context.Context, id int64) (*LinePreview, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	l, err := scanLine(tx.QueryRowContext(ctx, "SELECT "+lineCols+" FROM forward_lines WHERE id=?", id))
	if err != nil {
		return nil, err
	}
	p := emptyLinePreview()
	if err = lineDependenciesTx(ctx, tx, l, p); err != nil {
		return nil, err
	}
	// A dependency view never returns editor fields or credentials.
	p.Draft = ForwardLine{}
	return p, nil
}
func emptyLinePreview() *LinePreview {
	return &LinePreview{Changes: []LineNodeChange{}, Dependencies: []LineDependency{}, Edges: []LineEdge{}, Uses: []LineUse{}, Endpoints: []LineEndpoint{}}
}
func previewLineTx(ctx context.Context, tx *sql.Tx, input *ForwardLine) (*LinePreview, error) {
	if owned, err := isPathRouteTx(ctx, tx, input.ID); err != nil {
		return nil, err
	} else if owned {
		return nil, ErrPathRouteOwned
	}
	if input.PathRoute {
		return nil, ErrPathRouteOwned
	}

	// Strip response-only fields and copy nested slices before normalizing.
	next := ForwardLine{ID: input.ID, Name: input.Name, Version: input.Version, Protocol: input.Protocol, Backend: input.Backend, TargetInboundID: input.TargetInboundID, TargetAddressMode: input.TargetAddressMode}
	next.Groups = make([]LineGroup, len(input.Groups))
	for i, g := range input.Groups {
		next.Groups[i] = g
		next.Groups[i].Members = append([]LineMember(nil), g.Members...)
	}
	if next.ID > 0 {
		old, err := scanLine(tx.QueryRowContext(ctx, "SELECT "+lineCols+" FROM forward_lines WHERE id=?", next.ID))
		if err != nil {
			return nil, err
		}
		if old.Version != next.Version {
			return nil, ErrLineConflict
		}
		preserveLineAddressModes(&next, old)
	} else {
		next.ID, next.Version = 0, 0
	}
	if err := normalizeLine(&next); err != nil {
		return nil, err
	}
	compiled := next
	// Zero denotes manual forwards. A new line must not skip their port conflicts.
	if compiled.ID == 0 {
		compiled.ID = -1
	}
	generated, endpoints, err := compileLine(ctx, tx, &compiled)
	if err != nil {
		return nil, err
	}
	if _, err = linePublishedMembersTx(ctx, tx, compiled.ID, endpoints); err != nil {
		return nil, err
	}
	p := emptyLinePreview()
	p.Draft = next
	for _, g := range next.Groups {
		for _, m := range g.Members {
			if e, ok := endpoints[m.ID]; ok {
				p.Endpoints = append(p.Endpoints, e)
			}
		}
	}
	if err = lineDependenciesTx(ctx, tx, &next, p); err != nil {
		return nil, err
	}
	for i := range p.Uses {
		u := &p.Uses[i]
		if u.Kind == "line" {
			e := endpoints[u.MemberID]
			u.After = &LineEntryEndpoint{next.TargetInboundID, e.Host, e.Port}
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,name,forwards_json FROM nodes ORDER BY id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var name, raw string
		if err = rows.Scan(&id, &name, &raw); err != nil {
			break
		}
		var all []NodeForward
		if err = json.Unmarshal([]byte(raw), &all); err != nil {
			break
		}
		before := []NodeForward{}
		if next.ID > 0 {
			for _, f := range all {
				if f.LineID == next.ID {
					before = append(before, f)
				}
			}
		}
		b, a := expectedLineForwards(before), expectedLineForwards(generated[id])
		if !reflect.DeepEqual(b, a) {
			p.Changes = append(p.Changes, LineNodeChange{id, name, b, a})
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	// Bind the exact proposed input, expected changes, membership and uses. The
	// landing's private spec participates only in an opaque digest, never output.
	ib, err := scanInbound(tx.QueryRowContext(ctx, "SELECT "+inboundCols+" FROM inbounds WHERE id=?", next.TargetInboundID))
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(struct {
		Preview *LinePreview
		Landing any
	}{p, ib})
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	p.Revision = hex.EncodeToString(sum[:])
	return p, nil
}
func expectedLineForwards(fs []NodeForward) []LineExpectedForward {
	out := make([]LineExpectedForward, 0, len(fs))
	for _, f := range fs {
		targets := []LineExpectedTarget{}
		for _, hop := range f.Hops() {
			targets = append(targets, LineExpectedTarget{hop.Target, hop.Weight})
		}
		out = append(out, LineExpectedForward{PathHealth: projectLinePathHealth(f.PathHealth), MemberID: f.LineMemberID, Listen: f.Listen, Port: f.Port, Protocol: f.Protocol, Backend: f.Backend, ProxyProtocol: f.ProxyProtocol, PreserveSource: f.PreserveSource, Balance: f.BalanceMode(), Targets: targets})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MemberID < out[j].MemberID })
	return out
}

func lineDependenciesTx(ctx context.Context, tx *sql.Tx, l *ForwardLine, p *LinePreview) error {
	if owned, err := isPathRouteTx(ctx, tx, l.ID); err != nil {
		return err
	} else if owned {
		return pathRouteDependenciesTx(ctx, tx, l, p)
	}

	add := func(id int64, role, member string, group int) error {
		var name string
		if err := tx.QueryRowContext(ctx, `SELECT name FROM nodes WHERE id=?`, id).Scan(&name); err != nil {
			return wrapNotFound(err)
		}
		p.Dependencies = append(p.Dependencies, LineDependency{id, name, role, member, group})
		return nil
	}
	var landing int64
	if err := tx.QueryRowContext(ctx, `SELECT node_id FROM inbounds WHERE id=?`, l.TargetInboundID).Scan(&landing); err != nil {
		return wrapNotFound(err)
	}
	for i, g := range l.Groups {
		for _, m := range g.Members {
			if err := add(m.NodeID, "relay", m.ID, i); err != nil {
				return err
			}
			if i+1 == len(l.Groups) {
				p.Edges = append(p.Edges, LineEdge{m.NodeID, landing, "forward"})
			} else {
				for _, dest := range l.Groups[i+1].Members {
					p.Edges = append(p.Edges, LineEdge{m.NodeID, dest.NodeID, "candidate"})
				}
			}
		}
	}
	if err := add(landing, "landing", "", len(l.Groups)); err != nil {
		return err
	}
	// The visible landing can be the transit side of an owned reverse tunnel.
	rows, err := tx.QueryContext(ctx, `SELECT exit_id,'vless_reverse' FROM node_reverse_links WHERE user_inbound_id=? UNION SELECT exit_id,'wireguard' FROM node_wireguard_links WHERE user_inbound_id=? ORDER BY 1,2`, l.TargetInboundID, l.TargetInboundID)
	if err != nil {
		return err
	}
	var exits []LineEdge
	for rows.Next() {
		var e LineEdge
		e.From = landing
		if err = rows.Scan(&e.To, &e.Kind); err != nil {
			break
		}
		exits = append(exits, e)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, e := range exits {
		if err = add(e.To, "reverse_exit", "", len(l.Groups)+1); err != nil {
			return err
		}
		p.Edges = append(p.Edges, e)
	}
	return lineUsesTx(ctx, tx, l, p)
}
func lineUsesTx(ctx context.Context, tx *sql.Tx, l *ForwardLine, p *LinePreview) error {
	// UNION deduplicates and terminates even if old data contains a client cycle.
	rows, err := tx.QueryContext(ctx, `WITH RECURSIVE used(id) AS (
 SELECT id FROM entries WHERE line_id=?
 UNION SELECT e.id FROM entries e JOIN used u ON e.client_dialer_entry_id=u.id
 ) SELECT e.id,e.name,CASE WHEN e.line_id=? THEN 'line' ELSE 'client_chain' END,e.enabled,e.line_member_id,e.inbound_id,e.display_host,e.display_port
 FROM entries e JOIN used u ON u.id=e.id ORDER BY e.id`, l.ID, l.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var u LineUse
		var endpoint LineEntryEndpoint
		if err = rows.Scan(&u.EntryID, &u.Name, &u.Kind, &u.Enabled, &u.MemberID, &endpoint.InboundID, &endpoint.Host, &endpoint.Port); err != nil {
			return err
		}
		if u.Kind == "line" {
			u.Before = &endpoint
		}
		p.Uses = append(p.Uses, u)
	}
	return rows.Err()
}
