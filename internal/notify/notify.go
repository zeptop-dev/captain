// Package notify fans a user-facing notice out to Telegram (when the user
// linked a chat) and email (when configured), and operator notices to the
// admin Telegram chat.
package notify

import (
	"context"
	"log/slog"

	"github.com/zeptop-dev/captain/internal/mail"
	"github.com/zeptop-dev/captain/internal/store"
	"github.com/zeptop-dev/captain/internal/telegram"
)

// Notifier holds the channels; any may be nil.
type Notifier struct {
	Store    *store.Store
	Mail     *mail.Loader
	Bot      *telegram.Bot
	SiteName string
	Log      *slog.Logger
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
