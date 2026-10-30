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
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

var ErrTopologyConflict = errors.New("topology or draft changed; refresh and preview again")
var ErrTopologyUnsupported = errors.New("this operation is outside the supported topology draft fields")

const topologyOperationLimit = 32

type TopologyInboundPatch struct {
	NodeID             int64         `json:"node_id,omitempty"`
	Tag                *string       `json:"tag,omitempty"`
	Listen             *string       `json:"listen,omitempty"`
	Port               *int          `json:"port,omitempty"`
	Enabled            *bool         `json:"enabled,omitempty"`
	Sort               *int          `json:"sort,omitempty"`
	IngressID          *int64        `json:"ingress_id,omitempty"`
	Protocol           spec.Protocol `json:"protocol,omitempty"`
	Core               string        `json:"core,omitempty"`
	Settings           *spec.Inbound `json:"settings,omitempty"`
	SettingsConfigured bool          `json:"settings_configured,omitempty"`
}
type TopologyEntryPatch struct {
	Name         *string   `json:"name,omitempty"`
	InboundID    *int64    `json:"inbound_id,omitempty"`
	InboundRef   string    `json:"inbound_ref,omitempty"`
	LineID       *int64    `json:"line_id,omitempty"`
	LineRef      string    `json:"line_ref,omitempty"`
	LineMemberID *string   `json:"line_member_id,omitempty"`
	DisplayHost  *string   `json:"display_host,omitempty"`
	DisplayPort  *int      `json:"display_port,omitempty"`
	Rate         *float64  `json:"rate,omitempty"`
	Sort         *int      `json:"sort,omitempty"`
	Enabled      *bool     `json:"enabled,omitempty"`
	Tags         *[]string `json:"tags,omitempty"`
	Region       *string   `json:"region,omitempty"`
}
type TopologyOperation struct {
	Key        string                `json:"key"`
	Kind       string                `json:"kind"`
	ID         int64                 `json:"id,omitempty"`
	Revision   string                `json:"revision,omitempty"`
	Inbound    *TopologyInboundPatch `json:"inbound,omitempty"`
	Line       *ForwardLine          `json:"line,omitempty"`
	InboundRef string                `json:"inbound_ref,omitempty"`
	Entry      *TopologyEntryPatch   `json:"entry,omitempty"`
}
type TopologyResult struct {
	Key  string `json:"key"`
	Kind string `json:"kind"`
	ID   int64  `json:"id"`
}
type TopologyChangeset struct {
	bindings    map[string]string
	ID          string              `json:"id"`
	Version     int64               `json:"version"`
	Name        string              `json:"name"`
	Status      string              `json:"status"`
	Operations  []TopologyOperation `json:"operations"`
	Result      []TopologyResult    `json:"result"`
	CreatedAt   time.Time           `json:"created_at"`
	UpdatedAt   time.Time           `json:"updated_at"`
	CommittedAt *time.Time          `json:"committed_at,omitempty"`
}
type TopologyChange struct {
	Key      string           `json:"key"`
	Kind     string           `json:"kind"`
	ID       int64            `json:"id"`
	Before   any              `json:"before"`
	After    any              `json:"after"`
	Forwards []LineNodeChange `json:"forwards,omitempty"`
}
type TopologyPreview struct {
	Revision string           `json:"revision"`
	Changes  []TopologyChange `json:"changes"`
}

