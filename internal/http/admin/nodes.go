package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zeptop-dev/bosun/pkg/selfupdate"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/service"

	"github.com/zeptop-dev/captain/internal/auth"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

type nodeView struct {
	MonitorGroup         string        `json:"monitor_group"`
	ModeRevision         uint64        `json:"mode_revision"`
	ReportedModeRevision uint64        `json:"reported_mode_revision"`
	ModeSwitchAvailable  bool          `json:"mode_switch_available"`
	ProxyAllowed         bool          `json:"proxy_allowed"`
	Mode                 spec.NodeMode `json:"mode"`
	ReportedMode         spec.NodeMode `json:"reported_mode"`
	ModeReportedAt       *time.Time    `json:"mode_reported_at,omitempty"`
	ModeError            string        `json:"mode_error,omitempty"`
	ID                   int64         `json:"id"`
	Name                 string        `json:"name"`
	PublicAddr           string        `json:"public_addr"`
	InternalAddr         string        `json:"internal_addr"`
	V6Addr               string        `json:"v6_addr"`
	Domain               string        `json:"domain"`
	DomainShared         bool          `json:"domain_shared"`
	DomainConflict       bool          `json:"domain_conflict"`
	MonitorURL           string        `json:"monitor_url"`
	DecoyEnabled         bool          `json:"decoy_enabled"`
	DecoyUpstream        string        `json:"decoy_upstream"`
	UserSpeedLimitMbps   int           `json:"user_speed_limit_mbps"`
	MitaQuotas           bool          `json:"mita_quotas"`
	EgressByIngress      bool          `json:"egress_by_ingress"`
	DStatusSID           string        `json:"dstatus_sid"`
	Version              string        `json:"version"`
	Platform             string        `json:"platform"`
	Hostname             string        `json:"hostname"`
	LastSeenAt           *time.Time    `json:"last_seen_at"`
	Online               bool          `json:"online"`
	Paired               bool          `json:"paired"`
	PairCode             string        `json:"pair_code,omitempty"`
	TrafficToday         int64         `json:"traffic_today_bytes"`
	Inbounds             int           `json:"inbounds"`
	UpgradeTo            string        `json:"upgrade_to,omitempty"` // pending upgrade request
	Outdated             bool          `json:"outdated"`             // reported version older than the latest bosun release
	CertProblem          bool          `json:"cert_problem"`         // an automatic certificate failed or expires soon
	DoctorFail           bool          `json:"doctor_fail"`          // the node's last self-check had failures
}

func toNodeView(n *domain.Node, at time.Time) nodeView {
	return nodeView{ModeRevision: n.ModeRevision, ReportedModeRevision: n.ReportedModeRevision, ModeSwitchAvailable: !n.Paired || (n.ReportedMode != "" && n.ModeReportedAt != nil && at.Sub(*n.ModeReportedAt) < 3*time.Minute), ProxyAllowed: store.NodeProxyAllowed(n), Mode: n.Mode.Effective(), ReportedMode: n.ReportedMode, ModeReportedAt: n.ModeReportedAt, ModeError: n.ModeError,
		ID: n.ID, Name: n.Name, MonitorGroup: n.MonitorGroup, PublicAddr: n.PublicAddr, InternalAddr: n.InternalAddr, V6Addr: n.V6Addr, Domain: n.Domain, DomainShared: n.DomainShared, MonitorURL: n.MonitorURL, DecoyEnabled: n.DecoyEnabled, DecoyUpstream: n.DecoyUpstream, UserSpeedLimitMbps: n.UserSpeedLimitMbps, MitaQuotas: n.MitaQuotas, EgressByIngress: n.EgressByIngress, DStatusSID: n.DStatusSID,
		Version: n.Version, Platform: n.Platform, Hostname: n.Hostname, LastSeenAt: n.LastSeenAt,
		Online: n.LastSeenAt != nil && at.Sub(*n.LastSeenAt) < 3*time.Minute, Paired: n.Paired, PairCode: n.PairCode,
		UpgradeTo: n.UpgradeTo,
	}
}

// bosunLatest returns the newest bosun release tag, "" when unknown.
func (h *handlers) bosunLatest(ctx context.Context) string {
	if h.BosunReleases == nil {
		return ""
	}
	info := h.BosunReleases.Check(ctx, false)
	if info.Warning != "" && info.Latest == h.BosunReleases.Version {
		return ""
	}
	return info.Latest
}

