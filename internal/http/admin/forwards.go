package admin

import (
	"fmt"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/zeptop-dev/captain/internal/store"
)

// Port forwards (tunnels) on a node: bosun accepts on listen:port and relays
// raw TCP/UDP to target, so a node in front of a landing server becomes a
// relay entry. The panel validates; the node picks the change up on its
// next state pull.

func (h *handlers) getNodeForwards(w http.ResponseWriter, r *http.Request) {
	id := idOf(r)
	list, err := h.Store.NodeForwards(r.Context(), id)
	if err != nil {
		fail(w, http.StatusNotFound, "node not found")
		return
	}
	status, _ := h.Store.ForwardStatuses(r.Context(), id)
	ok(w, map[string]any{"forwards": list, "status": status})
}

func (h *handlers) putNodeForwards(w http.ResponseWriter, r *http.Request) {
	id := idOf(r)
	var in struct{ Forwards []store.NodeForward }
	if !decode(r, &in) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	inbounds, err := h.Store.AllInboundsByNode(r.Context(), id)
	if err != nil {
		fail(w, http.StatusNotFound, "node not found")
		return
	}
	used := map[string]string{} // "proto/port" -> what owns it
	for _, ib := range inbounds {
		used["tcp/"+strconv.Itoa(ib.Port)] = "inbound " + ib.Tag
		used["udp/"+strconv.Itoa(ib.Port)] = "inbound " + ib.Tag
	}
	tags := map[string]bool{}
	clean := make([]store.NodeForward, 0, len(in.Forwards))
	for i, f := range in.Forwards {
		f.Tag = strings.TrimSpace(f.Tag)
		if f.Tag == "" {
			f.Tag = fmt.Sprintf("fwd-%d", f.Port)
		}
		if !spec.ValidTag(f.Tag) {
			fail(w, http.StatusBadRequest, "tag may only contain letters, digits, . _ : - (max 64)")
			return
		}
		if !spec.ValidListen(strings.TrimSpace(f.Listen)) {
			fail(w, http.StatusBadRequest, "listen must be an IP address")
			return
		}
		if tags[f.Tag] {
			fail(w, http.StatusBadRequest, "duplicate tag "+f.Tag)
			return
		}
		tags[f.Tag] = true
		if f.Port < 1 || f.Port > 65535 {
			fail(w, http.StatusBadRequest, fmt.Sprintf("rule %d: port out of range", i+1))
			return
		}
		switch f.Protocol {
		case "", "both":
			f.Protocol = "both"
		case "tcp", "udp":
		default:
			fail(w, http.StatusBadRequest, "protocol must be tcp, udp or both")
			return
		}
		switch f.Backend {
		case "", "nft", "realm":
		default:
			fail(w, http.StatusBadRequest, "backend must be empty (built-in relay), nft or realm")
			return
		}
		if f.PreserveSource && f.Backend != "nft" {
			fail(w, http.StatusBadRequest, "preserve_source needs the nft backend")
			return
		}
		host, port, err := net.SplitHostPort(strings.TrimSpace(f.Target))
		if err != nil || host == "" {
			fail(w, http.StatusBadRequest, fmt.Sprintf("rule %d: target must be host:port", i+1))
			return
		}
		if p, err := strconv.Atoi(port); err != nil || p < 1 || p > 65535 {
			fail(w, http.StatusBadRequest, fmt.Sprintf("rule %d: bad target port", i+1))
			return
		}
		f.Target = net.JoinHostPort(host, port)
		if f.Listen != "" && net.ParseIP(f.Listen) == nil {
			fail(w, http.StatusBadRequest, fmt.Sprintf("rule %d: listen must be an IP or empty", i+1))
			return
		}
		for _, proto := range protosOf(f.Protocol) {
			key := proto + "/" + strconv.Itoa(f.Port)
			if owner, taken := used[key]; taken {
				fail(w, http.StatusBadRequest, fmt.Sprintf("%s port %d is already used by %s", proto, f.Port, owner))
				return
			}
			used[key] = "forward " + f.Tag
		}
		clean = append(clean, f)
	}
	if err := h.Store.SetNodeForwards(r.Context(), id, clean); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]any{"forwards": clean})
}

func protosOf(p string) []string {
	if p == "both" {
		return []string{"tcp", "udp"}
	}
	return []string{p}
}
