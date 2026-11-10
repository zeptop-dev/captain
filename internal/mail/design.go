package mail

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
)

const DesignKey = "mail_templates"

var Kinds = []string{"register", "reset", "expiry", "traffic", "notice", "test"}
var Variables = []string{"site", "code", "expires_at", "expires_date", "used_percent", "portal_url", "subject", "body", "plan", "used", "total", "reset_at"}

type Template struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
	Button  string `json:"button"`
}
type Design struct {
	Name      string                         `json:"name"`
	Logo      string                         `json:"logo"`
	Color     string                         `json:"color"`
	Footer    string                         `json:"footer"`
	Templates map[string]map[string]Template `json:"templates"` // language -> kind
}

var tokenPattern = regexp.MustCompile(`\{\{([^{}]+)\}\}`)
var colorPattern = regexp.MustCompile(`^#[a-fA-F0-9]{6}$`)

func validKind(kind string) bool {
	for _, k := range Kinds {
		if kind == k {
			return true
		}
	}
	return false
}
func (d Design) Validate() error {
	if len(d.Name) > 200 || len(d.Footer) > 2000 || len(d.Logo) > 2048 || strings.ContainsAny(d.Name, "\r\n") {
		return fmt.Errorf("invalid mail brand")
	}
	if d.Color != "" && !colorPattern.MatchString(d.Color) {
		return fmt.Errorf("color must be #RRGGBB")
	}
	if d.Logo != "" {
		u, e := url.Parse(d.Logo)
		if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
			return fmt.Errorf("logo must be an HTTPS URL without credentials")
		}
	}
	for lang, templates := range d.Templates {
		if !Supported(lang) {
			return fmt.Errorf("unsupported template language")
		}
		for kind, t := range templates {
			if !validKind(kind) {
				return fmt.Errorf("unsupported mail type")
			}
			if len(t.Subject) > 500 || len(t.Body) > 12000 || len(t.Button) > 100 || strings.ContainsAny(t.Subject+t.Button, "\r\n") {
				return fmt.Errorf("invalid template length or subject")
			}
			allowed := map[string]bool{"site": true}
			switch kind {
			case "register", "reset":
				allowed["code"] = true
			case "expiry":
				allowed["expires_at"] = true
				allowed["expires_date"] = true
				allowed["portal_url"] = true
			case "traffic":
				allowed["used_percent"] = true
				allowed["portal_url"] = true
			case "notice":
				allowed["subject"] = true
				allowed["body"] = true
			}
			if kind == "expiry" || kind == "traffic" {
				for _, v := range []string{"plan", "used", "total", "reset_at"} {
					allowed[v] = true
				}
			}
			for _, s := range []string{t.Subject, t.Body, t.Button} {
				for _, token := range tokenPattern.FindAllStringSubmatch(s, -1) {
					if !allowed[token[1]] {
						return fmt.Errorf("unsupported variable for %s: %s", kind, token[1])
					}
				}
				rest := tokenPattern.ReplaceAllString(s, "")
				if strings.Contains(rest, "{{") || strings.Contains(rest, "}}") {
					return fmt.Errorf("invalid variable syntax")
				}
				for _, c := range s {
					if unicode.IsControl(c) && c != '\n' && c != '\t' {
						return fmt.Errorf("invalid template character")
					}
				}
			}
		}
	}
	return nil
}

