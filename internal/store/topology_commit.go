package store

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
)

type topologyCompileContext struct{}

// CommitTopologyChangeset validates and applies the complete final view in one
// SQLite transaction. It never invokes an HTTP write or opens a nested transaction.
func (s *Store) CommitTopologyChangeset(ctx context.Context, id string, version int64, revision string) (*TopologyChangeset, error) {
	if revision == "" {
		return nil, ErrTopologyConflict
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	d, err := scanTopologyDraft(tx.QueryRowContext(ctx, `SELECT `+topologyCols+` FROM topology_changesets WHERE id=?`, id))
	if err != nil {
		return nil, err
	}
	if d.Status != "draft" || d.Version != version {
		return nil, ErrTopologyConflict
	}
	p, err := prepareTopologyTx(ctx, tx, d)
	if err != nil {
		return nil, err
	}
	if p.preview.Revision != revision {
		return nil, ErrTopologyConflict
	}
	ids := map[int64]int64{}
	// Free changed unique tags before any final insert/update. This permits an
	// atomic tag swap without exposing the temporary names outside the transaction.
	for _, oldID := range topologyKeys(p.inbounds) {
		ib := p.inbounds[oldID]
		if oldID > 0 && ib.Tag != p.base.inbounds[oldID].Tag {
			if _, err = tx.ExecContext(ctx, `UPDATE inbounds SET tag=? WHERE id=?`, "draft-"+rand.Text(), oldID); err != nil {
				return nil, err
			}
		}
	}

	// Allocate only inside this transaction. Generated creation secrets are already
	// persisted in the draft and do not change when preview or commit is retried.
	for _, oldID := range topologyKeys(p.inbounds) {
		ib := p.inbounds[oldID]
		if err = writeInboundTx(ctx, tx, ib, oldID < 0); err != nil {
			return nil, err
		}
		if oldID < 0 {
			ids[oldID] = ib.ID
			delete(p.view.inbounds, oldID)
			p.view.inbounds[ib.ID] = ib
		}
	}
	for _, oldID := range topologyKeys(p.lines) {
		l := p.lines[oldID]
		if mapped, ok := ids[l.TargetInboundID]; ok {
			l.TargetInboundID = mapped
		}
		if oldID < 0 {
			res, e := tx.ExecContext(ctx, `INSERT INTO forward_lines(name,version,protocol,backend,target_inbound_id,groups_json,updated_at) VALUES(?,1,?,?,?,'[]',?)`, l.Name, l.Protocol, l.Backend, l.TargetInboundID, now())
			if e != nil {
				return nil, e
			}
			l.ID, err = res.LastInsertId()
			if err != nil {
				return nil, err
			}
			ids[oldID] = l.ID
			delete(p.lines, oldID)
			p.lines[l.ID] = l
			delete(p.view.lines, oldID)
			p.view.lines[l.ID] = l
			p.explicitLines[l.ID] = p.explicitLines[oldID]
		}
	}
	// Persist final Entry references first, so canonical published-member checks
	// see entries moved away from a member by another operation in this draft.
	for _, oldID := range topologyKeys(p.entries) {
		e := p.entries[oldID]
		if mapped, ok := ids[e.InboundID]; ok {
			e.InboundID = mapped
		}
		if e.LineID != nil {
			if mapped, ok := ids[*e.LineID]; ok {
				e.LineID = &mapped
			}
		}
		if err = writeEntryTx(ctx, tx, e, oldID < 0); err != nil {
			return nil, err
		}
		if oldID < 0 {
			ids[oldID] = e.ID
		}
	}
	compileCtx := context.WithValue(ctx, topologyCompileContext{}, p)
	for _, lineID := range topologyKeys(p.lines) {
		kind := "save"
		if !p.explicitLines[lineID] {
			kind = "recompile"
		}
		if err = saveLineTx(compileCtx, tx, p.lines[lineID], kind); err != nil {
			return nil, err
		}
	}
	d.Result = []TopologyResult{}
	for _, op := range d.Operations {
		result := p.keys[op.Key]
		if mapped, ok := ids[result.ID]; ok {
			result.ID = mapped
		}
		if result.ID <= 0 {
			return nil, errors.New("unresolved topology operation")
		}
		d.Result = append(d.Result, result)
	}
	d.Status = "committed"
	stamp := unix(now())
	d.UpdatedAt = stamp
	d.CommittedAt = &stamp
	// Once committed, creation settings are owned by the live inbound. The draft
	// history retains only the redacted operation and resulting resource identities.
	d = topologyPublic(d)
	for i := range d.Operations {
		if d.Operations[i].Inbound != nil {
			d.Operations[i].Inbound.Settings = nil
		}
	}
	operations, _ := json.Marshal(d.Operations)
	result, _ := json.Marshal(d.Result)
	if _, err = tx.ExecContext(ctx, `UPDATE topology_changesets SET status='committed',operations_json=?,result_json=?,updated_at=?,committed_at=? WHERE id=? AND version=? AND status='draft'`, string(operations), string(result), stamp.Unix(), stamp.Unix(), id, version); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM topology_changesets WHERE status='committed' AND id NOT IN (SELECT id FROM topology_changesets WHERE status='committed' ORDER BY updated_at DESC,id DESC LIMIT 100)`); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return d, nil
}
