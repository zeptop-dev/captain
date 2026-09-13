// Package probe serves the public status page data: a snapshot of every
// visible node, per-node history and latency, gated by the operator's
// visibility choice. It is also the router for the page itself, which can
// live under a path of the main site or on dedicated hostnames.
package probe

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/service"
	"github.com/zeptop-dev/captain/internal/store"
)

// Deps are the handler dependencies.
type Deps struct {
	Store    *store.Store
	Probe    *service.Probe
	SiteName string
	// Resolve returns the signed-in user for a request, nil when none.
	Resolve func(r *http.Request) *domain.User
	// Page serves the built status SPA (index + assets) relative to "/".
	Page http.Handler
}

type handlers struct {
	Deps
	mu        sync.Mutex
	cached    []byte
	cachedT   time.Time
	cachedGen uint64
}

// Register mounts the JSON API under /api/probe.
func Register(mux *http.ServeMux, d Deps) *Router {
	h := &handlers{Deps: d}
	mux.HandleFunc("GET /api/probe", h.gate(h.snapshot))
	mux.HandleFunc("GET /api/probe/nodes/{id}/history", h.gate(h.history))
	mux.HandleFunc("GET /api/probe/nodes/{id}/pings", h.gate(h.pings))
	return &Router{h: h}
}

// allowed applies the visibility setting; false with a status to send.
func (h *handlers) allowed(r *http.Request, s store.ProbeSettings) (bool, int) {
	if !s.Enabled {
		return false, http.StatusNotFound
	}
	switch s.Visibility {
	case "users":
		if h.Resolve == nil || h.Resolve(r) == nil {
			return false, http.StatusUnauthorized
		}
	case "admins":
		if h.Resolve == nil {
			return false, http.StatusForbidden
		}
		if u := h.Resolve(r); u == nil || !u.IsStaff() {
			return false, http.StatusForbidden
		}
	}
	return true, 0
}

func (h *handlers) gate(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s := h.Probe.Settings(r.Context())
		if ok, code := h.allowed(r, s); !ok {
			http.Error(w, http.StatusText(code), code)
			return
		}
		next(w, r)
	}
}

func (h *handlers) isStaff(r *http.Request) bool {
	if h.Resolve == nil {
		return false
	}
	u := h.Resolve(r)
	return u != nil && u.IsStaff()
}

// nodeView is one server on the page.
type nodeView struct {
	ID       int64               `json:"id"`
	Name     string              `json:"name"`
	Online   bool                `json:"online"`
	Addr     string              `json:"addr,omitempty"`
	Info     store.NodeProbeInfo `json:"info"`
	Host     any                 `json:"host"`
	Version  string              `json:"version,omitempty"`
	LastSeen *time.Time          `json:"last_seen"`
	Traffic  struct {
		Used, Limit, PrevUsed int64  `json:"-"`
		UsedB                 int64  `json:"used"`
		LimitB                int64  `json:"limit"`
		PrevB                 int64  `json:"prev"`
		Mode                  string `json:"mode"`
		PeriodStart           int64  `json:"period_start"`
		ResetDay              int    `json:"reset_day"`
	} `json:"traffic"`
	Recent []service.Sample `json:"recent"`
}

