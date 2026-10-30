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
	"net"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/zeptop-dev/bosun/pkg/selfupdate"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
)

var (
	ErrLineConflict = errors.New("line changed; reload before saving")
	ErrLineInUse    = errors.New("resource is referenced by a line; edit or remove the line first")
	ErrLinePreview  = errors.New("line impact changed; preview again before saving")
)

// ForwardLine owns its generated listeners, but never owns its landing inbound
// or subscription entries. Groups are ordered layers, not DNS answer pools.
type ForwardLine struct {
	PathRoute bool `json:"path_route,omitempty"`
	// Optional on writes. Existing clients retain their immediate-save behavior.
	PreviewRevision   string             `json:"preview_revision,omitempty"`
	ID                int64              `json:"id"`
	Name              string             `json:"name"`
	Version           int64              `json:"version"`
	Protocol          string             `json:"protocol"`
	Backend           string             `json:"backend"`
	TargetInboundID   int64              `json:"target_inbound_id"`
	TargetAddressMode string             `json:"target_address_mode"`
	Groups            []LineGroup        `json:"groups"`
	UpdatedAt         time.Time          `json:"updated_at"`
	Endpoints         []LineEndpoint     `json:"endpoints"`
	Status            []LineMemberStatus `json:"status"`
}
type LineGroup struct {
	Strategy string       `json:"strategy"`
	Members  []LineMember `json:"members"`
}
type LineMember struct {
	ID          string `json:"id"`
	NodeID      int64  `json:"node_id"`
	Port        int    `json:"port"`
	IngressID   *int64 `json:"ingress_id,omitempty"`
	Weight      int    `json:"weight"`
	AddressMode string `json:"address_mode,omitempty"`
}
type LineEndpoint struct {
	MemberID string `json:"member_id"`
	NodeID   int64  `json:"node_id"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
}
type LineMemberStatus struct {
	MemberID      string     `json:"member_id"`
	NodeID        int64      `json:"node_id"`
	Health        string     `json:"health"`
	ProbeProtocol string     `json:"probe_protocol"`
	RTTMillis     int64      `json:"rtt_ms"`
	UpdatedAt     *time.Time `json:"updated_at"`
}

const lineCols = "id,name,version,protocol,backend,target_inbound_id,groups_json,updated_at,target_address_mode"

func scanLine(row interface{ Scan(...any) error }) (*ForwardLine, error) {
	var l ForwardLine
	var raw string
	var at int64
	if err := row.Scan(&l.ID, &l.Name, &l.Version, &l.Protocol, &l.Backend, &l.TargetInboundID, &raw, &at, &l.TargetAddressMode); err != nil {
		return nil, wrapNotFound(err)
	}
	if err := json.Unmarshal([]byte(raw), &l.Groups); err != nil {
		return nil, err
	}
	l.UpdatedAt = unix(at)
	l.Endpoints = []LineEndpoint{}
	l.Status = []LineMemberStatus{}
	return &l, nil
}

func (s *Store) LineByID(ctx context.Context, id int64) (*ForwardLine, error) {
	l, err := scanLine(s.db.QueryRowContext(ctx, "SELECT "+lineCols+" FROM forward_lines WHERE id=?", id))
	if err != nil {
		return nil, err
	}
	return l, s.decorateLine(ctx, l)
}
func (s *Store) ListLines(ctx context.Context) ([]*ForwardLine, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+lineCols+" FROM forward_lines ORDER BY id")
	if err != nil {
		return nil, err
	}
	out := []*ForwardLine{}
	for rows.Next() {
		l, e := scanLine(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		out = append(out, l)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, l := range out {
		if err = s.decorateLine(ctx, l); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Runtime identity follows only the compiled forwarding configuration.
// Renaming a line or changing unrelated server metadata must not close sessions.
func lineRuntimeTag(l *ForwardLine, m LineMember, f spec.Forward) string {
	f.Tag = ""
	raw, _ := json.Marshal(f)
	hash := sha256.Sum256(raw)
	return fmt.Sprintf("line-%d-%s-%x", l.ID, m.ID, hash[:6])
}

func (s *Store) decorateLine(ctx context.Context, l *ForwardLine) error {
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM forward_path_routes WHERE line_id=?)`, l.ID).Scan(&l.PathRoute); err != nil {
		return err
	}
	for i, g := range l.Groups {
		for _, m := range g.Members {
			n, err := s.NodeByID(ctx, m.NodeID)
			if err != nil {
				return err
			}
			var ing *Ingress
			if m.IngressID != nil {
				ing, err = s.IngressByID(ctx, *m.IngressID)
				if err != nil {
					return err
				}
			}
			if i == 0 {
				host, port := lineAddress(n, ing, m.Port, false)
				l.Endpoints = append(l.Endpoints, LineEndpoint{m.ID, m.NodeID, host, port})
			}
			st := LineMemberStatus{MemberID: m.ID, NodeID: m.NodeID, Health: "unknown", ProbeProtocol: "none"}
			if l.Protocol != "udp" {
				st.ProbeProtocol = "tcp"
			}
			statuses, err := s.ForwardStatuses(ctx, m.NodeID)
			if err != nil {
				return err
			}
			forwards, err := s.NodeForwards(ctx, m.NodeID)
			if err != nil {
				return err
			}
			tag := ""
			for _, f := range forwards {
				if f.LineID == l.ID && f.LineMemberID == m.ID {
					tag = f.Tag
					break
				}
			}
			if f, ok := statuses[tag]; ok {
				st.UpdatedAt = &f.UpdatedAt
				if time.Since(f.UpdatedAt) >= 0 && time.Since(f.UpdatedAt) < 3*time.Minute && n.LastSeenAt != nil && time.Since(*n.LastSeenAt) >= 0 && time.Since(*n.LastSeenAt) < 3*time.Minute {
					st.Health, st.ProbeProtocol, st.RTTMillis = f.Health, f.ProbeProtocol, f.RTTMillis
					if st.Health == "" {
						st.Health = "unknown"
					}
				}
			}
			l.Status = append(l.Status, st)
		}
	}
	return nil
}

