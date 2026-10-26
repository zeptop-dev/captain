package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
)

var (
	ErrAccessConflict             = errors.New("node access changed; reload before saving")
	ErrAccessConfirmationRequired = errors.New("confirm the shared node or plan impact before saving")
	ErrInvalidAccess              = errors.New("invalid node access policy or membership")
	ErrAccessManaged              = errors.New("this inbound uses explicit node access; edit its access policy instead of the legacy group")
)

type accessQuerier interface {
	rowQuerier
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type AccessReference struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type AccessPlan struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	GroupID *int64 `json:"group_id"`
	Enabled bool   `json:"enabled"`
}

type AccessInbound struct {
	domain.InboundAccess
	NodeID   int64             `json:"node_id"`
	NodeName string            `json:"node_name"`
	Tag      string            `json:"tag"`
	Protocol spec.Protocol     `json:"protocol"`
	Port     int               `json:"port"`
	Enabled  bool              `json:"enabled"`
	Editable bool              `json:"editable"`
	Entries  []AccessReference `json:"entries"`
	PlanIDs  []int64           `json:"plan_ids"`
	Revision string            `json:"revision"`
}

type InboundAccessCatalog struct {
	Revision string            `json:"revision"`
	Inbounds []AccessInbound   `json:"inbounds"`
	Groups   []AccessReference `json:"groups"`
	Plans    []AccessPlan      `json:"plans"`
}

type GroupAccessChange struct {
	InboundIDs    []int64 `json:"inbound_ids"`
	Revision      string  `json:"revision"`
	ConfirmShared bool    `json:"confirm_shared"`
}

func inboundAccess(ctx context.Context, q accessQuerier, id int64) (*domain.InboundAccess, error) {
	a := &domain.InboundAccess{InboundID: id, GroupIDs: []int64{}}
	if err := q.QueryRowContext(ctx, `SELECT e.policy, NOT EXISTS(SELECT 1 FROM inbound_access a WHERE a.inbound_id=e.inbound_id) FROM effective_inbound_access e WHERE e.inbound_id=?`, id).Scan(&a.Policy, &a.Legacy); err != nil {
		return nil, wrapNotFound(err)
	}
	if a.Policy != domain.AccessGroups {
		return a, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT group_id FROM effective_inbound_access_groups WHERE inbound_id=? ORDER BY group_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var g int64
		if err = rows.Scan(&g); err != nil {
			return nil, err
		}
		a.GroupIDs = append(a.GroupIDs, g)
	}
	return a, rows.Err()
}

