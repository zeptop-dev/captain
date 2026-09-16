package admin

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/service"
)

// userEntries lists the servers one user gets, each with that user's share
// link and the blacklist flag.
func (h *handlers) userEntries(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	u, err := h.Store.UserByID(r.Context(), id)
	if !okID || err != nil {
		fail(w, http.StatusNotFound, "user not found")
		return
	}
	svc := &service.Subscription{Store: h.Store}
	list, err := svc.EntryLinks(r.Context(), u)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, list)
}

// putUserEntries replaces the user's blacklist: {"blocked": [entry ids]}.
func (h *handlers) putUserEntries(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(r)
	if _, err := h.Store.UserByID(r.Context(), id); !okID || err != nil {
		fail(w, http.StatusNotFound, "user not found")
		return
	}
	var in struct {
		Blocked []int64 `json:"blocked"`
	}
	if !decode(r, &in) {
		return
	}
	if err := h.Store.SetUserEntryBlocks(r.Context(), id, in.Blocked); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.userEntries(w, r)
}

func (h *handlers) listSubLinks(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.ListSubLinks(r.Context(), idOf(r))
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	base := ""
	if h.SubLinks != nil {
		base = strings.TrimSuffix(h.SubLinks.URL(r.Context(), "x"), "/sub/x")
		base = strings.TrimSuffix(base, "/s/x")
		if i := strings.Index(base, "/s/"); i > 0 {
			base = base[:i]
		}
	}
	out := make([]map[string]any, 0, len(list))
	for _, l := range list {
		out = append(out, map[string]any{"id": l.ID, "code": l.Code, "kind": l.Kind, "max_uses": l.MaxUses, "uses": l.Uses, "expires_at": l.ExpiresAt, "enabled": l.Enabled, "created_at": l.CreatedAt, "url": base + "/s/" + l.Code})
	}
	ok(w, out)
}

func (h *handlers) createTempLink(w http.ResponseWriter, r *http.Request) {
	var in struct {
		MaxUses int
		Hours   int
	}
	if !decode(r, &in) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	if in.MaxUses <= 0 && in.Hours <= 0 {
		fail(w, http.StatusBadRequest, "set a use limit and/or an expiry")
		return
	}
	if _, err := h.Store.UserByID(r.Context(), idOf(r)); err != nil {
		fail(w, http.StatusNotFound, "user not found")
		return
	}
	l, err := h.Store.CreateTempLink(r.Context(), idOf(r), in.MaxUses, time.Duration(in.Hours)*time.Hour)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, l)
}

func (h *handlers) deleteSubLink(w http.ResponseWriter, r *http.Request) {
	lid, _ := strconv.ParseInt(r.PathValue("lid"), 10, 64)
	if err := h.Store.DeleteSubLink(r.Context(), idOf(r), lid); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]bool{"ok": true})
}
