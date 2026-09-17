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
	ID                 int64      `json:"id"`
	Name               string     `json:"name"`
	PublicAddr         string     `json:"public_addr"`
	InternalAddr       string     `json:"internal_addr"`
	V6Addr             string     `json:"v6_addr"`
	Domain             string     `json:"domain"`
	MonitorURL         string     `json:"monitor_url"`
	DecoyEnabled       bool       `json:"decoy_enabled"`
	DecoyUpstream      string     `json:"decoy_upstream"`
	UserSpeedLimitMbps int        `json:"user_speed_limit_mbps"`
	MitaQuotas         bool       `json:"mita_quotas"`
	EgressByIngress    bool       `json:"egress_by_ingress"`
	Version            string     `json:"version"`
	Platform           string     `json:"platform"`
	Hostname           string     `json:"hostname"`
	LastSeenAt         *time.Time `json:"last_seen_at"`
	Online             bool       `json:"online"`
	Paired             bool       `json:"paired"`
	PairCode           string     `json:"pair_code,omitempty"`
	TrafficToday       int64      `json:"traffic_today_bytes"`
	Inbounds           int        `json:"inbounds"`
	UpgradeTo          string     `json:"upgrade_to,omitempty"` // pending upgrade request
	Outdated           bool       `json:"outdated"`             // reported version older than the latest bosun release
	CertProblem        bool       `json:"cert_problem"`         // an automatic certificate failed or expires soon
	DoctorFail         bool       `json:"doctor_fail"`          // the node's last self-check had failures
}

func toNodeView(n *domain.Node, at time.Time) nodeView {
	return nodeView{
		ID: n.ID, Name: n.Name, PublicAddr: n.PublicAddr, InternalAddr: n.InternalAddr, V6Addr: n.V6Addr, Domain: n.Domain, MonitorURL: n.MonitorURL, DecoyEnabled: n.DecoyEnabled, DecoyUpstream: n.DecoyUpstream, UserSpeedLimitMbps: n.UserSpeedLimitMbps, MitaQuotas: n.MitaQuotas, EgressByIngress: n.EgressByIngress,
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
	out := make([]nodeView, 0, len(nodes))
	for _, n := range nodes {
		v := toNodeView(n, now)
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
	Name, PublicAddr, InternalAddr, V6Addr, Domain, MonitorURL string
	DecoyEnabled                                               bool
	DecoyUpstream                                              string
	UserSpeedLimitMbps                                         int
	MitaQuotas                                                 bool
	EgressByIngress                                            bool
}

func (h *handlers) createNode(w http.ResponseWriter, r *http.Request) {
	var in nodeInput
	if !decode(r, &in) || in.Name == "" {
		fail(w, http.StatusBadRequest, "name is required")
		return
	}
	n := &domain.Node{Name: in.Name, PublicAddr: in.PublicAddr, InternalAddr: in.InternalAddr, V6Addr: in.V6Addr, Domain: strings.ToLower(strings.TrimSpace(in.Domain)), MonitorURL: in.MonitorURL}
	if err := h.Store.CreateNode(r.Context(), n, auth.PairCode(), 24*time.Hour); err != nil {
		serverErr(w, err)
		return
	}
	v := toNodeView(n, time.Now()) // includes the pairing code once
	ok(w, struct {
		nodeView
		DNS []service.Result `json:"dns,omitempty"`
	}{v, h.DNS.EnsureMany(r.Context(), [2]string{n.Domain, n.PublicAddr}, [2]string{n.Domain, n.V6Addr})})
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
	id, okID := pathID(r)
	var in nodeInput
	if !okID || !decode(r, &in) || in.Name == "" {
		fail(w, http.StatusBadRequest, "name is required")
		return
	}
	n := &domain.Node{ID: id, Name: in.Name, PublicAddr: in.PublicAddr, InternalAddr: in.InternalAddr, V6Addr: in.V6Addr, Domain: strings.ToLower(strings.TrimSpace(in.Domain)), MonitorURL: in.MonitorURL, DecoyEnabled: in.DecoyEnabled, DecoyUpstream: strings.TrimSpace(in.DecoyUpstream), UserSpeedLimitMbps: in.UserSpeedLimitMbps, MitaQuotas: in.MitaQuotas, EgressByIngress: in.EgressByIngress}
	if err := h.Store.UpdateNode(r.Context(), n); err != nil {
		serverErr(w, err)
		return
	}
	ok(w, map[string]any{"ok": true, "dns": h.DNS.EnsureMany(r.Context(), [2]string{n.Domain, n.PublicAddr}, [2]string{n.Domain, n.V6Addr})})
}

func (h *handlers) deleteNode(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	if !okID {
		fail(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := h.Store.DeleteNode(r.Context(), id); err != nil {
		serverErr(w, err)
		return
	}
	ok(w, map[string]bool{"ok": true})
}

// repairNode issues a new pairing code and revokes the old token.
func (h *handlers) repairNode(w http.ResponseWriter, r *http.Request) {
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
	nodeID, okID := pathID(r)
	var ib domain.Inbound
	if !okID || !decode(r, &ib) || ib.Tag == "" || ib.Protocol == "" || ib.Port == 0 {
		fail(w, http.StatusBadRequest, "tag, protocol and port are required")
		return
	}
	ib.NodeID = nodeID
	ib.Enabled = true
	fillInboundSecrets(&ib)
	if msg := checkInboundFields(&ib); msg != "" {
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
	id, okID := pathID(r)
	cur, err := h.Store.InboundByID(r.Context(), id)
	if !okID || err != nil {
		fail(w, http.StatusNotFound, "inbound not found")
		return
	}
	ib := *cur
	if !decode(r, &ib) || ib.Tag == "" || ib.Protocol == "" || ib.Port == 0 {
		fail(w, http.StatusBadRequest, "tag, protocol and port are required")
		return
	}
	ib.ID, ib.NodeID = cur.ID, cur.NodeID
	fillInboundSecrets(&ib)
	if msg := checkInboundFields(&ib); msg != "" {
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
	id, okID := pathID(r)
	if !okID {
		fail(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := h.Store.DeleteInbound(r.Context(), id); err != nil {
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
	if !strings.HasPrefix(v, "v") {
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
			out = append(out, bind{l, o.Listen, "inbound " + o.Tag})
		}
	}
	if !skipForwards {
		fws, _ := h.Store.NodeForwards(ctx, nodeID)
		for _, f := range fws {
			for _, l := range forwardListeners(f) {
				out = append(out, bind{l, f.Listen, "forward " + f.Tag})
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
			if b.listener == l && listenOverlap(b.listen, ib.Listen) {
				return fmt.Sprintf("%s port %d is already used by %s", strings.ToUpper(l.proto), l.port, b.owner)
			}
		}
	}
	return ""
}
