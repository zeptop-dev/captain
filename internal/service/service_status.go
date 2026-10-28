package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/selfupdate"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

// EntryRuntimeStatus contains only customer-safe reason/evidence codes. It
// describes reported components, never an end-to-end connection test.
type EntryRuntimeStatus struct {
	State      string   `json:"state"`
	Reason     string   `json:"reason"`
	Evidence   []string `json:"evidence"`
	ObservedAt *int64   `json:"observed_at"`
	EndToEnd   bool     `json:"end_to_end"`
}

const serviceRuntimeFreshness = 3 * time.Minute

// ServiceStatus evaluates a subscription endpoint's actual dependency graph.
// Keep one instance for the server lifetime: negative observations are confirmed
// only after two independently accepted reports, not after repeated page reads.
type ServiceStatus struct {
	store    *store.Store
	state    *AgentState
	mu       sync.Mutex
	failures map[string]runtimeFailure
}
type runtimeFailure struct {
	nodeID   int64
	sequence int64
	count    int
	observed time.Time
	identity string
}

func NewServiceStatus(st *store.Store, state *AgentState) *ServiceStatus {
	if state == nil {
		state = &AgentState{Store: st}
	}
	return &ServiceStatus{store: st, state: state, failures: map[string]runtimeFailure{}}
}

// Evaluate expects the caller to keep topology stable (the catalogue holds
// Store.Topology). It resolves hidden dependencies internally but returns only
// the supplied entries and no component names, addresses or raw agent errors.
func (s *ServiceStatus) Evaluate(ctx context.Context, entries []*domain.Entry, at time.Time) (map[int64]EntryRuntimeStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, failure := range s.failures {
		if !freshRuntime(failure.observed, at) {
			delete(s.failures, key)
		}
	}
	ev := runtimeEvaluation{service: s, ctx: ctx, at: at, nodes: map[int64]*runtimeNode{}, entries: map[int64]EntryRuntimeStatus{}, visiting: map[int64]bool{}, inbounds: map[int64]EntryRuntimeStatus{}}
	out := make(map[int64]EntryRuntimeStatus, len(entries))
	for _, entry := range entries {
		if entry != nil {
			out[entry.ID] = ev.entry(entry, 0)
		}
	}
	return out, ev.err
}

type runtimeNode struct {
	node    *domain.Node
	report  *store.NodeRuntime
	desired *agentproto.State
	status  EntryRuntimeStatus
}
type runtimeEvaluation struct {
	service  *ServiceStatus
	ctx      context.Context
	at       time.Time
	err      error
	nodes    map[int64]*runtimeNode
	entries  map[int64]EntryRuntimeStatus
	visiting map[int64]bool
	inbounds map[int64]EntryRuntimeStatus
}

