// Package agent serves the bosun-facing API under /api/agent, implementing
// bosun/pkg/agentproto.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"

	"github.com/zeptop-dev/captain/internal/auth"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/http/ratelimit"
	"github.com/zeptop-dev/captain/internal/metrics"
	"github.com/zeptop-dev/captain/internal/service"
	"github.com/zeptop-dev/captain/internal/store"
)

// Deps are the handlers' dependencies.
type Deps struct {
	Store *store.Store
	Log   *slog.Logger
	State *service.AgentState
	// BaseURL is the public panel address baked into install scripts.
	BaseURL string
	// Probe takes host beats; nil ignores them.
	Probe *service.Probe
	// BosunInstaller overrides the upstream bosun install script URL (tests).
	BosunInstaller string
	// Pairs throttles pairing attempts per address (nil = unlimited).
	Pairs *ratelimit.Limiter
}

type handlers struct{ Deps }

// Register mounts the agent routes.
func Register(mux *http.ServeMux, d Deps) {
	h := &handlers{d}
	mux.HandleFunc("POST /api/agent/pair", h.pair)
	mux.HandleFunc("GET /api/agent/install.sh", h.installScript)
	mux.HandleFunc("GET /api/agent/state", h.requireNode(h.state))
	mux.HandleFunc("POST /api/agent/report", h.requireNode(h.report))
	mux.HandleFunc("POST /api/agent/beat", h.requireNode(h.beat))
}

type ctxKey struct{}

func nodeFrom(r *http.Request) *domain.Node {
	n, _ := r.Context().Value(ctxKey{}).(*domain.Node)
	return n
}

func (h *handlers) requireNode(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if tok == "" {
			fail(w, http.StatusUnauthorized, "missing token")
			return
		}
		n, err := h.Store.NodeByTokenHash(r.Context(), auth.SHA256Hex(tok))
		if errors.Is(err, store.ErrNotFound) {
			fail(w, http.StatusUnauthorized, "unknown token")
			return
		}
		if err != nil {
			fail(w, http.StatusInternalServerError, "internal error")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, n)))
	}
}

func (h *handlers) pair(w http.ResponseWriter, r *http.Request) {
	var in agentproto.PairRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Code == "" {
		fail(w, http.StatusBadRequest, "code is required")
		return
	}
	ip := ratelimit.ClientIP(r)
	if h.Pairs != nil {
		if allowed, wait := h.Pairs.Allow(ip); !allowed {
			fail(w, http.StatusTooManyRequests, "too many pairing attempts; try again in "+wait.String())
			return
		}
	}
	token := auth.Token(32)
	n, err := h.Store.RedeemPairCode(r.Context(), strings.ToUpper(strings.TrimSpace(in.Code)), auth.SHA256Hex(token), in.Hostname, in.Version, in.Platform)
	if errors.Is(err, store.ErrNotFound) {
		if h.Pairs != nil {
			h.Pairs.Fail(ip)
		}
		fail(w, http.StatusNotFound, "invalid or expired pairing code")
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, "internal error")
		return
	}
	if n.PublicAddr == "" {
		// The agent's source address is the best guess for a node the
		// operator has not addressed yet; only a public unicast IP counts
		// (a node behind NAT or a proxy still needs the real one).
		if p := net.ParseIP(ip); p != nil && p.IsGlobalUnicast() && !p.IsPrivate() {
			if err := h.Store.FillPublicAddr(r.Context(), n.ID, ip); err == nil {
				n.PublicAddr = ip
			}
		}
	}
	h.Log.Info("node paired", "node", n.ID, "name", n.Name, "hostname", in.Hostname, "version", in.Version, "public_addr", n.PublicAddr)
	ok(w, agentproto.PairResponse{NodeID: strconv.FormatInt(n.ID, 10), Token: token})
}