func (h *handlers) listNodes(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	nodes, err := h.Store.ListNodes(r.Context())
	if err != nil {
		serverErr(w, err)
		return
	}
	traffic, _ := h.Store.NodeTrafficToday(r.Context(), now)
	latest := h.bosunLatest(r.Context())
	certProblems, _ := h.Store.CertProblems(r.Context(), now)
	doctorFails, _ := h.Store.DoctorFails(r.Context())
	domainCounts := map[string]int{}
	for _, n := range nodes {
		if key := domain.NodeDomainKey(n.Domain); key != "" {
			domainCounts[key]++
		}
	}
	out := make([]nodeView, 0, len(nodes))
	for _, n := range nodes {
		v := toNodeView(n, now)
		v.DomainConflict = domainCounts[domain.NodeDomainKey(n.Domain)] > 1
		v.PairCode = "" // only shown on create/repair
		v.TrafficToday = traffic[n.ID]
		v.Outdated = latest != "" && n.Version != "" && selfupdate.Newer(latest, n.Version)
		v.CertProblem = certProblems[n.ID]
		v.DoctorFail = doctorFails[n.ID]
		if ibs, err := h.Store.AllInboundsByNode(r.Context(), n.ID); err == nil {
			v.Inbounds = len(ibs)
		}
		out = append(out, v)
	}
	ok(w, out)
}

type nodeInput struct {
	AutoIPv4, AutoIPv6                           *bool
	Mode                                         *spec.NodeMode
	Name, PublicAddr, V6Addr, Domain, MonitorURL string
	InternalAddr                                 *string
	DomainShared                                 *bool
	DecoyEnabled                                 bool
	DecoyUpstream                                string
	UserSpeedLimitMbps                           int
	MitaQuotas                                   bool
	EgressByIngress                              bool
	DStatusSID                                   string
}

func (h *handlers) createNode(w http.ResponseWriter, r *http.Request) {
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	var in nodeInput
	if !decode(r, &in) || in.Name == "" {
		fail(w, http.StatusBadRequest, "name is required")
		return
	}
	if !validNodeName(in.Name) {
		fail(w, http.StatusBadRequest, "node name may not contain control characters or markup")
		return
	}
	n := &domain.Node{Name: in.Name, PublicAddr: in.PublicAddr, V6Addr: in.V6Addr, Domain: strings.ToLower(strings.TrimSpace(in.Domain)), MonitorURL: in.MonitorURL, DStatusSID: strings.TrimSpace(in.DStatusSID)}
	if in.InternalAddr != nil {
		n.InternalAddr = strings.TrimSpace(*in.InternalAddr)
	}
	if in.Mode != nil {
		n.Mode = *in.Mode
	}
	if n.Mode.Validate() != nil {
		fail(w, 400, "invalid node mode")
		return
	}
	if in.DomainShared != nil {
		n.DomainShared = *in.DomainShared
	}
	auto4, auto6 := in.AutoIPv4 != nil && *in.AutoIPv4, in.AutoIPv6 != nil && *in.AutoIPv6
	if (auto4 && strings.TrimSpace(n.PublicAddr) != "") || (auto6 && strings.TrimSpace(n.V6Addr) != "") {
		fail(w, 400, "automatic addresses must be empty; choose fixed mode to provide an entry address")
		return
	}
	if err := h.Store.CreateNodeWithAddressPolicy(r.Context(), n, auth.PairCode(), 24*time.Hour, auto4, auto6); err != nil {
		nodeSaveError(w, err)
		return
	}
	v := toNodeView(n, time.Now()) // includes the pairing code once
	uses, err := h.Store.NodeDomainUses(r.Context(), n.Domain, n.ID)
	if err != nil {
		serverErr(w, err)
		return
	}
	v.DomainConflict = len(uses) > 0
	ok(w, struct {
		nodeView
		DNS []service.Result `json:"dns,omitempty"`
	}{v, h.DNS.EnsureMany(service.WithDNSOrigin(r.Context(), service.DNSOrigin{NodeID: n.ID, Source: "node", Actor: userFrom(r).Email}), [2]string{n.Domain, n.PublicAddr}, [2]string{n.Domain, n.V6Addr})})
}

