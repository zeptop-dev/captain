package admin

import (
	"context"
	"errors"
	"net/http"

	"github.com/zeptop-dev/bosun/pkg/spec"

	"github.com/zeptop-dev/captain/internal/domain"
)

func (h *handlers) listEntries(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.ListEntries(r.Context())
	if err != nil {
		serverErr(w, err)
		return
	}
	if list == nil {
		list = []*domain.Entry{}
	}
	ok(w, list)
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
	var e domain.Entry
	if !decode(r, &e) || e.Name == "" || e.InboundID == 0 {
		fail(w, http.StatusBadRequest, "name and inbound_id are required")
		return
	}
	if err := h.fillEntryDefaults(r.Context(), &e); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	e.Enabled = true
	if err := h.Store.CreateEntry(r.Context(), &e); err != nil {
		serverErr(w, err)
		return
	}
	ok(w, e)
}

func (h *handlers) updateEntry(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	var e domain.Entry
	if !okID || !decode(r, &e) || e.Name == "" || e.InboundID == 0 {
		fail(w, http.StatusBadRequest, "name and inbound_id are required")
		return
	}
	if err := h.fillEntryDefaults(r.Context(), &e); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	e.ID = id
	if err := h.Store.UpdateEntry(r.Context(), &e); err != nil {
		serverErr(w, err)
		return
	}
	ok(w, e)
}

func (h *handlers) deleteEntry(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	if !okID {
		fail(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := h.Store.DeleteEntry(r.Context(), id); err != nil {
		serverErr(w, err)
		return
	}
	ok(w, map[string]bool{"ok": true})
}
