package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"

	"github.com/zeptop-dev/bosun/pkg/spec"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/service"
	"github.com/zeptop-dev/captain/internal/store"
)

func (h *handlers) listEntries(w http.ResponseWriter, r *http.Request) {
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	list, err := h.Store.ListEntries(r.Context())
	if err != nil {
		serverErr(w, err)
		return
	}
	connections, err := h.Store.EntryConnections(r.Context())
	if err != nil {
		serverErr(w, err)
		return
	}
	type entryView struct {
		*domain.Entry
		ManagedConnection *store.EntryConnection   `json:"ManagedConnection,omitempty"`
		LocationSettings  *domain.LocationSettings `json:"LocationSettings"`
		Location          *domain.GeoLocation      `json:"Location"`
	}
	settings, err := h.Store.EntryLocationSettings(r.Context())
	if err != nil {
		serverErr(w, err)
		return
	}
	locations, err := service.EntryMapLocations(r.Context(), h.Store, list)
	if err != nil {
		serverErr(w, err)
		return
	}
	out := make([]entryView, 0, len(list))
	for _, e := range list {
		cfg := settings[e.ID]
		if cfg == nil {
			cfg = &domain.LocationSettings{Mode: "auto"}
		}
		out = append(out, entryView{e, connections[e.ID], cfg, locations[e.ID]})
	}
	ok(w, out)
}

func (h *handlers) reorderEntries(w http.ResponseWriter, r *http.Request) {
	var in struct{ IDs []int64 }
	if !decode(r, &in) || len(in.IDs) == 0 {
		fail(w, http.StatusBadRequest, "ids required")
		return
	}
	if err := h.Store.ReorderEntries(r.Context(), in.IDs); err != nil {
		serverErr(w, err)
		return
	}
	ok(w, map[string]bool{"ok": true})
}

func (h *handlers) entryTags(w http.ResponseWriter, r *http.Request) {
	tags, err := h.Store.EntryTags(r.Context())
	if err != nil {
		serverErr(w, err)
		return
	}
	ok(w, tags)
}

// fillEntryDefaults resolves a blank display address: the inbound's TLS
// domain (it has to point at the node anyway), else the node's public
// address; a blank port is the inbound's port.
func (h *handlers) fillEntryDefaults(ctx context.Context, e *domain.Entry) error {
	if e.LineID != nil {
		return h.Store.ResolveLineEntry(ctx, e)
	}
	if e.DisplayHost != "" && e.DisplayPort != 0 {
		return nil
	}
	ib, err := h.Store.InboundByID(ctx, e.InboundID)
	if err != nil {
		return err
	}
	if ib.IngressID != nil {
		// A line ingress: clients dial the provider's public entry (or a
		// relay in front of the line) on the mapped port.
		g, err := h.Store.IngressByID(ctx, *ib.IngressID)
		if err == nil {
			if e.DisplayPort == 0 {
				e.DisplayPort = g.EntryPort(ib.Port)
			}
			if e.DisplayHost == "" {
				if g.ClientHost() == "" {
					return errors.New("this ingress has no public entry: add a port forward on a relay node and use the relay's address here")
				}
				e.DisplayHost = g.ClientHost()
			}
			return nil
		}
	}
	if e.DisplayPort == 0 {
		e.DisplayPort = ib.Port
	}
	if e.DisplayHost == "" {
		if sp := ib.Spec(); sp.TLS != nil && sp.TLS.Mode == spec.TLSStandard && sp.TLS.ServerName != "" {
			e.DisplayHost = sp.TLS.ServerName
		} else if n, err := h.Store.NodeByID(ctx, ib.NodeID); err == nil {
			if n.Domain != "" {
				e.DisplayHost = n.Domain
			} else {
				e.DisplayHost = n.PublicAddr
			}
		}
	}
	if e.DisplayHost == "" {
		return errors.New("display_host is required: the inbound has no TLS domain and the node no public address or domain; on a line-only node pick a line ingress for the inbound")
	}
	return nil
}

