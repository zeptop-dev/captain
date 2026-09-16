package admin

import (
	"net/http"
	"strings"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/service"
	"github.com/zeptop-dev/captain/internal/store"
)

// Registered domains: the zones node host names, TLS names and
// subscription hosts live under, with the DNS authorisation used to issue
// certificates for them.

func (h *handlers) registerDomains(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/domains", h.requireAdmin(h.listDomains))
	mux.HandleFunc("POST /api/admin/domains", h.requireAdmin(h.createDomain))
	mux.HandleFunc("PATCH /api/admin/domains/{id}", h.requireAdmin(h.updateDomain))
	mux.HandleFunc("DELETE /api/admin/domains/{id}", h.requireAdmin(h.deleteDomain))
}

type domainUse struct {
	Nodes    []string `json:"nodes"`     // node host names under the domain
	TLS      []string `json:"tls"`       // inbound TLS server names
	SubHosts []string `json:"sub_hosts"` // subscription hosts
	Panel    bool     `json:"panel"`     // the panel's own base URL
}

// usage collects every host name the panel knows, grouped by registered domain.
func (h *handlers) usage(r *http.Request, domains []store.Domain) (map[int64]*domainUse, map[int64]int) {
	ctx := r.Context()
	use := map[int64]*domainUse{}
	for _, d := range domains {
		use[d.ID] = &domainUse{Nodes: []string{}, TLS: []string{}, SubHosts: []string{}}
	}
	find := func(host string) *domainUse {
		for _, d := range domains {
			hh := strings.ToLower(strings.TrimPrefix(host, "*."))
			if hh == d.Name || strings.HasSuffix(hh, "."+d.Name) {
				// longest match wins
				best := d
				for _, o := range domains {
					if len(o.Name) > len(best.Name) && (hh == o.Name || strings.HasSuffix(hh, "."+o.Name)) {
						best = o
					}
				}
				return use[best.ID]
			}
		}
		return nil
	}
	nodes, _ := h.Store.ListNodes(ctx)
	for _, n := range nodes {
		if n.Domain != "" {
			if u := find(n.Domain); u != nil {
				u.Nodes = append(u.Nodes, n.Domain)
			}
		}
		ibs, _ := h.Store.AllInboundsByNode(ctx, n.ID)
		for _, ib := range ibs {
			if sp := ib.Spec(); sp.TLS != nil && sp.TLS.Mode == spec.TLSStandard && sp.TLS.ServerName != "" {
				if u := find(sp.TLS.ServerName); u != nil {
					u.TLS = appendUnique(u.TLS, sp.TLS.ServerName)
				}
			}
		}
	}
	var subs service.SubscriptionSettings
	_ = h.Store.GetSetting(ctx, service.SettingSubscription, &subs)
	for _, u := range subs.URLs {
		host := u
		if i := strings.Index(host, "://"); i >= 0 {
			host = host[i+3:]
		}
		if i := strings.IndexAny(host, "/:"); i >= 0 {
			host = host[:i]
		}
		if du := find(host); du != nil {
			du.SubHosts = appendUnique(du.SubHosts, host)
		}
	}
	if base := h.BaseURL; base != "" {
		host := base
		if i := strings.Index(host, "://"); i >= 0 {
			host = host[i+3:]
		}
		if i := strings.IndexAny(host, "/:"); i >= 0 {
			host = host[:i]
		}
		if du := find(host); du != nil {
			du.Panel = true
		}
	}
	certs := map[int64]int{}
	list, _ := h.Store.ListCertificates(ctx)
	for _, c := range list {
		for _, d := range domains {
			if du := find(c.Domain); du != nil && du == use[d.ID] {
				certs[d.ID]++
			}
		}
	}
	return use, certs
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

func (h *handlers) listDomains(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.ListDomains(r.Context())
	if err != nil {
		serverErr(w, err)
		return
	}
	use, certs := h.usage(r, list)
	var acme store.ACMESettings
	_ = h.Store.GetSetting(r.Context(), store.SettingACME, &acme)
	out := make([]map[string]any, 0, len(list))
	for _, d := range list {
		out = append(out, map[string]any{"id": d.ID, "name": d.Name, "provider": d.Provider, "has_token": d.HasToken, "auto_dns": d.AutoDNS, "usage": use[d.ID], "certificates": certs[d.ID], "created_at": d.CreatedAt})
	}
	ok(w, map[string]any{"domains": out, "global_token": acme.CloudflareToken != "", "can_issue": h.Certs.Available()})
}

func (h *handlers) createDomain(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name, Provider, CFToken string
		AutoDNS                 *bool
	}
	if !readJSON(w, r, &in) {
		return
	}
	name := strings.ToLower(strings.TrimSpace(in.Name))
	if name == "" || strings.ContainsAny(name, " /:*") || !strings.Contains(name, ".") || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") {
		fail(w, http.StatusBadRequest, "a registrable domain such as example.com is required")
		return
	}
	if in.Provider != "manual" {
		in.Provider = "cloudflare"
	}
	d := &store.Domain{Name: name, Provider: in.Provider, CFToken: strings.TrimSpace(in.CFToken), AutoDNS: in.AutoDNS == nil || *in.AutoDNS}
	if err := h.Store.CreateDomain(r.Context(), d); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	ok(w, d)
}

// updateDomain changes the provider and token; a blank token keeps the
// stored one, "-" clears it.
func (h *handlers) updateDomain(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Provider, CFToken string
		AutoDNS           *bool
	}
	if !readJSON(w, r, &in) {
		return
	}
	d, err := h.Store.DomainByID(r.Context(), idOf(r))
	if err != nil {
		fail(w, http.StatusNotFound, "domain not found")
		return
	}
	if in.Provider == "manual" || in.Provider == "cloudflare" {
		d.Provider = in.Provider
	}
	if in.AutoDNS != nil {
		d.AutoDNS = *in.AutoDNS
	}
	switch strings.TrimSpace(in.CFToken) {
	case "":
	case "-":
		d.CFToken = ""
	default:
		d.CFToken = strings.TrimSpace(in.CFToken)
	}
	if err := h.Store.UpdateDomain(r.Context(), d); err != nil {
		serverErr(w, err)
		return
	}
	d.HasToken = d.CFToken != ""
	ok(w, d)
}

func (h *handlers) deleteDomain(w http.ResponseWriter, r *http.Request) {
	if err := h.Store.DeleteDomain(r.Context(), idOf(r)); err != nil {
		serverErr(w, err)
		return
	}
	ok(w, map[string]bool{"ok": true})
}
