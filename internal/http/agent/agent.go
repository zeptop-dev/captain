// Package agent serves the bosun-facing API under /api/agent, implementing
// bosun/pkg/agentproto.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"

	"github.com/zeptop-dev/captain/internal/auth"
	"github.com/zeptop-dev/captain/internal/domain"
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
	token := auth.Token(32)
	n, err := h.Store.RedeemPairCode(r.Context(), strings.ToUpper(strings.TrimSpace(in.Code)), auth.SHA256Hex(token), in.Hostname, in.Version, in.Platform)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "invalid or expired pairing code")
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, "internal error")
		return
	}
	h.Log.Info("node paired", "node", n.ID, "name", n.Name, "hostname", in.Hostname, "version", in.Version)
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
		st, err := h.State.Build(r.Context(), n, time.Now())
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
	}
	// Traffic is attributed to the node's first inbound for daily stats; the
	// subscription charge is per user regardless of inbound.
	inbounds, _ := h.Store.InboundsByNode(ctx, n.ID)
	var inboundID int64
	if len(inbounds) > 0 {
		inboundID = inbounds[0].ID
	}
	for _, t := range rep.Traffic {
		if err := h.Store.AddTraffic(ctx, t.UserID, inboundID, t.Up, t.Down, now); err != nil {
			h.Log.Error("add traffic", "user", t.UserID, "err", err)
		}
	}
	for name, ips := range rep.Online {
		if u, err := h.Store.UserByUUID(ctx, name); err == nil {
			_ = h.Store.UpsertOnline(ctx, u.ID, n.ID, ips, now)
		}
	}
	for _, f := range rep.Forwards {
		_ = h.Store.UpsertForwardStatus(ctx, n.ID, f.Tag, f.Up, f.RTTMillis, f.LastError, f.ActiveConn, f.TotalConn, f.BytesIn, f.BytesOut)
	}
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
