package admin

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/webhook"

	"github.com/zeptop-dev/captain/internal/store"
	"github.com/zeptop-dev/captain/internal/telegram"
)

func (h *handlers) getClients(w http.ResponseWriter, r *http.Request) {
	getSetting(h, w, r, store.SettingClients, func(v *store.ClientsSettings) {
		if v.Items == nil {
			v.Items = []store.ClientItem{}
		}
	})
}

func (h *handlers) putClients(w http.ResponseWriter, r *http.Request) {
	putSetting(h, w, r, store.SettingClients, func(_ context.Context, v *store.ClientsSettings) string {
		items := make([]store.ClientItem, 0, len(v.Items))
		for _, it := range v.Items {
			it.Name, it.URL = strings.TrimSpace(it.Name), strings.TrimSpace(it.URL)
			if it.Name == "" || it.URL == "" {
				continue
			}
			items = append(items, it)
		}
		v.Items = items
		return ""
	})
}

func (h *handlers) getTelegram(w http.ResponseWriter, r *http.Request) {
	var v store.TelegramSettings
	_ = h.Store.GetSetting(r.Context(), store.SettingTelegram, &v)
	has := v.BotToken != ""
	v.BotToken = ""
	ok(w, map[string]any{"settings": v, "has_token": has})
}

func (h *handlers) putTelegram(w http.ResponseWriter, r *http.Request) {
	var v store.TelegramSettings
	if !readJSON(w, r, &v) {
		return
	}
	var cur store.TelegramSettings
	_ = h.Store.GetSetting(r.Context(), store.SettingTelegram, &cur)
	v.BotToken = strings.TrimSpace(v.BotToken)
	switch v.BotToken {
	case "":
		v.BotToken, v.BotUsername = cur.BotToken, cur.BotUsername
	case "-": // clears, like the other masked tokens
		v.BotToken, v.BotUsername = "", ""
	}
	if v.BotToken != "" && (v.BotToken != cur.BotToken || v.BotUsername == "") {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		name, err := (&telegram.Client{Token: v.BotToken}).Me(ctx)
		if err != nil {
			fail(w, http.StatusBadRequest, "bot token rejected: "+err.Error())
			return
		}
		v.BotUsername = name
	}
	if err := h.Store.SetSetting(r.Context(), store.SettingTelegram, v); err != nil {
		serverErr(w, err)
		return
	}
	if h.Bot != nil {
		h.Bot.Invalidate()
	}
	h.getTelegram(w, r)
}

