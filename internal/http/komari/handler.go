// Package komari implements the read-only public probe contract of Komari 1.6.1.
// It deliberately has no agent, account, terminal, plugin or administration API.
package komari

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/probedata"
	"golang.org/x/net/websocket"
)

const (
	maxMessage     = 64 << 10
	maxBatch       = 32
	maxConnections = 32
	requestTimeout = 15 * time.Second
)

// Identity is a read-only probe identity. It must never contain an admin token,
// the management session ID, or a statement that the theme can administer nodes.
type Identity struct {
	Username     string `json:"username"`
	LoggedIn     bool   `json:"logged_in"`
	UUID         string `json:"uuid,omitempty"`
	TwoFAEnabled bool   `json:"2fa_enabled"`
}

type Deps struct {
	Reader probedata.Reader
	// Gate is mandatory, and must resolve current visibility and session state.
	// The same request is checked again during a long-lived WebSocket connection.
	Gate     func(*http.Request) bool
	Identity func(*http.Request) Identity
	// PublicSettings supplies public theme metadata/settings only. No injected
	// scripts or management configuration should be supplied by this callback.
	PublicSettings func(*http.Request) (map[string]any, error)
	Version, Hash  string
	Now            func() time.Time
}

type Handler struct {
	d       Deps
	sockets chan struct{}
}

func New(d Deps) *Handler { return &Handler{d: d, sockets: make(chan struct{}, maxConnections)} }

func Handles(path string) bool {
	switch path {
	case "/api/nodes", "/api/public", "/api/me", "/api/version", "/api/records/load", "/api/records/ping", "/api/task/ping", "/api/clients", "/api/rpc2":
		return true
	}
	return strings.HasPrefix(path, "/api/recent/") && len(strings.TrimPrefix(path, "/api/recent/")) > 0 && !strings.Contains(strings.TrimPrefix(path, "/api/recent/"), "/")
}

func (h *Handler) allowed(r *http.Request) bool {
	return h.d.Reader != nil && h.d.Gate != nil && h.d.Gate(r)
}
func (h *Handler) now() time.Time {
	if h.d.Now != nil {
		return h.d.Now().UTC()
	}
	return time.Now().UTC()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func restError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"status": "error", "message": msg})
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !Handles(r.URL.Path) {
		http.NotFound(w, r)
		return
	}
	if !h.allowed(r) {
		restError(w, http.StatusForbidden, "Probe access denied")
		return
	}
	if r.URL.Path == "/api/rpc2" {
		if r.Method == http.MethodGet {
			h.serveSocket(w, r, false)
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "GET, POST")
			restError(w, 405, "Method not allowed")
			return
		}
		if !sameOrigin(r) {
			restError(w, 403, "Origin not allowed")
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxMessage))
		if err != nil {
			restError(w, 413, "Request too large")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
		defer cancel()
		result := h.rpcMessage(r.WithContext(ctx), body)
		if !h.allowed(r) {
			restError(w, 403, "Probe access denied")
			return
		}
		if result == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, 200, result)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		restError(w, 405, "Method not allowed")
		return
	}
	if r.URL.Path == "/api/clients" {
		h.serveSocket(w, r, true)
		return
	}
	method := map[string]string{
		"/api/nodes": "public:getNodesInformation", "/api/public": "public:getPublicSettings",
		"/api/me": "public:getMe", "/api/version": "public:getVersion", "/api/records/load": "public:getRecordsByUUID",
		"/api/records/ping": "public:getPingRecords", "/api/task/ping": "public:getPublicPingTasks",
	}[r.URL.Path]
	params := map[string]any{}
	for _, key := range []string{"uuid", "load_type", "hours", "task_id", "max_count"} {
		if v := r.URL.Query().Get(key); v != "" {
			params[key] = v
		}
	}
	if strings.HasPrefix(r.URL.Path, "/api/recent/") {
		method = "public:getClientRecentRecords"
		params["uuid"] = strings.TrimPrefix(r.URL.Path, "/api/recent/")
	}
	raw, _ := json.Marshal(params)
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	result, e := h.call(r.WithContext(ctx), method, raw)
	if !h.allowed(r) {
		restError(w, 403, "Probe access denied")
		return
	}
	if e != nil {
		status := 400
		if e.Code == -32603 {
			status = 500
		}
		if e.Code == -32003 {
			status = 403
		}
		restError(w, status, e.Message)
		return
	}
	if r.URL.Path == "/api/me" {
		writeJSON(w, 200, result)
		return
	}
	writeJSON(w, 200, map[string]any{"status": "success", "message": "", "data": result})
}

// Compare the actual Host, never a user-supplied X-Forwarded-Host. A reverse
// proxy may terminate TLS, so the origin scheme can be either HTTP or HTTPS.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	} // non-browser clients
	u, err := url.Parse(origin)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.User == nil && u.RawQuery == "" && u.Fragment == "" && (u.Path == "" || u.Path == "/") && strings.EqualFold(u.Host, r.Host)
}

func (h *Handler) serveSocket(w http.ResponseWriter, r *http.Request, legacy bool) {
	if !sameOrigin(r) {
		restError(w, 403, "Origin not allowed")
		return
	}
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		restError(w, 400, "Require WebSocket upgrade")
		return
	}
	select {
	case h.sockets <- struct{}{}:
		defer func() { <-h.sockets }()
	default:
		restError(w, 429, "Too many probe connections")
		return
	}
	server := websocket.Server{
		Handshake: func(_ *websocket.Config, req *http.Request) error {
			if !sameOrigin(req) || !h.allowed(req) {
				return errDenied
			}
			return nil
		},
		Handler: func(ws *websocket.Conn) {
			defer ws.Close()
			ws.MaxPayloadBytes = maxMessage
			done := make(chan struct{})
			defer close(done)
			// Idle sockets must also lose access when the site/session is revoked.
			go func() {
				ticker := time.NewTicker(5 * time.Second)
				defer ticker.Stop()
				for {
					select {
					case <-done:
						return
					case <-r.Context().Done():
						_ = ws.Close()
						return
					case <-ticker.C:
						if !h.allowed(r) {
							_ = ws.Close()
							return
						}
					}
				}
			}()
			for {
				_ = ws.SetReadDeadline(time.Now().Add(2 * time.Minute))
				var body []byte
				if err := websocket.Message.Receive(ws, &body); err != nil {
					return
				}
				if !h.allowed(r) {
					return
				}
				ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
				var out any
				if legacy {
					out = h.legacyMessage(r.WithContext(ctx), string(body))
				} else {
					out = h.rpcMessage(r.WithContext(ctx), body)
				}
				cancel()
				if !h.allowed(r) {
					return
				}
				if out == nil {
					continue
				}
				_ = ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
				if err := websocket.JSON.Send(ws, out); err != nil {
					return
				}
			}
		},
	}
	server.ServeHTTP(w, r)
}
