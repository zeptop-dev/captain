// Package notify fans a user-facing notice out to Telegram (when the user
// linked a chat) and email (when configured), and operator notices to the
// admin Telegram chat.
package notify

import (
	"context"
	"log/slog"
	"time"

	"github.com/zeptop-dev/captain/internal/webhook"

	"github.com/zeptop-dev/captain/internal/mail"
	"github.com/zeptop-dev/captain/internal/store"
	"github.com/zeptop-dev/captain/internal/telegram"
)

// Notifier holds the channels; any may be nil.
type Notifier struct {
	Store    *store.Store
	Mail     *mail.Loader
	Bot      *telegram.Bot
	Hooks    *webhook.Hub // nil = no webhooks
	SiteName string
	Log      *slog.Logger
}

// Event forwards an integration event to the webhook hub.
func (n *Notifier) Event(ctx context.Context, event string, data map[string]any) {
	if n == nil || n.Hooks == nil {
		return
	}
	n.Hooks.Emit(ctx, event, data)
}

// User sends subject/body to a user by every available channel.
func (n *Notifier) User(ctx context.Context, userID int64, email, subject, body string) {
	if n == nil {
		return
	}
	sent := false
	if n.Bot != nil {
		ok, err := n.Bot.NotifyUser(ctx, userID, "<b>"+html(subject)+"</b>\n"+html(body))
		if err != nil && n.Log != nil {
			n.Log.Warn("telegram notify", "user", userID, "err", err)
		}
		sent = ok && err == nil
	}
	if !sent && n.Mail != nil && email != "" {
		ms := n.Mail.Settings(ctx)
		if ms.Enabled() {
			if err := mail.Send(ctx, ms, mail.Message{To: email, Subject: subject, HTML: mail.Layout(n.SiteName, "<p>"+html(body)+"</p>")}); err != nil && n.Log != nil {
				n.Log.Warn("mail notify", "to", email, "err", err)
			}
		}
	}
}

// Admin sends an operator notice to the admin Telegram chat.
func (n *Notifier) Admin(ctx context.Context, text string) {
	if n == nil || n.Bot == nil {
		return
	}
	if err := n.Bot.NotifyAdmin(ctx, text); err != nil && n.Log != nil {
		n.Log.Warn("telegram admin notify", "err", err)
	}
}

func html(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '&':
			out = append(out, "&amp;"...)
		case '<':
			out = append(out, "&lt;"...)
		case '>':
			out = append(out, "&gt;"...)
		default:
			out = append(out, s[i])
		}
	}
	return string(out)
}

// AdminAsync is Admin without making the caller wait: a node report must
// not hold the database connection (or time out and be re-sent) because
// Telegram is slow. The text is escaped for Telegram's HTML mode by
// Admin itself only for Admin's own markup, so callers that interpolate
// node- or user-supplied strings use Escape first.
func (n *Notifier) AdminAsync(text string) {
	if n == nil || n.Bot == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		n.Admin(ctx, text)
	}()
}

// Escape makes a string safe to place inside a Telegram HTML message.
func Escape(s string) string { return html(s) }