func topologyDigest(v any) string {
	b, _ := json.Marshal(v)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
func topologyCopy[T any](v T) T {
	b, _ := json.Marshal(v)
	var out T
	_ = json.Unmarshal(b, &out)
	return out
}
func topologyPublic(d *TopologyChangeset) *TopologyChangeset {
	out := topologyCopy(*d)
	for i := range out.Operations {
		if p := out.Operations[i].Inbound; p != nil {
			p.SettingsConfigured = p.SettingsConfigured || p.Settings != nil
			p.Settings = nil
		}
	}
	return &out
}
func scanTopologyDraft(row rowScanner) (*TopologyChangeset, error) {
	var d TopologyChangeset
	var ops, result, bindings string
	var cr, up int64
	var done sql.NullInt64
	err := row.Scan(&d.ID, &d.Version, &d.Name, &d.Status, &ops, &result, &cr, &up, &done, &bindings)
	if err != nil {
		return nil, wrapNotFound(err)
	}
	if err = json.Unmarshal([]byte(ops), &d.Operations); err != nil {
		return nil, err
	}
	if err = json.Unmarshal([]byte(result), &d.Result); err != nil {
		return nil, err
	}
	if err = json.Unmarshal([]byte(bindings), &d.bindings); err != nil {
		return nil, err
	}
	d.CreatedAt = unix(cr)
	d.UpdatedAt = unix(up)
	d.CommittedAt = unixPtr(done)
	return &d, nil
}

const topologyCols = "id,version,name,status,operations_json,result_json,created_at,updated_at,committed_at,bindings_json"

func (s *Store) TopologyChangeset(ctx context.Context, id string) (*TopologyChangeset, error) {
	d, e := scanTopologyDraft(s.db.QueryRowContext(ctx, "SELECT "+topologyCols+" FROM topology_changesets WHERE id=?", id))
	if e != nil {
		return nil, e
	}
	return topologyPublic(d), nil
}
func (s *Store) ListTopologyChangesets(ctx context.Context) ([]TopologyChangeset, error) {
	rows, e := s.db.QueryContext(ctx, "SELECT "+topologyCols+" FROM topology_changesets ORDER BY CASE status WHEN 'draft' THEN 0 ELSE 1 END,updated_at DESC,id LIMIT 150")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []TopologyChangeset{}
	for rows.Next() {
		d, e := scanTopologyDraft(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, *topologyPublic(d))
	}
	return out, rows.Err()
}
func (s *Store) SaveTopologyChangeset(ctx context.Context, d *TopologyChangeset) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	next := topologyCopy(*d)
	next.Name = strings.TrimSpace(next.Name)
	if next.Name == "" || len(next.Name) > 128 || !spec.PublicLabel(next.Name) {
		return errors.New("a plain draft name of at most 128 characters is required")
	}
	if len(next.Operations) == 0 || len(next.Operations) > topologyOperationLimit {
		return errors.New("a topology draft requires one to 32 operations")
	}
	oldOps := map[string]TopologyOperation{}
	oldBindings := map[string]string{}
	if next.ID == "" {
		var n int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM topology_changesets WHERE status='draft'`).Scan(&n); e != nil {
			return e
		}
		if n >= 50 {
			return errors.New("discard an unused draft before creating another")
		}
		next.ID = rand.Text()
		next.Version = 1
		next.CreatedAt = time.Now()
	} else {
		old, e := scanTopologyDraft(tx.QueryRowContext(ctx, "SELECT "+topologyCols+" FROM topology_changesets WHERE id=?", next.ID))
		if e != nil {
			return e
		}
		if old.Status != "draft" || old.Version != d.Version {
			return ErrTopologyConflict
		}
		oldBindings = old.bindings
		next.Version++
		next.CreatedAt = old.CreatedAt
		for _, op := range old.Operations {
			oldOps[op.Key] = op
		}
	}
	seen := map[string]bool{}
	objects := map[string]bool{}
	for i := range next.Operations {
		op := &next.Operations[i]
		if op.Key == "" || len(op.Key) > 48 || !spec.ValidTag(op.Key) || seen[op.Key] || op.ID < 0 {
			return errors.New("operation keys must be unique valid tags")
		}
		seen[op.Key] = true
		if op.ID > 0 {
			key := fmt.Sprintf("%s:%d", op.Kind, op.ID)
			if objects[key] {
				return errors.New("an object can appear only once in a draft")
			}
			objects[key] = true
			if op.Revision == "" {
				return ErrTopologyConflict
			}
		}
		count := 0
		if op.Inbound != nil {
			count++
		}
		if op.Line != nil {
			count++
		}
		if op.Entry != nil {
			count++
		}
		if count != 1 {
			return ErrTopologyUnsupported
		}
		switch op.Kind {
		case "inbound":
			if op.Inbound == nil || op.InboundRef != "" {
				return ErrTopologyUnsupported
			}
			p := op.Inbound
			p.SettingsConfigured = false
			if op.ID > 0 {
				if p.Protocol != "" || p.Core != "" || p.Settings != nil || p.NodeID != 0 {
					return ErrTopologyUnsupported
				}
			} else {
				prior, ok := oldOps[op.Key]
				if ok && prior.Kind == op.Kind && prior.ID == 0 && prior.Inbound != nil {
					if p.Protocol != prior.Inbound.Protocol || p.Core != prior.Inbound.Core || p.NodeID != prior.Inbound.NodeID {
						return ErrTopologyUnsupported
					}
					if p.Settings != nil {
						return errors.New("creation settings are fixed after saving; replace the draft operation to change them")
					}
					p.Settings = prior.Inbound.Settings
				} else {
					if p.Settings == nil {
						p.Settings = &spec.Inbound{}
					}
					if p.Settings.Reverse != nil || p.Settings.WireGuardSlot != 0 || p.Settings.PrivateAccess != nil || p.Settings.ScopedUsers || len(p.Settings.Users) > 0 {
						return ErrTopologyUnsupported
					}
					p.Settings.Protocol = p.Protocol
					spec.FillInboundSecrets(p.Settings)
				}
			}
		case "line":
			if op.Line == nil || op.Line.PathRoute {
				return ErrTopologyUnsupported
			}
			op.Line.PreviewRevision = ""
			for gi := range op.Line.Groups {
				for mi := range op.Line.Groups[gi].Members {
					m := &op.Line.Groups[gi].Members[mi]
					if m.ID == "" {
						m.ID = pathKey(rand.Text())
					}
				}
			}
		case "entry":
			if op.Entry == nil || op.InboundRef != "" {
				return ErrTopologyUnsupported
			}
		default:
			return ErrTopologyUnsupported
		}
	}
	next.bindings, e = topologyBindingsTx(ctx, tx, &next, oldBindings)
	if e != nil {
		return e
	}
	bindings, _ := json.Marshal(next.bindings)
	raw, _ := json.Marshal(next.Operations)
	if len(raw) > 256<<10 {
		return errors.New("topology draft exceeds 256 KiB")
	}
	at := now()
	next.Status = "draft"
	next.UpdatedAt = unix(at)
	next.Result = []TopologyResult{}
	if d.ID == "" {
		_, e = tx.ExecContext(ctx, `INSERT INTO topology_changesets(id,version,name,status,operations_json,actor,created_at,updated_at,bindings_json) VALUES(?,?,?,'draft',?,?,?,?,?)`, next.ID, next.Version, next.Name, string(raw), lineChangeActor(ctx), next.CreatedAt.Unix(), at, string(bindings))
	} else {
		_, e = tx.ExecContext(ctx, `UPDATE topology_changesets SET version=?,name=?,operations_json=?,updated_at=?,bindings_json=? WHERE id=?`, next.Version, next.Name, string(raw), at, string(bindings), next.ID)
	}
	if e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	*d = *topologyPublic(&next)
	return nil
}
func (s *Store) DiscardTopologyChangeset(ctx context.Context, id string, version int64) error {
	r, e := s.db.ExecContext(ctx, `DELETE FROM topology_changesets WHERE id=? AND version=? AND status='draft'`, id, version)
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return ErrTopologyConflict
	}
	return nil
}
