package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
)

type topologySnapshot struct {
	nodes             map[int64]*domain.Node
	inbounds          map[int64]*domain.Inbound
	lines             map[int64]*ForwardLine
	entries           map[int64]*domain.Entry
	ingresses         map[int64]*Ingress
	forwards          map[int64][]NodeForward
	candidates        map[int64][]spec.CoreCandidate
	identities        map[int64]string
	inboundIdentities map[int64]string
	entryIdentities   map[int64]string
	lineIdentities    map[int64]string
	ingressIdentities map[int64]string
	scopes            map[int64]string
	protectedInbound  map[int64]bool
	protectedEntry    map[int64]bool
	routes            map[int64]bool
	active            map[int64]bool
	unpublished       map[int64]bool
	digest            string
}
type TopologyNodeCatalog struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}
type TopologyInboundCatalog struct {
	ID           int64         `json:"id"`
	NodeID       int64         `json:"node_id"`
	Tag          string        `json:"tag"`
	Protocol     spec.Protocol `json:"protocol"`
	Listen       string        `json:"listen"`
	Port         int           `json:"port"`
	Enabled      bool          `json:"enabled"`
	Sort         int           `json:"sort"`
	IngressID    *int64        `json:"ingress_id"`
	Revision     string        `json:"revision"`
	Editable     bool          `json:"editable"`
	LineProtocol string        `json:"line_protocol"`
}
type TopologyLineCatalog struct {
	Line     ForwardLine `json:"line"`
	Revision string      `json:"revision"`
}
type TopologyEntryCatalog struct {
	ID           int64    `json:"id"`
	Name         string   `json:"name"`
	InboundID    int64    `json:"inbound_id"`
	LineID       *int64   `json:"line_id"`
	LineMemberID string   `json:"line_member_id"`
	DisplayHost  string   `json:"display_host"`
	DisplayPort  int      `json:"display_port"`
	Rate         float64  `json:"rate"`
	Sort         int      `json:"sort"`
	Enabled      bool     `json:"enabled"`
	Tags         []string `json:"tags"`
	Region       string   `json:"region"`
	Revision     string   `json:"revision"`
	Editable     bool     `json:"editable"`
}
type TopologyCatalog struct {
	Nodes     []TopologyNodeCatalog    `json:"nodes"`
	Ingresses []Ingress                `json:"ingresses"`
	Inbounds  []TopologyInboundCatalog `json:"inbounds"`
	Lines     []TopologyLineCatalog    `json:"lines"`
	Entries   []TopologyEntryCatalog   `json:"entries"`
}

