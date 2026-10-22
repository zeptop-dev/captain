package admin

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/zeptop-dev/captain/internal/store"
)

func lineError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		fail(w, 404, "line or referenced resource not found")
		return
	}
	if errors.Is(err, store.ErrLineConflict) || errors.Is(err, store.ErrLineInUse) {
		fail(w, 409, err.Error())
		return
	}
	fail(w, 400, err.Error())
}
func (h *handlers) listLines(w http.ResponseWriter, r *http.Request) {
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	lines, err := h.Store.ListLines(r.Context())
	if err != nil {
		serverErr(w, err)
		return
	}
	ok(w, lines)
}
func (h *handlers) getLine(w http.ResponseWriter, r *http.Request) {
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	l, err := h.Store.LineByID(r.Context(), idOf(r))
	if err != nil {
		lineError(w, err)
		return
	}
	ok(w, l)
}
func (h *handlers) saveLine(w http.ResponseWriter, r *http.Request) {
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	var l store.ForwardLine
	if !decode(r, &l) {
		fail(w, 400, "invalid line")
		return
	}
	if r.Method == http.MethodPost {
		l.ID, l.Version = 0, 0
	} else {
		l.ID = idOf(r)
		if l.Version < 1 {
			fail(w, 400, "version is required")
			return
		}
	}
	if err := h.Store.SaveLine(r.Context(), &l); err != nil {
		lineError(w, err)
		return
	}
	live, err := h.Store.LineByID(r.Context(), l.ID)
	if err != nil {
		serverErr(w, err)
		return
	}
	ok(w, live)
}
func (h *handlers) deleteLine(w http.ResponseWriter, r *http.Request) {
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	version, err := strconv.ParseInt(r.URL.Query().Get("version"), 10, 64)
	if err != nil || version < 1 {
		fail(w, 400, "version is required")
		return
	}
	if err = h.Store.DeleteLine(r.Context(), idOf(r), version); err != nil {
		lineError(w, err)
		return
	}
	ok(w, map[string]bool{"ok": true})
}

func (h *handlers) requireNoNodeLines(w http.ResponseWriter, r *http.Request, id int64) bool {
	used, err := h.Store.NodeHasLines(r.Context(), id)
	if err != nil {
		serverErr(w, err)
		return false
	}
	if used {
		fail(w, 409, store.ErrLineInUse.Error())
		return false
	}
	return true
}

func (h *handlers) lineOptions(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.URL.Query().Get("inbound_id"), 10, 64)
	if err != nil || id <= 0 {
		fail(w, 400, "inbound_id is required")
		return
	}
	ib, err := h.Store.InboundByID(r.Context(), id)
	if err != nil {
		lineError(w, err)
		return
	}
	protocol, err := store.LineProtocol(ib)
	if err != nil {
		ok(w, map[string]string{"protocol": "", "error": err.Error()})
		return
	}
	ok(w, map[string]string{"protocol": protocol})
}

func (h *handlers) internalAddressUsage(w http.ResponseWriter, r *http.Request) {
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	uses, err := h.Store.InternalAddressUses(r.Context(), idOf(r))
	if err != nil {
		lineError(w, err)
		return
	}
	ok(w, map[string]any{"uses": uses})
}
