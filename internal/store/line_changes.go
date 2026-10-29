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
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
)

type lineChangeActorKey struct{}

// WithLineChangeActor accepts an identity established by the trusted caller.
func WithLineChangeActor(ctx context.Context, actor string) context.Context {
	return context.WithValue(ctx, lineChangeActorKey{}, actor)
}
func lineChangeActor(ctx context.Context) string {
	v, _ := ctx.Value(lineChangeActorKey{}).(string)
	if v == "" {
		return "system"
	}
	if len(v) > 320 {
		return v[:320]
	}
	return v
}

type LineChange struct {
	ID           string             `json:"id"`
	Cursor       int64              `json:"cursor"`
	LineID       int64              `json:"line_id"`
	ScopeID      string             `json:"scope_id"`
	Version      int64              `json:"version"`
	Name         string             `json:"name"`
	Kind         string             `json:"kind"`
	Actor        string             `json:"actor"`
	CreatedAt    time.Time          `json:"created_at"`
	SupersededAt *time.Time         `json:"superseded_at,omitempty"`
	Status       string             `json:"status"`
	Targets      []LineChangeTarget `json:"targets"`
}
type LineChangeTarget struct {
	NodeID         int64               `json:"node_id"`
	NodeName       string              `json:"node_name"`
	Action         string              `json:"action"`
	Status         string              `json:"status"`
	ConfigRevision string              `json:"config_revision"`
	StateRevision  string              `json:"state_revision"`
	DeliveryID     string              `json:"delivery_id"`
	IssuedAt       *time.Time          `json:"issued_at,omitempty"`
	ConfirmedAt    *time.Time          `json:"confirmed_at,omitempty"`
	ObservedAt     *time.Time          `json:"observed_at,omitempty"`
	LastReceiptAt  *time.Time          `json:"last_receipt_at,omitempty"`
	FreshForMS     int64               `json:"fresh_for_ms"`
	Before         []LineChangeForward `json:"before"`
	After          []LineChangeForward `json:"after"`
}

// Explicit wire-field allowlist, independent of future secret-bearing spec fields.
type LineChangeForward struct {
	Tag            string               `json:"tag"`
	Listen         string               `json:"listen"`
	Port           int                  `json:"port"`
	Protocol       string               `json:"protocol"`
	Backend        string               `json:"backend"`
	ProxyProtocol  bool                 `json:"proxy_protocol"`
	PreserveSource bool                 `json:"preserve_source"`
	Balance        string               `json:"balance"`
	Targets        []LineExpectedTarget `json:"targets"`
}
type LineChangePage struct {
	Items      []LineChange `json:"items"`
	NextBefore int64        `json:"next_before,omitempty"`
}

type lineChangeTargetRecord struct {
	LineChangeTarget
	change, scope, identity, token, expected, session string
	lineID                                            int64
	superseded                                        sql.NullInt64
	sequence                                          int64
	applied                                           bool
}

