// Package service holds use cases that span several stores.
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"time"

	"gitlab.com/boyang-hu/bosun/pkg/agentproto"
	"gitlab.com/boyang-hu/bosun/pkg/spec"

	"gitlab.com/boyang-hu/captain/internal/domain"
	"gitlab.com/boyang-hu/captain/internal/store"
)

// AgentState builds desired state for nodes.
type AgentState struct {
	Store          *store.Store
	PullSeconds    int
	PushSeconds    int
	EnforceDevices bool
}

// deviceWindow is how far back online IPs count toward the device limit.
const deviceWindow = 3 * time.Minute

// Build assembles the state bosun applies on node n. Inbounds open to
// everyone use the node-level user list (all users with a usable
// subscription); inbounds restricted to a group carry their own scoped list.
func (a *AgentState) Build(ctx context.Context, n *domain.Node, at time.Time) (*agentproto.State, error) {
	inbounds, err := a.Store.InboundsByNode(ctx, n.ID)
	if err != nil {
		return nil, err
	}
	node := spec.Node{ID: strconv.FormatInt(n.ID, 10)}
	over := map[int64]bool{}
	if a.EnforceDevices {
		if over, err = a.Store.OverDeviceLimit(ctx, at.Add(-deviceWindow)); err != nil {
			return nil, err
		}
	}
	all, err := a.Store.UsersWithAccess(ctx, nil, at)
	if err != nil {
		return nil, err
	}
	users := toSpecUsers(all, over)
	byGroup := map[int64][]spec.User{}
	for _, ib := range inbounds {
		si := ib.Spec()
		if ib.GroupID != nil {
			list, ok := byGroup[*ib.GroupID]
			if !ok {
				members, err := a.Store.UsersWithAccess(ctx, ib.GroupID, at)
				if err != nil {
					return nil, err
				}
				list = toSpecUsers(members, over)
				byGroup[*ib.GroupID] = list
			}
			si.ScopedUsers = true
			si.Users = list
		}
		node.Inbounds = append(node.Inbounds, si)
	}
	st := &agentproto.State{Node: node, Users: users, Forwards: []spec.Forward{}, PullSeconds: a.PullSeconds, PushSeconds: a.PushSeconds}
	st.Revision = revision(st)
	return st, nil
}

// toSpecUsers converts users, skipping those currently over their device limit.
func toSpecUsers(list []*domain.User, over map[int64]bool) []spec.User {
	out := make([]spec.User, 0, len(list))
	for _, u := range list {
		if over[u.ID] {
			continue
		}
		out = append(out, spec.User{ID: u.ID, Name: u.UUID, UUID: u.UUID, Password: u.UUID})
	}
	return out
}

// revision is a content hash so identical state yields the same ETag.
func revision(st *agentproto.State) string {
	b, _ := json.Marshal(struct {
		N spec.Node
		U []spec.User
		F []spec.Forward
	}{st.Node, st.Users, st.Forwards})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}
