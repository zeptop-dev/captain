package admin

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/zeptop-dev/captain/internal/store"
)

func (h *handlers) registerNodeInfrastructure(m *http.ServeMux) {
	const base = "/api/admin/infrastructure"
	m.HandleFunc("GET "+base+"/nodes/{id}", h.requireAdmin(h.getNodeInfrastructure))
	m.HandleFunc("PUT "+base+"/nodes/{id}", h.requireAdmin(h.putNodeInfrastructure))
	m.HandleFunc("GET "+base+"/suppliers", h.requireAdmin(h.infraSupplierChoices))
	m.HandleFunc("POST "+base+"/suppliers", h.requireAdmin(h.createInfraSupplierChoice))
}
func (h *handlers) infrastructureWriteAllowed(r *http.Request, nodeID int64) bool {
	if !userFrom(r).IsAdmin() {
		return false
	}
	tok := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer"))
	if strings.HasPrefix(tok, "cap_") {
		u, scope, err := h.Store.UserByAPIToken(r.Context(), tok)
		path := "/api/admin/infrastructure/nodes/" + strconv.FormatInt(nodeID, 10)
		return err == nil && u != nil && u.IsAdmin() && u.Status == "active" && store.TokenAllowsRequest(scope, http.MethodPut, path, "PUT /api/admin/infrastructure/nodes/{id}")
	}
	return true
}
func (h *handlers) invalidateInfrastructureProbe() {
	if h.Probe != nil {
		h.Probe.Invalidate()
	}
}
func (h *handlers) getNodeInfrastructure(w http.ResponseWriter, r *http.Request) {
	v, err := h.Store.NodeInfrastructure(r.Context(), idOf(r))
	if err != nil {
		infraError(w, err)
		return
	}
	ok(w, v)
}
func (h *handlers) putNodeInfrastructure(w http.ResponseWriter, r *http.Request) {
	var in store.NodeInfrastructureChange
	if !readJSON(w, r, &in) {
		return
	}
	v, err := h.Store.ChangeNodeInfrastructure(r.Context(), idOf(r), in)
	if err != nil {
		infraError(w, err)
		return
	}
	h.invalidateInfrastructureProbe()
	ok(w, v)
}
func (h *handlers) infraSupplierChoices(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.InfraSuppliers(r.Context())
	if err != nil {
		serverErr(w, err)
		return
	}
	out := make([]store.InfraSupplierChoice, 0, len(list))
	for _, v := range list {
		out = append(out, store.InfraSupplierChoice{ID: v.ID, Name: v.Name, URL: v.URL})
	}
	ok(w, out)
}
func (h *handlers) createInfraSupplierChoice(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	v := store.InfraSupplier{Name: in.Name, URL: in.URL}
	if err := h.Store.SaveInfraSupplier(r.Context(), &v); err != nil {
		infraError(w, err)
		return
	}
	ok(w, store.InfraSupplierChoice{ID: v.ID, Name: v.Name, URL: v.URL})
}