func (s *Store) InboundAccess(ctx context.Context, id int64) (*domain.InboundAccess, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	a, err := inboundAccess(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	return a, tx.Commit()
}

// AccessCatalog is a consistent admin snapshot. The revision includes every
// shared Entry and plan relation, not merely the listener's selected groups.
func (s *Store) AccessCatalog(ctx context.Context) (*InboundAccessCatalog, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	c, err := accessCatalog(ctx, tx)
	if err != nil {
		return nil, err
	}
	return c, tx.Commit()
}

func accessCatalog(ctx context.Context, q accessQuerier) (*InboundAccessCatalog, error) {
	c := &InboundAccessCatalog{Inbounds: []AccessInbound{}, Groups: []AccessReference{}, Plans: []AccessPlan{}}
	rows, err := q.QueryContext(ctx, `SELECT id,name FROM user_groups ORDER BY id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var g AccessReference
		if err = rows.Scan(&g.ID, &g.Name); err != nil {
			rows.Close()
			return nil, err
		}
		c.Groups = append(c.Groups, g)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = q.QueryContext(ctx, `SELECT id,name,group_id,enabled FROM plans ORDER BY id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p AccessPlan
		var g sql.NullInt64
		if err = rows.Scan(&p.ID, &p.Name, &g, &p.Enabled); err != nil {
			rows.Close()
			return nil, err
		}
		p.GroupID = int64Ptr(g)
		c.Plans = append(c.Plans, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = q.QueryContext(ctx, `SELECT i.id,i.node_id,n.name,i.tag,i.protocol,i.port,i.enabled,e.policy,NOT EXISTS(SELECT 1 FROM inbound_access a WHERE a.inbound_id=i.id),i.settings_json,EXISTS(SELECT 1 FROM node_reverse_links r WHERE r.receiver_inbound_id=i.id) FROM inbounds i JOIN nodes n ON n.id=i.node_id JOIN effective_inbound_access e ON e.inbound_id=i.id ORDER BY i.id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var a AccessInbound
		var settings string
		var receiver bool
		if err = rows.Scan(&a.InboundID, &a.NodeID, &a.NodeName, &a.Tag, &a.Protocol, &a.Port, &a.Enabled, &a.Policy, &a.Legacy, &settings, &receiver); err != nil {
			rows.Close()
			return nil, err
		}
		var cfg spec.Inbound
		if err = json.Unmarshal([]byte(settings), &cfg); err != nil {
			rows.Close()
			return nil, err
		}
		a.Editable = !receiver && (cfg.Reverse == nil || !cfg.Reverse.Receiver)
		a.GroupIDs = []int64{}
		a.Entries = []AccessReference{}
		a.PlanIDs = []int64{}
		c.Inbounds = append(c.Inbounds, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	index := map[int64]int{}
	for i := range c.Inbounds {
		index[c.Inbounds[i].InboundID] = i
	}
	rows, err = q.QueryContext(ctx, `SELECT inbound_id,group_id FROM effective_inbound_access_groups ORDER BY inbound_id,group_id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, g int64
		if err = rows.Scan(&id, &g); err != nil {
			rows.Close()
			return nil, err
		}
		if i, ok := index[id]; ok && c.Inbounds[i].Policy == domain.AccessGroups {
			c.Inbounds[i].GroupIDs = append(c.Inbounds[i].GroupIDs, g)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = q.QueryContext(ctx, `SELECT inbound_id,id,name FROM entries ORDER BY id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var e AccessReference
		if err = rows.Scan(&id, &e.ID, &e.Name); err != nil {
			rows.Close()
			return nil, err
		}
		if i, ok := index[id]; ok {
			c.Inbounds[i].Entries = append(c.Inbounds[i].Entries, e)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range c.Inbounds {
		a := &c.Inbounds[i]
		for _, p := range c.Plans {
			if a.Policy == domain.AccessAll || (p.GroupID != nil && slices.Contains(a.GroupIDs, *p.GroupID)) {
				a.PlanIDs = append(a.PlanIDs, p.ID)
			}
		}
	}
	b, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(b)
	c.Revision = hex.EncodeToString(hash[:])
	for i := range c.Inbounds {
		c.Inbounds[i].Revision = c.Revision
	}
	return c, nil
}

func validAccess(c *InboundAccessCatalog, change domain.InboundAccessChange) ([]int64, error) {
	groups := append([]int64{}, change.GroupIDs...)
	slices.Sort(groups)
	for i, g := range groups {
		if g <= 0 || (i > 0 && groups[i-1] == g) {
			return nil, ErrInvalidAccess
		}
		found := false
		for _, known := range c.Groups {
			if known.ID == g {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("%w: group does not exist", ErrInvalidAccess)
		}
	}
	switch change.Policy {
	case domain.AccessAll, domain.AccessNone:
		if len(groups) != 0 {
			return nil, ErrInvalidAccess
		}
	case domain.AccessGroups:
		if len(groups) == 0 {
			return nil, ErrInvalidAccess
		}
	default:
		return nil, ErrInvalidAccess
	}
	return groups, nil
}

func writeAccess(ctx context.Context, tx *sql.Tx, id int64, policy string, groups []int64) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO inbound_access(inbound_id,policy) VALUES(?,?) ON CONFLICT(inbound_id) DO UPDATE SET policy=excluded.policy`, id, policy); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM inbound_access_groups WHERE inbound_id=?`, id); err != nil {
		return err
	}
	for _, g := range groups {
		if _, err := tx.ExecContext(ctx, `INSERT INTO inbound_access_groups(inbound_id,group_id) VALUES(?,?)`, id, g); err != nil {
			return err
		}
	}
	return nil
}

func updateInboundAccessTx(ctx context.Context, tx *sql.Tx, id int64, change domain.InboundAccessChange, addedEntries ...int) error {
	if change.InboundID != id || id <= 0 {
		return ErrInvalidAccess
	}
	c, err := accessCatalog(ctx, tx)
	if err != nil {
		return err
	}
	if change.Revision == "" || change.Revision != c.Revision {
		return ErrAccessConflict
	}
	groups, err := validAccess(c, change)
	if err != nil {
		return err
	}
	for _, a := range c.Inbounds {
		if a.InboundID != id {
			continue
		}
		if !a.Editable {
			return ErrInvalidAccess
		}
		changed := a.Policy != change.Policy || !slices.Equal(a.GroupIDs, groups)
		if !changed && !a.Legacy {
			return nil
		}
		shared := len(a.Entries)
		for _, n := range addedEntries {
			shared += n
		}
		if changed && shared > 1 && !change.ConfirmShared {
			return ErrAccessConfirmationRequired
		}
		return writeAccess(ctx, tx, id, change.Policy, groups)
	}
	return ErrNotFound
}

func (s *Store) UpdateInboundAccess(ctx context.Context, id int64, change domain.InboundAccessChange) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = updateInboundAccessTx(ctx, tx, id, change); err != nil {
		return err
	}
	return tx.Commit()
}

// UpdateGroupAccess edits a group's membership across listeners. All-policy
// listeners remain included, and removing the final explicit group means none.
func (s *Store) UpdateGroupAccess(ctx context.Context, groupID int64, change GroupAccessChange) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	c, err := accessCatalog(ctx, tx)
	if err != nil {
		return err
	}
	if change.Revision == "" || change.Revision != c.Revision {
		return ErrAccessConflict
	}
	found := false
	for _, g := range c.Groups {
		if g.ID == groupID {
			found = true
			break
		}
	}
	if !found {
		return ErrNotFound
	}
	selected := map[int64]bool{}
	for _, id := range change.InboundIDs {
		if id <= 0 || selected[id] {
			return ErrInvalidAccess
		}
		selected[id] = true
	}
	plans := 0
	for _, p := range c.Plans {
		if p.GroupID != nil && *p.GroupID == groupID {
			plans++
		}
	}
	for _, a := range c.Inbounds {
		want := selected[a.InboundID]
		delete(selected, a.InboundID)
		if !a.Editable {
			if want {
				return ErrInvalidAccess
			}
			continue
		}
		if a.Policy == domain.AccessAll {
			if !want {
				return fmt.Errorf("%w: all-users listeners cannot be removed from one group", ErrInvalidAccess)
			}
			continue
		}
		has := slices.Contains(a.GroupIDs, groupID)
		if has == want {
			continue
		}
		if (plans > 1 || len(a.Entries) > 1) && !change.ConfirmShared {
			return ErrAccessConfirmationRequired
		}
		groups := append([]int64{}, a.GroupIDs...)
		if want {
			groups = append(groups, groupID)
		} else {
			groups = slices.DeleteFunc(groups, func(g int64) bool { return g == groupID })
		}
		slices.Sort(groups)
		policy := domain.AccessGroups
		if len(groups) == 0 {
			policy = domain.AccessNone
		}
		if err = writeAccess(ctx, tx, a.InboundID, policy, groups); err != nil {
			return err
		}
	}
	if len(selected) > 0 {
		return ErrInvalidAccess
	}
	return tx.Commit()
}

// A legacy editor may round-trip the old field, but must not replace a newer
// explicit policy. Rejection also rolls back complete reverse-link edits.
func guardLegacyAccessTx(ctx context.Context, tx *sql.Tx, ib *domain.Inbound) error {
	var old sql.NullInt64
	var explicit bool
	if err := tx.QueryRowContext(ctx, `SELECT group_id,EXISTS(SELECT 1 FROM inbound_access a WHERE a.inbound_id=i.id) FROM inbounds i WHERE id=?`, ib.ID).Scan(&old, &explicit); err != nil {
		return wrapNotFound(err)
	}
	if explicit && old != nullInt64(ib.GroupID) {
		return ErrAccessManaged
	}
	return nil
}

// inboundGroupClause is the same rule used for subscription publication and
// actual credential delivery. An empty explicit group list never becomes all.
func inboundGroupClause(alias string, groups []int64) (string, []any) {
	q := `EXISTS(SELECT 1 FROM effective_inbound_access ia WHERE ia.inbound_id=` + alias + `.id AND (ia.policy='all'`
	args := []any{}
	if len(groups) > 0 {
		q += ` OR (ia.policy='groups' AND EXISTS(SELECT 1 FROM effective_inbound_access_groups ig WHERE ig.inbound_id=ia.inbound_id AND ig.group_id IN (`
		for i, g := range groups {
			if i > 0 {
				q += ","
			}
			q += "?"
			args = append(args, g)
		}
		q += `)))`
	}
	return q + `))`, args
}

// UsersWithInboundAccess preserves the existing usable-plan prerequisite and
// manual user group semantics while allowing any explicitly selected group.
func (s *Store) UsersWithInboundAccess(ctx context.Context, id int64, at time.Time) ([]*domain.User, error) {
	q := `SELECT ` + userCols + ` FROM users u WHERE u.status='active' AND u.role='user'
 AND EXISTS(SELECT 1 FROM subscriptions sub WHERE sub.user_id=u.id AND sub.status='active' AND ` + usableSQL + `)
 AND EXISTS(SELECT 1 FROM effective_inbound_access ia WHERE ia.inbound_id=? AND ` + inboundUserAccessSQL(true) + `) ORDER BY u.id`
	rows, err := s.db.QueryContext(ctx, q, at.Unix(), id, at.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*domain.User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// Caller aliases the user as u and the effective inbound policy as ia. The
// report identity path deliberately includes active subscriptions even after
// exhaustion/expiry so final deltas from already admitted sessions still bill.
func inboundUserAccessSQL(usable bool) string {
	q := `(ia.policy='all' OR (ia.policy='groups' AND EXISTS(SELECT 1 FROM effective_inbound_access_groups ig WHERE ig.inbound_id=ia.inbound_id AND (ig.group_id=u.group_id OR EXISTS(SELECT 1 FROM subscriptions sub JOIN plans p ON p.id=sub.plan_id WHERE sub.user_id=u.id AND sub.status='active' AND p.group_id=ig.group_id`
	if usable {
		q += ` AND ` + usableSQL
	}
	return q + `)))))`
}
