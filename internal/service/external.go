package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/zeptop-dev/bosun/pkg/subscription"
	"github.com/zeptop-dev/captain/internal/store"
)

// External syncs airport subscriptions into external nodes.
type External struct {
	Store *store.Store
	HTTP  *http.Client
}

// Sync fetches one source and replaces its nodes; the error is also
// recorded on the source for the admin page.
func (e *External) Sync(ctx context.Context, src *store.ExternalSource, at time.Time) (int, error) {
	nodes, err := e.fetch(ctx, src)
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	if rerr := e.Store.ReplaceSourceNodes(ctx, src, nodes, msg, at); rerr != nil {
		return 0, rerr
	}
	return len(nodes), err
}

func (e *External) fetch(ctx context.Context, src *store.ExternalSource) ([]store.ExternalNode, error) {
	hc := e.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", src.UserAgent)
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("subscription answered %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	lines, _ := subscription.ParseList(string(body))
	if len(lines) == 0 {
		return nil, errors.New("no share links found (is the subscription in base64/URI form? set the User-Agent to v2rayN)")
	}
	out := make([]store.ExternalNode, 0, len(lines))
	for i, l := range lines {
		out = append(out, store.ExternalNode{Name: l.Name, URI: subscription.ShareURI(l), Sort: i, Enabled: true})
	}
	return out, nil
}

// ProbeAll TCP-connects to every enabled external node's address from the
// panel and records the result (jobs, every 10 minutes; admin on demand).
// Returns how many were reachable and how many were probed.
func (e *External) ProbeAll(ctx context.Context, at time.Time) (int, int) {
	nodes, err := e.Store.ListExternalNodes(ctx)
	if err != nil {
		return 0, 0
	}
	up, total := 0, 0
	for _, n := range nodes {
		if !n.Enabled {
			continue
		}
		total++
		ms, perr := probeURI(ctx, n.URI)
		msg := ""
		if perr != nil {
			msg = perr.Error()
		} else {
			up++
		}
		_ = e.Store.SetExternalProbe(ctx, n.ID, ms, msg, at)
	}
	return up, total
}

// probeURI dials the share link's host:port three times; the best
// latency wins. A refused connection still proves the host is up.
func probeURI(ctx context.Context, uri string) (float64, error) {
	l, err := subscription.ParseURI(uri)
	if err != nil {
		return -1, errors.New("unparseable link")
	}
	if l.Host == "" || l.Port <= 0 {
		return -1, errors.New("no host:port")
	}
	addr := net.JoinHostPort(l.Host, strconv.Itoa(l.Port))
	best := -1.0
	var last error
	for i := 0; i < 3; i++ {
		dctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		start := time.Now()
		c, derr := (&net.Dialer{}).DialContext(dctx, "tcp", addr)
		ms := float64(time.Since(start).Microseconds()) / 1000
		cancel()
		if derr == nil {
			c.Close()
		} else if ne, ok := derr.(net.Error); !ok || ne.Timeout() {
			last = derr
			continue
		}
		if best < 0 || ms < best {
			best = ms
		}
	}
	if best < 0 {
		if last == nil {
			last = errors.New("unreachable")
		}
		return -1, last
	}
	return best, nil
}

// SyncAll refreshes every enabled source (jobs).
func (e *External) SyncAll(ctx context.Context, at time.Time) {
	srcs, err := e.Store.ListExternalSources(ctx)
	if err != nil {
		return
	}
	for i := range srcs {
		if srcs[i].Enabled {
			_, _ = e.Sync(ctx, &srcs[i], at)
		}
	}
}
