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
	Store       *store.Store
	PullSeconds int
	PushSeconds int
}

// Build assembles the state bosun applies on node n. Users are the union of
// users allowed on any of the node's inbounds; bosun provisions every user on
// every inbound, so group restrictions are enforced by which inbounds exist
// on a node (one node, one audience) until per-inbound user lists exist.
func (a *AgentState) Build(ctx context.Context, n *domain.Node, at time.Time) (*agentproto.State, error) {
	inbounds, err := a.Store.InboundsByNode(ctx, n.ID)
	if err != nil {
		return nil, err
	}
	node := spec.Node{ID: strconv.FormatInt(n.ID, 10)}
	seen := map[int64]bool{}
	var users []spec.User
	for _, ib := range inbounds {
		node.Inbounds = append(node.Inbounds, ib.Spec())
		list, err := a.Store.UsersWithAccess(ctx, ib.GroupID, at)
		if err != nil {
			return nil, err
		}
		for _, u := range list {
			if seen[u.ID] {
				continue
			}
			seen[u.ID] = true
			users = append(users, spec.User{ID: u.ID, Name: u.UUID, UUID: u.UUID, Password: u.UUID})
		}
	}
	if users == nil {
		users = []spec.User{}
	}
	st := &agentproto.State{Node: node, Users: users, Forwards: []spec.Forward{}, PullSeconds: a.PullSeconds, PushSeconds: a.PushSeconds}
	st.Revision = revision(st)
	return st, nil
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