func (h *handlers) getNode(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	if !okID {
		fail(w, http.StatusBadRequest, "bad id")
		return
	}
	n, err := h.Store.NodeByID(r.Context(), id)
	if err != nil {
		fail(w, http.StatusNotFound, "node not found")
		return
	}
	v := toNodeView(n, time.Now())
	uses, err := h.Store.NodeDomainUses(r.Context(), n.Domain, n.ID)
	if err != nil {
		serverErr(w, err)
		return
	}
	v.DomainConflict = len(uses) > 0
	if n.Paired {
		v.PairCode = ""
	}
	inbounds, _ := h.Store.AllInboundsByNode(r.Context(), id)
	if inbounds == nil {
		inbounds = []*domain.Inbound{}
	}
	ingresses, _ := h.Store.IngressesByNode(r.Context(), id)
	status, _ := h.Store.NodeStatus(r.Context(), id)
	traffic, _ := h.Store.InboundTrafficByNode(r.Context(), id, time.Now())
	ok(w, map[string]any{"node": v, "inbounds": inbounds, "ingresses": ingresses, "status": status, "traffic": traffic})
}

func (h *handlers) updateNode(w http.ResponseWriter, r *http.Request) {
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	id, okID := pathID(r)
	var in nodeInput
	if !okID || !decode(r, &in) || in.Name == "" {
		fail(w, http.StatusBadRequest, "name is required")
		return
	}
	if !validNodeName(in.Name) {
		fail(w, http.StatusBadRequest, "node name may not contain control characters or markup")
		return
	}
	n := &domain.Node{ID: id, Name: in.Name, PublicAddr: in.PublicAddr, V6Addr: in.V6Addr, Domain: strings.ToLower(strings.TrimSpace(in.Domain)), MonitorURL: in.MonitorURL, DecoyEnabled: in.DecoyEnabled, DecoyUpstream: strings.TrimSpace(in.DecoyUpstream), UserSpeedLimitMbps: in.UserSpeedLimitMbps, MitaQuotas: in.MitaQuotas, EgressByIngress: in.EgressByIngress, DStatusSID: strings.TrimSpace(in.DStatusSID)}
	old, err := h.Store.NodeByID(r.Context(), id)
	if err != nil {
		nodeSaveError(w, err)
		return
	}
	n.InternalAddr = old.InternalAddr
	if in.InternalAddr != nil {
		n.InternalAddr = strings.TrimSpace(*in.InternalAddr)
	}
	if in.Mode != nil && in.Mode.Effective() != old.Mode {
		fail(w, 400, "use the node mode endpoint to switch modes")
		return
	}
	if n.EgressByIngress {
		links, err := h.Store.WireGuardLinks(r.Context(), id)
		if err != nil {
			serverErr(w, err)
			return
		}
		if len(links) > 0 {
			fail(w, 409, "remove WireGuard connections before enabling per-ingress egress")
			return
		}
	}
	if !store.NodeProxyAllowed(old) && (n.DecoyEnabled || n.UserSpeedLimitMbps != 0 || n.MitaQuotas || n.EgressByIngress) {
		fail(w, 409, spec.ErrMonitorOnly.Error())
		return
	}
	n.DomainShared = old.DomainShared
	if in.DomainShared != nil {
		n.DomainShared = *in.DomainShared
	}
	if err := h.Store.UpdateNode(r.Context(), n); err != nil {
		nodeSaveError(w, err)
		return
	}
	ok(w, map[string]any{"ok": true, "dns": h.DNS.EnsureMany(service.WithDNSOrigin(r.Context(), service.DNSOrigin{NodeID: n.ID, Source: "node", Actor: userFrom(r).Email}), [2]string{n.Domain, n.PublicAddr}, [2]string{n.Domain, n.V6Addr})})
}

func (h *handlers) deleteNode(w http.ResponseWriter, r *http.Request) {
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	id, okID := pathID(r)
	if !okID {
		fail(w, http.StatusBadRequest, "bad id")
		return
	}
	if jobID := r.URL.Query().Get("removal_job"); jobID != "" {
		if !userFrom(r).IsAdmin() {
			fail(w, 403, "admin only")
			return
		}
		j, err := h.Store.NodeJob(r.Context(), id, jobID)
		var result struct {
			Phase string `json:"phase"`
		}
		if err != nil || j.Kind != store.NodeRemovalKind || j.DoneAt == nil || j.Error != "" || json.Unmarshal(j.Result, &result) != nil || result.Phase != "complete" {
			fail(w, 409, "remote operation has not completed successfully")
			return
		}
	}
	if !h.requireNoNodeLines(w, r, id) {
		return
	}
	if err := h.Store.DeleteNode(r.Context(), id); err != nil {
		if entryClientChainError(w, err) {
			return
		}
		serverErr(w, err)
		return
	}
	ok(w, map[string]bool{"ok": true})
}

