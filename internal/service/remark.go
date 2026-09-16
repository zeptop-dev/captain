package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/bosun/pkg/subscription"
	"github.com/zeptop-dev/captain/internal/domain"
)

// RemarkVars are the values behind the {{VARIABLE}} placeholders an
// operator may put in entry names and in the subscription's info lines,
// so the client's server list itself shows days and traffic left:
//
//	{{DAYS_LEFT}} {{EXPIRE_DATE}} {{TRAFFIC_LEFT}} {{TRAFFIC_USED}}
//	{{TRAFFIC_LIMIT}} {{USED_PERCENT}} {{STATUS}} {{PLAN}} {{EMAIL}}
//	{{USERNAME}} and {{STATUS:ACTIVE=✅|EXPIRED=😓|LIMITED=⛔|DISABLED=❌}}
//
// Unlimited values print as ∞.
type RemarkVars struct {
	Email  string
	Plan   string
	Status string // active, expired, limited, disabled
	Expire *time.Time
	Total  int64 // 0 = unlimited
	Used   int64
	At     time.Time
}

// NewRemarkVars derives the variables from the user's usable plans and
// the account summary (what the Subscription-Userinfo header carries).
func NewRemarkVars(u *domain.User, usable []*domain.Subscription, plans map[int64]string, acct subscription.Account, at time.Time) RemarkVars {
	v := RemarkVars{Email: u.Email, Status: "active", Total: acct.Total, Used: acct.Upload + acct.Download, At: at}
	if acct.Expire > 0 {
		t := time.Unix(acct.Expire, 0)
		v.Expire = &t
	}
	names := make([]string, 0, len(usable))
	for _, s := range usable {
		if n := plans[s.PlanID]; n != "" {
			names = append(names, n)
		}
	}
	v.Plan = strings.Join(names, "+")
	switch {
	case u.Status != "active":
		v.Status = "disabled"
	case len(usable) == 0 && v.Expire != nil && !at.Before(*v.Expire):
		v.Status = "expired"
	case len(usable) == 0 && v.Total > 0 && v.Used >= v.Total:
		v.Status = "limited"
	case len(usable) == 0:
		v.Status = "expired"
	}
	return v
}

// Expand replaces the placeholders in s. Text without "{{" is returned
// as is.
func (v RemarkVars) Expand(s string) string {
	if !strings.Contains(s, "{{") {
		return s
	}
	var b strings.Builder
	for {
		i := strings.Index(s, "{{")
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		j := strings.Index(s[i:], "}}")
		if j < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])
		b.WriteString(v.value(strings.TrimSpace(s[i+2 : i+j])))
		s = s[i+j+2:]
	}
}

func (v RemarkVars) value(key string) string {
	if strings.HasPrefix(strings.ToUpper(key), "STATUS:") {
		// {{STATUS:ACTIVE=✅|EXPIRED=😓|LIMITED=⛔|DISABLED=❌}}
		for _, pair := range strings.Split(key[len("STATUS:"):], "|") {
			k, val, _ := strings.Cut(pair, "=")
			if strings.EqualFold(strings.TrimSpace(k), v.Status) {
				return val
			}
		}
		return ""
	}
	switch strings.ToUpper(key) {
	case "DAYS_LEFT":
		if v.Expire == nil {
			return "∞"
		}
		d := v.Expire.Sub(v.At)
		if d < 0 {
			return "0"
		}
		return fmt.Sprintf("%d", int(d.Hours()/24))
	case "EXPIRE_DATE":
		if v.Expire == nil {
			return "∞"
		}
		return v.Expire.Local().Format("2006-01-02")
	case "TRAFFIC_LEFT":
		if v.Total <= 0 {
			return "∞"
		}
		return humanBytes(max(v.Total-v.Used, 0))
	case "TRAFFIC_USED":
		return humanBytes(v.Used)
	case "TRAFFIC_LIMIT":
		if v.Total <= 0 {
			return "∞"
		}
		return humanBytes(v.Total)
	case "USED_PERCENT":
		if v.Total <= 0 {
			return "0"
		}
		return fmt.Sprintf("%d", min(v.Used*100/v.Total, 100))
	case "STATUS":
		return v.Status
	case "PLAN":
		return v.Plan
	case "EMAIL":
		return v.Email
	case "USERNAME":
		name, _, _ := strings.Cut(v.Email, "@")
		return name
	}
	return "{{" + key + "}}"
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	val := float64(n) / float64(div)
	if val >= 100 {
		return fmt.Sprintf("%.0f %cB", val, "KMGTPE"[exp])
	}
	return fmt.Sprintf("%.1f %cB", val, "KMGTPE"[exp])
}

// InfoLine is a dummy server whose only purpose is its name: put at the
// top of the list, it shows the user their days and traffic left in the
// client. Shadowsocks to localhost renders in every format.
func InfoLine(name string, i int) subscription.Line {
	return subscription.Line{
		Name: name, Host: "127.0.0.1", Port: 1 + i,
		Inbound:  spec.Inbound{Tag: fmt.Sprintf("info-%d", i), Protocol: spec.Shadowsocks, Port: 1 + i, Cipher: "aes-128-gcm"},
		Password: "info",
	}
}