func lineChangeProjection(fs []spec.Forward) []LineChangeForward {
	out := make([]LineChangeForward, 0, len(fs))
	for _, f := range fs {
		targets := make([]LineExpectedTarget, 0, len(f.Hops()))
		for _, h := range f.Hops() {
			targets = append(targets, LineExpectedTarget{Target: h.Target, Weight: h.Weight})
		}
		out = append(out, LineChangeForward{Tag: f.Tag, Listen: f.Listen, Port: f.Port, Protocol: f.Protocol, Backend: f.Backend, ProxyProtocol: f.ProxyProtocol, PreserveSource: f.PreserveSource, Balance: f.BalanceMode(), Targets: targets})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Tag < out[j].Tag })
	return out
}
func lineChangeDigest(fs []LineChangeForward) string {
	raw, _ := json.Marshal(fs)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func lineWireForwardsTx(ctx context.Context, tx *sql.Tx, nodeID, lineID int64) ([]spec.Forward, error) {
	fs, err := nodeForwardsTx(ctx, tx, nodeID)
	if err != nil {
		return nil, err
	}
	out := []spec.Forward{}
	for _, f := range fs {
		if f.LineID == lineID {
			if f.Listen == "" && f.IngressID != "" {
				var bind string
				err = tx.QueryRowContext(ctx, `SELECT bind_ip FROM ingresses WHERE id=? AND node_id=?`, f.IngressID, nodeID).Scan(&bind)
				if err != nil {
					return nil, err
				}
				f.Listen = bind
			}
			f.IngressID = ""
			out = append(out, f.Forward)
		}
	}
	return out, nil
}
func lineOwnedSnapshotTx(ctx context.Context, tx *sql.Tx, lineID int64) (map[int64][]LineChangeForward, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id,forwards_json FROM nodes`)
	if err != nil {
		return nil, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		var raw string
		if err = rows.Scan(&id, &raw); err != nil {
			break
		}
		var fs []NodeForward
		if err = json.Unmarshal([]byte(raw), &fs); err != nil {
			break
		}
		for _, f := range fs {
			if f.LineID == lineID {
				ids = append(ids, id)
				break
			}
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := map[int64][]LineChangeForward{}
	for _, id := range ids {
		fs, e := lineWireForwardsTx(ctx, tx, id, lineID)
		if e != nil {
			return nil, e
		}
		out[id] = lineChangeProjection(fs)
	}
	return out, nil
}

// Called inside the same transaction that changes owned forwarding and entries.
func recordLineChangeTx(ctx context.Context, tx *sql.Tx, l *ForwardLine, kind string, before map[int64][]LineChangeForward) error {
	after, err := lineOwnedSnapshotTx(ctx, tx, l.ID)
	if err != nil {
		return err
	}
	var scope string
	if err = tx.QueryRowContext(ctx, `SELECT id FROM line_change_scopes WHERE line_id=?`, l.ID).Scan(&scope); err != nil {
		return err
	}
	change := rand.Text()
	at := time.Now().UnixMilli()
	if _, err = tx.ExecContext(ctx, `INSERT INTO line_changes(id,scope_id,line_id,version,name,kind,actor,created_at) VALUES(?,?,?,?,?,?,?,?)`, change, scope, l.ID, l.Version, l.Name, kind, lineChangeActor(ctx), at); err != nil {
		return err
	}
	ids := map[int64]bool{}
	for id := range before {
		ids[id] = true
	}
	for id := range after {
		ids[id] = true
	}
	for id := range ids {
		b, a := before[id], after[id]
		if b == nil {
			b = []LineChangeForward{}
		}
		if a == nil {
			a = []LineChangeForward{}
		}
		if reflect.DeepEqual(b, a) {
			continue
		}
		var name, identity, token string
		if err = tx.QueryRowContext(ctx, `SELECT n.name,p.uuid,COALESCE(n.token_hash,'') FROM nodes n JOIN probe_public_ids p ON p.node_id=n.id WHERE n.id=?`, id).Scan(&name, &identity, &token); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE line_change_targets SET superseded_at=? WHERE scope_id=? AND node_id=? AND superseded_at IS NULL`, at, scope, id); err != nil {
			return err
		}
		br, _ := json.Marshal(b)
		ar, _ := json.Marshal(a)
		action := "apply"
		if len(a) == 0 {
			action = "remove"
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO line_change_targets(change_id,scope_id,node_id,node_identity,node_name,token_identity,action,before_json,after_json,expected_digest) VALUES(?,?,?,?,?,?,?,?,?,?)`, change, scope, id, identity, name, token, action, string(br), string(ar), lineChangeDigest(a)); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE line_changes SET superseded_at=? WHERE scope_id=? AND superseded_at IS NULL AND EXISTS(SELECT 1 FROM line_change_targets t WHERE t.change_id=line_changes.id) AND NOT EXISTS(SELECT 1 FROM line_change_targets t WHERE t.change_id=line_changes.id AND t.superseded_at IS NULL)`, at, scope); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE line_change_scopes SET latest_change_id=? WHERE id=?`, change, scope); err != nil {
		return err
	}
	// Every active target remains an independently observable baseline. Only
	// superseded/no-runtime history outside the latest 100 records can be pruned.
	_, err = tx.ExecContext(ctx, `DELETE FROM line_changes WHERE scope_id=? AND ordinal NOT IN(SELECT ordinal FROM line_changes WHERE scope_id=? ORDER BY ordinal DESC LIMIT 100) AND NOT EXISTS(SELECT 1 FROM line_change_targets t WHERE t.change_id=line_changes.id AND t.superseded_at IS NULL)`, scope, scope)
	return err
}

const lineChangeCols = `id,ordinal,line_id,scope_id,version,name,kind,actor,created_at,superseded_at`

func scanLineChange(row rowScanner) (LineChange, error) {
	var c LineChange
	var at int64
	var superseded sql.NullInt64
	err := row.Scan(&c.ID, &c.Cursor, &c.LineID, &c.ScopeID, &c.Version, &c.Name, &c.Kind, &c.Actor, &at, &superseded)
	c.CreatedAt = time.UnixMilli(at)
	c.SupersededAt = lineMillisPtr(superseded)
	c.Targets = []LineChangeTarget{}
	return c, err
}
func lineMillisPtr(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := time.UnixMilli(v.Int64)
	return &t
}

const lineTargetCols = `t.change_id,t.scope_id,t.node_id,t.node_identity,t.node_name,t.token_identity,t.action,t.before_json,t.after_json,t.expected_digest,t.superseded_at,t.delivery_id,t.session,t.config_revision,t.state_revision,t.issued_at,t.confirmed_at,t.observed_at,t.last_receipt_at,t.receipt_sequence,t.applied,c.line_id`

func scanLineTarget(row rowScanner) (lineChangeTargetRecord, error) {
	var t lineChangeTargetRecord
	var before, after string
	var issued, confirmed, observed, received sql.NullInt64
	err := row.Scan(&t.change, &t.scope, &t.NodeID, &t.identity, &t.NodeName, &t.token, &t.Action, &before, &after, &t.expected, &t.superseded, &t.DeliveryID, &t.session, &t.ConfigRevision, &t.StateRevision, &issued, &confirmed, &observed, &received, &t.sequence, &t.applied, &t.lineID)
	if err != nil {
		return t, err
	}
	if err = json.Unmarshal([]byte(before), &t.Before); err != nil {
		return t, err
	}
	if err = json.Unmarshal([]byte(after), &t.After); err != nil {
		return t, err
	}
	t.IssuedAt = lineMillisPtr(issued)
	t.ConfirmedAt = lineMillisPtr(confirmed)
	t.ObservedAt = lineMillisPtr(observed)
	t.LastReceiptAt = lineMillisPtr(received)
	return t, nil
}
func lineTargetsTx(ctx context.Context, tx *sql.Tx, where string, args ...any) ([]lineChangeTargetRecord, error) {
	rows, err := tx.QueryContext(ctx, `SELECT `+lineTargetCols+` FROM line_change_targets t JOIN line_changes c ON c.id=t.change_id WHERE `+where+` ORDER BY t.node_id`, args...)
	if err != nil {
		return nil, err
	}
	out := []lineChangeTargetRecord{}
	for rows.Next() {
		t, e := scanLineTarget(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		out = append(out, t)
	}
	err = rows.Err()
	rows.Close()
	return out, err
}
func readLineTargetsTx(ctx context.Context, tx *sql.Tx, c *LineChange, at time.Time) error {
	targets, err := lineTargetsTx(ctx, tx, `t.change_id=?`, c.ID)
	if err != nil {
		return err
	}
	active, applied, superseded := 0, 0, 0
	for _, t := range targets {
		t.Status = "pending"
		var identity, token string
		err = tx.QueryRowContext(ctx, `SELECT p.uuid,COALESCE(n.token_hash,'') FROM nodes n JOIN probe_public_ids p ON p.node_id=n.id WHERE n.id=?`, t.NodeID).Scan(&identity, &token)
		switch {
		case t.superseded.Valid:
			t.Status = "superseded"
			superseded++
		case errors.Is(err, sql.ErrNoRows) || err == nil && identity != t.identity:
			t.Status = "removed"
			active++
		case err != nil:
			return err
		case t.token != "" && token != t.token:
			t.Status = "identity_changed"
			active++
		default:
			active++
			if t.DeliveryID != "" {
				t.Status = "delivered"
			}
			if t.LastReceiptAt != nil {
				t.Status = "stale"
				var current string
				e := tx.QueryRowContext(ctx, `SELECT h.delivery_id FROM node_config_heads h JOIN node_config_sessions s ON s.node_id=h.node_id AND s.token_identity=h.token_identity AND s.session=h.session WHERE h.node_id=? AND h.token_identity=? AND s.retired_at IS NULL`, t.NodeID, token).Scan(&current)
				if e != nil && !errors.Is(e, sql.ErrNoRows) {
					return e
				}
				live, liveErr := lineWireForwardsTx(ctx, tx, t.NodeID, t.lineID)
				if liveErr != nil {
					return liveErr
				}
				evidence := NodeConfigDeliveryRecord{Current: token != "" && current == t.DeliveryID && lineChangeDigest(lineChangeProjection(live)) == t.expected, ObservedAt: t.ObservedAt, LastReceiptAt: t.LastReceiptAt}
				setNodeConfigFreshness(&evidence, at)
				t.FreshForMS = evidence.FreshForMS
				if evidence.Fresh {
					t.Status = "not_applied"
					if t.applied {
						t.Status = "applied"
						applied++
					}
				}
			}
		}
		c.Targets = append(c.Targets, t.LineChangeTarget)
	}
	switch {
	case len(targets) == 0:
		c.Status = "no_runtime_change"
	case superseded == len(targets):
		c.Status = "superseded"
	case superseded == 0 && active > 0 && applied == active:
		c.Status = "applied"
	case applied > 0:
		c.Status = "partial"
	default:
		c.Status = "pending"
	}
	return nil
}
func (s *Store) LineChangeByID(ctx context.Context, id string, at time.Time) (*LineChange, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	c, err := scanLineChange(tx.QueryRowContext(ctx, `SELECT `+lineChangeCols+` FROM line_changes WHERE id=?`, id))
	if err != nil {
		return nil, wrapNotFound(err)
	}
	if err = readLineTargetsTx(ctx, tx, &c, at); err != nil {
		return nil, err
	}
	return &c, tx.Commit()
}
func (s *Store) ListLineChanges(ctx context.Context, lineID, before int64, limit int) (*LineChangePage, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	query := `SELECT ` + lineChangeCols + ` FROM line_changes WHERE (?=0 OR ordinal<?)`
	args := []any{before, before}
	if lineID > 0 {
		var scope string
		err = tx.QueryRowContext(ctx, `SELECT id FROM line_change_scopes WHERE original_line_id=? ORDER BY (line_id IS NOT NULL) DESC,ordinal DESC LIMIT 1`, lineID).Scan(&scope)
		if errors.Is(err, sql.ErrNoRows) {
			return &LineChangePage{Items: []LineChange{}}, nil
		}
		if err != nil {
			return nil, err
		}
		query += ` AND scope_id=?`
		args = append(args, scope)
	}
	query += ` ORDER BY ordinal DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	out := &LineChangePage{Items: []LineChange{}}
	for rows.Next() {
		c, e := scanLineChange(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		out.Items = append(out.Items, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
		out.NextBefore = out.Items[limit-1].Cursor
	}
	at := time.Now()
	for i := range out.Items {
		if err = readLineTargetsTx(ctx, tx, &out.Items[i], at); err != nil {
			return nil, err
		}
	}
	return out, tx.Commit()
}
func (s *Store) LatestLineChangeID(ctx context.Context, lineID int64) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT latest_change_id FROM line_change_scopes WHERE original_line_id=? ORDER BY (line_id IS NOT NULL) DESC,ordinal DESC LIMIT 1`, lineID).Scan(&id)
	return id, wrapNotFound(err)
}

// Match only line-owned wire fields after the service's capability filtering.
// Empty removals require the entire numeric-line prefix to be absent.
func matchingLineTargetsTx(ctx context.Context, tx *sql.Tx, nodeID int64, token string, forwards []spec.Forward) ([]lineChangeTargetRecord, error) {
	var identity string
	if err := tx.QueryRowContext(ctx, `SELECT uuid FROM probe_public_ids WHERE node_id=?`, nodeID).Scan(&identity); err != nil {
		return nil, err
	}
	targets, err := lineTargetsTx(ctx, tx, `t.node_id=? AND t.superseded_at IS NULL`, nodeID)
	if err != nil {
		return nil, err
	}
	out := []lineChangeTargetRecord{}
	for _, t := range targets {
		if t.identity != identity || t.token != "" && t.token != token {
			continue
		}
		prefix := fmt.Sprintf("line-%d-", t.lineID)
		delivered := []spec.Forward{}
		for _, f := range forwards {
			if strings.HasPrefix(f.Tag, prefix) {
				delivered = append(delivered, f)
			}
		}
		if lineChangeDigest(lineChangeProjection(delivered)) != t.expected {
			continue
		}
		// A stale cached State may match an old plan after another write committed.
		live, e := lineWireForwardsTx(ctx, tx, nodeID, t.lineID)
		if e != nil {
			return nil, e
		}
		if lineChangeDigest(lineChangeProjection(live)) != t.expected {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

// Every delivery must carry its own target mapping. A matching historical
// state digest cannot borrow a mapping from a different change (A -> B -> A).
func lineTargetsNeedDeliveryTx(ctx context.Context, tx *sql.Tx, targets []lineChangeTargetRecord, delivery string) (bool, error) {
	for _, t := range targets {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM line_change_deliveries WHERE delivery_id=? AND change_id=? AND node_id=?)`, delivery, t.change, t.NodeID).Scan(&exists); err != nil {
			return false, err
		}
		if !exists {
			return true, nil
		}
	}
	return false, nil
}
func mapLineTargetsTx(ctx context.Context, tx *sql.Tx, targets []lineChangeTargetRecord, d *agentproto.ConfigDelivery) error {
	for _, t := range targets {
		if _, err := tx.ExecContext(ctx, `INSERT INTO line_change_deliveries(delivery_id,change_id,node_id) VALUES(?,?,?) ON CONFLICT DO NOTHING`, d.ID, t.change, t.NodeID); err != nil {
			return err
		}
	}
	return nil
}
func bindLineTargetsTx(ctx context.Context, tx *sql.Tx, targets []lineChangeTargetRecord, token string, d *agentproto.ConfigDelivery) error {
	if d == nil {
		return nil
	}
	// The first GET of an unknown process only creates a challenge; it must
	// not erase the active process's line evidence before challenge acceptance.
	var active bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM node_config_heads WHERE delivery_id=? AND token_identity=? AND session=?)`, d.ID, token, d.Session).Scan(&active); err != nil {
		return err
	}
	if !active {
		return nil
	}
	var cfg, state string
	var issued int64
	if err := tx.QueryRowContext(ctx, `SELECT config_revision,state_revision,issued_at FROM node_config_deliveries WHERE id=?`, d.ID).Scan(&cfg, &state, &issued); err != nil {
		return err
	}
	for _, t := range targets {
		if t.DeliveryID == d.ID {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE line_change_targets SET token_identity=?,delivery_id=?,session=?,config_revision=?,state_revision=?,issued_at=?,observed_at=NULL,last_receipt_at=NULL,receipt_sequence=0,applied=0 WHERE change_id=? AND node_id=? AND superseded_at IS NULL`, token, d.ID, d.Session, cfg, state, issued, t.change, t.NodeID); err != nil {
			return err
		}
	}
	return nil
}
func recordLineTargetReceiptTx(ctx context.Context, tx *sql.Tx, nodeID int64, token string, r *agentproto.ConfigReceipt, applied bool, at time.Time) error {
	// The caller has validated sequence, timestamps, process/token identity and
	// candidate parent, then activated this delivery in the same transaction.
	targets, err := lineTargetsTx(ctx, tx, `t.node_id=? AND t.superseded_at IS NULL AND EXISTS(SELECT 1 FROM line_change_deliveries m WHERE m.change_id=t.change_id AND m.node_id=t.node_id AND m.delivery_id=?)`, nodeID, r.DeliveryID)
	if err != nil {
		return err
	}
	var identity string
	if err = tx.QueryRowContext(ctx, `SELECT uuid FROM probe_public_ids WHERE node_id=?`, nodeID).Scan(&identity); err != nil {
		return err
	}
	for _, t := range targets {
		if t.identity != identity || t.token != "" && t.token != token {
			continue
		}
		live, e := lineWireForwardsTx(ctx, tx, nodeID, t.lineID)
		if e != nil {
			return e
		}
		if lineChangeDigest(lineChangeProjection(live)) != t.expected {
			continue
		}
		if err = bindLineTargetsTx(ctx, tx, []lineChangeTargetRecord{t}, token, &agentproto.ConfigDelivery{ID: r.DeliveryID, Session: r.Session}); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE line_change_targets SET confirmed_at=CASE WHEN ? THEN COALESCE(confirmed_at,?) ELSE confirmed_at END,observed_at=?,last_receipt_at=?,receipt_sequence=?,applied=? WHERE change_id=? AND node_id=?`, applied, at.UnixMilli(), r.ObservedAt.UnixMilli(), at.UnixMilli(), int64(r.Sequence), applied, t.change, nodeID); err != nil {
			return err
		}
	}
	return nil
}
