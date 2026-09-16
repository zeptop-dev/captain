// Package mail sends transactional email over SMTP or the Resend API. The
// settings live in the database (key "mail") so the admin can change them
// without a restart.
package mail

import (
	"sort"
	"sync"

	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/smtp"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/store"
)

// Settings is the admin-edited mail configuration.
type Settings struct {
	Provider    string `json:"provider"` // "" (off), "smtp", "resend"
	FromName    string `json:"from_name"`
	FromAddress string `json:"from_address"`
	SMTP        struct {
		Host     string `json:"host"`
		Port     int    `json:"port"`     // 587 STARTTLS, 465 implicit TLS, 25 plain
		Username string `json:"username"` //
		Password string `json:"password"` //
		Security string `json:"security"` // "starttls" (default for 587), "tls" (465), "none"
	} `json:"smtp"`
	Resend struct {
		APIKey string `json:"api_key"`
	} `json:"resend"`
	// VerifyRegistration requires an emailed code to create an account.
	VerifyRegistration bool `json:"verify_registration"`
	// Reminders sends expiry (3 days ahead) and traffic-threshold notices.
	Reminders bool `json:"reminders"`
	// TrafficThresholds are the used-percentages that trigger a traffic
	// notice (and a subscription.traffic webhook), once each per quota
	// period; empty means 90.
	TrafficThresholds []int `json:"traffic_thresholds"`
}

// Thresholds returns the traffic thresholds to check, ascending, 1..100,
// deduplicated; 90 when none is configured.
func (s Settings) Thresholds() []int {
	seen := map[int]bool{}
	var out []int
	for _, t := range s.TrafficThresholds {
		if t >= 1 && t <= 100 && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return []int{90}
	}
	sort.Ints(out)
	return out
}

// SettingKey is the settings table key.
const SettingKey = "mail"

// Loader reads the settings from the store with a short cache.
type Loader struct {
	Store *store.Store

	mu      sync.Mutex
	cached  Settings
	fetched time.Time
}

// Settings returns the current mail settings.
func (l *Loader) Settings(ctx context.Context) Settings {
	l.mu.Lock()
	defer l.mu.Unlock()
	if time.Since(l.fetched) < 15*time.Second {
		return l.cached
	}
	var s Settings
	_ = l.Store.GetSetting(ctx, SettingKey, &s)
	l.cached, l.fetched = s, time.Now()
	return s
}

// Invalidate drops the cache after the settings changed.
func (l *Loader) Invalidate() {
	l.mu.Lock()
	l.fetched = time.Time{}
	l.mu.Unlock()
}

// Enabled reports whether a provider is configured.
func (s Settings) Enabled() bool {
	switch s.Provider {
	case "smtp":
		return s.SMTP.Host != "" && s.FromAddress != ""
	case "resend":
		return s.Resend.APIKey != "" && s.FromAddress != ""
	}
	return false
}

// Message is one email.
type Message struct {
	To      string
	Subject string
	HTML    string
	Text    string
}

// SendFunc is the delivery function; tests replace it to capture mail.
var SendFunc = deliver

// Send delivers m with the configured provider.
func Send(ctx context.Context, s Settings, m Message) error {
	if !s.Enabled() {
		return errors.New("mail is not configured")
	}
	return SendFunc(ctx, s, m)
}

func deliver(ctx context.Context, s Settings, m Message) error {
	if m.Text == "" {
		m.Text = stripTags(m.HTML)
	}
	switch s.Provider {
	case "smtp":
		return sendSMTP(ctx, s, m)
	case "resend":
		return sendResend(ctx, s, m)
	}
	return errors.New("unknown mail provider")
}

func from(s Settings) string {
	if s.FromName == "" {
		return s.FromAddress
	}
	return fmt.Sprintf("%s <%s>", s.FromName, s.FromAddress)
}