// lineAddress chooses a provider's far-end address for a relay, and its
// mapped public endpoint for clients. InternalAddr is used only by explicit
// internal address mode, never as a fallback for the default path.
func lineAddress(n *domain.Node, g *Ingress, port int, relay bool) (string, int) {
	if g != nil {
		if relay && g.LineIP != "" {
			return g.LineIP, port
		}
		return g.ClientHost(), g.EntryPort(port)
	}
	if n.Domain != "" {
		return n.Domain, port
	}
	if n.PublicAddr != "" {
		return n.PublicAddr, port
	}
	return n.V6Addr, port
}

func normalizeLine(l *ForwardLine) error {
	if err := validateAddressMode(l.TargetAddressMode); err != nil {
		return err
	}
	l.Name = strings.TrimSpace(l.Name)
	if l.Name == "" || len(l.Name) > 128 || !spec.PublicLabel(l.Name) {
		return errors.New("a plain line name of at most 128 characters is required")
	}
	if l.Protocol == "" {
		l.Protocol = "tcp"
	}
	if l.Protocol != "tcp" && l.Protocol != "udp" && l.Protocol != "both" {
		return errors.New("protocol must be tcp, udp or both")
	}
	if l.Backend == "" {
		l.Backend = "relay"
	}
	if l.Backend != "relay" && l.Backend != "nft" && l.Backend != "realm" {
		return errors.New("backend must be relay, nft or realm")
	}
	if len(l.Groups) < 1 || len(l.Groups) > 8 {
		return errors.New("a line needs between one and eight ordered groups")
	}
	ids, nodes := map[string]bool{}, map[int64]bool{}
	for i := range l.Groups {
		g := &l.Groups[i]
		if g.Strategy == "" {
			g.Strategy = spec.BalanceFailover
		}
		if g.Strategy != spec.BalanceFailover && g.Strategy != spec.BalanceRoundRobin {
			return errors.New("group strategy must be failover or roundrobin")
		}
		if len(g.Members) < 1 || len(g.Members) > spec.MaxForwardHops {
			return errors.New("each group needs between one and eight members")
		}
		if i > 0 && len(g.Members) > 1 && l.Protocol != "tcp" && g.Strategy != spec.BalanceRoundRobin {
			return errors.New("UDP candidate groups support roundrobin only; TCP health cannot drive UDP failover")
		}
		if i > 0 && len(g.Members) > 1 && l.Backend != "relay" {
			return errors.New("candidate groups require the built-in relay backend")
		}
		for j := range g.Members {
			m := &g.Members[j]
			if err := validateAddressMode(m.AddressMode); err != nil {
				return err
			}
			if i == 0 && m.AddressMode == AddressModeInternal {
				return errors.New("first-group members publish client endpoints; internal address mode applies to subsequent groups")
			}
			if m.ID == "" {
				b := make([]byte, 8)
				if _, err := rand.Read(b); err != nil {
					return err
				}
				m.ID = hex.EncodeToString(b)
			}
			if len(m.ID) > 24 || !spec.ValidTag(m.ID) || ids[m.ID] {
				return errors.New("member IDs must be unique valid tags of at most 24 characters")
			}
			ids[m.ID] = true
			if m.NodeID <= 0 || nodes[m.NodeID] {
				return errors.New("a server may occur only once in a line")
			}
			nodes[m.NodeID] = true
			if m.Port < 1 || m.Port > 65535 {
				return errors.New("member port must be between 1 and 65535")
			}
			if m.Weight == 0 {
				m.Weight = 1
			}
			if m.Weight < 1 || m.Weight > 100 {
				return errors.New("member weight must be between 1 and 100")
			}
		}
	}
	return nil
}

