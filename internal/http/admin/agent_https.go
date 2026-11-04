package admin

import (
	"errors"
	"net"
	"net/http"
	"net/url"

	"github.com/zeptop-dev/bosun/pkg/selfupdate"
	"github.com/zeptop-dev/captain/internal/agenthttps"
	"github.com/zeptop-dev/captain/internal/store"
)

func (h *handlers) getAgentHTTPS(w http.ResponseWriter, r *http.Request) {
	v, err := h.Store.AgentHTTPSSettings(r.Context())
	if err != nil {
		serverErr(w, err)
		return
	}
	status := agenthttps.Status{State: "disabled"}
	ready := false
	if h.AgentHTTPS != nil {
		status, ready = h.AgentHTTPS.Status(), h.AgentHTTPS.Ready(v)
	}
	// A suggested hostname is a form default, not an active deployment.
	if v.URL == "" {
		if base, err := url.Parse(h.BaseURL); err == nil && base.Hostname() != "" {
			v.URL = "https://" + net.JoinHostPort(base.Hostname(), "2096")
		}
	}
	ok(w, map[string]any{"settings": v, "status": status, "ready": ready,
		"base_url": h.BaseURL, "in_container": selfupdate.InContainer()})
}

func (h *handlers) putAgentHTTPS(w http.ResponseWriter, r *http.Request) {
	var in store.AgentHTTPSSettings
	if !readJSON(w, r, &in) {
		return
	}
	if h.AgentHTTPS != nil {
		if err := h.AgentHTTPS.ValidateConfig(in.Config); err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if _, err := h.Store.SaveAgentHTTPS(r.Context(), in); err != nil {
		switch {
		case errors.Is(err, store.ErrAgentHTTPSRevision):
			fail(w, http.StatusConflict, err.Error())
		case errors.Is(err, store.ErrAgentHTTPSInvalid):
			fail(w, http.StatusBadRequest, err.Error())
		default:
			serverErr(w, err)
		}
		return
	}
	if h.AgentHTTPS != nil {
		h.AgentHTTPS.Refresh(r.Context())
	}
	h.getAgentHTTPS(w, r)
}

// nodeAgentHTTPS gives node operators only the installation origin, without
// granting access to certificate inventory, listener addresses or diagnostics.
func (h *handlers) nodeAgentHTTPS(w http.ResponseWriter, r *http.Request) {
	v, err := h.Store.AgentHTTPSSettings(r.Context())
	if err != nil {
		serverErr(w, err)
		return
	}
	ready := h.AgentHTTPS != nil && h.AgentHTTPS.Ready(v)
	u := ""
	if v.Enabled {
		u = v.URL
	}
	ok(w, map[string]any{"enabled": v.Enabled, "url": u, "ready": ready})
}
