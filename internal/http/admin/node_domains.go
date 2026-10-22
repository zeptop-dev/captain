package admin

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

func nodeSaveError(w http.ResponseWriter, err error) {
	var conflict *store.NodeDomainConflict
	switch {
	case errors.As(err, &conflict):
		fail(w, http.StatusConflict, conflict.Error())
	case errors.Is(err, store.ErrInternalAddress):
		fail(w, http.StatusConflict, err.Error())
	case errors.Is(err, domain.ErrNodeDomain):
		fail(w, http.StatusBadRequest, domain.ErrNodeDomain.Error())
	case errors.Is(err, store.ErrNotFound):
		fail(w, http.StatusNotFound, "node not found")
	default:
		serverErr(w, err)
	}
}

// Advisory only. The store validates again in the save transaction.
func (h *handlers) checkNodeDomain(w http.ResponseWriter, r *http.Request) {
	var id int64
	if raw := r.URL.Query().Get("exclude_id"); raw != "" {
		var err error
		id, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			fail(w, http.StatusBadRequest, "bad node id")
			return
		}
	}
	name, err := domain.NormalizeNodeDomain(r.URL.Query().Get("domain"))
	valid := err == nil
	unchanged := false
	if id != 0 {
		old, err := h.Store.NodeByID(r.Context(), id)
		if err != nil {
			nodeSaveError(w, err)
			return
		}
		unchanged = valid && !old.DomainShared && domain.NodeDomainKey(old.Domain) == name
	}
	uses := []store.NodeDomainUse{}
	if valid {
		uses, err = h.Store.NodeDomainUses(r.Context(), name, id)
		if err != nil {
			serverErr(w, err)
			return
		}
	}
	ok(w, map[string]any{"domain": name, "valid": valid, "conflicts": uses, "unchanged": unchanged})
}
