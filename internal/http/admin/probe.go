package admin

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"

	"github.com/zeptop-dev/captain/internal/store"
)

func (h *handlers) getProbe(w http.ResponseWriter, r *http.Request) {
	var v store.ProbeSettings
	_ = h.Store.GetSetting(r.Context(), store.SettingProbe, &v)
	v.Normalize()
	if v.Hosts == nil {
		v.Hosts = []string{}
	}
	ok(w, v)
}

func (h *handlers) putProbe(w http.ResponseWriter, r *http.Request) {
	var v store.ProbeSettings
	if !readJSON(w, r, &v) {
		return
	}
	v.Path = strings.TrimSpace(v.Path)
	if v.Path != "" {
		v.Path = "/" + strings.Trim(v.Path, "/")
		for _, reserved := range []string{"/admin", "/portal", "/sub", "/api", "/assets"} {
			if v.Path == reserved || strings.HasPrefix(v.Path, reserved+"/") {
				fail(w, http.StatusBadRequest, "that path is reserved")
				return
			}
		}
	}
	hosts := []string{}
	for _, hh := range v.Hosts {
		if hh = strings.ToLower(strings.TrimSpace(hh)); hh != "" {
			hosts = append(hosts, hh)
		}
	}
	v.Hosts = hosts
	carriers := []spec.Carrier{}
	seen := map[string]bool{}
	for i, c := range v.Carriers {
		c.Name, c.Addr = strings.TrimSpace(c.Name), strings.TrimSpace(c.Addr)
		if c.Name == "" && c.Addr == "" {
			continue
		}
		if host, port, err := net.SplitHostPort(c.Addr); err != nil || host == "" || port == "" {
			fail(w, http.StatusBadRequest, fmt.Sprintf("carrier %d: address must be host:port", i+1))
			return
		}
		if c.Name == "" || seen[c.Name] {
			fail(w, http.StatusBadRequest, fmt.Sprintf("carrier %d: a unique name is required", i+1))
			return
		}
		seen[c.Name] = true
		carriers = append(carriers, c)
	}
	v.Carriers = carriers
	v.Normalize()
	if err := h.Store.SetSetting(r.Context(), store.SettingProbe, v); err != nil {
		serverErr(w, err)
		return
	}
	if h.Probe != nil {
		h.Probe.Invalidate()
	}
	if h.SubLinks != nil {
		h.SubLinks.Invalidate()
	}
	ok(w, v)
}

func (h *handlers) listPingTasks(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.ListPingTasks(r.Context())
	if err != nil {
		serverErr(w, err)
		return
	}
	ok(w, list)
}

func (h *handlers) savePingTask(w http.ResponseWriter, r *http.Request) {
	var t store.PingTask
	if !decode(r, &t) || strings.TrimSpace(t.Name) == "" || strings.TrimSpace(t.Target) == "" {
		fail(w, http.StatusBadRequest, "name and target are required")
		return
	}
	switch t.Type {
	case "icmp", "tcp", "http", "download":
	default:
		fail(w, http.StatusBadRequest, "type must be icmp, tcp, http or download")
		return
	}
	if t.Type == "download" && t.IntervalSeconds < 600 {
		t.IntervalSeconds = 600
	}
	if t.IntervalSeconds < 5 {
		t.IntervalSeconds = 30
	}
	t.ID = idOf(r)
	if err := h.Store.SavePingTask(r.Context(), &t); err != nil {
		serverErr(w, err)
		return
	}
	ok(w, t)
}

func (h *handlers) deletePingTask(w http.ResponseWriter, r *http.Request) {
	if err := h.Store.DeletePingTask(r.Context(), idOf(r)); err != nil {
		serverErr(w, err)
		return
	}
	ok(w, map[string]bool{"ok": true})
}

func (h *handlers) getNodeProbe(w http.ResponseWriter, r *http.Request) {
	np, err := h.Store.NodeProbe(r.Context(), idOf(r))
	if err != nil {
		fail(w, http.StatusNotFound, "node not found")
		return
	}
	out := map[string]any{"probe": np, "billed": np.Billed()}
	if h.Probe != nil {
		if l, ok := h.Probe.Live(np.NodeID); ok {
			out["live"] = l
		}
	}
	ok(w, out)
}

func (h *handlers) putNodeProbe(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Hidden     bool
		Info       store.NodeProbeInfo
		LimitBytes int64
		ResetDay   int
		Mode       string
	}
	if !readJSON(w, r, &in) {
		return
	}
	if err := h.Store.UpdateNodeProbe(r.Context(), idOf(r), in.Hidden, in.Info, in.LimitBytes, in.ResetDay, in.Mode); err != nil {
		serverErr(w, err)
		return
	}
	if h.Probe != nil {
		h.Probe.Invalidate()
	}
	h.getNodeProbe(w, r)
}

func (h *handlers) resetNodeTraffic(w http.ResponseWriter, r *http.Request) {
	if err := h.Store.ResetNodeTraffic(r.Context(), idOf(r), time.Now()); err != nil {
		serverErr(w, err)
		return
	}
	_ = h.Store.ClearAlert(r.Context(), idOf(r), "traffic80")
	_ = h.Store.ClearAlert(r.Context(), idOf(r), "traffic100")
	h.getNodeProbe(w, r)
}
