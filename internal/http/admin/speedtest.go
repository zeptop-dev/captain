package admin

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/zeptop-dev/captain/internal/store"
)

// Speed test workbench: the panel TCP-pings every entry's display address
// (what clients actually connect to) and shows the nodes' own latency and
// download results from the probe tasks.

type tcpingResult struct {
	Host  string  `json:"host"`
	Port  int     `json:"port"`
	MinMs float64 `json:"min_ms"` // -1 = unreachable
	AvgMs float64 `json:"avg_ms"`
	At    int64   `json:"at"`
}

var (
	tcpingMu    sync.Mutex
	tcpingCache = map[string]tcpingResult{}
)

func (h *handlers) registerSpeedtest(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/speedtest", h.requireAdmin(h.speedtest))
	mux.HandleFunc("POST /api/admin/speedtest/tcping", h.requireAdmin(h.tcping))
}

// tcping dials host:port three times from the panel and keeps the result.
func (h *handlers) tcping(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Host string
		Port int
	}
	if !decode(r, &in) || in.Host == "" || in.Port <= 0 || in.Port > 65535 {
		fail(w, http.StatusBadRequest, "host and port are required")
		return
	}
	res := tcpingResult{Host: in.Host, Port: in.Port, MinMs: -1, At: time.Now().Unix()}
	addr := net.JoinHostPort(in.Host, strconv.Itoa(in.Port))
	var okN int
	var sum float64
	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		start := time.Now()
		c, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
		ms := float64(time.Since(start).Microseconds()) / 1000
		cancel()
		if err == nil {
			c.Close()
		} else if ne, ok := err.(net.Error); !ok || ne.Timeout() {
			continue // timeouts count as unreachable; a refusal still proves the host is up
		}
		okN++
		sum += ms
		if res.MinMs < 0 || ms < res.MinMs {
			res.MinMs = ms
		}
	}
	if okN > 0 {
		res.AvgMs = sum / float64(okN)
	}
	tcpingMu.Lock()
	tcpingCache[addr] = res
	tcpingMu.Unlock()
	ok(w, res)
}

// speedtest lists entries with their last TCPing and every node's latest
// probe task results (latency and download throughput).
func (h *handlers) speedtest(w http.ResponseWriter, r *http.Request) {
	entries, err := h.Store.ListEntries(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	tcpingMu.Lock()
	cache := make(map[string]tcpingResult, len(tcpingCache))
	for k, v := range tcpingCache {
		cache[k] = v
	}
	tcpingMu.Unlock()
	ents := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		addr := net.JoinHostPort(e.DisplayHost, strconv.Itoa(e.DisplayPort))
		row := map[string]any{"id": e.ID, "name": e.Name, "host": e.DisplayHost, "port": e.DisplayPort, "inbound_id": e.InboundID, "enabled": e.Enabled}
		if res, ok := cache[addr]; ok {
			row["tcping"] = res
		}
		ents = append(ents, row)
	}
	nodes, _ := h.Store.ListNodes(r.Context())
	nv := make([]map[string]any, 0, len(nodes))
	for _, n := range nodes {
		row := map[string]any{"id": n.ID, "name": n.Name}
		if h.Probe != nil {
			if l, ok := h.Probe.Live(n.ID); ok {
				row["pings"] = l.Host.Pings
				row["at"] = l.At
			}
		}
		nv = append(nv, row)
	}
	var ps store.ProbeSettings
	_ = h.Store.GetSetting(r.Context(), store.SettingProbe, &ps)
	ok(w, map[string]any{"entries": ents, "nodes": nv, "probe_enabled": ps.Enabled})
}
