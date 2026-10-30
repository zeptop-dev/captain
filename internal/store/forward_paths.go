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
	"strings"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

var ErrPathConflict = errors.New("path revision changed; refresh before saving")
var ErrPathInUse = errors.New("path or source resource is referenced by an immutable path revision")
var ErrPathRouteOwned = errors.New("edit this complete path route through its route editor")

const maxPathDepth = 8
const maxPathHops = 8

// A step selects exactly one source. References are fixed revisions; editing a
// source never silently changes the listeners of an existing composition.
type PathStep struct {
	ID             string `json:"id"`
	NodeID         int64  `json:"node_id,omitempty"`
	IngressID      *int64 `json:"ingress_id,omitempty"`
	AddressMode    string `json:"address_mode,omitempty"`
	PathID         int64  `json:"path_id,omitempty"`
	PathRevision   int64  `json:"path_revision,omitempty"`
	LineID         int64  `json:"line_id,omitempty"`
	LineVersion    int64  `json:"line_version,omitempty"`
	IncludeLanding bool   `json:"include_landing,omitempty"`
}
type PathHop struct {
	Key         string `json:"key"`
	NodeID      int64  `json:"node_id"`
	IngressID   *int64 `json:"ingress_id,omitempty"`
	AddressMode string `json:"address_mode,omitempty"`
}
type ForwardPath struct {
	ID      int64      `json:"id"`
	Name    string     `json:"name"`
	Version int64      `json:"version"`
	Steps   []PathStep `json:"steps"`
	Hops    []PathHop  `json:"hops"`
	Depth   int        `json:"depth"`
}
type PathUse struct {
	Kind    string `json:"kind"`
	ID      int64  `json:"id"`
	Version int64  `json:"version"`
	Name    string `json:"name"`
}