// snapshot is the page's main document, cached for a few seconds because
// it is polled by every visitor.
func (h *handlers) snapshot(w http.ResponseWriter, r *http.Request) {
	staff := h.isStaff(r)
	gen := h.Probe.Gen()
	h.mu.Lock()
	if !staff && time.Since(h.cachedT) < 3*time.Second && h.cached != nil && h.cachedGen == gen {
		b := h.cached
		h.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(b)
		return
	}
	h.mu.Unlock()
	s := h.Probe.Settings(r.Context())
	nodes, err := h.Store.ListNodes(r.Context())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	probes, _ := h.Store.ListNodeProbes(r.Context())
	now := time.Now()
	grace := time.Duration(s.Alerts.OfflineSeconds) * time.Second
	out := []nodeView{}
	for _, n := range nodes {
		np := probes[n.ID]
		if !n.Paired || (np != nil && np.Hidden && !staff) {
			continue
		}
		v := nodeView{ID: n.ID, Name: n.Name, Version: n.Version}
		if s.ShowIP || staff {
			v.Addr = n.PublicAddr
		}
		last := n.LastSeenAt
		if l, ok := h.Probe.Live(n.ID); ok {
			v.Host = l.Host
			v.Recent = l.Ring
			if len(v.Recent) > 120 {
				v.Recent = v.Recent[len(v.Recent)-120:]
			}
			if last == nil || l.At.After(*last) {
				t := l.At
				last = &t
			}
		}
		if v.Recent == nil {
			v.Recent = []service.Sample{}
		}
		v.LastSeen = last
		v.Online = last != nil && now.Sub(*last) <= grace
		if np != nil {
			v.Info = np.Info
			v.Traffic.UsedB, v.Traffic.LimitB, v.Traffic.PrevB, v.Traffic.Mode, v.Traffic.ResetDay = np.Billed(), np.LimitBytes, np.PrevUsed, np.Mode, np.ResetDay
			if !np.PeriodStart.IsZero() {
				v.Traffic.PeriodStart = np.PeriodStart.Unix()
			}
		}
		out = append(out, v)
	}
	doc := map[string]any{
		"title": firstNonEmpty(s.Title, h.SiteName), "logo": s.Logo, "show_globe": s.ShowGlobe, "beat_seconds": s.BeatSeconds,
		"visibility": s.Visibility, "carrier_ping": s.CarrierPing, "now": now.Unix(), "nodes": out, "staff": staff,
	}
	b, _ := json.Marshal(doc)
	if !staff {
		h.mu.Lock()
		h.cached, h.cachedT, h.cachedGen = b, now, gen
		h.mu.Unlock()
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(b)
}

// rangeOf maps ?range= onto a resolution and window.
func rangeOf(q string) (res string, window time.Duration) {
	switch q {
	case "24h":
		return "m", 24 * time.Hour
	case "7d":
		return "h", 7 * 24 * time.Hour
	case "30d":
		return "h", 30 * 24 * time.Hour
	case "1y":
		return "d", 365 * 24 * time.Hour
	}
	return "", time.Hour // in-memory ring
}

func (h *handlers) nodeID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	np, err := h.Store.NodeProbe(r.Context(), id)
	if err != nil || (np.Hidden && !h.isStaff(r)) {
		return 0, false
	}
	return id, true
}

func (h *handlers) history(w http.ResponseWriter, r *http.Request) {
	id, ok := h.nodeID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	res, window := rangeOf(r.URL.Query().Get("range"))
	now := time.Now()
	w.Header().Set("Content-Type", "application/json")
	if res == "" {
		_ = json.NewEncoder(w).Encode(map[string]any{"res": "raw", "points": h.Probe.Recent(id, now.Add(-window))})
		return
	}
	pts, err := h.Store.NodeStats(r.Context(), id, res, now.Add(-window), now)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"res": res, "points": pts})
}

func (h *handlers) pings(w http.ResponseWriter, r *http.Request) {
	id, ok := h.nodeID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	res, window := rangeOf(r.URL.Query().Get("range"))
	if res == "" {
		res = "m"
	}
	now := time.Now()
	pts, err := h.Store.NodePingStats(r.Context(), id, res, now.Add(-window), now)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"res": res, "points": pts})
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// Router mounts the status page in front of the main mux: on dedicated
// hosts only the page and its API answer; on the main site the page is
// served under the configured path.
type Router struct{ h *handlers }

// Wrap returns the outer handler.
func (rt *Router) Wrap(next http.Handler) http.Handler {
	h := rt.h
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := h.Probe.Settings(r.Context())
		host := strings.ToLower(r.Host)
		if i := strings.LastIndexByte(host, ':'); i >= 0 && !strings.Contains(host[i:], "]") {
			host = host[:i]
		}
		onHost := false
		for _, hh := range s.Hosts {
			if strings.EqualFold(strings.TrimSpace(hh), host) {
				onHost = true
				break
			}
		}
		path := r.URL.Path
		if onHost {
			if strings.HasPrefix(path, "/api/probe") || path == "/api/health" || strings.HasPrefix(path, "/api/portal/login") || strings.HasPrefix(path, "/api/portal/me") {
				next.ServeHTTP(w, r)
				return
			}
			if strings.HasPrefix(path, "/api/") || path == "/admin" || strings.HasPrefix(path, "/admin/") || path == "/portal" || strings.HasPrefix(path, "/portal/") || strings.HasPrefix(path, "/sub/") {
				http.NotFound(w, r)
				return
			}
			if ok, code := h.allowed(r, s); !ok {
				http.Error(w, http.StatusText(code), code)
				return
			}
			h.Page.ServeHTTP(w, r)
			return
		}
		if s.Enabled && s.Path != "" && s.Path != "/" {
			p := "/" + strings.Trim(s.Path, "/")
			if path == p {
				http.Redirect(w, r, p+"/"+optQuery(r), http.StatusMovedPermanently)
				return
			}
			if strings.HasPrefix(path, p+"/") {
				if ok, code := h.allowed(r, s); !ok {
					http.Error(w, http.StatusText(code), code)
					return
				}
				r2 := r.Clone(r.Context())
				r2.URL.Path = strings.TrimPrefix(path, p)
				h.Page.ServeHTTP(w, r2)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func optQuery(r *http.Request) string {
	if r.URL.RawQuery != "" {
		return "?" + r.URL.RawQuery
	}
	return ""
}