func Configured(s Settings, lang string) Builder {
	b := For(lang)
	// Persisted invalid drafts must not break registration or reset delivery.
	if s.Design.Validate() == nil {
		b.design = s.Design
	}
	return b
}
func (b Builder) Defaults() map[string]Template {
	t := b.t
	return map[string]Template{
		"register": {fmt.Sprintf(t.codeSubject, "{{site}}", "{{code}}"), t.codeIntro, ""},
		"reset":    {fmt.Sprintf(t.resetSubject, "{{site}}", "{{code}}"), t.resetIntro, ""},
		"expiry":   {fmt.Sprintf(t.expirySubject, "{{site}}", "{{expires_date}}"), stripTags(fmt.Sprintf(t.expiryBody, "{{expires_at}}")), t.expiryButton},
		"traffic":  {strings.ReplaceAll(fmt.Sprintf(t.trafficSubject, "{{site}}", 98765), "98765", "{{used_percent}}"), strings.ReplaceAll(stripTags(fmt.Sprintf(t.trafficBody, 98765)), "98765", "{{used_percent}}"), t.trafficButton},
		"notice":   {"{{subject}}", "{{body}}", ""},
		"test":     {fmt.Sprintf(t.testSubject, "{{site}}"), t.testBody, ""},
	}
}
func replaceValues(s string, values map[string]string) string {
	// One pass: a value containing {{...}} is data, never a second template.
	return tokenPattern.ReplaceAllStringFunc(s, func(token string) string { return values[token[2:len(token)-2]] })
}
func cleanSubject(s string) string {
	return strings.Map(func(c rune) rune {
		if unicode.IsControl(c) {
			return ' '
		}
		return c
	}, s)
}
func (b Builder) decorate(old Message, kind, site string, values map[string]string, extra ...map[string]string) Message {
	if b.design.Name == "" && b.design.Logo == "" && b.design.Color == "" && b.design.Footer == "" && len(b.design.Templates) == 0 {
		return old
	}
	if values == nil {
		values = map[string]string{}
	}
	for _, m := range extra {
		for k, v := range m {
			values[k] = v
		}
	}
	if b.design.Name != "" {
		site = b.design.Name
	}
	values["site"] = site
	t := b.Defaults()[kind]
	// b.lang is normalized by For; custom text never crosses languages.
	if custom, ok := b.design.Templates[b.lang][kind]; ok {
		if custom.Subject != "" {
			t.Subject = custom.Subject
		}
		if custom.Body != "" {
			t.Body = custom.Body
		}
		if custom.Button != "" {
			t.Button = custom.Button
		}
	}
	body := "<p>" + strings.ReplaceAll(html.EscapeString(replaceValues(t.Body, values)), "\n", "<br>") + "</p>"
	if kind == "register" || kind == "reset" {
		body += `<p style="font-size:32px;letter-spacing:8px;font-weight:700">` + html.EscapeString(values["code"]) + `</p>`
	}
	color := b.design.Color
	if color == "" {
		color = "#0ea5e9"
	}
	if kind == "expiry" || kind == "traffic" {
		body += strings.ReplaceAll(button(values["portal_url"], replaceValues(t.Button, values)), "#0ea5e9", color)
	}
	logo := ""
	if b.design.Logo != "" {
		logo = `<img src="` + html.EscapeString(b.design.Logo) + `" alt="" width="48" style="max-width:48px;max-height:48px"><br>`
	}
	footer := b.design.Footer
	if footer == "" {
		footer = site
	}
	frame := Layout(site, body)
	frame = strings.Replace(frame, html.EscapeString(site)+"</td></tr>", logo+html.EscapeString(site)+"</td></tr>", 1)
	at := strings.LastIndex(frame, html.EscapeString(site)+"</td></tr>")
	if at >= 0 {
		frame = frame[:at] + html.EscapeString(footer) + frame[at+len(html.EscapeString(site)):]
	}
	old.Subject = cleanSubject(replaceValues(t.Subject, values))
	old.HTML = frame
	old.Text = stripTags(frame)
	return old
}
func (b Builder) Notice(site, to, subject, body string) Message {
	return b.decorate(Message{To: to, Subject: subject, HTML: Layout(site, "<p>"+html.EscapeString(body)+"</p>")}, "notice", site, map[string]string{"subject": subject, "body": body})
}

// Sample never creates authentication codes, reads customers or marks reminders.
func (b Builder) Sample(kind, site, to, portal string) (Message, error) {
	var m Message
	switch kind {
	case "register", "reset":
		m = b.Code(site, to, kind, "123456")
	case "expiry":
		m = b.Expiry(site, to, portal, time.Date(2030, 1, 15, 12, 0, 0, 0, time.UTC), sampleDetails())
	case "traffic":
		m = b.Traffic(site, to, portal, 90, sampleDetails())
	case "notice":
		m = b.Notice(site, to, "Example notification", "Example notification content")
	case "test":
		m = b.Test(site, to)
	default:
		return m, fmt.Errorf("unsupported mail type")
	}
	m.Subject = "[TEST] " + m.Subject
	return m, nil
}

func sampleDetails() map[string]string {
	return map[string]string{"plan": "Example plan", "used": "90.00 GiB", "total": "100.00 GiB", "reset_at": "2030-01-01 00:00 UTC"}
}

// ReminderValues describes one subscription row, never aggregate account dates.
func ReminderValues(plan string, used, total, reset int64) map[string]string {
	amount := func(n int64) string { return fmt.Sprintf("%.2f GiB", float64(n)/(1<<30)) }
	quota := amount(total)
	if total == 0 {
		quota = "Unlimited"
	}
	date := "—"
	if reset > 0 {
		date = time.Unix(reset, 0).UTC().Format("2006-01-02 15:04") + " UTC"
	}
	return map[string]string{"plan": plan, "used": amount(used), "total": quota, "reset_at": date}
}