// state returns the desired state; a matching If-None-Match yields 304.
// state returns the node's desired state. With ?wait=30s and a matching
// If-None-Match the request is held (re-checking every two seconds) until
// the revision changes or the window ends, so edits reach nodes almost at
// once without a persistent connection.
func (h *handlers) state(w http.ResponseWriter, r *http.Request) {
	n := nodeFrom(r)
	wait, _ := time.ParseDuration(r.URL.Query().Get("wait"))
	if wait > 50*time.Second {
		wait = 50 * time.Second
	}
	deadline := time.Now().Add(wait)
	for {
		st, err := h.State.Cached(r.Context(), n, time.Now())
		if err != nil {
			h.Log.Error("build state", "node", n.ID, "err", err)
			fail(w, http.StatusInternalServerError, "internal error")
			return
		}
		etag := `"` + st.Revision + `"`
		if !strings.Contains(r.Header.Get("If-None-Match"), etag) {
			w.Header().Set("ETag", etag)
			ok(w, st)
			return
		}
		if time.Now().After(deadline) {
			w.Header().Set("ETag", etag)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

func (h *handlers) report(w http.ResponseWriter, r *http.Request) {
	n := nodeFrom(r)
	var rep agentproto.Report
	if err := json.NewDecoder(r.Body).Decode(&rep); err != nil {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	metrics.NodeReports.Inc(nil)
	ctx := r.Context()
	now := time.Now()
	if err := h.Store.TouchNode(ctx, n.ID, rep.Version, rep.Revision, rep.Host, rep.Cores, rep.Certs); err != nil {
		h.Log.Error("touch node", "err", err)
	}
	if rep.Doctor != nil {
		_ = h.Store.SetNodeDoctor(ctx, n.ID, rep.Doctor)
	}
	for _, jr := range rep.Jobs {
		if err := h.Store.CompleteNodeJob(ctx, n.ID, jr.ID, jr.Result, jr.Error); err != nil {
			h.Log.Error("complete node job", "job", jr.ID, "err", err)
		}
		if jr.Kind == "warp_register" && len(jr.Result) > 0 {
			_ = h.Store.SetNodeWARP(ctx, n.ID, jr.Result)
		}
	}
	// Traffic is attributed to the node's first inbound for daily stats; the
	// subscription charge is per user regardless of inbound.
	inbounds, _ := h.Store.InboundsByNode(ctx, n.ID)
	var inboundID int64
	if len(inbounds) > 0 {
		inboundID = inbounds[0].ID
	}
	// A node may only account for users it actually serves: a compromised
	// box must not be able to drain other users' quotas or trip their
	// device limits.
	allowed, err := h.Store.NodeUserIDs(ctx, n.ID)
	if err != nil {
		h.Log.Error("node users", "node", n.ID, "err", err)
		allowed = map[int64]bool{}
	}
	// Agents from bosun v0.36 say which inbound each delta went through:
	// the daily bucket is exact and the charge goes to that inbound's
	// group. Older agents (or cores that cannot tell) fall back to the
	// node's groups as a whole.
	groups, _ := h.Store.NodeGroups(ctx, n.ID)
	byTag := map[string]*domain.Inbound{}
	for _, ib := range inbounds {
		byTag[ib.Tag] = ib
	}
	dropped := 0
	samples := make([]store.TrafficSample, 0, len(rep.Traffic))
	for _, t := range rep.Traffic {
		if !allowed[t.UserID] || t.Up < 0 || t.Down < 0 {
			dropped++
			continue
		}
		s := store.TrafficSample{UserID: t.UserID, InboundID: inboundID, Groups: groups, Up: t.Up, Down: t.Down}
		if ib, ok := byTag[t.Inbound]; ok && t.Inbound != "" {
			s.InboundID, s.Groups = ib.ID, nil
			if ib.GroupID != nil {
				s.Groups = []int64{*ib.GroupID}
			}
		}
		samples = append(samples, s)
	}
	if err := h.Store.AddTrafficSamples(ctx, samples, now); err != nil {
		h.Log.Error("add traffic", "node", n.ID, "samples", len(samples), "err", err)
	}
	if dropped > 0 {
		h.Log.Warn("traffic samples for users this node does not serve were dropped", "node", n.ID, "dropped", dropped)
	}
	for tag, t := range rep.Outbounds {
		_ = h.Store.AddOutboundTraffic(ctx, n.ID, tag, t.Up, t.Down, now)
	}
	if len(rep.Inbounds) > 0 {
		byTag := map[string]int64{}
		for _, ib := range inbounds {
			byTag[ib.Tag] = ib.ID
		}
		for tag, t := range rep.Inbounds {
			if id, ok := byTag[tag]; ok {
				_ = h.Store.AddInboundTraffic(ctx, id, t.Up, t.Down, now)
			}
		}
	}
	for name, ips := range rep.Online {
		if u, err := h.Store.UserByUUID(ctx, name); err == nil && allowed[u.ID] {
			_ = h.Store.UpsertOnline(ctx, u.ID, n.ID, ips, now)
		}
	}
	for _, f := range rep.Forwards {
		_ = h.Store.UpsertForwardStatus(ctx, n.ID, f.Tag, f.Up, f.RTTMillis, f.LastError, f.ActiveConn, f.TotalConn, f.BytesIn, f.BytesOut)
	}
	// The report just wrote traffic and client addresses: rebuild fresh
	// so device-limit changes show up in this very answer.
	h.State.Invalidate()
	st, err := h.State.Build(ctx, n, now)
	changed := err == nil && st.Revision != rep.Revision
	resp := agentproto.ReportResponse{StateChanged: changed}
	if n.UpgradeTo != "" && n.UpgradeTo != rep.Version {
		resp.UpgradeTo = n.UpgradeTo
	}
	ok(w, resp)
}

func ok(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// installScript serves the one-liner target Captain prints for a new node:
// a shell script that runs the bosun installer with this panel's address and
// the pairing code. It only exists while the code is redeemable, so the URL
// is as secret as the code itself.
func (h *handlers) installScript(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.URL.Query().Get("pair"))
	if code == "" {
		http.Error(w, "pair code required", http.StatusBadRequest)
		return
	}
	okCode, err := h.Store.PairCodeValid(r.Context(), code)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !okCode {
		http.Error(w, "pairing code unknown, expired or already used; issue a new one from the node page", http.StatusNotFound)
		return
	}
	upstream := h.BosunInstaller
	if upstream == "" {
		upstream = "https://raw.githubusercontent.com/zeptop-dev/bosun/master/scripts/install.sh"
	}
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprintf(w, `#!/bin/sh
# Generated by Captain (%s): installs bosun on this server and pairs it with the panel.
set -eu
[ "$(id -u)" = 0 ] || { echo "run as root" >&2; exit 1; }
command -v curl >/dev/null || { echo "curl is required" >&2; exit 1; }
curl -fsSL %q | sh -s -- --captain %q --pair %q
`, h.BaseURL, upstream, h.BaseURL, code)
}

// beat stores one frequent host sample while probing is enabled.
func (h *handlers) beat(w http.ResponseWriter, r *http.Request) {
	var b agentproto.Beat
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	if h.Probe != nil {
		if err := h.Probe.Record(r.Context(), nodeFrom(r), b.Version, b.Host, time.Now()); err != nil {
			h.Log.Error("record beat", "node", nodeFrom(r).ID, "err", err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
