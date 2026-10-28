package admin

import (
	"context"
	"net/http"

	"github.com/zeptop-dev/captain/internal/service"
)

// nodeDNSSync authenticates briefly, reads provider state without locks, and
// reauthorizes under the mutation lock before returning a plan or applying it.
func (h *handlers) nodeDNSSync(w http.ResponseWriter, r *http.Request) {
	// ServeMux routes HEAD through GET. It must follow the read path and own
	// its account locks too, never fall through to the mutation branch.
	if r.Method == http.MethodHead {
		r = r.Clone(r.Context())
		r.Method = http.MethodGet
	}
	var in struct {
		Revision string `json:"revision"`
	}
	var plan *service.DNSNodeSyncPlan
	var prepareErr error
	authorized := false
	h.Store.Accounts.RLock()
	h.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && !readJSON(w, r, &in) {
			return
		}
		if r.Method == http.MethodPost && len(in.Revision) != 64 {
			fail(w, 400, "a reviewed DNS revision is required")
			return
		}
		if h.DNS == nil {
			fail(w, 503, "DNS synchronization is unavailable")
			return
		}
		authorized = true
		plan, prepareErr = h.DNS.PrepareNodeSync(r.Context(), idOf(r))
	})(w, r.WithContext(context.WithValue(r.Context(), subPresetAuthOnlyKey{}, true)))
	h.Store.Accounts.RUnlock()
	if !authorized {
		return
	}
	finish := func(next http.HandlerFunc) {
		// Ordinary staff mutations use Accounts.RLock. The exclusive lock prevents
		// revocation/reset/domain-token edits after this final authorization.
		h.Store.Accounts.Lock()
		defer h.Store.Accounts.Unlock()
		h.requireAdmin(next)(w, r)
	}
	if prepareErr != nil {
		finish(func(w http.ResponseWriter, _ *http.Request) { dnsHealthError(w, prepareErr) })
		return
	}
	if !h.dnsSync.TryLock() {
		finish(func(w http.ResponseWriter, _ *http.Request) { fail(w, 429, "DNS synchronization is already running") })
		return
	}
	defer h.dnsSync.Unlock()
	h.DNS.ReadNodeSyncPlan(r.Context(), plan)
	finish(func(w http.ResponseWriter, r *http.Request) {
		if err := h.DNS.ValidateNodeSync(r.Context(), plan); err != nil {
			dnsHealthError(w, err)
			return
		}
		if r.Method == http.MethodGet {
			ok(w, plan)
			return
		}
		ctx := service.WithDNSOrigin(r.Context(), service.DNSOrigin{NodeID: plan.NodeID, Source: "manual_sync", Actor: userFrom(r).Email})
		results, err := h.DNS.ApplyNodeSync(ctx, plan, in.Revision)
		if err != nil {
			dnsHealthError(w, err)
			return
		}
		ok(w, map[string]any{"results": results})
	})
}
