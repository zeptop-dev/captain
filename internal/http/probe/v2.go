package probe

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/zeptop-dev/captain/internal/probedata"
)

// registerV2 exposes the same public read model consumed by Komari adapters.
// Legacy /api/probe remains stable for installed clients and built-in themes.
func (h *handlers) registerV2(mux *http.ServeMux) {
	if h.Data == nil {
		return
	}
	mux.HandleFunc("GET /api/probe/v2", h.gate(h.v2Snapshot))
	mux.HandleFunc("GET /api/probe/v2/nodes", h.gate(h.v2Nodes))
	mux.HandleFunc("GET /api/probe/v2/status", h.gate(h.v2Status))
	mux.HandleFunc("GET /api/probe/v2/metrics", h.gate(h.v2Definitions))
	mux.HandleFunc("POST /api/probe/v2/query", h.gate(h.v2Query))
	mux.HandleFunc("POST /api/probe/v2/ping-stats", h.gate(h.v2PingStats))
}

func v2Write(w http.ResponseWriter, value any, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err != nil {
		code, message := http.StatusInternalServerError, "Probe data unavailable"
		switch {
		case errors.Is(err, probedata.ErrPolicyChanged):
			code, message = http.StatusServiceUnavailable, "Probe visibility changed; retry the request"
		case errors.Is(err, probedata.ErrNotFound):
			code, message = http.StatusNotFound, "Probe data not found"
		case errors.Is(err, probedata.ErrInvalidQuery):
			code, message = http.StatusBadRequest, "Invalid probe query"
		case errors.Is(err, probedata.ErrUnsupportedAggregation):
			code, message = http.StatusUnprocessableEntity, "Aggregation unavailable for stored observations"
		}
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
		return
	}
	_ = json.NewEncoder(w).Encode(value)
}

func (h *handlers) v2Snapshot(w http.ResponseWriter, r *http.Request) {
	v, e := h.Data.Snapshot(r.Context(), probedata.Scope{})
	v2Write(w, v, e)
}

func (h *handlers) v2Nodes(w http.ResponseWriter, r *http.Request) {
	s, err := h.Data.Snapshot(r.Context(), probedata.Scope{})
	type inventory struct {
		UUID      string              `json:"uuid"`
		Name      string              `json:"name"`
		Group     string              `json:"group"`
		Inventory probedata.Inventory `json:"inventory"`
		Info      *probedata.Info     `json:"info,omitempty"`
	}
	nodes := make([]inventory, 0, len(s.Nodes))
	for _, n := range s.Nodes {
		nodes = append(nodes, inventory{n.UUID, n.Name, n.Group, n.Inventory, n.Info})
	}
	v2Write(w, map[string]any{"schema": s.Schema, "now": s.Now, "nodes": nodes}, err)
}

func (h *handlers) v2Status(w http.ResponseWriter, r *http.Request) {
	s, err := h.Data.Snapshot(r.Context(), probedata.Scope{})
	type status struct {
		UUID    string              `json:"uuid"`
		Status  probedata.Status    `json:"status"`
		Traffic *probedata.Traffic  `json:"traffic,omitempty"`
		Storage []probedata.Storage `json:"storage"`
		Pings   []probedata.Ping    `json:"pings"`
	}
	nodes := make([]status, 0, len(s.Nodes))
	for _, n := range s.Nodes {
		nodes = append(nodes, status{n.UUID, n.Status, n.Traffic, n.Storage, n.Pings})
	}
	v2Write(w, map[string]any{"schema": s.Schema, "now": s.Now, "nodes": nodes}, err)
}

func (h *handlers) v2Definitions(w http.ResponseWriter, r *http.Request) {
	v, e := h.Data.Definitions(r.Context(), probedata.Scope{})
	v2Write(w, map[string]any{"schema": "captain.probe.v2", "metrics": v}, e)
}

type metricQuery struct {
	MetricKeys          []string          `json:"metric_keys"`
	EntityIDs           []string          `json:"entity_ids"`
	Start               time.Time         `json:"start"`
	End                 time.Time         `json:"end"`
	Tags                map[string]string `json:"tags"`
	FillEmpty           bool              `json:"fill_empty"`
	MaxPoints           int               `json:"max_points"`
	MaxPointsByMetric   map[string]int    `json:"max_points_by_metric"`
	Aggregation         string            `json:"aggregation"`
	AggregationByMetric map[string]string `json:"aggregation_by_metric"`
}

func readV2(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		v2Write(w, nil, probedata.ErrInvalidQuery)
		return false
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		v2Write(w, nil, probedata.ErrInvalidQuery)
		return false
	}
	return true
}

func (h *handlers) v2Query(w http.ResponseWriter, r *http.Request) {
	var q metricQuery
	if !readV2(w, r, &q) {
		return
	}
	v, e := h.Data.Query(r.Context(), probedata.Scope{}, probedata.Query{MetricKeys: q.MetricKeys, EntityIDs: q.EntityIDs, Start: q.Start, End: q.End, Tags: q.Tags, FillEmpty: q.FillEmpty, MaxPoints: q.MaxPoints, MaxPointsByMetric: q.MaxPointsByMetric, Aggregation: q.Aggregation, AggregationByMetric: q.AggregationByMetric})
	v2Write(w, map[string]any{"schema": "captain.probe.v2", "series": v}, e)
}

func (h *handlers) v2PingStats(w http.ResponseWriter, r *http.Request) {
	var q struct {
		EntityIDs []string  `json:"entity_ids"`
		TaskIDs   []string  `json:"task_ids"`
		Start     time.Time `json:"start"`
		End       time.Time `json:"end"`
	}
	if !readV2(w, r, &q) {
		return
	}
	v, e := h.Data.PingStats(r.Context(), probedata.Scope{}, probedata.PingQuery{EntityIDs: q.EntityIDs, TaskIDs: q.TaskIDs, Start: q.Start, End: q.End})
	v2Write(w, map[string]any{"schema": "captain.probe.v2", "stats": v}, e)
}
