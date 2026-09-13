package service

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"

	"github.com/zeptop-dev/captain/internal/dns"
	"github.com/zeptop-dev/captain/internal/store"
)

// DNS creates and updates records for names the panel manages, on the
// registered domain they fall under, when that domain allows it.
type DNS struct {
	Store *store.Store
	Log   *slog.Logger
	Base  string // Cloudflare API base override (tests)
}

// Result is one record's outcome, returned to the console.
type Result struct {
	Name   string `json:"name"`
	IP     string `json:"ip"`
	Action string `json:"action"` // created | updated | unchanged | skipped
	Error  string `json:"error,omitempty"`
}

// Ensure points fqdn at ip when a registered Cloudflare domain with auto
// DNS covers it; otherwise the record is skipped, never an error.
func (d *DNS) Ensure(ctx context.Context, fqdn, ip string) Result {
	res := Result{Name: strings.ToLower(strings.TrimSpace(fqdn)), IP: strings.TrimSpace(ip), Action: "skipped"}
	if d == nil || res.Name == "" || net.ParseIP(res.IP) == nil || net.ParseIP(res.Name) != nil {
		return res
	}
	dom, err := d.Store.DomainFor(ctx, res.Name)
	if err != nil || dom == nil || dom.Provider != "cloudflare" || !dom.AutoDNS {
		return res
	}
	token := dom.CFToken
	if token == "" {
		var acme store.ACMESettings
		_ = d.Store.GetSetting(ctx, store.SettingACME, &acme)
		token = acme.CloudflareToken
	}
	if token == "" {
		res.Error = "no Cloudflare token for " + dom.Name
		return res
	}
	cf := &dns.Cloudflare{Token: token, Base: d.Base}
	action, err := cf.EnsureAddress(ctx, dom.Name, res.Name, res.IP)
	if err != nil {
		res.Error = err.Error()
		if d.Log != nil {
			d.Log.Warn("dns record", "component", "dns", "name", res.Name, "err", err)
		}
		return res
	}
	res.Action = action
	if action != "unchanged" && d.Log != nil {
		d.Log.Info("dns record "+action, "component", "dns", "name", res.Name, "ip", res.IP)
	}
	return res
}

// EnsureMany runs Ensure for each name/ip pair, skipping blanks.
func (d *DNS) EnsureMany(ctx context.Context, pairs ...[2]string) []Result {
	var out []Result
	for _, p := range pairs {
		if p[0] == "" || p[1] == "" {
			continue
		}
		out = append(out, d.Ensure(ctx, p[0], p[1]))
	}
	return out
}

// Summary is a one-line description for toasts; "" when nothing happened.
func Summary(rs []Result) string {
	var parts []string
	for _, r := range rs {
		switch {
		case r.Error != "":
			parts = append(parts, fmt.Sprintf("%s: %s", r.Name, r.Error))
		case r.Action == "created" || r.Action == "updated":
			parts = append(parts, fmt.Sprintf("%s → %s (%s)", r.Name, r.IP, r.Action))
		}
	}
	return strings.Join(parts, "; ")
}
