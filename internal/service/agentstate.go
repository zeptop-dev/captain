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
	Probe          *Probe // nil = no probe config in state
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
	if ov, err := a.Store.NodeOverrides(ctx, n.ID); err == nil {
		for c, raw := range ov {
			if raw != "" {
				if node.Overrides == nil {
					node.Overrides = map[string]json.RawMessage{}
				}
				node.Overrides[c] = json.RawMessage(raw)
			}
		}
	}
	if nr, err := a.Store.NodeRouting(ctx, n.ID); err == nil {
		node.Outbounds, node.Routes, node.DefaultOutbound, node.DNS = nr.Outbounds, nr.Routes, nr.DefaultOutbound, nr.DNS
	}
	var acme store.ACMESettings
	if err := a.Store.GetSetting(ctx, store.SettingACME, &acme); err != nil {
		return nil, err
	}
	if acme.Email != "" || acme.CloudflareToken != "" {
		node.ACME = &spec.ACME{Email: acme.Email, CloudflareToken: acme.CloudflareToken}
	}
	if n.DecoyEnabled && n.Domain != "" {
		method := "http"
		if acme.CloudflareToken != "" {
			method = "dns"
		}
		node.Decoy = &spec.Decoy{Domain: n.Domain, Port: spec.DefaultDecoyPort, Upstream: n.DecoyUpstream, ACME: method}
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
	speeds, _ := a.Store.SpeedLimits(ctx)
	node.UserSpeedLimitMbps = n.UserSpeedLimitMbps
	all, err := a.Store.UsersWithAccess(ctx, nil, at)
	if err != nil {
		return nil, err
	}
	users := toSpecUsers(all, over, limits, speeds)
	byGroup := map[int64][]spec.User{}
	ingresses, _ := a.Store.IngressesByNode(ctx, n.ID)
	bindFor := map[int64]string{}
	for _, g := range ingresses {
		bindFor[g.ID] = g.BindIP
	}
	for _, ib := range inbounds {
		si := ib.Spec()
		// A line ingress with its own NIC address: bind there so replies
		// leave through the line, unless the inbound sets a listen itself.
		if ib.IngressID != nil && si.Listen == "" {
			si.Listen = bindFor[*ib.IngressID]
		}
		if ib.GroupID != nil {
			list, ok := byGroup[*ib.GroupID]
			if !ok {
				members, err := a.Store.UsersWithAccess(ctx, ib.GroupID, at)
				if err != nil {
					return nil, err
				}
				list = toSpecUsers(members, over, limits, speeds)
				byGroup[*ib.GroupID] = list
			}
			si.ScopedUsers = true
			si.Users = list
		}
		node.Inbounds = append(node.Inbounds, si)
	}
	var tlsNames []string
	for _, ib := range node.Inbounds {
		if ib.TLS != nil && ib.TLS.Mode == spec.TLSStandard && ib.TLS.ServerName != "" {
			tlsNames = append(tlsNames, ib.TLS.ServerName)
		}
	}
	if certs, err := a.Store.CertificatesFor(ctx, tlsNames); err == nil {
		for _, c := range certs {
			node.Certificates = append(node.Certificates, spec.Certificate{Domain: c.Domain, CertPEM: c.CertPEM, KeyPEM: c.KeyPEM})
		}
	}
	st := &agentproto.State{Node: node, Users: users, Forwards: []spec.Forward{}, PullSeconds: a.PullSeconds, PushSeconds: a.PushSeconds}
	if fwds, err := a.Store.NodeForwards(ctx, n.ID); err == nil {
		for _, f := range fwds {
			st.Forwards = append(st.Forwards, f.Forward)
		}
	}
	if a.Probe != nil {
		st.Probe = a.Probe.AgentConfig(ctx, n.ID)
	}
	// Komari: one panel-wide setting, each node registers under its name.
	var km store.KomariSettings
	if err := a.Store.GetSetting(ctx, store.SettingKomari, &km); err == nil && km.Enabled && km.Server != "" {
		st.Komari = &spec.Komari{Enabled: true, Server: km.Server, Key: km.Key, Name: n.Name, Interval: km.Interval}
	}
	if jobs, err := a.Store.PendingNodeJobs(ctx, n.ID); err == nil {
		for _, j := range jobs {
			st.Jobs = append(st.Jobs, agentproto.Job{ID: j.ID, Kind: j.Kind, Params: j.Params})
		}
	}
	st.Revision = revision(st)
	return st, nil
}

// toSpecUsers converts users, skipping those currently over their device limit.
func toSpecUsers(list []*domain.User, over map[int64]bool, limits map[int64]int, speeds map[int64]int) []spec.User {
	out := make([]spec.User, 0, len(list))
	for _, u := range list {
		if over[u.ID] {
			continue
		}
		out = append(out, spec.User{ID: u.ID, Name: u.UUID, UUID: u.UUID, Password: u.UUID, DeviceLimit: limits[u.ID], SpeedLimitMbps: speeds[u.ID]})
	}
	return out
}

// revision is a content hash so identical state yields the same ETag.
func revision(st *agentproto.State) string {
	b, _ := json.Marshal(struct {
		N spec.Node
		U []spec.User
		F []spec.Forward
		P *spec.Probe
		K *spec.Komari
	}{st.Node, st.Users, st.Forwards, st.Probe, st.Komari})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}
