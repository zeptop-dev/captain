package admin

import (
	"github.com/zeptop-dev/captain/internal/store"
	"net/http"
)

func (h *handlers) loginClients(w http.ResponseWriter, r *http.Request) {
	clients, e := h.Store.LoginClients(r.Context())
	if e != nil {
		serverErr(w, e)
		return
	}
	ok(w, map[string]any{"clients": clients, "base_url": h.BaseURL})
}
func (h *handlers) saveLoginClient(w http.ResponseWriter, r *http.Request) {
	var c store.LoginClient
	if !readJSON(w, r, &c) {
		return
	}
	create := r.Method == http.MethodPost
	c.ID = r.PathValue("id")
	c, secret, e := h.Store.SaveLoginClient(r.Context(), c, create)
	if e != nil {
		if e == store.ErrLoginClient {
			fail(w, 400, e.Error())
		} else if e == store.ErrNotFound {
			fail(w, 404, "not found")
		} else {
			serverErr(w, e)
		}
		return
	}
	ok(w, map[string]any{"client": c, "client_secret": secret})
}
func (h *handlers) rotateLoginClient(w http.ResponseWriter, r *http.Request) {
	secret, e := h.Store.RotateLoginClient(r.Context(), r.PathValue("id"))
	if e != nil {
		if e == store.ErrNotFound {
			fail(w, 404, "not found")
		} else {
			serverErr(w, e)
		}
		return
	}
	ok(w, map[string]string{"client_secret": secret})
}
func (h *handlers) deleteLoginClient(w http.ResponseWriter, r *http.Request) {
	if e := h.Store.DeleteLoginClient(r.Context(), r.PathValue("id")); e != nil {
		serverErr(w, e)
		return
	}
	ok(w, map[string]bool{"ok": true})
}