// SaveLine is called under Topology. One transaction owns the specification,
// membership references, generated forwards, and all bound entry addresses.
func (s *Store) SaveLine(ctx context.Context, l *ForwardLine) error {
	_, err := s.SaveLineWithChange(ctx, l)
	return err
}

// SaveLineWithChange returns the record committed by this transaction, even if
// a background recompile starts immediately after the commit.
func (s *Store) SaveLineWithChange(ctx context.Context, l *ForwardLine) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if owned, err := isPathRouteTx(ctx, tx, l.ID); err != nil {
		return "", err
	} else if owned {
		return "", ErrPathRouteOwned
	}
	if l.PathRoute {
		return "", ErrPathRouteOwned
	}

	if l.PreviewRevision != "" {
		preview, err := previewLineTx(ctx, tx, l)
		if err != nil {
			return "", err
		}
		if preview.Revision != l.PreviewRevision {
			return "", ErrLinePreview
		}
	}
	if l.ID > 0 {
		if err = checkLineRolloutWriteTx(ctx, tx, l.ID); err != nil {
			return "", err
		}
	}
	next := *l
	if l.ID == 0 {
		r, err := tx.ExecContext(ctx, `INSERT INTO forward_lines(name,version,protocol,backend,target_inbound_id,groups_json,updated_at) VALUES(?,1,?,?,?,'[]',?)`, l.Name, l.Protocol, l.Backend, l.TargetInboundID, now())
		if err != nil {
			return "", err
		}
		next.ID, _ = r.LastInsertId()
		next.Version = 1
	} else {
		old, err := scanLine(tx.QueryRowContext(ctx, "SELECT "+lineCols+" FROM forward_lines WHERE id=?", l.ID))
		if err != nil {
			return "", err
		}
		if old.Version != l.Version {
			return "", ErrLineConflict
		}
		preserveLineAddressModes(&next, old)
		next.Version++
	}
	if err = normalizeLine(&next); err != nil {
		return "", err
	}
	if err = saveLineTx(ctx, tx, &next, "save"); err != nil {
		return "", err
	}
	var change string
	if err = tx.QueryRowContext(ctx, `SELECT latest_change_id FROM line_change_scopes WHERE line_id=?`, next.ID).Scan(&change); err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	*l = next
	l.PreviewRevision = ""
	return change, nil
}

