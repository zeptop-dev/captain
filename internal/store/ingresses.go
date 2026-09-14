package store

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// Ingress is a way into a node other than its public address: an IPLC /
// dedicated line with its own local NIC address, a far-end address relays
// forward to, an optional provider-supplied public entry and a port range.
// Inbounds pick one; entries and relays derive their addresses from it.
type Ingress struct {
	ID        int64  `json:"id"`
	NodeID    int64  `json:"node_id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	BindIP    string `json:"bind_ip"`
	LineIP    string `json:"line_ip"`
	EntryHost string `json:"entry_host"`
	// EntryDomain names the public entry; a DNS record points it at EntryHost.
	EntryDomain string `json:"entry_domain"`
	PortFrom    int    `json:"port_from"`
	PortTo      int    `json:"port_to"`
	PortOffset  int    `json:"port_offset"`
	// ReservedPorts are mapped ports the provider uses for something else
	// (typically SSH); inbounds may not take them.
	ReservedPorts []int     `json:"reserved_ports"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// ClientHost is what entries advertise: the entry domain when set, else
// the provider's entry address.
func (g *Ingress) ClientHost() string {
	if g.EntryDomain != "" {
		return g.EntryDomain
	}
	return g.EntryHost
}

// EntryPort maps a local inbound port to the port clients dial.
func (g *Ingress) EntryPort(local int) int { return local + g.PortOffset }

// AllowsPort reports whether a local port fits the line's range and is
// not reserved.
func (g *Ingress) AllowsPort(p int) bool {
	for _, r := range g.ReservedPorts {
		if r == p {
			return false
		}
	}
	return g.PortFrom == 0 || (p >= g.PortFrom && p <= g.PortTo)
}

// ProbePort is the far-end port the line RTT task connects to when no
// inbound uses the ingress: a reserved (provider-owned, e.g. SSH) port
// answers, else the range start, else 80.
func (g *Ingress) ProbePort() int {
	if len(g.ReservedPorts) > 0 {
		return g.ReservedPorts[0]
	}
	if g.PortFrom > 0 {
		return g.PortFrom
	}
	return 80
}

func parsePorts(s string) []int {
	out := []int{}
	for _, f := range strings.Split(s, ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(f)); err == nil && n > 0 && n < 65536 {
			out = append(out, n)
		}
	}
	return out
}

func joinPorts(ps []int) string {
	parts := make([]string, 0, len(ps))
	for _, p := range ps {
		parts = append(parts, strconv.Itoa(p))
	}
	return strings.Join(parts, ",")
}

const ingressCols = "id, node_id, name, kind, bind_ip, line_ip, entry_host, entry_domain, port_from, port_to, port_offset, reserved_ports, created_at, updated_at"

func scanIngress(row interface{ Scan(...any) error }) (*Ingress, error) {
	var g Ingress
	var cr, up int64
	var reserved string
	if err := row.Scan(&g.ID, &g.NodeID, &g.Name, &g.Kind, &g.BindIP, &g.LineIP, &g.EntryHost, &g.EntryDomain, &g.PortFrom, &g.PortTo, &g.PortOffset, &reserved, &cr, &up); err != nil {
		return nil, wrapNotFound(err)
	}
	g.ReservedPorts = parsePorts(reserved)
	g.CreatedAt, g.UpdatedAt = unix(cr), unix(up)
	return &g, nil
}

func (s *Store) CreateIngress(ctx context.Context, g *Ingress) error {
	if g.Kind == "" {
		g.Kind = "mapped"
	}
	ts := now()
	res, err := s.db.ExecContext(ctx, `INSERT INTO ingresses (node_id, name, kind, bind_ip, line_ip, entry_host, entry_domain, port_from, port_to, port_offset, reserved_ports, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		g.NodeID, g.Name, g.Kind, g.BindIP, g.LineIP, g.EntryHost, g.EntryDomain, g.PortFrom, g.PortTo, g.PortOffset, joinPorts(g.ReservedPorts), ts, ts)
	if err != nil {
		return err
	}
	g.ID, _ = res.LastInsertId()
	g.CreatedAt, g.UpdatedAt = unix(ts), unix(ts)
	return nil
}

func (s *Store) UpdateIngress(ctx context.Context, g *Ingress) error {
	_, err := s.db.ExecContext(ctx, `UPDATE ingresses SET name = ?, kind = ?, bind_ip = ?, line_ip = ?, entry_host = ?, entry_domain = ?, port_from = ?, port_to = ?, port_offset = ?, reserved_ports = ?, updated_at = ? WHERE id = ?`,
		g.Name, g.Kind, g.BindIP, g.LineIP, g.EntryHost, g.EntryDomain, g.PortFrom, g.PortTo, g.PortOffset, joinPorts(g.ReservedPorts), now(), g.ID)
	return err
}

func (s *Store) DeleteIngress(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM ingresses WHERE id = ?`, id)
	return err
}

func (s *Store) IngressByID(ctx context.Context, id int64) (*Ingress, error) {
	return scanIngress(s.db.QueryRowContext(ctx, `SELECT `+ingressCols+` FROM ingresses WHERE id = ?`, id))
}

func (s *Store) IngressesByNode(ctx context.Context, nodeID int64) ([]Ingress, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+ingressCols+` FROM ingresses WHERE node_id = ? ORDER BY id`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Ingress{}
	for rows.Next() {
		g, err := scanIngress(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *g)
	}
	return out, rows.Err()
}

// AllIngresses returns every ingress keyed by id (for relay target pickers).
func (s *Store) AllIngresses(ctx context.Context) (map[int64]Ingress, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+ingressCols+` FROM ingresses`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]Ingress{}
	for rows.Next() {
		g, err := scanIngress(rows)
		if err != nil {
			return nil, err
		}
		out[g.ID] = *g
	}
	return out, rows.Err()
}
