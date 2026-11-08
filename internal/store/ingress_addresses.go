package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
)

const (
	IngressAddressCustom = "custom"
	IngressAddressIPv4   = "server_ipv4"
	IngressAddressIPv6   = "server_ipv6"
)

var ErrIngressAddress = errors.New("invalid ingress address source")
var ErrEntryIngress = errors.New("invalid subscription ingress binding")

// ResolveIngressAddress resolves only the explicitly selected server entry
// address. Public address observations and geolocation never enter this path.
func (s *Store) ResolveIngressAddress(ctx context.Context, g *Ingress) error {
	return resolveIngressAddress(ctx, s.db, g)
}
func resolveIngressAddress(ctx context.Context, q addressQuery, g *Ingress) error {
	if g.EntryAddressSource == "" {
		g.EntryAddressSource = IngressAddressCustom
	}
	if g.EntryAddressSource == IngressAddressCustom {
		return nil
	}
	if g.EntryAddressSource != IngressAddressIPv4 && g.EntryAddressSource != IngressAddressIPv6 {
		return fmt.Errorf("%w: choose custom, server_ipv4 or server_ipv6", ErrIngressAddress)
	}
	if g.Kind != "nat" {
		return fmt.Errorf("%w: server address inheritance requires a NAT ingress", ErrIngressAddress)
	}
	var v4, v6 string
	if err := q.QueryRowContext(ctx, `SELECT public_addr,v6_addr FROM nodes WHERE id=?`, g.NodeID).Scan(&v4, &v6); err != nil {
		return err
	}
	host := v4
	if g.EntryAddressSource == IngressAddressIPv6 {
		host = v6
	}
	ip := net.ParseIP(strings.TrimSpace(host))
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() || (g.EntryAddressSource == IngressAddressIPv4) != (ip.To4() != nil) {
		return fmt.Errorf("%w: the server has no usable %s entry address", ErrIngressAddress, strings.TrimPrefix(g.EntryAddressSource, "server_"))
	}
	g.EntryHost = ip.String()
	return nil
}

// refreshInheritedIngressesTx keeps every opted-in ingress on its selected
// server family before line compilation. A missing family or release conflict
// rolls back the server edit as well as every dependent endpoint.
func refreshInheritedIngressesTx(ctx context.Context, tx *sql.Tx, nodeID int64) error {
	rows, err := tx.QueryContext(ctx, `SELECT `+ingressCols+` FROM ingresses WHERE node_id=? AND entry_address_source!='custom'`, nodeID)
	if err != nil {
		return err
	}
	var all []*Ingress
	for rows.Next() {
		g, e := scanIngress(rows)
		if e != nil {
			rows.Close()
			return e
		}
		all = append(all, g)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, g := range all {
		old := g.EntryHost
		if err = resolveIngressAddress(ctx, tx, g); err != nil {
			return err
		}
		if old == g.EntryHost {
			continue
		}
		if err = checkRolloutIngressMutationTx(ctx, tx, g); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE ingresses SET entry_host=?,address_dns_pending=CASE WHEN entry_domain!='' THEN 1 ELSE 0 END,address_dns_attempt=0,updated_at=? WHERE id=?`, g.EntryHost, now(), g.ID); err != nil {
			return err
		}
	}
	return refreshFollowingEntriesTx(ctx, tx, nodeID)
}

func resolveFollowingEntryTx(ctx context.Context, tx *sql.Tx, e *domain.Entry) error {
	if !e.FollowIngress {
		return nil
	}
	if e.LineID != nil || e.ChainID != nil {
		return fmt.Errorf("%w: managed lines and chains use their own endpoints", ErrEntryIngress)
	}
	ib, err := scanInbound(tx.QueryRowContext(ctx, `SELECT `+inboundCols+` FROM inbounds WHERE id=?`, e.InboundID))
	if err != nil {
		return err
	}
	if ib.IngressID == nil || ib.Settings.Reverse != nil || ib.Settings.WireGuardSlot != 0 {
		return fmt.Errorf("%w: select an ordinary inbound with an ingress", ErrEntryIngress)
	}
	g, err := scanIngress(tx.QueryRowContext(ctx, `SELECT `+ingressCols+` FROM ingresses WHERE id=? AND node_id=?`, *ib.IngressID, ib.NodeID))
	if err != nil {
		return err
	}
	if g.ClientHost() == "" || !g.AllowsPort(ib.Port) || g.EntryPort(ib.Port) < 1 {
		return fmt.Errorf("%w: ingress has no valid public endpoint for this inbound", ErrEntryIngress)
	}
	e.DisplayHost, e.DisplayPort = g.ClientHost(), g.EntryPort(ib.Port)
	return nil
}

func refreshFollowingEntriesTx(ctx context.Context, tx *sql.Tx, nodeID int64) error {
	rows, err := tx.QueryContext(ctx, `SELECT `+entryCols+` FROM entries e JOIN inbounds i ON i.id=e.inbound_id WHERE e.follow_ingress=1 AND i.node_id=?`, nodeID)
	if err != nil {
		return err
	}
	var all []*domain.Entry
	for rows.Next() {
		e, x := scanEntry(rows)
		if x != nil {
			rows.Close()
			return x
		}
		all = append(all, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, e := range all {
		if err = resolveFollowingEntryTx(ctx, tx, e); err != nil {
			return err
		}
		if err = validateEntryIPVersionTx(ctx, tx, e); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE entries SET display_host=?,display_port=?,updated_at=? WHERE id=?`, e.DisplayHost, e.DisplayPort, now(), e.ID); err != nil {
			return err
		}
	}
	return nil
}

// PendingIngressDNS includes unpaired servers: manual edits can change their
// inherited endpoint too. Retries read current intent under Topology.
func (s *Store) PendingIngressDNS(ctx context.Context, nodeID int64, at time.Time) ([]*Ingress, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+ingressCols+` FROM ingresses WHERE node_id=? AND address_dns_pending=1 AND (address_dns_attempt=0 OR address_dns_attempt<=?)`, nodeID, at.Add(-5*time.Minute).Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Ingress
	for rows.Next() {
		g, e := scanIngress(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
func (s *Store) BeginIngressDNS(ctx context.Context, id int64, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE ingresses SET address_dns_attempt=? WHERE id=?`, at.Unix(), id)
	return err
}
func (s *Store) FinishIngressDNS(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE ingresses SET address_dns_pending=0 WHERE id=?`, id)
	return err
}

func (s *Store) HasPendingIngressDNS(ctx context.Context, nodeID int64) (bool, error) {
	var pending bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ingresses WHERE node_id=? AND address_dns_pending=1)`, nodeID).Scan(&pending)
	return pending, err
}