func (h *handlers) testTelegram(w http.ResponseWriter, r *http.Request) {
	if h.Bot == nil {
		fail(w, http.StatusBadRequest, "telegram not available")
		return
	}
	s := h.Bot.Settings(r.Context())
	if s.BotToken == "" || s.AdminChatID == 0 {
		fail(w, http.StatusBadRequest, "set the bot token and admin chat id first")
		return
	}
	if err := h.Bot.NotifyAdmin(r.Context(), "✅ "+h.SiteName+": Telegram notifications work."); err != nil {
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	ok(w, map[string]bool{"ok": true})
}

func (h *handlers) getWebhooks(w http.ResponseWriter, r *http.Request) {
	var v webhook.Settings
	_ = h.Store.GetSetting(r.Context(), webhook.SettingKey, &v)
	if v.Endpoints == nil {
		v.Endpoints = []webhook.Endpoint{}
	}
	type view struct {
		webhook.Endpoint
		HasSecret bool `json:"has_secret"`
	}
	eps := make([]view, 0, len(v.Endpoints))
	for _, ep := range v.Endpoints {
		has := ep.Secret != ""
		ep.Secret = ""
		eps = append(eps, view{Endpoint: ep, HasSecret: has})
	}
	ok(w, map[string]any{"settings": map[string]any{"endpoints": eps}, "events": webhook.Events})
}

func (h *handlers) putWebhooks(w http.ResponseWriter, r *http.Request) {
	var v webhook.Settings
	if !readJSON(w, r, &v) {
		return
	}
	var cur webhook.Settings
	_ = h.Store.GetSetting(r.Context(), webhook.SettingKey, &cur)
	kept := map[string]string{}
	for _, ep := range cur.Endpoints {
		kept[ep.URL] = ep.Secret
	}
	eps := make([]webhook.Endpoint, 0, len(v.Endpoints))
	for _, ep := range v.Endpoints {
		ep.URL = strings.TrimSpace(ep.URL)
		if ep.URL == "" {
			continue
		}
		if ep.Secret == "" {
			ep.Secret = kept[ep.URL] // blank keeps the stored secret
		}
		if !strings.HasPrefix(ep.URL, "http://") && !strings.HasPrefix(ep.URL, "https://") {
			fail(w, http.StatusBadRequest, "endpoint URLs must start with http:// or https://")
			return
		}
		eps = append(eps, ep)
	}
	v.Endpoints = eps
	if err := h.Store.SetSetting(r.Context(), webhook.SettingKey, v); err != nil {
		serverErr(w, err)
		return
	}
	if h.Hooks != nil {
		h.Hooks.Invalidate()
	}
	h.getWebhooks(w, r)
}

func (h *handlers) testWebhook(w http.ResponseWriter, r *http.Request) {
	var in struct{ URL, Secret string }
	if !decode(r, &in) || strings.TrimSpace(in.URL) == "" {
		fail(w, http.StatusBadRequest, "url required")
		return
	}
	hub := h.Hooks
	if hub == nil {
		hub = &webhook.Hub{}
	}
	if err := hub.Deliver(webhook.Endpoint{URL: strings.TrimSpace(in.URL), Secret: in.Secret, Enabled: true}, webhook.Test, map[string]any{"site": h.SiteName, "by": userFrom(r).Email}); err != nil {
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	ok(w, map[string]bool{"ok": true})
}

func (h *handlers) getKomari(w http.ResponseWriter, r *http.Request) {
	var v store.KomariSettings
	_ = h.Store.GetSetting(r.Context(), store.SettingKomari, &v)
	ok(w, map[string]any{"enabled": v.Enabled, "server": v.Server, "interval": v.Interval, "has_key": v.Key != ""})
}

// putKomari stores the setting; a blank key keeps the stored one, "-" clears it.
func (h *handlers) putKomari(w http.ResponseWriter, r *http.Request) {
	var in store.KomariSettings
	if !readJSON(w, r, &in) {
		return
	}
	var cur store.KomariSettings
	_ = h.Store.GetSetting(r.Context(), store.SettingKomari, &cur)
	in.Server = strings.TrimRight(strings.TrimSpace(in.Server), "/")
	if in.Enabled && !strings.HasPrefix(in.Server, "http://") && !strings.HasPrefix(in.Server, "https://") {
		fail(w, http.StatusBadRequest, "Komari URL must start with http:// or https://")
		return
	}
	if in.Interval < 0 || in.Interval > 300 {
		fail(w, http.StatusBadRequest, "interval must be 0-300 seconds")
		return
	}
	switch strings.TrimSpace(in.Key) {
	case "":
		in.Key = cur.Key
	case "-":
		in.Key = ""
	default:
		in.Key = strings.TrimSpace(in.Key)
	}
	if in.Enabled && in.Key == "" {
		fail(w, http.StatusBadRequest, "the auto-discovery key is required to register nodes")
		return
	}
	if err := h.Store.SetSetting(r.Context(), store.SettingKomari, in); err != nil {
		serverErr(w, err)
		return
	}
	ok(w, map[string]any{"enabled": in.Enabled, "server": in.Server, "interval": in.Interval, "has_key": in.Key != ""})
}

func (h *handlers) getDStatus(w http.ResponseWriter, r *http.Request) {
	var v store.DStatusSettings
	_ = h.Store.GetSetting(r.Context(), store.SettingDStatus, &v)
	ok(w, map[string]any{"enabled": v.Enabled, "listen": v.Listen, "has_key": v.Key != ""})
}

// putDStatus stores the setting; a blank key keeps the stored one, "-"
// clears it, as everywhere else.
func (h *handlers) putDStatus(w http.ResponseWriter, r *http.Request) {
	var in store.DStatusSettings
	if !readJSON(w, r, &in) {
		return
	}
	var cur store.DStatusSettings
	_ = h.Store.GetSetting(r.Context(), store.SettingDStatus, &cur)
	in.Listen = strings.TrimSpace(in.Listen)
	if in.Listen != "" {
		if _, port, err := net.SplitHostPort(in.Listen); err != nil {
			fail(w, http.StatusBadRequest, "listen must be host:port, e.g. :9999")
			return
		} else if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			fail(w, http.StatusBadRequest, "listen must be host:port, e.g. :9999")
			return
		}
	}
	switch strings.TrimSpace(in.Key) {
	case "":
		in.Key = cur.Key
	case "-":
		in.Key = ""
	default:
		in.Key = strings.TrimSpace(in.Key)
	}
	// The endpoint is reachable from outside; without a key it would hand
	// every host's details to anyone who finds the port.
	if in.Enabled && in.Key == "" {
		fail(w, http.StatusBadRequest, "a key is required: the endpoint is reachable from the network")
		return
	}
	if err := h.Store.SetSetting(r.Context(), store.SettingDStatus, in); err != nil {
		serverErr(w, err)
		return
	}
	ok(w, map[string]any{"enabled": in.Enabled, "listen": in.Listen, "has_key": in.Key != ""})
}
