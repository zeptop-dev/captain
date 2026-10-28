package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/dns"
	"github.com/zeptop-dev/captain/internal/domain"
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
	Name    string `json:"name"`
	IP      string `json:"ip"`
	Action  string `json:"action"` // created | updated | unchanged | skipped
	Error   string `json:"error,omitempty"`
	Outcome string `json:"outcome,omitempty"` // success | failed | unknown for attempted manual writes
	Reason  string `json:"reason,omitempty"`  // shared_domain | domain_address_conflict
}

type dnsOriginKey struct{}
type DNSOrigin struct {
	NodeID        int64
	Source, Actor string
}

// WithDNSOrigin adds a non-secret audit label to automatic writes.
func WithDNSOrigin(ctx context.Context, origin DNSOrigin) context.Context {
	return context.WithValue(ctx, dnsOriginKey{}, origin)
}

// Ensure points fqdn at ip when a registered Cloudflare domain with auto
// DNS covers it; otherwise the record is skipped, never an error.
func (d *DNS) Ensure(ctx context.Context, fqdn, ip string) Result {
	res := Result{Name: strings.ToLower(strings.TrimSpace(fqdn)), IP: strings.TrimSpace(ip), Action: "skipped"}
	if d == nil || res.Name == "" || net.ParseIP(res.IP) == nil || net.ParseIP(res.Name) != nil {
		return res
	}
	name, err := domain.NormalizeNodeDomain(res.Name)
	if err != nil {
		res.Error = domain.ErrNodeDomain.Error()
		return res
	}
	res.Name = name
	// Guard every caller, including ingress saves. Skipping only the node
	// whose checkbox is enabled would still let another user of the name
	// overwrite it. Topology writes hold Store.Topology through EnsureMany.
	uses, err := d.Store.NodeDomainUses(ctx, name, 0)
	if err != nil {
		res.Error = "could not check domain ownership"
		return res
	}
	if len(uses) > 1 || len(uses) == 1 && uses[0].Shared {
		res.Reason = "shared_domain"
		return res
	}
	if len(uses) == 1 {
		ip := net.ParseIP(res.IP)
		if !ip.Equal(net.ParseIP(uses[0].PublicAddr)) && !ip.Equal(net.ParseIP(uses[0].V6Addr)) {
			res.Reason = "domain_address_conflict"
			return res
		}
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
	plan, err := cf.PlanAddress(ctx, dom.Name, res.Name, res.IP)
	if err != nil {
		res.Error = err.Error()
		if d.Log != nil {
			d.Log.Warn("dns record", "component", "dns", "name", res.Name, "err", err)
		}
		return res
	}
	if plan.Action == "unchanged" {
		res.Action = plan.Action
		return res
	}
	return d.applyPlan(ctx, cf, dom.Name, plan, res)
}

// applyPlan journals intent before any external mutation. Callers hold the
// topology stable, so the addresses reviewed still belong to this server.
func (d *DNS) applyPlan(ctx context.Context, cf *dns.Cloudflare, zone string, plan dns.AddressPlan, res Result) Result {
	if plan.Action == "unchanged" {
		res.Action = "unchanged"
		res.Outcome = "success"
		return res
	}
	origin, _ := ctx.Value(dnsOriginKey{}).(DNSOrigin)
	change := store.DNSChange{Source: origin.Source, Actor: origin.Actor, Zone: zone, Name: res.Name, Action: plan.Action, Before: plan.Before, Wanted: plan.Wanted}
	if origin.NodeID > 0 {
		change.NodeID = &origin.NodeID
	}
	if err := d.Store.BeginDNSChange(ctx, &change); err != nil {
		res.Outcome = "failed"
		res.Error = "could not save DNS history; no DNS write sent"
		return res
	}
	after, writeErr := cf.ApplyAddress(ctx, plan)
	outcome := "success"
	if writeErr != nil {
		outcome = "unknown"
		var rejected *dns.Rejected
		if errors.As(writeErr, &rejected) {
			outcome = "failed"
		}
	}
	// A disconnected browser must not discard the outcome of a completed write.
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	finishErr := d.Store.FinishDNSChange(finishCtx, change.ID, outcome, after)
	res.Outcome = outcome
	if writeErr != nil {
		res.Error = "DNS write outcome unknown; check DNS history and provider records before retrying"
		if outcome == "failed" {
			res.Error = "DNS provider rejected the write; previous record saved in DNS history"
		}
		return res
	}
	res.Action = plan.Action
	if finishErr != nil {
		res.Outcome = "unknown"
		res.Error = "DNS write succeeded but history completion failed; saved previous record remains available"
	}
	if d.Log != nil {
		d.Log.Info("dns record "+plan.Action, "component", "dns", "change_id", change.ID, "name", res.Name, "ip", res.IP)
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
