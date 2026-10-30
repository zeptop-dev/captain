package admin

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/store"
)

type lineRolloutInput struct {
	Line           store.ForwardLine `json:"line"`
	Remove         bool              `json:"remove"`
	TimeoutSeconds int               `json:"timeout_seconds"`
}

func lineRolloutError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		fail(w, http.StatusNotFound, "release plan not found")
	case errors.Is(err, store.ErrLineRolloutConflict), errors.Is(err, store.ErrLineRolloutState), errors.Is(err, store.ErrLineConflict), errors.Is(err, store.ErrLinePreview), errors.Is(err, store.ErrPathInUse), errors.Is(err, store.ErrPathRouteOwned):
		fail(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrLineRolloutUnsupported), errors.Is(err, store.ErrLineRolloutUnsafe):
		fail(w, http.StatusBadRequest, err.Error())
	default:
		serverErr(w, err)
	}
}

func decodeLineRollout(w http.ResponseWriter, r *http.Request) (*lineRolloutInput, bool) {
	var in lineRolloutInput
	if !decode(r, &in) {
		fail(w, 400, "invalid release plan")
		return nil, false
	}
	if in.TimeoutSeconds == 0 {
		in.TimeoutSeconds = 300
	}
	if in.TimeoutSeconds < 30 || in.TimeoutSeconds > 3600 {
		fail(w, 400, "timeout must be between 30 and 3600 seconds")
		return nil, false
	}
	return &in, true
}

func (h *handlers) previewLineRollout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	in, valid := decodeLineRollout(w, r)
	if !valid {
		return
	}
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	value, err := h.Store.PreviewLineRollout(r.Context(), &in.Line, in.Remove, time.Duration(in.TimeoutSeconds)*time.Second)
	if err != nil {
		lineRolloutError(w, err)
		return
	}
	ok(w, value)
}

func (h *handlers) planLineRollout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	in, valid := decodeLineRollout(w, r)
	if !valid {
		return
	}
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	value, err := h.Store.PlanLineRollout(r.Context(), &in.Line, in.Remove, time.Duration(in.TimeoutSeconds)*time.Second)
	if err != nil {
		lineRolloutError(w, err)
		return
	}
	ok(w, value)
}

func (h *handlers) listLineRollouts(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	q := r.URL.Query()
	parse := func(key string, fallback int64) (int64, error) {
		if q.Get(key) == "" {
			return fallback, nil
		}
		return strconv.ParseInt(q.Get(key), 10, 64)
	}
	resourceID := "line_id"
	if pathRolloutRequest(r) {
		resourceID = "route_id"
	}
	lineID, e1 := parse(resourceID, 0)
	before, e2 := parse("before", 0)
	limit, e3 := parse("limit", 20)
	if e1 != nil || e2 != nil || e3 != nil || lineID < 0 || before < 0 || limit < 1 || limit > 100 {
		fail(w, 400, "invalid release history query")
		return
	}
	var value *store.LineRolloutPage
	var err error
	if pathRolloutRequest(r) {
		value, err = h.Store.ListPathRollouts(r.Context(), lineID, before, int(limit))
	} else {
		value, err = h.Store.ListLineRollouts(r.Context(), lineID, before, int(limit))
	}
	if err != nil {
		lineRolloutError(w, err)
		return
	}
	ok(w, value)
}

func (h *handlers) getLineRollout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := r.PathValue("rollout")
	if len(id) == 0 || len(id) > 128 {
		fail(w, 400, "invalid release id")
		return
	}
	value, err := h.Store.LineRolloutByID(r.Context(), id)
	if err != nil {
		lineRolloutError(w, err)
		return
	}
	if !rolloutResourceAllowed(r, value) {
		fail(w, 404, "release plan not found")
		return
	}
	ok(w, value)
}

func (h *handlers) actLineRollout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var in struct {
		Version int64 `json:"version"`
	}
	if !decode(r, &in) || in.Version < 1 {
		fail(w, 400, "version is required")
		return
	}
	id := r.PathValue("rollout")
	if len(id) == 0 || len(id) > 128 {
		fail(w, 400, "invalid release id")
		return
	}
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	current, check := h.Store.LineRolloutByID(r.Context(), id)
	if check != nil {
		lineRolloutError(w, check)
		return
	}
	if !rolloutResourceAllowed(r, current) {
		fail(w, 404, "release plan not found")
		return
	}
	var value *store.LineRollout
	var err error
	switch r.PathValue("action") {
	case "pause":
		value, err = h.Store.PauseLineRollout(r.Context(), id, in.Version)
	case "retry":
		value, err = h.Store.RetryLineRollout(r.Context(), id, in.Version)
	case "compensate":
		value, err = h.Store.CompensateLineRollout(r.Context(), id, in.Version)
	default:
		fail(w, 404, "unknown release action")
		return
	}
	if err != nil {
		lineRolloutError(w, err)
		return
	}
	ok(w, value)
}

func pathRolloutRequest(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, "/api/admin/path-rollouts")
}
func rolloutResourceAllowed(r *http.Request, p *store.LineRollout) bool {
	return pathRolloutRequest(r) == (p.SnapshotVersion == "path-relay-v1")
}