func saveLineTx(ctx context.Context, tx *sql.Tx, l *ForwardLine, kind string) error {
	before, err := lineOwnedSnapshotTx(ctx, tx, l.ID)
	if err != nil {
		return err
	}
	generated, endpoints, err := compileLine(ctx, tx, l)
	if err != nil {
		return err
	}
	if kind == "recompile" {
		skip, e := allowRolloutRecompileTx(ctx, tx, l, generated, endpoints)
		if e != nil {
			return e
		}
		if skip {
			return nil
		}
	} else if err = checkLineRolloutWriteTx(ctx, tx, l.ID); err != nil {
		return err
	}
	entryMembers, err := linePublishedMembersTx(ctx, tx, l.ID, endpoints)
	if err != nil {
		return err
	}
	if err = replaceLineForwardsTx(ctx, tx, l.ID, generated); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM forward_line_members WHERE line_id=?`, l.ID); err != nil {
		return err
	}
	for _, g := range l.Groups {
		for _, m := range g.Members {
			if _, err = tx.ExecContext(ctx, `INSERT INTO forward_line_members(line_id,member_id,node_id,ingress_id) VALUES(?,?,?,?)`, l.ID, m.ID, m.NodeID, nullInt64(m.IngressID)); err != nil {
				return err
			}
		}
	}
	raw, _ := json.Marshal(l.Groups)
	l.UpdatedAt = unix(now())
	if _, err = tx.ExecContext(ctx, `UPDATE forward_lines SET name=?,version=?,protocol=?,backend=?,target_inbound_id=?,groups_json=?,updated_at=?,target_address_mode=? WHERE id=?`, l.Name, l.Version, l.Protocol, l.Backend, l.TargetInboundID, string(raw), l.UpdatedAt.Unix(), l.TargetAddressMode, l.ID); err != nil {
		return err
	}
	for id, m := range entryMembers {
		e := endpoints[m]
		if _, err = tx.ExecContext(ctx, `UPDATE entries SET inbound_id=?,display_host=?,display_port=?,updated_at=? WHERE id=?`, l.TargetInboundID, e.Host, e.Port, now(), id); err != nil {
			return err
		}
	}
	return recordLineChangeTx(ctx, tx, l, kind, before)
}

// Both preview and save enforce published-member protection from the same snapshot.
func linePublishedMembersTx(ctx context.Context, tx *sql.Tx, id int64, endpoints map[string]LineEndpoint) (map[int64]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id,line_member_id FROM entries WHERE line_id=?`, id)
	if err != nil {
		return nil, err
	}
	entryMembers := map[int64]string{}
	for rows.Next() {
		var id int64
		var m string
		if err = rows.Scan(&id, &m); err != nil {
			rows.Close()
			return nil, err
		}
		entryMembers[id] = m
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, m := range entryMembers {
		e, ok := endpoints[m]
		if !ok || e.Host == "" {
			return nil, errors.New("a published member cannot be removed or lose its public endpoint; edit its entries first")
		}
	}
	return entryMembers, nil
}

func (s *Store) DeleteLine(ctx context.Context, id, version int64) error {
	_, err := s.DeleteLineWithChange(ctx, id, version)
	return err
}

// DeleteLineWithChange returns the cleanup record from this exact transaction.
func (s *Store) DeleteLineWithChange(ctx context.Context, id, version int64) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if owned, err := isPathRouteTx(ctx, tx, id); err != nil {
		return "", err
	} else if owned {
		return "", ErrPathRouteOwned
	}
	if err := pathSourceLineInUseTx(ctx, tx, id); err != nil {
		return "", err
	}

	if err = checkLineRolloutWriteTx(ctx, tx, id); err != nil {
		return "", err
	}
	var v, n int64
	if err = tx.QueryRowContext(ctx, `SELECT version FROM forward_lines WHERE id=?`, id).Scan(&v); err != nil {
		return "", wrapNotFound(err)
	}
	if v != version {
		return "", ErrLineConflict
	}
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries WHERE line_id=?`, id).Scan(&n); err != nil {
		return "", err
	}
	if n > 0 {
		return "", fmt.Errorf("%w: subscription nodes still publish this line", ErrLineInUse)
	}
	old, err := scanLine(tx.QueryRowContext(ctx, "SELECT "+lineCols+" FROM forward_lines WHERE id=?", id))
	if err != nil {
		return "", err
	}
	before, err := lineOwnedSnapshotTx(ctx, tx, id)
	if err != nil {
		return "", err
	}
	if err = replaceLineForwardsTx(ctx, tx, id, nil); err != nil {
		return "", err
	}
	old.Version++
	if err = recordLineChangeTx(ctx, tx, old, "delete", before); err != nil {
		return "", err
	}
	var change string
	if err = tx.QueryRowContext(ctx, `SELECT latest_change_id FROM line_change_scopes WHERE line_id=?`, id).Scan(&change); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM forward_lines WHERE id=?`, id); err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return change, nil
}

