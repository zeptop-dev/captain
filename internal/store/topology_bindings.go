package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
)

// These private bindings survive ordinary draft edits. Reusing a numeric target
// ID must never silently retarget an already-saved draft to a new object.
func topologyIdentityTx(ctx context.Context, tx *sql.Tx, kind string, id int64) (string, error) {
	var identity string
	var q string
	switch kind {
	case "node":
		q = `SELECT uuid FROM probe_public_ids WHERE node_id=?`
	case "inbound":
		q = `SELECT identity FROM topology_inbound_identities WHERE inbound_id=?`
	case "entry":
		q = `SELECT identity FROM topology_entry_identities WHERE entry_id=?`
	case "line":
		q = `SELECT identity FROM topology_line_identities WHERE line_id=?`
	case "ingress":
		q = `SELECT identity FROM topology_ingress_identities WHERE ingress_id=?`
	default:
		return "", ErrTopologyUnsupported
	}
	e := tx.QueryRowContext(ctx, q, id).Scan(&identity)
	if e != nil {
		return "", wrapNotFound(e)
	}
	if identity == "" {
		return "", ErrTopologyConflict
	}
	return identity, nil
}
func topologyBindingsTx(ctx context.Context, tx *sql.Tx, d *TopologyChangeset, old map[string]string) (map[string]string, error) {
	out := map[string]string{}
	for _, op := range d.Operations {
		add := func(kind string, id int64) error {
			if id < 0 {
				return errors.New("existing resource references cannot use temporary numeric IDs")
			}
			if id == 0 {
				return nil
			}
			key := op.Key + "/" + kind + "/" + strconv.FormatInt(id, 10)
			if prior, ok := old[key]; ok {
				out[key] = prior
				return nil
			}
			value, e := topologyIdentityTx(ctx, tx, kind, id)
			if e != nil {
				return e
			}
			out[key] = value
			return nil
		}
		addInbound := func(id int64) error {
			if e := add("inbound", id); e != nil {
				return e
			}
			if id <= 0 {
				return nil
			}
			var node int64
			var ingress sql.NullInt64
			if e := tx.QueryRowContext(ctx, `SELECT node_id,ingress_id FROM inbounds WHERE id=?`, id).Scan(&node, &ingress); e != nil {
				return wrapNotFound(e)
			}
			if e := add("node", node); e != nil {
				return e
			}
			return add("ingress", ingress.Int64)
		}
		addLine := func(id int64) error {
			if e := add("line", id); e != nil {
				return e
			}
			if id <= 0 {
				return nil
			}
			l, e := scanLine(tx.QueryRowContext(ctx, "SELECT "+lineCols+" FROM forward_lines WHERE id=?", id))
			if e != nil {
				return e
			}
			if e = addInbound(l.TargetInboundID); e != nil {
				return e
			}
			for _, g := range l.Groups {
				for _, m := range g.Members {
					if e = add("node", m.NodeID); e != nil {
						return e
					}
					if m.IngressID != nil {
						if e = add("ingress", *m.IngressID); e != nil {
							return e
						}
					}
				}
			}
			return nil
		}
		if op.ID > 0 {
			if e := add(op.Kind, op.ID); e != nil {
				return nil, e
			}
		}
		switch op.Kind {
		case "inbound":
			v := op.Inbound
			node := v.NodeID
			var ingress int64
			if op.ID > 0 {
				var stored sql.NullInt64
				if e := tx.QueryRowContext(ctx, `SELECT node_id,ingress_id FROM inbounds WHERE id=?`, op.ID).Scan(&node, &stored); e != nil {
					return nil, wrapNotFound(e)
				}
				ingress = stored.Int64
			}
			if v.IngressID != nil {
				ingress = *v.IngressID
			}
			if e := add("node", node); e != nil {
				return nil, e
			}
			if e := add("ingress", ingress); e != nil {
				return nil, e
			}
		case "line":
			if op.InboundRef == "" {
				if e := addInbound(op.Line.TargetInboundID); e != nil {
					return nil, e
				}
			}
			for _, g := range op.Line.Groups {
				for _, m := range g.Members {
					if e := add("node", m.NodeID); e != nil {
						return nil, e
					}
					if m.IngressID != nil {
						if e := add("ingress", *m.IngressID); e != nil {
							return nil, e
						}
					}
				}
			}
		case "entry":
			v := op.Entry
			var inbound, line int64
			if op.ID > 0 {
				var bound sql.NullInt64
				if e := tx.QueryRowContext(ctx, `SELECT inbound_id,line_id FROM entries WHERE id=?`, op.ID).Scan(&inbound, &bound); e != nil {
					return nil, wrapNotFound(e)
				}
				line = bound.Int64
			}
			if v.InboundID != nil {
				inbound = *v.InboundID
			}
			if v.LineID != nil {
				line = *v.LineID
			}
			if v.LineRef == "" && v.InboundRef == "" {
				if line != 0 {
					if e := addLine(line); e != nil {
						return nil, e
					}
				} else if e := addInbound(inbound); e != nil {
					return nil, e
				}
			}
		}
	}
	return out, nil
}
func validateTopologyBindingsTx(ctx context.Context, tx *sql.Tx, d *TopologyChangeset) error {
	for key, want := range d.bindings {
		parts := strings.Split(key, "/")
		if len(parts) != 3 {
			return ErrTopologyConflict
		}
		id, e := strconv.ParseInt(parts[2], 10, 64)
		if e != nil {
			return ErrTopologyConflict
		}
		got, e := topologyIdentityTx(ctx, tx, parts[1], id)
		if e != nil || got != want {
			return ErrTopologyConflict
		}
	}
	return nil
}