func runtimeStatus(state, reason string, evidence ...string) EntryRuntimeStatus {
	return EntryRuntimeStatus{State: state, Reason: reason, Evidence: append([]string{}, evidence...)}
}
func observedStatus(state, reason string, at time.Time, evidence ...string) EntryRuntimeStatus {
	out := runtimeStatus(state, reason, evidence...)
	if !at.IsZero() && at.Unix() > 0 {
		stamp := at.Unix()
		out.ObservedAt = &stamp
	}
	return out
}
func freshRuntime(observed, at time.Time) bool {
	age := at.Sub(observed)
	return !observed.IsZero() && age >= 0 && age <= serviceRuntimeFreshness
}
func (e *runtimeEvaluation) failure(err error) EntryRuntimeStatus {
	if !errors.Is(err, store.ErrNotFound) && e.err == nil {
		e.err = err
	}
	return runtimeStatus("unknown", "no_data")
}
func (e *runtimeEvaluation) node(id int64) *runtimeNode {
	if n, ok := e.nodes[id]; ok {
		return n
	}
	n := &runtimeNode{status: runtimeStatus("unknown", "no_data")}
	e.nodes[id] = n
	defer func() {
		if n.status.State != "healthy" {
			for key, failure := range e.service.failures {
				if failure.nodeID == id {
					delete(e.service.failures, key)
				}
			}
		}
	}()
	var err error
	n.node, err = e.service.store.NodeByID(e.ctx, id)
	if err != nil {
		n.status = e.failure(err)
		return n
	}
	n.report, err = e.service.store.NodeRuntime(e.ctx, id)
	if err != nil {
		n.status = e.failure(err)
		return n
	}
	if n.report == nil || !n.node.Paired {
		return n
	}
	n.status = observedStatus("unknown", "stale", n.report.ObservedAt)
	if !freshRuntime(n.report.ObservedAt, e.at) {
		return n
	}
	n.status = observedStatus("unknown", "config_pending", n.report.ObservedAt)
	if !store.NodeProxyAllowed(n.node) {
		return n
	}
	n.desired, err = e.service.state.Cached(e.ctx, n.node, e.at)
	if err != nil {
		n.status = e.failure(err)
		return n
	}
	if n.report.Revision == "" || n.report.Revision != n.desired.Revision {
		return n
	}
	// Revision records what the driver fetched, not what apply completed.
	// A failed early apply can leave old inbounds running under the same tags.
	// Only the independent completed-apply confirmation proves this revision.
	if n.report.AppliedRevision == "" {
		n.status = observedStatus("unknown", "unsupported", n.report.ObservedAt)
		return n
	}
	if n.report.AppliedRevision != n.desired.Revision {
		return n
	}
	if desired := n.desired.Node.ServiceControl; desired != nil {
		control := n.report.ServiceControl
		if control == nil || !control.Supported {
			n.status = observedStatus("unknown", "unsupported", n.report.ObservedAt)
			return n
		}
		checked := time.Unix(control.CheckedAt, 0)
		if !freshRuntime(checked, e.at) {
			n.status = observedStatus("unknown", "stale", checked)
			return n
		}
		if control.Error != "" || control.DesiredRevision != desired.Revision || control.AppliedRevision != desired.Revision || control.Mode != desired.Mode || control.LimitMbps != desired.LimitMbps {
			return n
		}
		if control.Mode == "pause" {
			// An explicit, independently confirmed pause is not a failed core.
			// Publish only the service consequence, never its quota or operator.
			n.status = observedStatus("down", "service_paused", checked)
			return n
		}
	}
	n.status = observedStatus("healthy", "healthy", n.report.ObservedAt)
	return n
}

