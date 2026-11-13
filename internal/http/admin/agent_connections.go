package admin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/zeptop-dev/captain/internal/agentendpoint"
	"github.com/zeptop-dev/captain/internal/agenthttps"
	"github.com/zeptop-dev/captain/internal/store"
)

func registerAgentConnections(m *http.ServeMux, h *handlers) {
	m.HandleFunc("POST /api/admin/settings/agent-endpoints/{entry}/check", h.checkAgentEndpoint)
	m.HandleFunc("GET /api/admin/settings/agent-endpoints", h.requireAdmin(h.agentEndpoints))
	m.HandleFunc("POST /api/admin/settings/agent-endpoints", h.requireAdmin(h.saveAgentEndpoint))
	m.HandleFunc("PUT /api/admin/settings/agent-endpoints/{entry}", h.requireAdmin(h.saveAgentEndpoint))
	m.HandleFunc("DELETE /api/admin/settings/agent-endpoints/{entry}", h.requireAdmin(h.deleteAgentEndpoint))
	m.HandleFunc("POST /api/admin/agent-connections/{migration}/{action}", h.requireAdmin(h.agentConnectionAction))
	m.HandleFunc("GET /api/admin/agent-connections/entries/{entry}", h.requireAdmin(h.agentEndpointUsage))
	m.HandleFunc("GET /api/admin/agent-connections", h.requireAdmin(h.agentConnections))
	m.HandleFunc("POST /api/admin/agent-connections/preview", h.requireAdmin(h.previewAgentConnections))
	m.HandleFunc("POST /api/admin/agent-connections/migrate", h.requireAdmin(h.migrateAgentConnections))
	m.HandleFunc("GET /api/admin/nodes/{id}/agent-connection", h.requireAdmin(h.nodeAgentConnection))
}
func (h *handlers) connectionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		fail(w, 404, "connection or entry not found")
	case errors.Is(err, store.ErrAgentHTTPSRevision):
		fail(w, 409, "connection settings changed; reload and retry")
	case errors.Is(err, store.ErrAgentHTTPSInvalid):
		fail(w, 400, err.Error())
	default:
		serverErr(w, err)
	}
}
func (h *handlers) agentEndpoints(w http.ResponseWriter, r *http.Request) {
	entries, err := h.Store.AgentEndpoints(r.Context())
	if err != nil {
		h.connectionError(w, err)
		return
	}
	statuses := map[string]agenthttps.Status{}
	for _, v := range entries {
		if h.AgentHTTPS != nil {
			statuses[v.ID] = h.AgentHTTPS.EntryStatus(v.URL)
		}
	}
	ok(w, map[string]any{"entries": entries, "statuses": statuses})
}
func (h *handlers) saveAgentEndpoint(w http.ResponseWriter, r *http.Request) {
	var in store.AgentEndpoint
	if !readJSON(w, r, &in) {
		return
	}
	if id := r.PathValue("entry"); id != "" {
		in.ID = id
	} else {
		in.ID = ""
	}
	if h.AgentHTTPS != nil {
		if err := h.AgentHTTPS.ValidateConfig(in.Config); err != nil {
			fail(w, 400, err.Error())
			return
		}
	}
	v, err := h.Store.SaveAgentEndpoint(r.Context(), in)
	if err != nil {
		h.connectionError(w, err)
		return
	}
	if h.AgentHTTPS != nil {
		h.AgentHTTPS.Refresh(r.Context())
	}
	ok(w, v)
}
func (h *handlers) deleteAgentEndpoint(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Revision string `json:"revision"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if err := h.Store.DeleteAgentEndpoint(r.Context(), r.PathValue("entry"), in.Revision); err != nil {
		h.connectionError(w, err)
		return
	}
	if h.AgentHTTPS != nil {
		h.AgentHTTPS.Refresh(r.Context())
	}
	ok(w, map[string]bool{"deleted": true})
}
func (h *handlers) nodeAgentConnection(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fail(w, 400, "invalid node")
		return
	}
	v, err := h.Store.AgentConnection(r.Context(), id)
	if err != nil {
		h.connectionError(w, err)
		return
	}
	ok(w, v)
}
func (h *handlers) agentConnections(w http.ResponseWriter, r *http.Request) {
	values, err := h.Store.AgentConnectionMigrations(r.Context())
	if err != nil {
		h.connectionError(w, err)
		return
	}
	ok(w, map[string]any{"migrations": values})
}

type connectionBatchRequest struct {
	NodeIDs  []int64 `json:"node_ids"`
	Revision string  `json:"revision"`
	store.AgentConnectionChoice
}

func (h *handlers) previewAgentConnections(w http.ResponseWriter, r *http.Request) {
	var in connectionBatchRequest
	if !readJSON(w, r, &in) {
		return
	}
	if len(in.NodeIDs) == 0 || len(in.NodeIDs) > 500 {
		fail(w, 400, "select 1 to 500 nodes")
		return
	}
	target, _, err := h.Store.ResolveAgentConnection(r.Context(), in.AgentConnectionChoice, agentendpoint.EffectiveURL("", h.AgentURL, h.BaseURL))
	if err != nil {
		h.connectionError(w, err)
		return
	}
	revision, nodes, err := h.Store.PreviewAgentConnections(r.Context(), in.NodeIDs, in.AgentConnectionChoice, agentendpoint.EffectiveURL("", h.AgentURL, h.BaseURL))
	if err != nil {
		h.connectionError(w, err)
		return
	}
	ok(w, map[string]any{"target_url": target, "nodes": nodes, "concurrency": 5, "revision": revision})
}
func (h *handlers) migrateAgentConnections(w http.ResponseWriter, r *http.Request) {
	var in connectionBatchRequest
	if !readJSON(w, r, &in) {
		return
	}
	batch, err := h.Store.StartAgentConnectionBatch(r.Context(), in.NodeIDs, in.AgentConnectionChoice, agentendpoint.EffectiveURL("", h.AgentURL, h.BaseURL), in.Revision)
	if err != nil {
		h.connectionError(w, err)
		return
	}
	ok(w, map[string]string{"batch_id": batch})
}

func (h *handlers) agentConnectionAction(w http.ResponseWriter, r *http.Request) {
	if err := h.Store.AgentConnectionAction(r.Context(), r.PathValue("migration"), r.PathValue("action")); err != nil {
		h.connectionError(w, err)
		return
	}
	ok(w, map[string]bool{"accepted": true})
}

// Like the legacy diagnostic, this request drops Accounts during network I/O
// and revalidates the exact entry before publishing the result.
func (h *handlers) checkAgentEndpoint(w http.ResponseWriter, r *http.Request) {
	r = r.WithContext(context.WithValue(r.Context(), subPresetAuthOnlyKey{}, true))
	authorized := false
	h.Store.Accounts.RLock()
	h.requireAdmin(func(http.ResponseWriter, *http.Request) { authorized = true })(w, r)
	h.Store.Accounts.RUnlock()
	if !authorized {
		return
	}
	var in struct {
		Revision string `json:"revision"`
	}
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Now().Add(5 * time.Second))
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	_ = controller.SetReadDeadline(time.Time{})
	if err != nil || json.Unmarshal(body, &in) != nil {
		fail(w, 400, "invalid check request")
		return
	}
	lookup := func() (store.AgentEndpoint, error) {
		entries, e := h.Store.AgentEndpoints(r.Context())
		if e != nil {
			return store.AgentEndpoint{}, e
		}
		for _, v := range entries {
			if v.ID == r.PathValue("entry") {
				return v, nil
			}
		}
		return store.AgentEndpoint{}, store.ErrNotFound
	}
	h.Store.Accounts.RLock()
	saved, err := lookup()
	h.Store.Accounts.RUnlock()
	if err != nil {
		h.connectionError(w, err)
		return
	}
	if saved.Revision != in.Revision {
		h.connectionError(w, store.ErrAgentHTTPSRevision)
		return
	}
	if !h.agentCheck.TryLock() {
		fail(w, 429, "another check is running")
		return
	}
	defer h.agentCheck.Unlock()
	result := agenthttps.AccessCheck{URL: saved.URL, Revision: saved.Revision, Perspective: "panel", Status: "not_ready", CheckedAt: time.Now().UTC()}
	if h.AgentHTTPS != nil && h.AgentCheck != nil && saved.Enabled && h.AgentHTTPS.EntryStatus(saved.URL).State == "listening" {
		result = h.AgentCheck.Check(r.Context(), saved.URL)
		result.Revision = saved.Revision
	}
	h.Store.Accounts.RLock()
	defer h.Store.Accounts.RUnlock()
	h.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		current, e := lookup()
		if e != nil || current.Revision != saved.Revision {
			h.connectionError(w, store.ErrAgentHTTPSRevision)
			return
		}
		ok(w, result)
	})(w, r)
}

func (h *handlers) agentEndpointUsage(w http.ResponseWriter, r *http.Request) {
	nodes, truncated, err := h.Store.AgentEndpointNodes(r.Context(), r.PathValue("entry"))
	if err != nil {
		h.connectionError(w, err)
		return
	}
	ok(w, map[string]any{"nodes": nodes, "truncated": truncated})
}
