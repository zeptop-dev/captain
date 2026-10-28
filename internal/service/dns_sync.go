package service

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/dns"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

var ErrDNSSyncDisabled = errors.New("DNS synchronization is unavailable for this server")

// DNSNodeSyncPlan contains no credentials and no caller-provided DNS name or
// address. Revision binds the reviewed local configuration AND provider records.
type DNSNodeSyncPlan struct {
	NodeID   int64               `json:"node_id"`
	Host     string              `json:"host"`
	Revision string              `json:"revision"`
	Enabled  bool                `json:"enabled"`
	Reason   string              `json:"reason,omitempty"`
	Records  []DNSNodeSyncRecord `json:"records"`
	config   dnsNodeSyncConfig
	plans    []dns.AddressPlan
}
type DNSNodeSyncRecord struct {
	Type     string      `json:"type"`
	Expected string      `json:"expected"`
	Action   string      `json:"action"`
	Before   *dns.Record `json:"before,omitempty"`
	Reason   string      `json:"reason,omitempty"`
}
type dnsNodeSyncConfig struct {
	NodeID            int64
	CreatedAt         time.Time
	Host, Zone, Token string
	Identity          string
	DomainID          int64
	Addresses         []string
	Reason            string
}

// nodeSyncConfig must run under Topology and an account lock. Credentials stay
// in the private plan; only the resulting opaque revision is returned.
func (d *DNS) nodeSyncConfig(ctx context.Context, id int64) (dnsNodeSyncConfig, error) {
	c := dnsNodeSyncConfig{NodeID: id, Addresses: []string{}}
	n, err := d.Store.NodeByID(ctx, id)
	if err != nil {
		return c, err
	}
	c.CreatedAt = n.CreatedAt
	c.Identity, err = d.Store.DNSNodeIdentity(ctx, id)
	if err != nil {
		return c, err
	}
	if strings.TrimSpace(n.Domain) == "" {
		c.Reason = "no_domain"
		return c, nil
	}
	c.Host, err = domain.NormalizeNodeDomain(n.Domain)
	if err != nil {
		c.Reason = "invalid_domain"
		return c, nil
	}
	if n.DomainShared {
		c.Reason = "shared_domain"
		return c, nil
	}
	for _, v := range []string{n.PublicAddr, n.V6Addr} {
		if strings.TrimSpace(v) == "" {
			continue
		}
		ip := net.ParseIP(strings.TrimSpace(v))
		if ip == nil {
			c.Reason = "invalid_address"
			return c, nil
		}
		c.Addresses = append(c.Addresses, ip.String())
	}
	c.Addresses = dnsIPs(c.Addresses...)
	if len(c.Addresses) == 0 {
		c.Reason = "no_address"
		return c, nil
	}
	// Existing untyped API fields may contain the wrong family. Never generate
	// two competing writes for one RR type from such legacy data.
	if len(dnsFamilyIPs(c.Addresses, "ip4")) > 1 || len(dnsFamilyIPs(c.Addresses, "ip6")) > 1 {
		c.Reason = "invalid_address"
		return c, nil
	}
	uses, err := d.Store.NodeDomainUses(ctx, c.Host, 0)
	if err != nil {
		return c, err
	}
	if len(uses) != 1 || uses[0].Shared {
		c.Reason = "shared_domain"
		return c, nil
	}
	ingress, err := d.Store.AllIngresses(ctx)
	if err != nil {
		return c, err
	}
	for _, g := range ingress {
		host := g.EntryDomain
		if host == "" && net.ParseIP(g.EntryHost) == nil {
			host = g.EntryHost
		}
		normal, e := domain.NormalizeNodeDomain(host)
		if e != nil || normal != c.Host {
			continue
		}
		if ip := net.ParseIP(g.EntryHost); ip != nil {
			found := false
			for _, address := range c.Addresses {
				found = found || ip.Equal(net.ParseIP(address))
			}
			if !found {
				c.Reason = "domain_address_conflict"
				return c, nil
			}
		}
	}
	dom, err := d.Store.DomainFor(ctx, c.Host)
	if err != nil {
		return c, err
	}
	if dom == nil || dom.Provider != "cloudflare" {
		c.Reason = "unmanaged_domain"
		return c, nil
	}
	c.Zone, c.DomainID = dom.Name, dom.ID
	// AutoDNS controls unattended writes only. An explicitly reviewed manual
	// synchronization is available with managed credentials even when it is
	// disabled; this operation neither enables nor persists that preference.
	c.Token = dom.CFToken
	if c.Token == "" {
		c.Token, err = d.globalDNSToken(ctx)
		if err != nil {
			return c, err
		}
	}
	if c.Token == "" {
		c.Reason = "missing_token"
	}
	return c, nil
}
func (d *DNS) PrepareNodeSync(ctx context.Context, id int64) (*DNSNodeSyncPlan, error) {
	d.Store.Topology.Lock()
	defer d.Store.Topology.Unlock()
	c, err := d.nodeSyncConfig(ctx, id)
	if err != nil {
		return nil, err
	}
	p := &DNSNodeSyncPlan{NodeID: id, Host: c.Host, Enabled: c.Reason == "", Reason: c.Reason, Records: []DNSNodeSyncRecord{}, config: c}
	for _, ip := range c.Addresses {
		kind := "A"
		if net.ParseIP(ip).To4() == nil {
			kind = "AAAA"
		}
		p.Records = append(p.Records, DNSNodeSyncRecord{Type: kind, Expected: ip, Action: "skipped", Reason: c.Reason})
	}
	return p, nil
}

