package admin

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/auth"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/http/ratelimit"
	"github.com/zeptop-dev/captain/internal/service"
	"github.com/zeptop-dev/captain/internal/store"
)

func registerNodeDiagnostics(mux *http.ServeMux, h *handlers) {
	for _, route := range []string{"GET /api/admin/node-diagnostics/{id}", "POST /api/admin/node-diagnostics/{id}/leases", "PUT /api/admin/node-diagnostics/{id}/leases/{lease}", "DELETE /api/admin/node-diagnostics/{id}/leases/{lease}"} {
		mux.HandleFunc(route, h.requireAdmin(h.nodeDiagnostics))
	}
}
func diagnosticsError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrDiagnosticsUnavailable):
		fail(w, 409, err.Error())
	case errors.Is(err, service.ErrDiagnosticsLimit):
		fail(w, 429, err.Error())
	case errors.Is(err, service.ErrDiagnosticsLease):
		fail(w, 404, err.Error())
	default:
		fail(w, 500, "internal error")
	}
}

// owner remains private to the broker. It binds a lease to this exact session
// or API token, not just the reusable staff ID. The callback rechecks current
// role, scope, account status, session expiry and address policy on agent polls.
func (h *handlers) diagnosticOwner(r *http.Request) (string, func(context.Context) bool) {
	ip := ratelimit.ClientIP(r)
	if token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer")); strings.HasPrefix(token, "cap_") {
		return auth.SHA256Hex("token:" + token), func(ctx context.Context) bool {
			u, policy, err := h.Store.ReadAPIToken(ctx, token)
			return err == nil && u != nil && u.Role == domain.RoleAdmin && u.Status == "active" && h.adminAllowed(ctx, ip) &&
				store.TokenAllowsRequest(policy, "GET", "/api/admin/nodes/1", "GET /api/admin/nodes/{id}") &&
				store.TokenAllowsRequest(policy, "POST", "/api/admin/node-diagnostics/1/leases", "POST /api/admin/node-diagnostics/{id}/leases")
		}
	}
	c, err := r.Cookie(cookieName)
	if err != nil {
		return "", nil
	}
	session := c.Value
	return auth.SHA256Hex("session:" + session), func(ctx context.Context) bool {
		u, admin, err := h.Sessions.Resolve(ctx, session)
		return err == nil && admin && u != nil && u.Role == domain.RoleAdmin && u.Status == "active" && h.adminAllowed(ctx, ip)
	}
}
func (h *handlers) nodeDiagnostics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if h.Diagnostics == nil {
		fail(w, 409, "diagnostics_unavailable")
		return
	}
	if token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer")); strings.HasPrefix(token, "cap_") {
		_, policy, err := h.Store.ReadAPIToken(r.Context(), token)
		if err != nil || !store.TokenAllowsRequest(policy, "GET", "/api/admin/nodes/"+r.PathValue("id"), "GET /api/admin/nodes/{id}") {
			fail(w, 403, "token does not permit reading this node")
			return
		}
	}
	id := idOf(r)
	identity, err := h.Store.NodeDiagnosticsIdentity(r.Context(), id)
	if err != nil {
		serverErr(w, err)
		return
	}
	owner, valid := h.diagnosticOwner(r)
	if owner == "" {
		fail(w, 403, "administrator session required")
		return
	}
	lease := r.PathValue("lease")
	switch r.Method {
	case http.MethodGet:
		lease = r.URL.Query().Get("lease_id")
		if !service.DiagnosticsIDPattern.MatchString(lease) {
			fail(w, 400, "lease_id is required")
			return
		}
		after, err := strconv.ParseUint(r.URL.Query().Get("after"), 10, 64)
		if (err != nil && r.URL.Query().Get("after") != "") || after > 9007199254740991 {
			fail(w, 400, "invalid sample cursor")
			return
		}
		out, err := h.Diagnostics.Snapshot(r.Context(), id, identity, owner, lease, after, time.Now())
		if err != nil {
			diagnosticsError(w, err)
			return
		}
		ok(w, out)
	case http.MethodPost, http.MethodPut:
		if r.Method == http.MethodPost {
			var input struct {
				RequestID string `json:"request_id"`
			}
			r.Body = http.MaxBytesReader(w, r.Body, 1024)
			if !readJSON(w, r, &input) {
				return
			}
			lease = input.RequestID
		}
		if !service.DiagnosticsIDPattern.MatchString(lease) {
			fail(w, 400, "request_id must be a random 16 to 64 character identifier")
			return
		}
		out, err := h.Diagnostics.Lease(r.Context(), id, identity, owner, userFrom(r).ID, lease, r.Method == http.MethodPut, valid, time.Now())
		if err != nil {
			diagnosticsError(w, err)
			return
		}
		ok(w, out)
	case http.MethodDelete:
		if err := h.Diagnostics.Release(id, identity, owner, lease, time.Now()); err != nil {
			diagnosticsError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