// repairNode issues a new pairing code and revokes the old token.
func (h *handlers) repairNode(w http.ResponseWriter, r *http.Request) {
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	id, okID := pathID(r)
	if !okID {
		fail(w, http.StatusBadRequest, "bad id")
		return
	}
	code := auth.PairCode()
	if err := h.Store.ResetPairCode(r.Context(), id, code, 24*time.Hour); err != nil {
		serverErr(w, err)
		return
	}
	ok(w, map[string]string{"pair_code": code})
}

// checkInboundFields rejects values the node could not embed safely: the
// tag becomes file names and nft comments, the listen address goes into
// nft rules.
// checkInboundFields runs the shape check bosun's cores agree on
// (pkg/spec Validate) so a bad inbound is refused here with the same
// wording the node's doctor would use, instead of being pushed and skipped.
func checkInboundFields(ib *domain.Inbound) string {
	ib.Tag = strings.TrimSpace(ib.Tag)
	ib.Listen = strings.TrimSpace(ib.Listen)
	if err := ib.Spec().Validate(); err != nil {
		return err.Error()
	}
	return ""
}

func (h *handlers) createInbound(w http.ResponseWriter, r *http.Request) {
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	nodeID, okID := pathID(r)
	var ib domain.Inbound
	if !okID || !decode(r, &ib) || ib.Tag == "" || ib.Protocol == "" || ib.Port == 0 {
		fail(w, http.StatusBadRequest, "tag, protocol and port are required")
		return
	}
	if !h.requireProxyNode(w, r, nodeID) {
		return
	}
	ib.NodeID = nodeID
	if ib.Settings.PrivateAccess.Enabled() && !userFrom(r).IsAdmin() {
		fail(w, 403, "only administrators can grant private access")
		return
	}
	if ib.Settings.Reverse != nil || ib.Settings.WireGuardSlot != 0 {
		fail(w, 400, "use the reverse connection wizard")
		return
	}
	ib.Enabled = true
	fillInboundSecrets(&ib)
	if msg := checkInboundFields(&ib); msg != "" {
		fail(w, http.StatusBadRequest, msg)
		return
	}
	if msg := h.checkPrivateAccess(r.Context(), ib.NodeID, &ib); msg != "" {
		fail(w, 400, msg)
		return
	}
	if msg := h.checkInboundCore(r.Context(), &ib); msg != "" {
		fail(w, http.StatusBadRequest, msg)
		return
	}
	if msg := h.checkIngress(r, &ib); msg != "" {
		fail(w, http.StatusBadRequest, msg)
		return
	}
	if msg := h.checkPortConflict(r.Context(), &ib); msg != "" {
		fail(w, http.StatusConflict, msg)
		return
	}
	if err := h.Store.CreateInbound(r.Context(), &ib); err != nil {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	ok(w, ib)
}

func (h *handlers) updateInbound(w http.ResponseWriter, r *http.Request) {
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	id, okID := pathID(r)
	cur, err := h.Store.InboundByID(r.Context(), id)
	if !okID || err != nil {
		fail(w, http.StatusNotFound, "inbound not found")
		return
	}
	if cur.Settings.PrivateAccess.Enabled() && !userFrom(r).IsAdmin() {
		fail(w, 403, "only administrators can edit an inbound granting private access")
		return
	}
	ib := *cur
	// A supplied policy replaces the complete grant. Decoding into the old
	// pointer would retain custom rules when switching off/internal, and retain
	// omitted protocol/port restrictions in replacement custom rules. Restore
	// the old grant below only when the client omits it or explicitly sends null.
	ib.Settings.PrivateAccess = nil
	if h.Store.ReverseOwnedInbound(r.Context(), id) {
		fail(w, 409, "edit this inbound in its exit node's reverse connections")
		return
	}
	if !decode(r, &ib) || ib.Tag == "" || ib.Protocol == "" || ib.Port == 0 {
		fail(w, http.StatusBadRequest, "tag, protocol and port are required")
		return
	}
	ib.ID, ib.NodeID = cur.ID, cur.NodeID
	if ib.Settings.PrivateAccess == nil {
		ib.Settings.PrivateAccess = cur.Settings.PrivateAccess
	}
	if !samePrivateAccess(cur.Settings.PrivateAccess, ib.Settings.PrivateAccess) && !userFrom(r).IsAdmin() {
		fail(w, 403, "only administrators can change private access")
		return
	}
	if ib.Settings.Reverse != nil || ib.Settings.WireGuardSlot != 0 {
		fail(w, 400, "use the reverse connection wizard")
		return
	}
	if !h.requireProxyNode(w, r, ib.NodeID) {
		return
	}
	fillInboundSecrets(&ib)
	if msg := checkInboundFields(&ib); msg != "" {
		fail(w, http.StatusBadRequest, msg)
		return
	}
	if msg := h.checkPrivateAccess(r.Context(), ib.NodeID, &ib); msg != "" {
		fail(w, 400, msg)
		return
	}
	if msg := h.checkInboundCore(r.Context(), &ib); msg != "" {
		fail(w, http.StatusBadRequest, msg)
		return
	}
	if msg := h.checkIngress(r, &ib); msg != "" {
		fail(w, http.StatusBadRequest, msg)
		return
	}
	if msg := h.checkPortConflict(r.Context(), &ib); msg != "" {
		fail(w, http.StatusConflict, msg)
		return
	}
	if err := h.Store.UpdateInbound(r.Context(), &ib); err != nil {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	ok(w, ib)
}

func (h *handlers) deleteInbound(w http.ResponseWriter, r *http.Request) {
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	id, okID := pathID(r)
	if h.Store.ReverseOwnedInbound(r.Context(), id) {
		fail(w, 409, "remove this inbound through its reverse connection")
		return
	}
	if !okID {
		fail(w, http.StatusBadRequest, "bad id")
		return
	}
	if used, err := h.Store.InboundHasLines(r.Context(), id); err != nil {
		serverErr(w, err)
		return
	} else if used {
		fail(w, 409, store.ErrLineInUse.Error())
		return
	}
	if err := h.Store.DeleteInbound(r.Context(), id); err != nil {
		if entryClientChainError(w, err) {
			return
		}
		serverErr(w, err)
		return
	}
	ok(w, map[string]bool{"ok": true})
}

type upgradeInput struct {
	Version string // release tag; empty = latest bosun release
}

func (h *handlers) resolveBosunVersion(ctx context.Context, in upgradeInput) (string, error) {
	v := strings.TrimSpace(in.Version)
	if v == "" {
		v = h.bosunLatest(ctx)
	}
	if v == "" {
		return "", errors.New("could not determine the latest bosun release; pass a version")
	}
	if !selfupdate.ValidTag(v) {
		return "", errors.New("version must be a release tag like v0.6.0")
	}
	return v, nil
}

// upgradeNode asks one node to move to a bosun release; the request rides
// on the next report response and clears once the node reports that version.
func (h *handlers) upgradeNode(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fail(w, http.StatusBadRequest, "bad id")
		return
	}
	var in upgradeInput
	_ = json.NewDecoder(r.Body).Decode(&in)
	v, err := h.resolveBosunVersion(r.Context(), in)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	// The node refuses downgrades, so a request for an older tag would
	// only sit in the node row forever; say so here instead.
	if n, err := h.Store.NodeByID(r.Context(), id); err == nil && n.Version != "" && v != n.Version && !selfupdate.Newer(v, n.Version) {
		fail(w, http.StatusBadRequest, v+" is older than the node's "+n.Version+"; nodes only move forward (roll back on the node itself)")
		return
	}
	if err := h.Store.SetNodeUpgrade(r.Context(), id, v); err != nil {
		serverErr(w, err)
		return
	}
	ok(w, map[string]any{"upgrade_to": v})
}

func (h *handlers) upgradeAllNodes(w http.ResponseWriter, r *http.Request) {
	var in upgradeInput
	_ = json.NewDecoder(r.Body).Decode(&in)
	v, err := h.resolveBosunVersion(r.Context(), in)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	n, err := h.Store.SetAllNodesUpgrade(r.Context(), v)
	if err != nil {
		serverErr(w, err)
		return
	}
	ok(w, map[string]any{"upgrade_to": v, "nodes": n})
}

// listener is one (transport, port) an inbound or forward occupies.
type listener struct {
	proto string
	port  int
}

// inboundListeners mirrors what the node binds for the inbound: UDP for
// the QUIC and WireGuard protocols, both for Shadowsocks and snell, mieru
// per its transport (BOTH takes port+1 for UDP), TCP otherwise.
func inboundListeners(ib *domain.Inbound) []listener {
	sp := ib.Spec()
	switch sp.Protocol {
	case spec.Hysteria2, spec.TUIC, spec.WireGuard:
		return []listener{{"udp", sp.Port}}
	case spec.Mieru:
		switch strings.ToUpper(sp.MieruTransport) {
		case "UDP":
			return []listener{{"udp", sp.Port}}
		case "BOTH":
			return []listener{{"tcp", sp.Port}, {"udp", sp.Port + 1}}
		}
		return []listener{{"tcp", sp.Port}}
	case spec.Shadowsocks, spec.Snell:
		return []listener{{"tcp", sp.Port}, {"udp", sp.Port}}
	}
	return []listener{{"tcp", sp.Port}}
}

// forwardListeners mirrors what a forward binds: only the protocols it
// relays, never both unless it says "both". An inbound on udp/443 and a
// tcp-only forward on 443 can coexist.
func forwardListeners(f store.NodeForward) []listener {
	out := make([]listener, 0, 2)
	for _, proto := range protosOf(f.Protocol) {
		out = append(out, listener{proto, f.Port})
	}
	return out
}

// bind is one socket a node opens, with the address it opens it on.
type bind struct {
	listener
	listen string
	owner  string
}

// nodeBinds lists every socket the node already opens, excluding the
// inbound and the forwards given (the ones being replaced). Disabled
// inbounds are left out: they bind nothing.
func (h *handlers) nodeBinds(ctx context.Context, nodeID, skipInbound int64, skipForwards bool) []bind {
	var out []bind
	ibs, _ := h.Store.InboundsByNode(ctx, nodeID)
	for _, o := range ibs {
		if o.ID == skipInbound || !o.Enabled {
			continue
		}
		for _, l := range inboundListeners(o) {
			out = append(out, bind{l, h.ingressListen(ctx, o.IngressID, o.Listen), "inbound " + o.Tag})
		}
	}
	links, _ := h.Store.WireGuardLinks(ctx, nodeID)
	for _, link := range links {
		if link.TransitID != nodeID || link.UserInboundID == skipInbound {
			continue
		}
		user, err := h.Store.InboundByID(ctx, link.UserInboundID)
		if err == nil && user.Enabled {
			out = append(out, bind{listener{"udp", link.ListenPort}, "", "managed WireGuard tunnel"})
		}
	}
	if !skipForwards {
		fws, _ := h.Store.NodeForwards(ctx, nodeID)
		for _, f := range fws {
			for _, l := range forwardListeners(f) {
				out = append(out, bind{l, h.forwardIngressListen(ctx, f), "forward " + f.Tag})
			}
		}
	}
	return out
}

// listenOverlap: two binds collide unless both are specific and differ.
func listenOverlap(a, b string) bool {
	any := func(s string) bool { return s == "" || s == "0.0.0.0" || s == "::" }
	return any(a) || any(b) || a == b
}

// checkPortConflict refuses saving (or enabling) an inbound whose listener
// is already taken by another enabled inbound or a forward on the same
// node; the node would otherwise fail to bind and the doctor would report
// it after the fact.
func (h *handlers) checkPortConflict(ctx context.Context, ib *domain.Inbound) string {
	if !ib.Enabled {
		return ""
	}
	for _, l := range inboundListeners(ib) {
		for _, b := range h.nodeBinds(ctx, ib.NodeID, ib.ID, false) {
			if b.listener == l && listenOverlap(b.listen, h.ingressListen(ctx, ib.IngressID, ib.Listen)) {
				return fmt.Sprintf("%s port %d is already used by %s", strings.ToUpper(l.proto), l.port, b.owner)
			}
		}
	}
	return ""
}

// validNodeName bounds a node name for the public status page: the name is
// a label there, so markup characters are refused at the door even though
// the page escapes them (operators can create nodes; the page is viewed by
// administrators on the same origin).
func validNodeName(name string) bool {
	return len(name) <= 128 && spec.PublicLabel(name)
}
