package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

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

// checkAgentHTTPS probes only the saved origin, outside the account lock. Both
// authorization and the saved revision are checked again before exposing results.
func (h *handlers) checkAgentHTTPS(w http.ResponseWriter, r *http.Request) {
	r = r.WithContext(context.WithValue(r.Context(), subPresetAuthOnlyKey{}, true))
	authorized := false
	h.Store.Accounts.RLock()
	h.requireAdmin(func(http.ResponseWriter, *http.Request) { authorized = true })(w, r)
	h.Store.Accounts.RUnlock()
	if !authorized {
		return
	}
	// Slow request bodies must not pin Accounts and block reset/renumbering.
	// The outer response wrappers expose Unwrap for the real network deadline.
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Now().Add(5 * time.Second))
	body, readErr := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	_ = controller.SetReadDeadline(time.Time{})
	var in struct {
		Revision string `json:"revision"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if readErr != nil || decoder.Decode(&in) != nil || decoder.Decode(new(any)) != io.EOF {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	var saved store.AgentHTTPSSettings
	localReady := false
	authorized = false
	h.Store.Accounts.RLock()
	h.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		var err error
		saved, err = h.Store.AgentHTTPSSettings(r.Context())
		if err != nil {
			serverErr(w, err)
			return
		}
		if in.Revision == "" || in.Revision != saved.Revision {
			fail(w, http.StatusConflict, store.ErrAgentHTTPSRevision.Error())
			return
		}
		localReady = h.AgentHTTPS != nil && h.AgentHTTPS.Ready(saved)
		authorized = true
	})(w, r)
	h.Store.Accounts.RUnlock()
	if !authorized {
		return
	}
	finish := func(next http.HandlerFunc) {
		h.Store.Accounts.Lock()
		defer h.Store.Accounts.Unlock()
		h.requireAdmin(next)(w, r)
	}
	if !h.agentCheck.TryLock() {
		finish(func(w http.ResponseWriter, _ *http.Request) {
			fail(w, http.StatusTooManyRequests, "Agent HTTPS check is already running")
		})
		return
	}
	defer h.agentCheck.Unlock()
	result := agenthttps.AccessCheck{URL: saved.URL, Perspective: "panel", Status: "disabled", CheckedAt: time.Now().UTC()}
	if saved.Enabled {
		result.Status = "not_ready"
		if localReady && h.AgentCheck != nil {
			result = h.AgentCheck.Check(r.Context(), saved.URL)
		}
	}
	result.Revision = saved.Revision
	finish(func(w http.ResponseWriter, r *http.Request) {
		current, err := h.Store.AgentHTTPSSettings(r.Context())
		if err != nil {
			serverErr(w, err)
			return
		}
		if current != saved {
			fail(w, http.StatusConflict, store.ErrAgentHTTPSRevision.Error())
			return
		}
		if saved.Enabled && (h.AgentHTTPS == nil || !h.AgentHTTPS.Ready(current)) {
			result.Status = "not_ready"
		}
		ok(w, result)
	})
}
