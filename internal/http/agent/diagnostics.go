package agent

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/captain/internal/auth"
	"github.com/zeptop-dev/captain/internal/service"
)

func (h *handlers) diagnostics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if h.Diagnostics == nil {
		fail(w, 409, "diagnostics_unavailable")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, agentproto.DiagnosticsMaxSampleBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var sample agentproto.DiagnosticsSample
	if err := decoder.Decode(&sample); err != nil {
		fail(w, 400, "invalid diagnostics sample")
		return
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		fail(w, 400, "invalid diagnostics sample")
		return
	}
	identity := auth.SHA256Hex(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	err := h.Diagnostics.Record(r.Context(), nodeFrom(r).ID, identity, sample, time.Now())
	if err != nil {
		if errors.Is(err, service.ErrDiagnosticsLimit) {
			fail(w, 429, "diagnostics_budget_exceeded")
		} else {
			fail(w, 409, "diagnostics_lease_inactive")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
