// Package service holds use cases that span several stores.
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
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
	var acme store.ACMESettings
	if err := a.Store.GetSetting(ctx, store.SettingACME, &acme); err != nil {
		return nil, err
	}
	if acme.Email != "" || acme.CloudflareToken != "" {
		node.ACME = &spec.ACME{Email: acme.Email, CloudflareToken: acme.CloudflareToken}
	}
	over := map[int64]bool{}
	limits := map[int64]int{}
	if a.EnforceDevices {
		if over, err = a.Store.OverDeviceLimit(ctx, at.Add(-deviceWindow)); err != nil {
			return nil, err
		}
		// Nodes see the limit too: sing-box only logs client addresses at a
		// verbose level, which bosun switches on when a limited user exists.
		if limits, err = a.Store.DeviceLimits(ctx); err != nil {
			return nil, err
		}
	}
	all, err := a.Store.UsersWithAccess(ctx, nil, at)
	if err != nil {
		return nil, err
	}
	users := toSpecUsers(all, over, limits)
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
				list = toSpecUsers(members, over, limits)
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
func toSpecUsers(list []*domain.User, over map[int64]bool, limits map[int64]int) []spec.User {
	out := make([]spec.User, 0, len(list))
	for _, u := range list {
		if over[u.ID] {
			continue
		}
		out = append(out, spec.User{ID: u.ID, Name: u.UUID, UUID: u.UUID, Password: u.UUID, DeviceLimit: limits[u.ID]})
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
