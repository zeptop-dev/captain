package agent

import (
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/zeptop-dev/bosun/pkg/bootstrap"
	"github.com/zeptop-dev/captain/internal/agentdownload"
	"github.com/zeptop-dev/captain/internal/auth"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/http/ratelimit"
	"github.com/zeptop-dev/captain/internal/store"
)

// downloadIdentity takes a short account lock. Neither upstream downloads nor
// slow clients hold the global reset/renumber lock. Every response revalidates
// credentials after any upstream wait, including a cache hit.
func (h *handlers) downloadIdentity(r *http.Request) (*domain.Node, error) {
	h.Store.Accounts.RLock()
	defer h.Store.Accounts.RUnlock()
	if v := r.Header.Get("Authorization"); v != "" {
		if !strings.HasPrefix(v, "Bearer ") || len(v) <= 7 {
			return nil, store.ErrNotFound
		}
		return h.Store.NodeByTokenHash(r.Context(), auth.SHA256Hex(strings.TrimPrefix(v, "Bearer ")))
	}
	code := strings.ToUpper(strings.TrimSpace(r.Header.Get("X-Bosun-Pair-Code")))
	if code == "" {
		return nil, store.ErrNotFound
	}
	return h.Store.NodeByPairCode(r.Context(), code)
}

func (h *handlers) downloads(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	ip := ratelimit.ClientIP(r)
	if h.Pairs != nil {
		if allowed, _ := h.Pairs.Allow(ip); !allowed {
			fail(w, 429, "too many failed authentication attempts")
			return
		}
	}
	n, e := h.downloadIdentity(r)
	if e != nil {
		if errors.Is(e, store.ErrNotFound) {
			if h.Pairs != nil {
				h.Pairs.Fail(ip)
			}
			fail(w, 401, "invalid download credentials")
		} else {
			fail(w, 500, "internal error")
		}
		return
	}
	if h.Downloads == nil {
		fail(w, 503, "agent downloads unavailable")
		return
	}
	var payload any
	var data []byte
	var file *agentdownload.File
	parts := strings.Split(r.PathValue("rest"), "/")
	switch {
	case len(parts) == 2 && parts[0] == "bootstrap":
		var found bool
		data, found = bootstrap.File(parts[1])
		if !found {
			e = agentdownload.ErrNotFound
		}
	case len(parts) == 5 && strings.Join(parts[:4], "/") == "repos/zeptop-dev/bosun/releases" && parts[4] == "latest":
		payload, e = h.Downloads.Release(r.Context(), "latest")
	case len(parts) == 6 && strings.Join(parts[:5], "/") == "repos/zeptop-dev/bosun/releases/tags":
		payload, e = h.Downloads.Release(r.Context(), parts[5])
	case len(parts) == 4 && strings.Join(parts, "/") == "repos/zeptop-dev/bosun/releases":
		payload, e = h.Downloads.Releases(r.Context())
	case len(parts) == 3 && parts[0] == "bosun":
		file, e = h.Downloads.Bosun(r.Context(), parts[1], parts[2])
	case len(parts) == 6 && parts[0] == "cores" && (parts[5] == "manifest" || parts[5] == "asset"):
		file, e = h.Downloads.Core(r.Context(), parts[1], parts[2], parts[3], parts[4])
		if e == nil && parts[5] == "manifest" {
			payload = map[string]any{"sha256": file.SHA256, "size": file.Size, "source_verified": file.SourceVerified, "url": agentdownload.Prefix + "/" + strings.Join(parts[:5], "/") + "/asset"}
			file = nil
		}
	default:
		e = agentdownload.ErrNotFound
	}
	// A reset, repair, deletion or pairing while an upstream request was in
	// flight cannot leave a stale install ticket or token authorized.
	fresh, authErr := h.downloadIdentity(r)
	if authErr != nil || fresh.ID != n.ID {
		fail(w, 401, "download credentials are no longer valid")
		return
	}
	if e != nil {
		if errors.Is(e, agentdownload.ErrNotFound) {
			fail(w, 404, "artifact is not supported; Captain downloads require bosun v0.88.0 or newer")
		} else {
			h.Log.Warn("agent download failed", "node", n.ID, "err", e)
			fail(w, 502, "Captain could not obtain the requested artifact")
		}
		return
	}
	if payload != nil {
		ok(w, payload)
		return
	}
	if data != nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write(data)
		return
	}
	if file != nil {
		f, e := os.Open(file.Path)
		if e != nil {
			fail(w, 503, "artifact cache changed; retry download")
			return
		}
		defer f.Close()
		st, e := f.Stat()
		if e != nil {
			fail(w, 503, "artifact cache unavailable")
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("ETag", `"sha256-`+file.SHA256+`"`)
		w.Header().Set("X-Content-SHA256", file.SHA256)
		http.ServeContent(w, r, parts[len(parts)-1], st.ModTime(), f)
	}
}
