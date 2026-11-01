package admin

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/store"
)

func registerOperations(mux *http.ServeMux, h *handlers) {
	mux.HandleFunc("GET /api/admin/operations", h.requireAdmin(h.listOperations))
	mux.HandleFunc("GET /api/admin/operations/{id}", h.requireAdmin(h.getOperation))
}

func (h *handlers) operationAccess(r *http.Request) (store.OperationAccess, error) {
	policy := "full"
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer"))
	if strings.HasPrefix(token, "cap_") {
		u, p, e := h.Store.UserByAPIToken(r.Context(), token)
		if e != nil || u == nil || !u.IsAdmin() || u.Status != "active" {
			return store.OperationAccess{}, store.ErrNotFound
		}
		policy = p
	}
	allows := func(path, pattern string) bool {
		return store.TokenAllowsRequest(policy, http.MethodGet, path, "GET "+pattern)
	}
	node := allows("/api/admin/nodes/1", "/api/admin/nodes/{id}")
	line := allows("/api/admin/lines", "/api/admin/lines")
	return store.OperationAccess{
		LineRollouts:       node && line && allows("/api/admin/line-rollouts/1", "/api/admin/line-rollouts/{rollout}"),
		PathRollouts:       node && allows("/api/admin/path-routes", "/api/admin/path-routes") && allows("/api/admin/path-rollouts/1", "/api/admin/path-rollouts/{rollout}"),
		NodeJobs:           node && allows("/api/admin/nodes/1/jobs/1", "/api/admin/nodes/{id}/jobs/{job}"),
		NodeUpgrades:       node,
		TopologyChangesets: allows("/api/admin/topology-changesets/1", "/api/admin/topology-changesets/{id}"),
		TopologyInbounds:   node, TopologyLines: line, TopologyEntries: allows("/api/admin/entries", "/api/admin/entries"),
	}, nil
}

func (h *handlers) listOperations(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	a, err := h.operationAccess(r)
	if err != nil {
		fail(w, 403, "administrator token required")
		return
	}
	q := store.OperationQuery{Source: r.URL.Query().Get("source"), Status: r.URL.Query().Get("status"), Before: r.URL.Query().Get("before"), Limit: 20}
	for name, dst := range map[string]*int64{"node_id": &q.NodeID, "line_id": &q.LineID} {
		if raw := r.URL.Query().Get(name); raw != "" {
			v, e := strconv.ParseInt(raw, 10, 64)
			if e != nil || v < 1 {
				fail(w, 400, "invalid operation filter")
				return
			}
			*dst = v
		}
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		v, e := strconv.Atoi(raw)
		if e != nil {
			fail(w, 400, "invalid limit")
			return
		}
		q.Limit = v
	}
	page, err := h.Store.Operations(r.Context(), q, a, time.Now())
	if errors.Is(err, store.ErrOperationQuery) {
		fail(w, 400, "invalid operation query")
		return
	}
	if err != nil {
		serverErr(w, err)
		return
	}
	ok(w, page)
}

func (h *handlers) getOperation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	a, err := h.operationAccess(r)
	if err != nil {
		fail(w, 404, "operation not found")
		return
	}
	v, err := h.Store.Operation(r.Context(), r.PathValue("id"), a, time.Now())
	if errors.Is(err, store.ErrNotFound) {
		fail(w, 404, "operation not found")
		return
	}
	if err != nil {
		serverErr(w, err)
		return
	}
	ok(w, v)
}
