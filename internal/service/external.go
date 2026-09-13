package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/zeptop-dev/captain/internal/store"
	"github.com/zeptop-dev/captain/internal/subscription"
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