func pathKey(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "/")))
	return hex.EncodeToString(h[:8])
}
func pathName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 128 || !spec.PublicLabel(name) {
		return "", errors.New("a plain name of at most 128 characters is required")
	}
	return name, nil
}
func pathRevisionTx(ctx context.Context, tx *sql.Tx, id, revision int64) (*ForwardPath, error) {
	var p ForwardPath
	var definition, hops string
	err := tx.QueryRowContext(ctx, `SELECT p.id,p.name,r.revision,r.definition_json,r.hops_json,r.depth FROM forward_paths p JOIN forward_path_revisions r ON r.path_id=p.id WHERE p.id=? AND r.revision=?`, id, revision).Scan(&p.ID, &p.Name, &p.Version, &definition, &hops, &p.Depth)
	if err != nil {
		return nil, wrapNotFound(err)
	}
	if err = json.Unmarshal([]byte(definition), &p.Steps); err != nil {
		return nil, err
	}
	if err = json.Unmarshal([]byte(hops), &p.Hops); err != nil {
		return nil, err
	}
	return &p, nil
}
func (s *Store) ForwardPath(ctx context.Context, id, revision int64) (*ForwardPath, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if revision == 0 {
		if err = tx.QueryRowContext(ctx, `SELECT version FROM forward_paths WHERE id=?`, id).Scan(&revision); err != nil {
			return nil, wrapNotFound(err)
		}
	}
	return pathRevisionTx(ctx, tx, id, revision)
}
func (s *Store) ListForwardPaths(ctx context.Context) ([]ForwardPath, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id,version FROM forward_paths ORDER BY id`)
	if err != nil {
		return nil, err
	}
	var keys [][2]int64
	for rows.Next() {
		var k [2]int64
		if err = rows.Scan(&k[0], &k[1]); err != nil {
			break
		}
		keys = append(keys, k)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := []ForwardPath{}
	for _, k := range keys {
		p, e := pathRevisionTx(ctx, tx, k[0], k[1])
		if e != nil {
			return nil, e
		}
		out = append(out, *p)
	}
	return out, nil
}
func pathAncestorTx(ctx context.Context, tx *sql.Tx, id, revision, wanted int64) (bool, error) {
	var found bool
	err := tx.QueryRowContext(ctx, `WITH RECURSIVE refs(id,revision) AS (SELECT ?,? UNION SELECT r.source_path_id,r.source_revision FROM forward_path_refs r JOIN refs a ON r.path_id=a.id AND r.revision=a.revision WHERE r.source_path_id IS NOT NULL) SELECT EXISTS(SELECT 1 FROM refs WHERE id=?)`, id, revision, wanted).Scan(&found)
	return found, err
}
func expandPathTx(ctx context.Context, tx *sql.Tx, p *ForwardPath) error {
	var err error
	p.Name, err = pathName(p.Name)
	if err != nil {
		return err
	}
	if len(p.Steps) < 1 || len(p.Steps) > maxPathHops {
		return errors.New("a path requires one to eight steps")
	}
	p.Hops = []PathHop{}
	p.Depth = 1
	ids := map[string]bool{}
	nodes := map[int64]bool{}
	for i := range p.Steps {
		st := &p.Steps[i]
		if st.ID == "" {
			st.ID = pathKey(rand.Text())
		}
		if len(st.ID) > 24 || !spec.ValidTag(st.ID) || ids[st.ID] {
			return errors.New("step IDs must be unique valid tags of at most 24 characters")
		}
		ids[st.ID] = true
		sources := 0
		for _, v := range []int64{st.NodeID, st.PathID, st.LineID} {
			if v < 0 {
				return errors.New("source IDs must be positive")
			}
			if v > 0 {
				sources++
			}
		}
		if sources != 1 {
			return errors.New("each path step must select one node, path revision or line version")
		}
		var hops []PathHop
		switch {
		case st.NodeID > 0:
			if st.PathRevision != 0 || st.LineVersion != 0 || st.IncludeLanding {
				return errors.New("node steps cannot contain reference options")
			}
			if err = validateAddressMode(st.AddressMode); err != nil {
				return err
			}
			hops = []PathHop{{Key: st.ID, NodeID: st.NodeID, IngressID: st.IngressID, AddressMode: st.AddressMode}}
		case st.PathID > 0:
			if st.PathRevision < 1 || st.IngressID != nil || st.AddressMode != "" || st.LineVersion != 0 || st.IncludeLanding {
				return errors.New("path references require an explicit revision and no node options")
			}
			cycle, e := pathAncestorTx(ctx, tx, st.PathID, st.PathRevision, p.ID)
			if e != nil {
				return e
			}
			if cycle {
				return errors.New("path reference cycle")
			}
			source, e := pathRevisionTx(ctx, tx, st.PathID, st.PathRevision)
			if e != nil {
				return e
			}
			if source.Depth+1 > p.Depth {
				p.Depth = source.Depth + 1
			}
			if p.Depth > maxPathDepth {
				return errors.New("path reference depth exceeds eight")
			}
			hops = append(hops, source.Hops...)
			for j := range hops {
				hops[j].Key = pathKey(st.ID, hops[j].Key)
			}
		case st.LineID > 0:
			if st.LineVersion < 1 || st.IngressID != nil || st.AddressMode != "" || st.PathRevision != 0 {
				return errors.New("line references require an explicit version and no node options")
			}
			source, e := pinnedLineHopsTx(ctx, tx, st)
			if e != nil {
				return e
			}
			hops = append(hops, source...)
			for j := range hops {
				hops[j].Key = pathKey(st.ID, hops[j].Key)
			}

		}
		for _, h := range hops {
			if nodes[h.NodeID] {
				return errors.New("a complete path cannot revisit a server")
			}
			nodes[h.NodeID] = true
			n, e := loadLineNode(ctx, tx, h.NodeID, 0)
			if e != nil {
				return e
			}
			if h.IngressID != nil && n.ingresses[*h.IngressID] == nil {
				return errors.New("path ingress does not belong to its server")
			}
			p.Hops = append(p.Hops, h)
		}
		if len(p.Hops) > maxPathHops {
			return errors.New("expanded path exceeds eight hops")
		}
	}
	return nil
}
func (s *Store) SaveForwardPath(ctx context.Context, p *ForwardPath) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	next := *p
	next.Steps = append([]PathStep(nil), p.Steps...)
	if p.ID == 0 {
		res, e := tx.ExecContext(ctx, `INSERT INTO forward_paths(name,version,created_at,updated_at) VALUES(?,1,?,?)`, p.Name, now(), now())
		if e != nil {
			return e
		}
		next.ID, _ = res.LastInsertId()
		next.Version = 1
	} else {
		var version int64
		if err = tx.QueryRowContext(ctx, `SELECT version FROM forward_paths WHERE id=?`, p.ID).Scan(&version); err != nil {
			return wrapNotFound(err)
		}
		if version != p.Version {
			return ErrPathConflict
		}
		next.Version++
	}
	if err = expandPathTx(ctx, tx, &next); err != nil {
		return err
	}
	steps, _ := json.Marshal(next.Steps)
	hops, _ := json.Marshal(next.Hops)
	if _, err = tx.ExecContext(ctx, `INSERT INTO forward_path_revisions(path_id,revision,definition_json,hops_json,depth,created_at) VALUES(?,?,?,?,?,?)`, next.ID, next.Version, string(steps), string(hops), next.Depth, now()); err != nil {
		return err
	}
	for _, h := range next.Hops {
		if _, err = tx.ExecContext(ctx, `INSERT INTO forward_path_hops(path_id,revision,hop_key,node_id,ingress_id) VALUES(?,?,?,?,?)`, next.ID, next.Version, h.Key, h.NodeID, nullInt64(h.IngressID)); err != nil {
			return err
		}
	}
	for _, st := range next.Steps {
		if st.PathID > 0 {
			_, err = tx.ExecContext(ctx, `INSERT INTO forward_path_refs(path_id,revision,step_id,source_path_id,source_revision) VALUES(?,?,?,?,?)`, next.ID, next.Version, st.ID, st.PathID, st.PathRevision)
		}
		if st.LineID > 0 {
			_, err = tx.ExecContext(ctx, `INSERT INTO forward_path_refs(path_id,revision,step_id,source_line_id,source_line_version,source_include_landing) VALUES(?,?,?,?,?,?)`, next.ID, next.Version, st.ID, st.LineID, st.LineVersion, boolInt(st.IncludeLanding))
		}
		if err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE forward_paths SET name=?,version=?,updated_at=? WHERE id=?`, next.Name, next.Version, now(), next.ID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	*p = next
	return nil
}
func pathUsesTx(ctx context.Context, tx *sql.Tx, id int64) ([]PathUse, error) {
	rows, err := tx.QueryContext(ctx, `WITH RECURSIVE users(id,revision) AS (SELECT path_id,revision FROM forward_path_refs WHERE source_path_id=? UNION SELECT r.path_id,r.revision FROM forward_path_refs r JOIN users u ON r.source_path_id=u.id AND r.source_revision=u.revision)
 SELECT 'path',p.id,u.revision,p.name FROM users u JOIN forward_paths p ON p.id=u.id
 UNION SELECT 'route',l.id,l.version,l.name FROM forward_path_route_refs r JOIN forward_lines l ON l.id=r.line_id WHERE r.path_id=? OR EXISTS(SELECT 1 FROM users u WHERE u.id=r.path_id AND u.revision=r.revision)
 UNION SELECT 'route',l.id,l.version,l.name FROM path_rollout_refs r JOIN line_rollouts p ON p.id=r.plan_id JOIN forward_lines l ON l.id=p.line_id WHERE p.status IN ('running','paused') AND (r.path_id=? OR EXISTS(SELECT 1 FROM users u WHERE u.id=r.path_id AND u.revision=r.revision)) ORDER BY 1,2,3`, id, id, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PathUse{}
	for rows.Next() {
		var u PathUse
		if err = rows.Scan(&u.Kind, &u.ID, &u.Version, &u.Name); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
func (s *Store) ForwardPathUses(ctx context.Context, id int64) ([]PathUse, error) {
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	return pathUsesTx(ctx, tx, id)
}
func (s *Store) DeleteForwardPath(ctx context.Context, id, version int64) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var v int64
	if e = tx.QueryRowContext(ctx, `SELECT version FROM forward_paths WHERE id=?`, id).Scan(&v); e != nil {
		return wrapNotFound(e)
	}
	if v != version {
		return ErrPathConflict
	}
	uses, e := pathUsesTx(ctx, tx, id)
	if e != nil {
		return e
	}
	if len(uses) > 0 {
		return ErrPathInUse
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM forward_paths WHERE id=?`, id); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM forward_path_line_versions WHERE NOT EXISTS(SELECT 1 FROM forward_path_refs r WHERE r.source_line_id=forward_path_line_versions.line_id AND r.source_line_version=forward_path_line_versions.version AND r.source_include_landing=forward_path_line_versions.include_landing)`); e != nil {
		return e
	}
	return tx.Commit()
}
func pathSourceLineInUseTx(ctx context.Context, tx *sql.Tx, id int64) error {
	var n int
	if e := tx.QueryRowContext(ctx, `SELECT count(*) FROM forward_path_refs WHERE source_line_id=?`, id).Scan(&n); e != nil {
		return e
	}
	if n > 0 {
		return fmt.Errorf("%w: source line", ErrPathInUse)
	}
	return nil
}

// A source line can continue evolving independently. Reuse the first captured
// structure for this pin instead of requiring the live line to remain unchanged.
func pinnedLineHopsTx(ctx context.Context, tx *sql.Tx, st *PathStep) ([]PathHop, error) {
	// A fixed pin may use a published source during an edit, but must not
	// resurrect a retiring line or expose a creation before its entry is ready.
	var unavailable bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM line_rollouts WHERE line_id=? AND status IN ('running','paused') AND (kind='delete' OR (kind='create' AND published_at IS NULL)))`, st.LineID).Scan(&unavailable); err != nil {
		return nil, err
	}
	if unavailable {
		return nil, ErrLineRolloutConflict
	}
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT hops_json FROM forward_path_line_versions WHERE line_id=? AND version=? AND include_landing=?`, st.LineID, st.LineVersion, boolInt(st.IncludeLanding)).Scan(&raw)
	if err == nil {
		var hops []PathHop
		err = json.Unmarshal([]byte(raw), &hops)
		return hops, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if owned, e := isPathRouteTx(ctx, tx, st.LineID); e != nil {
		return nil, e
	} else if owned {
		return nil, errors.New("reference path templates instead of a composed route")
	}
	l, e := scanLine(tx.QueryRowContext(ctx, "SELECT "+lineCols+" FROM forward_lines WHERE id=?", st.LineID))
	if e != nil {
		return nil, e
	}
	if l.Version != st.LineVersion {
		return nil, ErrPathConflict
	}
	if l.Protocol != "tcp" || l.Backend != "relay" {
		return nil, errors.New("only ordinary TCP relay lines can be reused")
	}
	hops := []PathHop{}
	for _, g := range l.Groups {
		if len(g.Members) != 1 {
			return nil, errors.New("source lines must be linear; select complete paths explicitly")
		}
		m := g.Members[0]
		hops = append(hops, PathHop{m.ID, m.NodeID, m.IngressID, m.AddressMode})
	}
	if st.IncludeLanding {
		ib, e := scanInbound(tx.QueryRowContext(ctx, "SELECT "+inboundCols+" FROM inbounds WHERE id=?", l.TargetInboundID))
		if e != nil {
			return nil, e
		}
		if ib.Settings.Reverse != nil || ib.Settings.WireGuardSlot != 0 {
			return nil, errors.New("reverse and WireGuard landings cannot be imported as ordinary paths")
		}
		hops = append(hops, PathHop{"landing", ib.NodeID, ib.IngressID, l.TargetAddressMode})
	}
	encoded, _ := json.Marshal(hops)
	if _, e = tx.ExecContext(ctx, `INSERT INTO forward_path_line_versions(line_id,version,include_landing,hops_json) VALUES(?,?,?,?)`, st.LineID, st.LineVersion, boolInt(st.IncludeLanding), string(encoded)); e != nil {
		return nil, e
	}
	return hops, nil
}
