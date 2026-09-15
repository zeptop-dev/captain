// Package telegram is a minimal Bot API client and a long-polling command
// handler. No third-party library: sendMessage, getMe and getUpdates are the
// only methods used.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

// APIBase is the Bot API root; tests point it at a fake server.
var APIBase = "https://api.telegram.org"

// Client calls the Bot API for one token.
type Client struct {
	Token string
	HTTP  *http.Client
}

func (c *Client) call(ctx context.Context, method string, params any, out any) error {
	body, _ := json.Marshal(params)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, APIBase+"/bot"+c.Token+"/"+method, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var env struct {
		OK          bool            `json:"ok"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&env); err != nil {
		return fmt.Errorf("telegram %s: %w", method, err)
	}
	if !env.OK {
		return fmt.Errorf("telegram %s: %s", method, env.Description)
	}
	if out != nil {
		return json.Unmarshal(env.Result, out)
	}
	return nil
}

// Me returns the bot's username.
func (c *Client) Me(ctx context.Context) (string, error) {
	var me struct {
		Username string `json:"username"`
	}
	err := c.call(ctx, "getMe", map[string]any{}, &me)
	return me.Username, err
}

// Send posts a text message (HTML parse mode).
func (c *Client) Send(ctx context.Context, chatID int64, text string) error {
	return c.call(ctx, "sendMessage", map[string]any{"chat_id": chatID, "text": text, "parse_mode": "HTML", "disable_web_page_preview": true}, nil)
}

// Update is the subset of a Bot API update we act on.
type Update struct {
	ID      int64 `json:"update_id"`
	Message *struct {
		Text string `json:"text"`
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
		From struct {
			Username string `json:"username"`
		} `json:"from"`
	} `json:"message"`
}

func (c *Client) updates(ctx context.Context, offset int64, timeout int) ([]Update, error) {
	var out []Update
	err := c.call(ctx, "getUpdates", map[string]any{"offset": offset, "timeout": timeout, "allowed_updates": []string{"message"}}, &out)
	return out, err
}

// Bot polls for commands and answers them. Settings are re-read from the
// store on every idle cycle so a token change needs no restart.
type Bot struct {
	Store     *store.Store
	Log       *slog.Logger
	SiteName  string
	PortalURL string
	SubURL    func(ctx context.Context, token string) string
	// PollTimeout is the long-poll wait in seconds (tests lower it).
	PollTimeout int

	mu       sync.Mutex
	settings store.TelegramSettings
	fetched  time.Time
	client   *Client
	offset   int64
}

// Settings returns the cached bot settings (15s).
func (b *Bot) Settings(ctx context.Context) store.TelegramSettings {
	b.mu.Lock()
	defer b.mu.Unlock()
	if time.Since(b.fetched) < 15*time.Second {
		return b.settings
	}
	var s store.TelegramSettings
	_ = b.Store.GetSetting(ctx, store.SettingTelegram, &s)
	if s.BotToken != b.settings.BotToken || b.client == nil {
		b.client = &Client{Token: s.BotToken}
	}
	b.settings, b.fetched = s, time.Now()
	return s
}

// Invalidate drops the cache after the settings changed.
func (b *Bot) Invalidate() {
	b.mu.Lock()
	b.fetched = time.Time{}
	b.mu.Unlock()
}

func (b *Bot) clientFor(ctx context.Context) (*Client, store.TelegramSettings) {
	s := b.Settings(ctx)
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.client, s
}

// Enabled reports whether a token is configured.
func (b *Bot) Enabled(ctx context.Context) bool { return b.Settings(ctx).BotToken != "" }

// NotifyUser sends text to the user's linked chat; false when not linked.
func (b *Bot) NotifyUser(ctx context.Context, userID int64, text string) (bool, error) {
	c, s := b.clientFor(ctx)
	if s.BotToken == "" {
		return false, nil
	}
	chat, err := b.Store.TelegramID(ctx, userID)
	if err != nil || chat == 0 {
		return false, err
	}
	return true, c.Send(ctx, chat, text)
}

// NotifyAdmin sends text to the configured admin chat.
func (b *Bot) NotifyAdmin(ctx context.Context, text string) error {
	c, s := b.clientFor(ctx)
	if s.BotToken == "" || s.AdminChatID == 0 {
		return nil
	}
	return c.Send(ctx, s.AdminChatID, text)
}

// Run polls until ctx ends.
func (b *Bot) Run(ctx context.Context) {
	log := b.Log
	if log == nil {
		log = slog.Default()
	}
	log = log.With("component", "telegram")
	timeout := b.PollTimeout
	if timeout <= 0 {
		timeout = 30
	}
	for ctx.Err() == nil {
		c, s := b.clientFor(ctx)
		if s.BotToken == "" {
			select {
			case <-ctx.Done():
				return
			case <-time.After(15 * time.Second):
			}
			continue
		}
		ups, err := c.updates(ctx, b.offset, timeout)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Warn("poll", "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Second):
			}
			continue
		}
		for _, u := range ups {
			b.offset = u.ID + 1
			if u.Message == nil {
				continue
			}
			reply := b.handle(ctx, u.Message.Chat.ID, strings.TrimSpace(u.Message.Text))
			if reply != "" {
				if err := c.Send(ctx, u.Message.Chat.ID, reply); err != nil {
					log.Warn("reply", "err", err)
				}
			}
		}
	}
}

// Poll processes one batch of updates (tests).
func (b *Bot) Poll(ctx context.Context) error {
	c, s := b.clientFor(ctx)
	if s.BotToken == "" {
		return errors.New("no token")
	}
	ups, err := c.updates(ctx, b.offset, 0)
	if err != nil {
		return err
	}
	for _, u := range ups {
		b.offset = u.ID + 1
		if u.Message == nil {
			continue
		}
		if reply := b.handle(ctx, u.Message.Chat.ID, strings.TrimSpace(u.Message.Text)); reply != "" {
			if err := c.Send(ctx, u.Message.Chat.ID, reply); err != nil {
				return err
			}
		}
	}
	return nil
}

// handle answers one command.
func (b *Bot) handle(ctx context.Context, chatID int64, text string) string {
	cmd, arg, _ := strings.Cut(text, " ")
	cmd = strings.ToLower(strings.TrimSpace(cmd))
	if i := strings.IndexByte(cmd, '@'); i > 0 {
		cmd = cmd[:i]
	}
	arg = strings.TrimSpace(arg)
	switch cmd {
	case "/start", "/bind":
		if arg == "" {
			if u, err := b.Store.UserByTelegramID(ctx, chatID); err == nil {
				return fmt.Sprintf("Linked to <b>%s</b>. Commands: /sub /status /unbind", esc(u.Email))
			}
			return fmt.Sprintf("Welcome to %s. Open the portal, copy your bind code and send:\n<code>/bind CODE</code>", esc(b.SiteName))
		}
		u, err := b.Store.BindTelegram(ctx, strings.ToUpper(arg), chatID)
		if err != nil {
			return "That code is unknown or expired. Get a fresh one from the portal."
		}
		return fmt.Sprintf("Linked to <b>%s</b>. You will get expiry, traffic and ticket notices here.\nCommands: /sub /status /unbind", esc(u.Email))
	case "/unbind":
		u, err := b.Store.UserByTelegramID(ctx, chatID)
		if err != nil {
			return "Not linked."
		}
		_ = b.Store.UnbindTelegram(ctx, u.ID)
		return "Unlinked."
	case "/sub":
		u, err := b.Store.UserByTelegramID(ctx, chatID)
		if err != nil {
			return "Not linked. Send <code>/bind CODE</code> first."
		}
		url := ""
		if b.SubURL != nil {
			url = b.SubURL(ctx, u.SubToken)
		}
		return fmt.Sprintf("Subscription link (keep it private):\n<code>%s</code>", esc(url))
	case "/status", "/traffic":
		u, err := b.Store.UserByTelegramID(ctx, chatID)
		if err != nil {
			return "Not linked. Send <code>/bind CODE</code> first."
		}
		return b.status(ctx, u)
	case "/help":
		return "/bind CODE – link this chat\n/sub – subscription link\n/status – plan, traffic, expiry\n/unbind – unlink"
	}
	return ""
}

func (b *Bot) status(ctx context.Context, u *domain.User) string {
	subs, err := b.Store.Subscriptions(ctx, u.ID)
	if err != nil || len(subs) == 0 {
		return fmt.Sprintf("<b>%s</b>\nNo active plan. Balance: %s", esc(u.Email), money(u.BalanceCents))
	}
	out := "<b>" + esc(u.Email) + "</b>"
	for _, sub := range subs {
		name := fmt.Sprintf("plan #%d", sub.PlanID)
		if p, err := b.Store.PlanByID(ctx, sub.PlanID); err == nil {
			name = p.Name
		}
		used := sub.UsedUpBytes + sub.UsedDownBytes
		quota := "unlimited"
		if sub.QuotaBytes > 0 {
			quota = fmt.Sprintf("%s (%d%%)", gb(sub.QuotaBytes), used*100/sub.QuotaBytes)
		}
		exp := "never"
		if sub.ExpiresAt != nil {
			exp = sub.ExpiresAt.Format("2006-01-02")
		}
		state := "active"
		switch {
		case sub.Status == "queued":
			state = "queued (starts when the current plan lapses)"
		case !sub.Usable(time.Now()):
			state = "expired / exhausted"
		}
		out += fmt.Sprintf("\n\n<b>%s</b>\nStatus: %s\nUsed: %s of %s\nExpires: %s", esc(name), state, gb(used), quota, exp)
	}
	return out + "\nBalance: " + money(u.BalanceCents)
}

func gb(b int64) string {
	const g = 1 << 30
	if b < g {
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	}
	return fmt.Sprintf("%.2f GB", float64(b)/g)
}

func money(cents int64) string { return strconv.FormatFloat(float64(cents)/100, 'f', 2, 64) }

func esc(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}