// negative never keeps an old green result. A first observed failure becomes
// unknown immediately, and only a different accepted report can confirm down.
func (e *runtimeEvaluation) negative(key string, n *runtimeNode, reason string, evidence ...string) EntryRuntimeStatus {
	out := observedStatus("unknown", reason, n.report.ObservedAt, evidence...)
	if n.report.Sequence <= 0 {
		e.clear(key)
		return out
	}
	identity := n.report.Identity + "/" + n.report.Epoch + "/" + n.report.AppliedRevision
	old := e.service.failures[key]
	if old.identity != identity || !freshRuntime(old.observed, e.at) || n.report.Sequence < old.sequence {
		old = runtimeFailure{identity: identity, nodeID: n.node.ID}
	}
	if n.report.Sequence != old.sequence {
		// Two distinct negative observations within the fresh window suffice.
		// Page polling may skip reports; this is not a claim of consecutive
		// failures or of the duration of an outage between observations.
		old.count++
		old.sequence = n.report.Sequence
		old.observed = n.report.ObservedAt
	}
	e.service.failures[key] = old
	if old.count >= 2 {
		out.State = "down"
	}
	return out
}
func (e *runtimeEvaluation) positive(key string, n *runtimeNode, evidence ...string) EntryRuntimeStatus {
	delete(e.service.failures, key)
	return observedStatus("healthy", "healthy", n.report.ObservedAt, evidence...)
}
func (e *runtimeEvaluation) clear(key string) { delete(e.service.failures, key) }
func (e *runtimeEvaluation) clearPrefix(prefix string) {
	for key := range e.service.failures {
		if strings.HasPrefix(key, prefix) {
			delete(e.service.failures, key)
		}
	}
}
func (e *runtimeEvaluation) entry(entry *domain.Entry, depth int) EntryRuntimeStatus {
	if result, ok := e.entries[entry.ID]; ok {
		return result
	}
	if depth >= 32 || e.visiting[entry.ID] {
		return runtimeStatus("unknown", "no_route", "client_chain")
	}
	e.visiting[entry.ID] = true
	defer delete(e.visiting, entry.ID)
	if !entry.Enabled {
		return runtimeStatus("unknown", "config_pending")
	}
	ib, err := e.service.store.InboundByID(e.ctx, entry.InboundID)
	if err != nil {
		return e.failure(err)
	}
	result := e.inbound(ib)
	if entry.LineID != nil {
		line, err := e.service.store.LineByID(e.ctx, *entry.LineID)
		if err != nil {
			return e.failure(err)
		}
		if line.TargetInboundID != ib.ID || len(line.Groups) == 0 {
			result = runtimeStatus("unknown", "no_route")
		} else {
			result = e.line(line, entry.LineMemberID, result)
		}
	} else if e.external(entry, ib) {
		result = runtimeSeries(result, runtimeStatus("unknown", "external_path"))
	}
	if entry.ClientDialerEntryID != nil && *entry.ClientDialerEntryID != 0 {
		front, err := e.service.store.EntryByID(e.ctx, *entry.ClientDialerEntryID)
		if err != nil {
			result = runtimeSeries(result, e.failure(err))
		} else {
			result = runtimeSeries(result, e.entry(front, depth+1))
		}
		result.Evidence = runtimeEvidence(result.Evidence, []string{"client_chain"})
	}
	e.entries[entry.ID] = result
	return result
}
func (e *runtimeEvaluation) external(entry *domain.Entry, ib *domain.Inbound) bool {
	if entry.ChainID != nil || ib.IngressID != nil || entry.DisplayPort != ib.Port {
		return true
	}
	for key := range entry.ClientExtra {
		key = strings.ToLower(strings.ReplaceAll(key, "_", "-"))
		if key == "dialer-proxy" || key == "detour" {
			if entry.ClientDialerEntryID == nil {
				return true
			}
		}
	}
	n := e.node(ib.NodeID)
	if n.node == nil {
		return true
	}
	host := strings.Trim(strings.ToLower(strings.TrimSpace(entry.DisplayHost)), "[]")
	for _, owned := range []string{n.node.PublicAddr, n.node.V6Addr, n.node.Domain} {
		if owned != "" && strings.EqualFold(host, strings.Trim(owned, "[]")) {
			return n.node.DomainShared && strings.EqualFold(host, n.node.Domain)
		}
	}
	return true
}
func (e *runtimeEvaluation) inbound(ib *domain.Inbound) EntryRuntimeStatus {
	if result, ok := e.inbounds[ib.ID]; ok {
		return result
	}
	result := e.coreInbound(ib)
	wg, err := e.service.store.WireGuardLinks(e.ctx, ib.NodeID)
	if err != nil {
		return e.failure(err)
	}
	for _, link := range wg {
		if link.UserInboundID == ib.ID {
			result = runtimeSeries(result, e.wireguard(link))
			e.inbounds[ib.ID] = result
			return result
		}
	}
	reverse, err := e.service.store.ReverseLinks(e.ctx, ib.NodeID)
	if err != nil {
		return e.failure(err)
	}
	for _, link := range reverse {
		if link.UserInboundID == ib.ID {
			result = runtimeSeries(result, e.reverse(link))
			break
		}
	}
	e.inbounds[ib.ID] = result
	return result
}
func (e *runtimeEvaluation) coreInbound(ib *domain.Inbound) EntryRuntimeStatus {
	key := fmt.Sprintf("inbound/%d", ib.ID)
	n := e.node(ib.NodeID)
	if !ib.Enabled {
		e.clear(key)
		return runtimeStatus("unknown", "config_pending", "inbound")
	}
	if n.status.State != "healthy" {
		e.clear(key)
		out := n.status
		out.Evidence = []string{"inbound"}
		return out
	}
	for _, core := range n.report.Cores {
		if core.Running && slices.Contains(core.Inbounds, ib.Tag) {
			return e.positive(key, n, "inbound")
		}
	}
	// Before 0.55 an absent list did not report failed application. Preserve
	// unknown for old/custom agents instead of interpreting an omitted field.
	if n.report.Version != "v0.55.0" && !selfupdate.Newer(n.report.Version, "v0.55.0") {
		e.clear(key)
		return observedStatus("unknown", "unsupported", n.report.ObservedAt, "inbound")
	}
	var names []string
	for name, core := range n.report.Cores {
		if core.Capabilities != nil {
			names = append(names, name)
		}
	}
	sort.Slice(names, func(i, j int) bool {
		a, b := n.report.Cores[names[i]], n.report.Cores[names[j]]
		if a.Priority == b.Priority {
			return names[i] < names[j]
		}
		return a.Priority < b.Priority
	})
	candidates := make([]spec.CoreCandidate, 0, len(names))
	for _, name := range names {
		candidates = append(candidates, spec.CoreCandidate{Name: name, Capabilities: *n.report.Cores[name].Capabilities})
	}
	desired := ib.Spec()
	for _, configured := range n.desired.Node.Inbounds {
		if configured.Tag == ib.Tag {
			desired = configured
			break
		}
	}
	selected, err := spec.SelectCore(desired, candidates)
	if err != nil {
		e.clear(key)
		return observedStatus("unknown", "unsupported", n.report.ObservedAt, "inbound")
	}
	// These authenticated cores deliberately leave an empty listener idle.
	// Match bosun's apply-time condition and the final scoped user list;
	// absence of an authorized user is not a failed listener.
	waitsForUsers := selected == "mita" || selected == "singbox-extended" && (desired.Protocol == spec.Mieru || desired.Protocol == spec.SSH)
	if waitsForUsers && len(desired.EffectiveUsers(n.desired.Users)) == 0 {
		e.clear(key)
		return observedStatus("unknown", "config_pending", n.report.ObservedAt, "inbound")
	}
	return e.negative(key, n, "component_failure", "inbound")
}