func replaceLineForwardsTx(ctx context.Context, tx *sql.Tx, id int64, generated map[int64][]NodeForward) error {
	rows, err := tx.QueryContext(ctx, `SELECT id,forwards_json FROM nodes`)
	if err != nil {
		return err
	}
	changes := map[int64][]NodeForward{}
	for rows.Next() {
		var node int64
		var raw string
		if err = rows.Scan(&node, &raw); err != nil {
			rows.Close()
			return err
		}
		var old []NodeForward
		if err = json.Unmarshal([]byte(raw), &old); err != nil {
			rows.Close()
			return err
		}
		next := []NodeForward{}
		changed := len(generated[node]) > 0
		for _, f := range old {
			if f.LineID == id {
				changed = true
			} else {
				next = append(next, f)
			}
		}
		if changed {
			next = append(next, generated[node]...)
			if !reflect.DeepEqual(old, next) {
				changes[node] = next
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for node, next := range changes {
		raw, _ := json.Marshal(next)
		if _, err = tx.ExecContext(ctx, `UPDATE nodes SET forwards_json=?,updated_at=? WHERE id=?`, string(raw), now(), node); err != nil {
			return err
		}
		// Configuration hashes prevent late reports for replaced paths appearing current.
		if _, err = tx.ExecContext(ctx, `DELETE FROM forward_status WHERE node_id=? AND tag LIKE ?`, node, fmt.Sprintf("line-%d-%%", id)); err != nil {
			return err
		}
	}
	return nil
}

// PreserveLineForwards lets old clients replace their own rules without
// erasing generated ones; changing or forging any managed rule is rejected.
func (s *Store) PreserveLineForwards(ctx context.Context, node int64, in []NodeForward) ([]NodeForward, error) {
	old, err := s.NodeForwards(ctx, node)
	if err != nil {
		return nil, err
	}
	owned := map[string]NodeForward{}
	for _, f := range old {
		if f.LineID != 0 {
			owned[f.Tag] = f
		}
	}
	out := []NodeForward{}
	for _, f := range in {
		if o, ok := owned[f.Tag]; ok {
			if !reflect.DeepEqual(f.Forward, o.Forward) || f.InboundID != o.InboundID || (f.TargetAddressMode != "" && f.TargetAddressMode != o.TargetAddressMode) || (f.LineID != 0 && f.LineID != o.LineID) || (f.LineMemberID != "" && f.LineMemberID != o.LineMemberID) {
				return nil, errors.New("edit generated forwards through their line")
			}
			out = append(out, o)
			delete(owned, f.Tag)
		} else {
			if f.LineID != 0 || f.LineMemberID != "" || strings.HasPrefix(f.Tag, "line-") {
				return nil, errors.New("line forward tags and ownership are reserved")
			}
			out = append(out, f)
		}
	}
	for _, f := range old {
		if _, ok := owned[f.Tag]; ok {
			out = append(out, f)
		}
	}
	return out, nil
}

func (s *Store) ResolveLineEntry(ctx context.Context, e *domain.Entry) error {
	if e.LineID != nil && e.ClientDialerEntryID != nil && *e.ClientDialerEntryID != 0 {
		return fmt.Errorf("%w: choose a server line or a client chain", ErrEntryClientChain)
	}
	if e.LineID == nil {
		if e.LineMemberID != "" {
			return errors.New("line_member_id requires line_id")
		}
		return nil
	}
	var unpublished bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM line_rollouts WHERE line_id=? AND status IN ('running','paused') AND (kind='delete' OR (kind='create' AND published_at IS NULL)))`, *e.LineID).Scan(&unpublished); err != nil {
		return err
	}
	if unpublished {
		return ErrLineRolloutConflict
	}
	l, err := s.LineByID(ctx, *e.LineID)
	if err != nil {
		return err
	}
	if e.LineMemberID == "" && len(l.Endpoints) == 1 {
		e.LineMemberID = l.Endpoints[0].MemberID
	}
	for _, p := range l.Endpoints {
		if p.MemberID == e.LineMemberID {
			if p.Host == "" {
				return errors.New("selected line member has no public endpoint")
			}
			e.InboundID, e.DisplayHost, e.DisplayPort = l.TargetInboundID, p.Host, p.Port
			return nil
		}
	}
	return errors.New("select a member of the line's first group")
}

func (s *Store) NodeHasLines(ctx context.Context, id int64) (bool, error) {
	return nodeHasLines(ctx, s.db, id)
}

func nodeHasLines(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, id int64) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM forward_path_hops WHERE node_id=?) + (SELECT COUNT(*) FROM forward_line_members WHERE node_id=?) + (SELECT COUNT(*) FROM forward_lines l WHERE l.target_inbound_id IN (
	SELECT id FROM inbounds WHERE node_id=?
	UNION SELECT user_inbound_id FROM node_reverse_links WHERE exit_id=?
	UNION SELECT receiver_inbound_id FROM node_reverse_links WHERE exit_id=?
	UNION SELECT user_inbound_id FROM node_wireguard_links WHERE exit_id=?))`, id, id, id, id, id, id).Scan(&n)
	if err != nil || n > 0 {
		return n > 0, err
	}
	return hasInternalForwardReference(ctx, q, id, 0)
}
func (s *Store) InboundHasLines(ctx context.Context, id int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM forward_lines WHERE target_inbound_id=?`, id).Scan(&n)
	if err != nil || n > 0 {
		return n > 0, err
	}
	return hasInternalForwardReference(ctx, s.db, 0, id)
}

func lineVersionOK(n *domain.Node, min string) bool {
	return n.Version == "dev" || n.Version == min || selfupdate.Newer(n.Version, min)
}

// RefreshLinesForNodeTx keeps literal next-hop addresses and bound entries
// aligned with node, mapping, or landing edits in the same transaction.
func refreshLinesForNodeTx(ctx context.Context, tx *sql.Tx, node int64) error {
	rows, err := tx.QueryContext(ctx, `SELECT `+lineCols+` FROM forward_lines WHERE id IN (SELECT line_id FROM forward_line_members WHERE node_id=?) OR target_inbound_id IN (SELECT id FROM inbounds WHERE node_id=?)`, node, node)
	if err != nil {
		return err
	}
	var lines []*ForwardLine
	for rows.Next() {
		l, e := scanLine(rows)
		if e != nil {
			rows.Close()
			return e
		}
		lines = append(lines, l)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, l := range lines {
		ib, err := scanInbound(tx.QueryRowContext(ctx, "SELECT "+inboundCols+" FROM inbounds WHERE id=?", l.TargetInboundID))
		if err != nil {
			return err
		}
		protocol, err := LineProtocol(ib)
		if err != nil {
			return fmt.Errorf("line %q: %w; choose another landing or remove this line first", l.Name, err)
		}
		l.Protocol = protocol
		owned, e := isPathRouteTx(ctx, tx, l.ID)
		if e != nil {
			return e
		}
		if !owned {
			if err = normalizeLine(l); err != nil {
				return fmt.Errorf("line %q: %w; update its candidate group strategy before changing the landing transport", l.Name, err)
			}
		}
		l.Version++
		if err = saveLineTx(ctx, tx, l, "recompile"); err != nil {
			return err
		}
	}
	return refreshInternalForwardsForNodeTx(ctx, tx, node)
}

func lineHostPort(host string, port int) (string, error) {
	if host == "" || !spec.Plain(host) || strings.ContainsAny(host, " /\\\"'") || port < 1 || port > 65535 {
		return "", errors.New("line member or landing has no usable relay address")
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}

// LineForwardSupported prevents older agents from ignoring candidate fields.
func LineForwardSupported(n *domain.Node, f NodeForward) bool {
	if f.LineID == 0 {
		return true
	}
	if f.Backend == "nft" {
		return lineVersionOK(n, "v0.62.0")
	}
	if f.Backend == "realm" || len(f.Targets) > 0 || f.ProxyProtocol {
		return lineVersionOK(n, "v0.49.0")
	}
	return true
}