// ReadNodeSyncPlan only reads the provider, without holding account/topology
// locks. Both the client revision and current local configuration are checked
// again before applying; a read cannot itself authorize any later write.
func (d *DNS) ReadNodeSyncPlan(ctx context.Context, p *DNSNodeSyncPlan) {
	if p.Enabled {
		ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
		defer cancel()
		cf := &dns.Cloudflare{Token: p.config.Token, Base: d.Base}
		for i := range p.Records {
			rec := &p.Records[i]
			plan, err := cf.PlanAddress(ctx, p.config.Zone, p.Host, rec.Expected)
			if err != nil {
				rec.Reason = "provider_unavailable"
				if errors.Is(err, dns.ErrExternalRecords) {
					rec.Reason = "external_records"
				}
				p.Enabled = false
				p.Reason = rec.Reason
			} else {
				rec.Action, rec.Before = plan.Action, plan.Before
			}
			p.plans = append(p.plans, plan)
		}
	}
	// Do not return token material or its hash as an independently visible field.
	p.Revision = dnsFingerprint(struct {
		Config  dnsNodeSyncConfig
		Records []DNSNodeSyncRecord
	}{p.config, p.Records})
}
func (d *DNS) ValidateNodeSync(ctx context.Context, p *DNSNodeSyncPlan) error {
	d.Store.Topology.Lock()
	defer d.Store.Topology.Unlock()
	return d.validateNodeSync(ctx, p)
}
func (d *DNS) validateNodeSync(ctx context.Context, p *DNSNodeSyncPlan) error {
	current, err := d.nodeSyncConfig(ctx, p.NodeID)
	if err != nil {
		return err
	}
	if dnsFingerprint(current) != dnsFingerprint(p.config) {
		return ErrDNSConfigChanged
	}
	return nil
}

// ApplyNodeSync is called with Accounts exclusively locked after reauthorization.
// Hold Topology during bounded writes, as automatic EnsureMany does, to avoid
// applying a stale address after an overlapping local ownership/address edit.
// Cloudflare provides no CAS here: an external console edit can still race.
func (d *DNS) ApplyNodeSync(ctx context.Context, p *DNSNodeSyncPlan, revision string) ([]Result, error) {
	d.Store.Topology.Lock()
	defer d.Store.Topology.Unlock()
	if err := d.validateNodeSync(ctx, p); err != nil {
		return nil, err
	}
	if revision == "" || p.Revision != revision {
		return nil, ErrDNSConfigChanged
	}
	if !p.Enabled {
		return nil, ErrDNSSyncDisabled
	}
	if err := d.Store.InvalidateDNSHealth(ctx, p.NodeID); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	cf := &dns.Cloudflare{Token: p.config.Token, Base: d.Base}
	results := make([]Result, 0, len(p.plans))
	for i, plan := range p.plans {
		res := Result{Name: p.Host, IP: p.Records[i].Expected, Action: plan.Action}
		results = append(results, d.applyPlan(ctx, cf, p.config.Zone, plan, res))
	}
	return results, nil
}

func (d *DNS) globalDNSToken(ctx context.Context) (string, error) {
	var acme store.ACMESettings
	err := d.Store.GetSetting(ctx, store.SettingACME, &acme)
	return acme.CloudflareToken, err
}