func (e *runtimeEvaluation) wireguard(link store.WireGuardLink) EntryRuntimeStatus {
	a, b := e.node(link.TransitID), e.node(link.ExitID)
	prerequisite := runtimeSeries(a.status, b.status)
	prerequisite.Evidence = []string{"wireguard"}
	key := fmt.Sprintf("wg/%s", link.ID)
	if prerequisite.State != "healthy" {
		e.clearPrefix(key)
		return prerequisite
	}
	ar, br := a.report.WireGuard, b.report.WireGuard
	if ar == nil || br == nil || !ar.Available || !br.Available {
		e.clearPrefix(key)
		return runtimeSeries(prerequisite, runtimeStatus("unknown", "unsupported", "wireguard"))
	}
	if !ar.Applied || !br.Applied {
		e.clearPrefix(key)
		return runtimeSeries(prerequisite, runtimeStatus("unknown", "config_pending", "wireguard"))
	}
	if link.AutoForwarding {
		f := br.Forwarding
		if !br.ForwardingSupported || f == nil {
			e.clearPrefix(key)
			return runtimeSeries(prerequisite, runtimeStatus("unknown", "unsupported", "wireguard"))
		}
		// Active WG links are reconciled every 15 seconds, including the
		// host forwarding check. A fresh report cannot renew an old check
		// when reconciliation has stopped progressing.
		checked := time.Unix(f.CheckedAt, 0)
		if !freshRuntime(checked, e.at) {
			e.clearPrefix(key)
			return runtimeSeries(prerequisite, observedStatus("unknown", "stale", checked, "wireguard"))
		}
		prerequisite = runtimeSeries(prerequisite, observedStatus("healthy", "healthy", checked, "wireguard"))
		firewallKey := key + "/firewall"
		switch f.State {
		case "error", "unsupported":
			return runtimeSeries(prerequisite, e.negative(firewallKey, b, "component_failure", "wireguard"))
		case "synced", "external", "not_required":
			e.clear(firewallKey)
		default:
			e.clearPrefix(key)
			return runtimeSeries(prerequisite, runtimeStatus("unknown", "unsupported", "wireguard"))
		}
	}
	for _, status := range ar.Links {
		if status.Slot == link.Slot {
			if status.Error != "" {
				e.clear(key)
				return runtimeSeries(prerequisite, runtimeStatus("unknown", "no_data", "wireguard"))
			}
			if status.Connected && freshRuntime(time.Unix(status.Handshake, 0), e.at) {
				return runtimeSeries(prerequisite, e.positive(key, a, "wireguard"))
			}
			if status.Connected {
				// Time passing in a GET is not a new negative observation.
				// An expired/inconsistent positive handshake becomes unknown;
				// only a fresh agent report of disconnection can count down.
				e.clear(key)
				return runtimeSeries(prerequisite, runtimeStatus("unknown", "stale", "wireguard"))
			}
			return runtimeSeries(prerequisite, e.negative(key, a, "component_failure", "wireguard"))
		}
	}
	e.clear(key)
	return runtimeSeries(prerequisite, runtimeStatus("unknown", "no_data", "wireguard"))
}
func (e *runtimeEvaluation) reverse(link store.ReverseLink) EntryRuntimeStatus {
	a, b := e.node(link.TransitID), e.node(link.ExitID)
	prerequisite := runtimeSeries(a.status, b.status)
	prerequisite.Evidence = []string{"vless_reverse"}
	key := fmt.Sprintf("reverse/%s", link.ID)
	if prerequisite.State != "healthy" {
		e.clearPrefix(key)
		return prerequisite
	}
	receiver, err := e.service.store.InboundByID(e.ctx, link.ReceiverInboundID)
	if err != nil {
		return e.failure(err)
	}
	prerequisite = runtimeSeries(prerequisite, e.coreInbound(receiver))
	if prerequisite.State != "healthy" {
		e.clearPrefix(key)
		return prerequisite
	}
	bc, ok := b.report.Cores["xray"]
	if !ok || bc.Capabilities == nil || !bc.Capabilities.VLESSReverse {
		e.clearPrefix(key)
		return runtimeSeries(prerequisite, runtimeStatus("unknown", "unsupported", "vless_reverse"))
	}
	if !bc.Running {
		return runtimeSeries(prerequisite, e.negative(key+"/exit", b, "component_failure", "vless_reverse"))
	}
	e.clear(key + "/exit")
	if _, ok := bc.Reverse[link.ID]; !ok {
		e.clear(key)
		return runtimeSeries(prerequisite, runtimeStatus("unknown", "config_pending", "vless_reverse"))
	}
	connected := a.report.Cores["xray"].Reverse[link.ID]
	if connected == nil {
		e.clear(key)
		return runtimeSeries(prerequisite, runtimeStatus("unknown", "no_data", "vless_reverse"))
	}
	if *connected {
		return runtimeSeries(prerequisite, e.positive(key, a, "vless_reverse"))
	}
	return runtimeSeries(prerequisite, e.negative(key, a, "component_failure", "vless_reverse"))
}

