package agent

import (
	"encoding/json"
	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/captain/internal/auth"
	"io"
	"net/http"
	"strings"
)

func decodeConnectionResult(r *http.Request, out *agentproto.ConnectionResult) error {
	return json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(out)
}
func (h *handlers) connectionProof(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Header.Get("Authorization") != "" {
		fail(w, 400, "proof must be anonymous")
		return
	}
	binding, err := agentproto.ConnectionBinding(r.TLS, r.URL.Query().Get("challenge"))
	if err != nil {
		fail(w, 412, "automatic migration target requires direct Captain TLS")
		return
	}
	v, err := h.Store.AgentConnectionProof(r.Context(), r.URL.Query().Get("id"), r.URL.Query().Get("challenge"), r.Host, binding)
	if err != nil {
		fail(w, 404, "proof unavailable")
		return
	}
	ok(w, v)
}
func (h *handlers) connectionResult(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get(agentproto.ConnectionVersionHeader) != agentproto.ConnectionVersion {
		fail(w, 409, "upgrade bosun before confirming migration")
		return
	}
	var in agentproto.ConnectionResult
	if err := decodeConnectionResult(r, &in); err != nil {
		fail(w, 400, "invalid migration result")
		return
	}
	n := nodeFrom(r)
	hash := auth.SHA256Hex(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if err := h.Store.CompleteAgentConnection(r.Context(), n.ID, hash, r.Host, in.ID, in.Status, r.Header.Get(agentproto.ConnectionURLHeader)); err != nil {
		fail(w, 409, "migration is no longer active")
		return
	}
	ok(w, map[string]bool{"accepted": true})
}
func observeConnection(h *handlers, r *http.Request, nodeID int64, hash string) {
	u := r.Header.Get(agentproto.ConnectionURLHeader)
	// This is an authenticated node statement, not independent path proof.
	// Reverse proxies may rewrite Host/port; target approval remains strictly
	// bound to the actual end-to-end TLS session in connectionProof.
	if u == "" {
		return
	}
	_ = h.Store.ObserveAgentConnection(r.Context(), nodeID, hash, u, r.Header.Get(agentproto.ConnectionVersionHeader) == agentproto.ConnectionVersion)
}
