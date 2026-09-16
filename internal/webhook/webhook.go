// Package webhook delivers signed event notifications to operator-defined
// URLs. It is Captain's integration point for external automation (n8n,
// scripts, a CRM): every endpoint gets a JSON body, an event name header
// and an HMAC-SHA256 signature over the raw body.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/zeptop-dev/captain/internal/store"
)

// Endpoint is one subscriber.
type Endpoint struct {
	URL     string   `json:"url"`
	Secret  string   `json:"secret"`
	Events  []string `json:"events"` // empty = all
	Enabled bool     `json:"enabled"`
}

// Settings is the admin-edited list.
type Settings struct {
	Endpoints []Endpoint `json:"endpoints"`
}

// SettingKey is the settings table key.
const SettingKey = "webhooks"

// Event names.
const (
	UserRegistered       = "user.registered"
	OrderPaid            = "order.paid"
	TicketCreated        = "ticket.created"
	TicketReplied        = "ticket.replied" // by the user
	WithdrawalRequested  = "withdrawal.requested"
	SubscriptionExpiring = "subscription.expiring"
	SubscriptionTraffic  = "subscription.traffic" // a traffic threshold was crossed
	UserFirstConnected   = "user.first_connected" // first traffic ever seen for the user
	UserNotConnected     = "user.not_connected"   // holds a plan for a day but never connected
	NodeAlert            = "node.alert"
	Test                 = "test"
)

// Events lists every event name for the settings UI.
var Events = []string{UserRegistered, OrderPaid, TicketCreated, TicketReplied, WithdrawalRequested, SubscriptionExpiring, SubscriptionTraffic, UserFirstConnected, UserNotConnected, NodeAlert}

// Hub loads endpoints from the store and delivers events asynchronously.
type Hub struct {
	Store *store.Store
	Log   *slog.Logger
	HTTP  *http.Client
	// Sync delivers inline instead of in a goroutine (tests).
	Sync bool

	mu      sync.Mutex
	cached  Settings
	fetched time.Time
}

func (h *Hub) settings(ctx context.Context) Settings {
	h.mu.Lock()
	defer h.mu.Unlock()
	if time.Since(h.fetched) < 15*time.Second {
		return h.cached
	}
	var s Settings
	_ = h.Store.GetSetting(ctx, SettingKey, &s)
	h.cached, h.fetched = s, time.Now()
	return s
}

// Invalidate drops the settings cache.
func (h *Hub) Invalidate() {
	h.mu.Lock()
	h.fetched = time.Time{}
	h.mu.Unlock()
}

// Emit sends event with payload to every matching endpoint. It never blocks
// the caller (unless Sync) and never returns an error: delivery problems
// are logged.
func (h *Hub) Emit(ctx context.Context, event string, payload map[string]any) {
	if h == nil || h.Store == nil {
		return
	}
	s := h.settings(ctx)
	if len(s.Endpoints) == 0 {
		return
	}
	body, _ := json.Marshal(map[string]any{"event": event, "at": time.Now().UTC().Format(time.RFC3339), "data": payload})
	for _, ep := range s.Endpoints {
		if !ep.Enabled || ep.URL == "" || !wants(ep, event) {
			continue
		}
		if h.Sync {
			h.deliver(ep, event, body)
		} else {
			go h.deliver(ep, event, body)
		}
	}
}

// Deliver sends one event to one endpoint synchronously (the admin "test").
func (h *Hub) Deliver(ep Endpoint, event string, payload map[string]any) error {
	body, _ := json.Marshal(map[string]any{"event": event, "at": time.Now().UTC().Format(time.RFC3339), "data": payload})
	return h.post(ep, event, body)
}

func wants(ep Endpoint, event string) bool {
	if len(ep.Events) == 0 {
		return true
	}
	for _, e := range ep.Events {
		if e == event {
			return true
		}
	}
	return false
}

// deliver retries a few times with growing delays; endpoints are expected
// to be idempotent on the (event, at, data) triple.
func (h *Hub) deliver(ep Endpoint, event string, body []byte) {
	var err error
	for attempt, delay := 0, 2*time.Second; attempt < 3; attempt, delay = attempt+1, delay*3 {
		if err = h.post(ep, event, body); err == nil {
			return
		}
		if h.Sync {
			break
		}
		time.Sleep(delay)
	}
	if h.Log != nil {
		h.Log.Warn("webhook failed", "url", ep.URL, "event", event, "err", err)
	}
}

func (h *Hub) post(ep Endpoint, event string, body []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "captain-webhook")
	req.Header.Set("X-Captain-Event", event)
	req.Header.Set("X-Captain-Timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	if ep.Secret != "" {
		req.Header.Set("X-Captain-Signature", "sha256="+Sign(ep.Secret, body))
	}
	hc := h.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return &statusError{resp.StatusCode}
	}
	return nil
}

// Sign returns the hex HMAC-SHA256 of body under secret.
func Sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

type statusError struct{ code int }

func (e *statusError) Error() string { return "endpoint answered " + strconv.Itoa(e.code) }