func topologyRows(ctx context.Context, tx *sql.Tx, q string, visit func(*sql.Rows) error) error {
	rows, e := tx.QueryContext(ctx, q)
	if e != nil {
		return e
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		n++
		if n > 10000 {
			return errors.New("topology catalogue exceeds the bounded draft limit")
		}
		if e = visit(rows); e != nil {
			return e
		}
	}
	return rows.Err()
}
func topologyKeys[T any](m map[int64]T) []int64 {
	out := make([]int64, 0, len(m))
	for id := range m {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
func loadTopologySnapshotTx(ctx context.Context, tx *sql.Tx) (*topologySnapshot, error) {
	s := &topologySnapshot{nodes: map[int64]*domain.Node{}, inbounds: map[int64]*domain.Inbound{}, lines: map[int64]*ForwardLine{}, entries: map[int64]*domain.Entry{}, ingresses: map[int64]*Ingress{}, forwards: map[int64][]NodeForward{}, candidates: map[int64][]spec.CoreCandidate{}, identities: map[int64]string{}, inboundIdentities: map[int64]string{}, entryIdentities: map[int64]string{}, lineIdentities: map[int64]string{}, ingressIdentities: map[int64]string{}, scopes: map[int64]string{}, protectedInbound: map[int64]bool{}, protectedEntry: map[int64]bool{}, routes: map[int64]bool{}, active: map[int64]bool{}, unpublished: map[int64]bool{}}
	reads := []struct {
		q string
		v func(*sql.Rows) error
	}{
		{"SELECT " + nodeCols + " FROM nodes ORDER BY id", func(r *sql.Rows) error {
			n, e := scanNode(r)
			if e == nil {
				s.nodes[n.ID] = n
			}
			return e
		}},
		{"SELECT " + inboundCols + " FROM inbounds ORDER BY id", func(r *sql.Rows) error {
			v, e := scanInbound(r)
			if e == nil {
				s.inbounds[v.ID] = v
			}
			return e
		}},
		{"SELECT " + lineCols + " FROM forward_lines ORDER BY id", func(r *sql.Rows) error {
			v, e := scanLine(r)
			if e == nil {
				s.lines[v.ID] = v
			}
			return e
		}},
		{"SELECT " + entryCols + " FROM entries e ORDER BY e.id", func(r *sql.Rows) error {
			v, e := scanEntry(r)
			if e == nil {
				s.entries[v.ID] = v
			}
			return e
		}},
		{"SELECT " + ingressCols + " FROM ingresses ORDER BY id", func(r *sql.Rows) error {
			v, e := scanIngress(r)
			if e == nil {
				s.ingresses[v.ID] = v
			}
			return e
		}},
		{`SELECT n.id,n.forwards_json,n.cores_json,COALESCE(p.uuid,'') FROM nodes n LEFT JOIN probe_public_ids p ON p.node_id=n.id ORDER BY n.id`, func(r *sql.Rows) error {
			var id int64
			var fs, cores, identity string
			if e := r.Scan(&id, &fs, &cores, &identity); e != nil {
				return e
			}
			var decoded []NodeForward
			if e := json.Unmarshal([]byte(fs), &decoded); e != nil {
				return e
			}
			s.forwards[id] = decoded
			s.identities[id] = identity
			var statuses map[string]agentproto.CoreStatus
			if n := s.nodes[id]; n.LastSeenAt != nil && time.Since(*n.LastSeenAt) < 3*time.Minute && json.Unmarshal([]byte(cores), &statuses) == nil {
				s.candidates[id] = agentproto.CoreCandidates(statuses)
				for i := range s.candidates[id] {
					c := &s.candidates[id][i].Capabilities
					if r := c.PrivateAccessReadiness; r != nil {
						age := time.Now().Unix() - r.CheckedAt
						if r.CheckedAt <= 0 || age >= 180 || age < -30 {
							c.PrivateAccessReadiness = nil
							c.PrivateAccess = false
							c.CoreOnlyPrivateAccess = false
						} else {
							r.CheckedAt = 0
						}
					}
				}
			}
			return nil
		}},
		{`SELECT inbound_id,identity FROM topology_inbound_identities`, func(r *sql.Rows) error {
			var id int64
			var identity string
			e := r.Scan(&id, &identity)
			s.inboundIdentities[id] = identity
			return e
		}},
		{`SELECT entry_id,identity FROM topology_entry_identities`, func(r *sql.Rows) error {
			var id int64
			var identity string
			e := r.Scan(&id, &identity)
			s.entryIdentities[id] = identity
			return e
		}},
		{`SELECT line_id,identity FROM topology_line_identities`, func(r *sql.Rows) error {
			var id int64
			var identity string
			e := r.Scan(&id, &identity)
			s.lineIdentities[id] = identity
			return e
		}},
		{`SELECT ingress_id,identity FROM topology_ingress_identities`, func(r *sql.Rows) error {
			var id int64
			var identity string
			e := r.Scan(&id, &identity)
			s.ingressIdentities[id] = identity
			return e
		}},
		{`SELECT line_id,id FROM line_change_scopes WHERE line_id IS NOT NULL`, func(r *sql.Rows) error {
			var id int64
			var scope string
			e := r.Scan(&id, &scope)
			s.scopes[id] = scope
			return e
		}},
		{`SELECT line_id FROM forward_path_routes`, func(r *sql.Rows) error {
			var id int64
			e := r.Scan(&id)
			s.routes[id] = true
			if l := s.lines[id]; l != nil {
				l.PathRoute = true
				s.protectedInbound[l.TargetInboundID] = true
			}
			return e
		}},
		{`SELECT user_inbound_id,receiver_inbound_id,entry_id FROM node_reverse_links UNION ALL SELECT user_inbound_id,NULL,entry_id FROM node_wireguard_links`, func(r *sql.Rows) error {
			var user, receiver, entry sql.NullInt64
			e := r.Scan(&user, &receiver, &entry)
			s.protectedInbound[user.Int64] = true
			s.protectedInbound[receiver.Int64] = true
			s.protectedEntry[entry.Int64] = true
			return e
		}},
		{`SELECT line_id,kind,published_at FROM line_rollouts WHERE status IN ('running','paused')`, func(r *sql.Rows) error {
			var id int64
			var kind string
			var published sql.NullInt64
			e := r.Scan(&id, &kind, &published)
			s.active[id] = true
			s.unpublished[id] = kind == "delete" || (kind == "create" && !published.Valid)
			return e
		}},
	}
	for _, read := range reads {
		if e := topologyRows(ctx, tx, read.q, read.v); e != nil {
			return nil, e
		}
	}
	for _, fs := range s.forwards {
		for _, f := range fs {
			if f.LineID == 0 && f.InboundID != 0 {
				s.protectedInbound[f.InboundID] = true
			}
		}
	}
	for _, e := range s.entries {
		if e.LineID != nil && s.routes[*e.LineID] {
			s.protectedEntry[e.ID] = true
		}
	}
	for _, e := range s.entries {
		if e.ClientDialerEntryID != nil {
			s.protectedEntry[*e.ClientDialerEntryID] = true
		}
		if e.ClientDialerEntryID != nil || e.ChainID != nil {
			s.protectedEntry[e.ID] = true
		}
	}
	// Hash only stable topology inputs and identities. Resource beats, users,
	// traffic and Jobs unrelated to removal do not invalidate a topology preview.
	var raw []any
	for _, q := range []string{
		`SELECT n.id,n.name,n.token_hash,p.uuid,n.public_addr,n.internal_addr,n.v6_addr,n.domain,n.domain_shared,n.version,n.node_mode,n.mode_managed,n.mode_revision,n.reported_mode,n.reported_mode_revision,n.mita_quotas,n.forwards_json,n.outbounds_json,n.routes_json,n.dns_json,n.overrides_json,n.default_outbound FROM nodes n LEFT JOIN probe_public_ids p ON p.node_id=n.id ORDER BY n.id`,
		`SELECT * FROM inbounds ORDER BY id`, `SELECT * FROM forward_lines ORDER BY id`, `SELECT * FROM forward_line_members ORDER BY line_id,member_id`, `SELECT * FROM entries ORDER BY id`, `SELECT * FROM ingresses ORDER BY id`, `SELECT * FROM inbound_access ORDER BY inbound_id`, `SELECT * FROM inbound_access_groups ORDER BY inbound_id,group_id`, `SELECT id,node_id,kind,done_at,error FROM node_jobs WHERE kind='node_remove' ORDER BY id`, `SELECT id,line_id,version,status,stage FROM line_rollouts WHERE status IN ('running','paused') ORDER BY id`,
	} {
		if e := topologyRows(ctx, tx, q, func(r *sql.Rows) error {
			cols, e := r.Columns()
			if e != nil {
				return e
			}
			values := make([]any, len(cols))
			dest := make([]any, len(cols))
			for i := range values {
				dest[i] = &values[i]
			}
			if e = r.Scan(dest...); e != nil {
				return e
			}
			raw = append(raw, values)
			return nil
		}); e != nil {
			return nil, e
		}
	}
	raw = append(raw, s.candidates, s.scopes, s.inboundIdentities, s.entryIdentities, s.lineIdentities, s.ingressIdentities)
	s.digest = topologyDigest(raw)
	return s, nil
}
func (s *topologySnapshot) inboundView(v *domain.Inbound) TopologyInboundCatalog {
	protocol, _ := LineProtocol(v)
	return TopologyInboundCatalog{v.ID, v.NodeID, v.Tag, v.Protocol, v.Listen, v.Port, v.Enabled, v.Sort, v.IngressID, topologyDigest([]any{v, s.identities[v.NodeID], s.inboundIdentities[v.ID]}), !s.protectedInbound[v.ID] && v.Settings.Reverse == nil && v.Settings.WireGuardSlot == 0 && !v.Settings.PrivateAccess.Enabled(), protocol}
}
func (s *topologySnapshot) entryView(e *domain.Entry) TopologyEntryCatalog {
	return TopologyEntryCatalog{e.ID, e.Name, e.InboundID, e.LineID, e.LineMemberID, e.DisplayHost, e.DisplayPort, e.Rate, e.Sort, e.Enabled, e.Tags, e.Region, topologyDigest([]any{e, s.entryIdentities[e.ID]}), !s.protectedEntry[e.ID]}
}
func (s *topologySnapshot) lineRevision(l *ForwardLine) string {
	return topologyDigest([]any{l, s.scopes[l.ID], s.lineIdentities[l.ID]})
}
func (s *Store) TopologyChangesetCatalog(ctx context.Context) (*TopologyCatalog, error) {
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	v, e := loadTopologySnapshotTx(ctx, tx)
	if e != nil {
		return nil, e
	}
	out := &TopologyCatalog{Nodes: []TopologyNodeCatalog{}, Ingresses: []Ingress{}, Inbounds: []TopologyInboundCatalog{}, Lines: []TopologyLineCatalog{}, Entries: []TopologyEntryCatalog{}}
	for _, id := range topologyKeys(v.nodes) {
		n := v.nodes[id]
		if NodeProxyAllowed(n) {
			out.Nodes = append(out.Nodes, TopologyNodeCatalog{id, n.Name})
		}
	}
	for _, id := range topologyKeys(v.ingresses) {
		out.Ingresses = append(out.Ingresses, *v.ingresses[id])
	}
	for _, id := range topologyKeys(v.inbounds) {
		out.Inbounds = append(out.Inbounds, v.inboundView(v.inbounds[id]))
	}
	for _, id := range topologyKeys(v.lines) {
		ib := v.inbounds[v.lines[id].TargetInboundID]
		if ib != nil && (ib.Settings.Reverse != nil || ib.Settings.WireGuardSlot != 0) {
			continue
		}
		out.Lines = append(out.Lines, TopologyLineCatalog{*v.lines[id], v.lineRevision(v.lines[id])})
	}
	for _, id := range topologyKeys(v.entries) {
		out.Entries = append(out.Entries, v.entryView(v.entries[id]))
	}
	return out, nil
}
