package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/certs"
	"github.com/zeptop-dev/captain/internal/notify"
	"github.com/zeptop-dev/captain/internal/store"
)

// RenewBefore is how long before expiry an ACME certificate is renewed.
const RenewBefore = 30 * 24 * time.Hour

// Certs issues and renews panel-managed certificates. Nodes receive
// whatever covers their TLS names through the agent state, so nothing
// here talks to nodes directly.
type Certs struct {
	Store  *store.Store
	Issuer certs.Issuer // nil = issuance unavailable (uploads and webhooks still work)
	Log    *slog.Logger
	Notify *notify.Notifier
	Now    func() time.Time

	lastRenew time.Time
}

func (c *Certs) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// Available reports whether the panel can issue certificates itself.
func (c *Certs) Available() bool { return c != nil && c.Issuer != nil }

// token picks the Cloudflare token: the chosen domain's own, else the
// registered domain the first name falls under, else the ACME settings.
func (c *Certs) token(ctx context.Context, names []string, domainID *int64) (email, token string, dom *store.Domain, err error) {
	var acme store.ACMESettings
	_ = c.Store.GetSetting(ctx, store.SettingACME, &acme)
	email = acme.Email
	if domainID != nil {
		dom, err = c.Store.DomainByID(ctx, *domainID)
		if err != nil {
			return "", "", nil, errors.New("domain not found")
		}
	} else if len(names) > 0 {
		dom, _ = c.Store.DomainFor(ctx, names[0])
	}
	if dom != nil {
		if dom.Provider != "cloudflare" {
			return "", "", dom, fmt.Errorf("%s is registered as manual DNS; upload its certificate or switch it to Cloudflare", dom.Name)
		}
		if dom.CFToken != "" {
			return email, dom.CFToken, dom, nil
		}
	}
	if acme.CloudflareToken == "" {
		return "", "", dom, errors.New("no Cloudflare token: set one in Settings → ACME or on the domain")
	}
	return email, acme.CloudflareToken, dom, nil
}

// Issue obtains a certificate for the names and stores it under the first.
func (c *Certs) Issue(ctx context.Context, name string, names []string, domainID *int64) (*store.Certificate, error) {
	if !c.Available() {
		return nil, errors.New("certificate issuance is not available on this panel")
	}
	clean := make([]string, 0, len(names))
	seen := map[string]bool{}
	for _, n := range names {
		n = strings.ToLower(strings.TrimSpace(n))
		if n == "" || seen[n] {
			continue
		}
		if strings.ContainsAny(n, " /:") || strings.Count(n, "*") > 1 || (strings.Contains(n, "*") && !strings.HasPrefix(n, "*.")) {
			return nil, fmt.Errorf("bad name %q", n)
		}
		seen[n] = true
		clean = append(clean, n)
	}
	if len(clean) == 0 {
		return nil, errors.New("at least one domain name is required")
	}
	email, token, dom, err := c.token(ctx, clean, domainID)
	if err != nil {
		return nil, err
	}
	certPEM, keyPEM, err := c.Issuer.Issue(ctx, certs.IssueRequest{Names: clean, Email: email, CloudflareToken: token})
	if err != nil {
		return nil, err
	}
	rec, err := store.ParseCertificate(clean[0], certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("issued certificate does not parse: %w", err)
	}
	rec.Name, rec.Source, rec.AutoRenew = strings.TrimSpace(name), "acme", true
	if dom != nil {
		id := dom.ID
		rec.DomainID = &id
	}
	if err := c.Store.UpsertCertificate(ctx, rec); err != nil {
		return nil, err
	}
	if stored, err := c.Store.CertificateByID(ctx, rec.ID); err == nil {
		rec = stored
	}
	if c.Log != nil {
		c.Log.Info("certificate issued", "component", "certs", "domain", rec.Domain, "names", rec.Names, "not_after", rec.NotAfter)
	}
	return rec, nil
}

// Renew re-issues an ACME certificate with the same names.
func (c *Certs) Renew(ctx context.Context, id int64) (*store.Certificate, error) {
	cur, err := c.Store.CertificateByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if cur.Source != "acme" {
		return nil, errors.New("only panel-issued certificates can be renewed; upload a new one instead")
	}
	rec, err := c.Issue(ctx, cur.Name, cur.Names, cur.DomainID)
	if err != nil {
		_ = c.Store.SetCertificateError(ctx, id, err.Error())
		return nil, err
	}
	return rec, nil
}

// RenewDue runs at most hourly: every auto-renewing ACME certificate
// within RenewBefore of expiry is re-issued; the first failure notifies.
func (c *Certs) RenewDue(ctx context.Context) {
	if !c.Available() {
		return
	}
	now := c.now()
	if now.Sub(c.lastRenew) < time.Hour {
		return
	}
	c.lastRenew = now
	due, err := c.Store.CertificatesDue(ctx, now.Add(RenewBefore))
	if err != nil {
		return
	}
	for _, cert := range due {
		if _, err := c.Renew(ctx, cert.ID); err != nil {
			if c.Log != nil {
				c.Log.Error("certificate renewal failed", "component", "certs", "domain", cert.Domain, "err", err)
			}
			if cert.LastError == "" && c.Notify != nil {
				c.Notify.Admin(ctx, fmt.Sprintf("证书 %s 自动续期失败：%v（%s 到期）", cert.Name, err, cert.NotAfter.Format("2006-01-02")))
			}
		}
	}
}