func (h *handlers) createEntry(w http.ResponseWriter, r *http.Request) {
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	var e domain.Entry
	if !decode(r, &e) || e.Name == "" || (e.InboundID == 0 && e.LineID == nil) {
		fail(w, http.StatusBadRequest, "name and inbound_id are required")
		return
	}
	if !h.entryAccessAllowed(w, r, e.Access) {
		return
	}
	if e.LineID != nil && !userFrom(r).IsAdmin() {
		fail(w, 403, "admin only: line binding")
		return
	}
	if ib, err := h.Store.InboundByID(r.Context(), e.InboundID); err == nil && ib.Settings.Reverse != nil && ib.Settings.Reverse.Receiver {
		fail(w, 400, "tunnel receiver is not a subscription entry")
		return
	}
	if err := h.fillEntryDefaults(r.Context(), &e); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	e.Enabled = true
	if err := h.Store.CreateEntry(r.Context(), &e); err != nil {
		if nodeAccessError(w, err) || entryClientChainError(w, err) || entryCategoryError(w, err) {
			return
		}
		serverErr(w, err)
		return
	}
	ok(w, e)
}

func (h *handlers) updateEntry(w http.ResponseWriter, r *http.Request) {
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	id, okID := pathID(r)
	if h.Store.ReverseOwnedEntry(r.Context(), id) {
		fail(w, 409, "edit this entry through its reverse connection")
		return
	}
	cur, err := h.Store.EntryByID(r.Context(), id)
	if err != nil {
		fail(w, 404, "entry not found")
		return
	}
	var input struct {
		domain.Entry
		ClientExtra json.RawMessage
	}
	raw, _ := json.Marshal(cur)
	_ = json.Unmarshal(raw, &input.Entry)
	if !okID || !decode(r, &input) || input.Name == "" || (input.InboundID == 0 && input.LineID == nil) {
		fail(w, http.StatusBadRequest, "name and inbound_id are required")
		return
	}
	e := input.Entry
	if !h.entryAccessAllowed(w, r, e.Access) {
		return
	}
	// Omission and null from older editors preserve the display category.
	// CategoryID: 0 is the explicit request to make an entry unclassified.
	if e.CategoryID == nil {
		e.CategoryID = cur.CategoryID
	}
	if len(input.ClientExtra) > 0 {
		e.ClientExtra = nil
		if err := json.Unmarshal(input.ClientExtra, &e.ClientExtra); err != nil {
			fail(w, 400, "invalid client extra fields")
			return
		}
	}
	if !userFrom(r).IsAdmin() && (cur.LineID != nil || e.LineID != nil) && (!reflect.DeepEqual(cur.LineID, e.LineID) || cur.LineMemberID != e.LineMemberID || cur.InboundID != e.InboundID || cur.DisplayHost != e.DisplayHost || cur.DisplayPort != e.DisplayPort || !reflect.DeepEqual(cur.ClientExtra, e.ClientExtra)) {
		fail(w, 403, "admin only: line binding")
		return
	}
	if e.LineID == nil {
		e.LineMemberID = ""
	}
	if err := h.fillEntryDefaults(r.Context(), &e); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	e.ID = id
	if ib, err := h.Store.InboundByID(r.Context(), e.InboundID); err == nil && ib.Settings.Reverse != nil && ib.Settings.Reverse.Receiver {
		fail(w, 400, "tunnel receiver is not a subscription entry")
		return
	}
	if err := h.Store.UpdateEntry(r.Context(), &e); err != nil {
		if nodeAccessError(w, err) || entryClientChainError(w, err) || entryCategoryError(w, err) {
			return
		}
		serverErr(w, err)
		return
	}
	ok(w, e)
}

func (h *handlers) deleteEntry(w http.ResponseWriter, r *http.Request) {
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	id, okID := pathID(r)
	if h.Store.ReverseOwnedEntry(r.Context(), id) {
		fail(w, 409, "remove this entry through its reverse connection")
		return
	}
	if !okID {
		fail(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := h.Store.DeleteEntry(r.Context(), id); err != nil {
		if entryClientChainError(w, err) {
			return
		}
		serverErr(w, err)
		return
	}
	ok(w, map[string]bool{"ok": true})
}

func entryClientChainError(w http.ResponseWriter, err error) bool {
	if errors.Is(err, store.ErrEntryClientChain) {
		fail(w, 400, err.Error())
		return true
	}
	if errors.Is(err, store.ErrEntryClientChainInUse) {
		fail(w, 409, err.Error())
		return true
	}
	return false
}