func (e *runtimeEvaluation) line(line *store.ForwardLine, first string, landing EntryRuntimeStatus) EntryRuntimeStatus {
	next := map[string]EntryRuntimeStatus{}
	for i := len(line.Groups) - 1; i >= 0; i-- {
		current := map[string]EntryRuntimeStatus{}
		for _, member := range line.Groups[i].Members {
			n := e.node(member.NodeID)
			if n.status.State != "healthy" {
				current[member.ID] = n.status
				continue
			}
			forwards, err := e.service.store.NodeForwards(e.ctx, member.NodeID)
			if err != nil {
				return e.failure(err)
			}
			var compiled *store.NodeForward
			for j := range forwards {
				if forwards[j].LineID == line.ID && forwards[j].LineMemberID == member.ID {
					compiled = &forwards[j]
					break
				}
			}
			if compiled == nil {
				e.clearPrefix(fmt.Sprintf("forward/%d/line-%d-%s-", member.NodeID, line.ID, member.ID))
				current[member.ID] = runtimeStatus("unknown", "config_pending")
				continue
			}
			var reported *agentproto.ForwardStatus
			for j := range n.report.Forwards {
				if n.report.Forwards[j].Tag == compiled.Tag {
					reported = &n.report.Forwards[j]
					break
				}
			}
			if reported == nil {
				e.clearPrefix(fmt.Sprintf("forward/%d/%s/", member.NodeID, compiled.Tag))
				current[member.ID] = observedStatus("unknown", "no_data", n.report.ObservedAt, "forward_tcp")
				continue
			}
			targets := []string{compiled.Target}
			for _, target := range compiled.Targets {
				targets = append(targets, target.Target)
			}
			children := []EntryRuntimeStatus{landing}
			if i+1 < len(line.Groups) {
				children = nil
				for _, child := range line.Groups[i+1].Members {
					children = append(children, next[child.ID])
				}
			}
			if len(targets) != len(children) {
				current[member.ID] = runtimeStatus("unknown", "no_route")
				continue
			}
			paths := make([]EntryRuntimeStatus, 0, len(children))
			for j, child := range children {
				health, protocol := "unknown", "none"
				if len(targets) == 1 {
					health, protocol = reported.Health, reported.ProbeProtocol
				} else if j < len(reported.Targets) && reported.Targets[j].Target == targets[j] {
					health, protocol = reported.Targets[j].Health, reported.Targets[j].ProbeProtocol
				}
				if line.Protocol == "udp" && protocol != "backend" {
					protocol = "none"
				}
				edge := e.forwardEdge(n, compiled.Tag+fmt.Sprintf("/%d", j), health, protocol)
				paths = append(paths, runtimeSeries(edge, child))
			}
			current[member.ID] = runtimeAlternatives(paths)
		}
		next = current
	}
	out, ok := next[first]
	if !ok {
		return runtimeStatus("unknown", "no_route")
	}
	for _, member := range line.Groups[0].Members {
		if member.ID == first && member.IngressID != nil {
			out = runtimeSeries(out, runtimeStatus("unknown", "external_path"))
		}
	}
	return out
}
func (e *runtimeEvaluation) forwardEdge(n *runtimeNode, tag, health, protocol string) EntryRuntimeStatus {
	key := fmt.Sprintf("forward/%d/%s", n.node.ID, tag)
	if protocol == "backend" && health == "down" {
		return e.negative(key, n, "component_failure", "forward_tcp")
	}
	if protocol != "tcp" {
		e.clear(key)
		return observedStatus("unknown", "unsupported", n.report.ObservedAt)
	}
	if health == "down" {
		return e.negative(key, n, "no_route", "forward_tcp")
	}
	if health == "up" {
		return e.positive(key, n, "forward_tcp")
	}
	e.clear(key)
	return observedStatus("unknown", "no_data", n.report.ObservedAt, "forward_tcp")
}