func sendSMTP(ctx context.Context, s Settings, m Message) error {
	port := s.SMTP.Port
	if port == 0 {
		port = 587
	}
	sec := s.SMTP.Security
	if sec == "" {
		switch port {
		case 465:
			sec = "tls"
		case 25:
			sec = "none"
		default:
			sec = "starttls"
		}
	}
	addr := net.JoinHostPort(s.SMTP.Host, fmt.Sprint(port))
	d := net.Dialer{Timeout: 15 * time.Second}
	var conn net.Conn
	var err error
	if sec == "tls" {
		conn, err = tls.DialWithDialer(&d, "tcp", addr, &tls.Config{ServerName: s.SMTP.Host})
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("smtp connect: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	c, err := smtp.NewClient(conn, s.SMTP.Host)
	if err != nil {
		return fmt.Errorf("smtp: %w", err)
	}
	defer c.Close()
	if sec == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("smtp: server offers no STARTTLS; set security to none or tls")
		}
		if err := c.StartTLS(&tls.Config{ServerName: s.SMTP.Host}); err != nil {
			return fmt.Errorf("smtp starttls: %w", err)
		}
	}
	if s.SMTP.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", s.SMTP.Username, s.SMTP.Password, s.SMTP.Host)); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := c.Mail(s.FromAddress); err != nil {
		return fmt.Errorf("smtp from: %w", err)
	}
	if err := c.Rcpt(m.To); err != nil {
		return fmt.Errorf("smtp rcpt: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(build(from(s), m)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// build renders a multipart/alternative message.
func build(from string, m Message) []byte {
	boundary := "captain-" + fmt.Sprint(time.Now().UnixNano())
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\nSubject: =?UTF-8?B?%s?=\r\nMIME-Version: 1.0\r\nDate: %s\r\n", from, m.To, b64(m.Subject), time.Now().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", boundary)
	fmt.Fprintf(&b, "--%s\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n\r\n%s\r\n", boundary, wrap(b64(m.Text)))
	fmt.Fprintf(&b, "--%s\r\nContent-Type: text/html; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n\r\n%s\r\n", boundary, wrap(b64(m.HTML)))
	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	return b.Bytes()
}

func sendResend(ctx context.Context, s Settings, m Message) error {
	body, _ := json.Marshal(map[string]any{"from": from(s), "to": []string{m.To}, "subject": m.Subject, "html": m.HTML, "text": m.Text})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.Resend.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("resend: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		out, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("resend: %s: %s", resp.Status, strings.TrimSpace(string(out)))
	}
	return nil
}

func stripTags(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == '<':
			in = true
		case r == '>':
			in = false
		case !in:
			b.WriteRune(r)
		}
	}
	return html.UnescapeString(strings.TrimSpace(b.String()))
}

// ---- templates ---------------------------------------------------------------

// Layout wraps body HTML in a plain, mail-client-safe frame.
func Layout(site, body string) string {
	return fmt.Sprintf(`<!doctype html><html><body style="margin:0;background:#f4f5f7;font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;color:#1f2933">
<table width="100%%" cellpadding="0" cellspacing="0"><tr><td align="center" style="padding:32px 16px">
<table width="480" cellpadding="0" cellspacing="0" style="max-width:480px;background:#fff;border-radius:12px;padding:32px">
<tr><td style="font-size:20px;font-weight:700;padding-bottom:16px">%s</td></tr>
<tr><td style="font-size:15px;line-height:1.6">%s</td></tr>
<tr><td style="font-size:12px;color:#8a94a6;padding-top:24px">%s</td></tr>
</table></td></tr></table></body></html>`, html.EscapeString(site), body, html.EscapeString(site))
}

// CodeMessage is the verification / reset code email.
func CodeMessage(site, to, purpose, code string) Message {
	title := "验证码"
	intro := "你正在注册账号，验证码如下，10 分钟内有效："
	if purpose == "reset" {
		title = "重置密码"
		intro = "你正在重置密码，验证码如下，10 分钟内有效。如果不是你本人操作，忽略这封邮件即可："
	}
	body := fmt.Sprintf(`<p>%s</p><p style="font-size:32px;letter-spacing:8px;font-weight:700;margin:16px 0">%s</p>`, intro, html.EscapeString(code))
	return Message{To: to, Subject: fmt.Sprintf("[%s] %s %s", site, title, code), HTML: Layout(site, body)}
}

// ExpiryMessage reminds about an expiring subscription.
func ExpiryMessage(site, to, portalURL string, expires time.Time) Message {
	body := fmt.Sprintf(`<p>你的订阅将在 <b>%s</b> 到期。到期后节点会停止服务，续费后立即恢复。</p><p><a href="%s" style="display:inline-block;background:#0ea5e9;color:#fff;text-decoration:none;padding:10px 18px;border-radius:8px">前往续费</a></p>`,
		expires.Format("2006-01-02 15:04"), html.EscapeString(portalURL))
	return Message{To: to, Subject: fmt.Sprintf("[%s] 订阅将于 %s 到期", site, expires.Format("01-02")), HTML: Layout(site, body)}
}

// TrafficMessage warns that most of the quota is used.
func TrafficMessage(site, to, portalURL string, usedPct int) Message {
	body := fmt.Sprintf(`<p>本周期流量已使用 <b>%d%%</b>。用完后节点会停止服务，可以购买流量或等待重置。</p><p><a href="%s" style="display:inline-block;background:#0ea5e9;color:#fff;text-decoration:none;padding:10px 18px;border-radius:8px">查看用量</a></p>`,
		usedPct, html.EscapeString(portalURL))
	return Message{To: to, Subject: fmt.Sprintf("[%s] 流量已使用 %d%%", site, usedPct), HTML: Layout(site, body)}
}

// TestMessage is what the admin's "send test" button mails.
func TestMessage(site, to string) Message {
	return Message{To: to, Subject: fmt.Sprintf("[%s] 测试邮件", site), HTML: Layout(site, "<p>邮件配置正常，这是一封测试邮件。</p>")}
}
