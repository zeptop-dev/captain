package store

import (
	"context"
	"database/sql"

	"github.com/zeptop-dev/captain/internal/domain"
)

const entryCols = "e.id, e.name, e.inbound_id, e.chain_id, e.display_host, e.display_port, e.rate, e.sort, e.enabled"

func scanEntry(row interface{ Scan(...any) error }) (*domain.Entry, error) {
	var e domain.Entry
	var chain sql.NullInt64
	var enabled int
	if err := row.Scan(&e.ID, &e.Name, &e.InboundID, &chain, &e.DisplayHost, &e.DisplayPort, &e.Rate, &e.Sort, &enabled); err != nil {
		return nil, wrapNotFound(err)
	}
	e.ChainID = int64Ptr(chain)
	e.Enabled = enabled == 1
	return &e, nil
}

func (s *Store) CreateEntry(ctx context.Context, e *domain.Entry) error {
	if e.Rate == 0 {
		e.Rate = 1
	}
	ts := now()
	res, err := s.db.ExecContext(ctx, `INSERT INTO entries (name, inbound_id, chain_id, display_host, display_port, rate, sort, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.Name, e.InboundID, nullInt64(e.ChainID), e.DisplayHost, e.DisplayPort, e.Rate, e.Sort, boolInt(e.Enabled), ts, ts)
	if err != nil {
		return err
	}
	e.ID, _ = res.LastInsertId()
	return nil
}

// EntryLine is an entry joined with the inbound it points at.
type EntryLine struct {
	Entry   domain.Entry
	Inbound domain.Inbound
}

// EntriesForUser returns enabled entries whose inbound the user may use:
// inbound group is NULL or equals the user's group.
func (s *Store) EntriesForUser(ctx context.Context, u *domain.User) ([]EntryLine, error) {
	q := `SELECT ` + entryCols + `, ` + inboundColsPrefixed("i") + `
		FROM entries e JOIN inbounds i ON i.id = e.inbound_id
		WHERE e.enabled = 1 AND i.enabled = 1 AND (i.group_id IS NULL`
	args := []any{}
	if u.GroupID != nil {
		q += ` OR i.group_id = ?`
		args = append(args, *u.GroupID)
	}
	q += `) ORDER BY e.sort, e.id`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EntryLine
	for rows.Next() {
		var l EntryLine
		var chain, group sql.NullInt64
		var eEnabled, iEnabled int
		var settings string
		if err := rows.Scan(&l.Entry.ID, &l.Entry.Name, &l.Entry.InboundID, &chain, &l.Entry.DisplayHost, &l.Entry.DisplayPort, &l.Entry.Rate, &l.Entry.Sort, &eEnabled,
			&l.Inbound.ID, &l.Inbound.NodeID, &l.Inbound.Tag, &l.Inbound.Protocol, &l.Inbound.Listen, &l.Inbound.Port, &l.Inbound.Core, &settings, &group, &iEnabled, &l.Inbound.Sort); err != nil {
			return nil, err
		}
		if err := unmarshalSettings(settings, &l.Inbound); err != nil {
			return nil, err
		}
		l.Entry.ChainID, l.Inbound.GroupID = int64Ptr(chain), int64Ptr(group)
		l.Entry.Enabled, l.Inbound.Enabled = eEnabled == 1, iEnabled == 1
		out = append(out, l)
	}
	return out, rows.Err()
}

// ListEntries returns every entry.
func (s *Store) ListEntries(ctx context.Context) ([]*domain.Entry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+entryCols+` FROM entries e ORDER BY e.sort, e.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) UpdateEntry(ctx context.Context, e *domain.Entry) error {
	_, err := s.db.ExecContext(ctx, `UPDATE entries SET name = ?, inbound_id = ?, chain_id = ?, display_host = ?, display_port = ?, rate = ?, sort = ?, enabled = ?, updated_at = ? WHERE id = ?`,
		e.Name, e.InboundID, nullInt64(e.ChainID), e.DisplayHost, e.DisplayPort, e.Rate, e.Sort, boolInt(e.Enabled), now(), e.ID)
	return err
}

func (s *Store) DeleteEntry(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM entries WHERE id = ?`, id)
	return err
}