func runtimeEvidence(a, b []string) []string {
	out := append([]string{}, a...)
	for _, v := range b {
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}
func runtimeMetadata(out *EntryRuntimeStatus, other EntryRuntimeStatus) {
	out.Evidence = runtimeEvidence(out.Evidence, other.Evidence)
	if other.ObservedAt != nil && (out.ObservedAt == nil || *other.ObservedAt < *out.ObservedAt) {
		stamp := *other.ObservedAt
		out.ObservedAt = &stamp
	}
}
func runtimeSeries(parts ...EntryRuntimeStatus) EntryRuntimeStatus {
	out := runtimeStatus("healthy", "healthy")
	rank := map[string]int{"healthy": 0, "degraded": 1, "unknown": 2, "down": 3}
	for _, part := range parts {
		if rank[part.State] > rank[out.State] {
			out.State, out.Reason = part.State, part.Reason
		}
		runtimeMetadata(&out, part)
	}
	if out.State == "down" {
		// One confirmed failed component is sufficient to break a series
		// path. Unobserved/stale siblings do not age that failure evidence.
		out.ObservedAt = nil
		for _, part := range parts {
			if part.State == "down" {
				runtimeMetadata(&out, part)
			}
		}
	}
	return out
}
func runtimeAlternatives(parts []EntryRuntimeStatus) EntryRuntimeStatus {
	if len(parts) == 0 {
		return runtimeStatus("unknown", "no_route")
	}
	if len(parts) == 1 {
		return parts[0]
	}
	out := runtimeStatus("healthy", "healthy")
	healthy, unknown, failed := 0, 0, 0
	for _, part := range parts {
		runtimeMetadata(&out, part)
		switch part.State {
		case "healthy":
			healthy++
		case "degraded":
			healthy++
			failed++
		case "down":
			failed++
		default:
			unknown++
		}
	}
	if healthy > 0 {
		// Partial availability is supported by the still-usable paths.
		// An old unavailable alternative must not make a live degraded
		// result look like an entirely stale cached observation.
		out.ObservedAt = nil
		for _, part := range parts {
			if part.State == "healthy" || part.State == "degraded" {
				runtimeMetadata(&out, part)
			}
		}
		if failed > 0 {
			out.State, out.Reason = "degraded", "partial_failure"
		} else if unknown > 0 {
			out.State, out.Reason = "degraded", "partial_unknown"
		}
	} else if unknown > 0 {
		out.State, out.Reason = "unknown", "partial_unknown"
	} else {
		out.State, out.Reason = "down", "no_route"
	}
	return out
}
