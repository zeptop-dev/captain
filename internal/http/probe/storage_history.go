package probe

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/zeptop-dev/captain/internal/store"
)

func storageHistoryWindow(r *http.Request, now time.Time) (time.Time, time.Time, error) {
	q := r.URL.Query()
	if q.Has("from") || q.Has("to") {
		from, err := strconv.ParseInt(q.Get("from"), 10, 64)
		to, errTo := strconv.ParseInt(q.Get("to"), 10, 64)
		if err != nil || errTo != nil || from <= 0 || to <= from {
			return time.Time{}, time.Time{}, store.ErrStorageHistoryRange
		}
		start, end := time.Unix(from, 0), time.Unix(to, 0)
		_, _, err = store.StorageHistoryRange(start, end, now)
		return start, end, err
	}
	window := time.Hour
	switch q.Get("range") {
	case "", "1h":
	case "4h":
		window = 4 * time.Hour
	case "24h":
		window = 24 * time.Hour
	case "7d":
		window = 7 * 24 * time.Hour
	case "30d":
		window = 30 * 24 * time.Hour
	default:
		return time.Time{}, time.Time{}, store.ErrStorageHistoryRange
	}
	return now.Add(-window), now, nil
}

func (h *handlers) storageHistory(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	from, to, err := storageHistoryWindow(r, now)
	if err != nil {
		http.Error(w, "invalid history range", http.StatusBadRequest)
		return
	}
	// An operator can change visibility while buckets are being decoded. Retry
	// against the new configuration once; never send an obsolete projection.
	for attempt := 0; attempt < 2; attempt++ {
		generation := h.Probe.Gen()
		s := h.Probe.Settings(r.Context())
		if allowed, code := h.allowed(r, s); !allowed {
			http.Error(w, http.StatusText(code), code)
			return
		}
		if !s.PublicShows("history") || !s.PublicShows("disk") {
			http.NotFound(w, r)
			return
		}
		id, ok := h.nodeID(r)
		if !ok {
			http.NotFound(w, r)
			return
		}
		result, err := h.Store.StorageHistory(r.Context(), id, from, to, now, h.isStaff(r))
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if generation != h.Probe.Gen() {
			continue
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(result)
		return
	}
	http.Error(w, "configuration changed; retry", http.StatusServiceUnavailable)
}
