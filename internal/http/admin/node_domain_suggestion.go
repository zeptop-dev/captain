package admin

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/dns"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/service"
	"github.com/zeptop-dev/captain/internal/store"
)

type nodeDomainSuggestion struct {
	Prefix    string `json:"prefix"`
	Domain    string `json:"domain"`
	DNSCheck  string `json:"dns_check"`
	DNSReason string `json:"dns_reason,omitempty"`
}

type nodeDomainZone struct {
	Domain store.Domain
	Token  string
}

var errAmbiguousSuggestionZone = errors.New("registered domains contain equivalent names; remove the duplicate before generating a hostname")

// The catalog predates canonical IDNA storage. Match a normalized copy while
// retaining the original row for provider credentials and change validation.
func (h *handlers) suggestionDomainFor(ctx context.Context, name string) (*store.Domain, error) {
	list, err := h.Store.ListDomains(ctx)
	if err != nil {
		return nil, err
	}
	var best *store.Domain
	bestLength := 0
	seen := map[string]bool{}
	for i := range list {
		candidate := &list[i]
		canonical, err := domain.NormalizeNodeDomain(candidate.Name)
		if err != nil || canonical == "" || (name != canonical && !strings.HasSuffix(name, "."+canonical)) {
			continue
		}
		if seen[canonical] {
			return nil, errAmbiguousSuggestionZone
		}
		seen[canonical] = true
		if len(canonical) > bestLength {
			best, bestLength = candidate, len(canonical)
		}
	}
	return best, nil
}

func (h *handlers) suggestionZone(ctx context.Context, name string) (*nodeDomainZone, error) {
	d, err := h.suggestionDomainFor(ctx, name)
	if err != nil {
		return nil, err
	}
	canonical := ""
	if d != nil {
		canonical, _ = domain.NormalizeNodeDomain(d.Name)
	}
	if d == nil || canonical != name {
		return nil, store.ErrNotFound
	}
	token := d.CFToken
	if token == "" && d.Provider == "cloudflare" {
		var acme store.ACMESettings
		if err := h.Store.GetSetting(ctx, store.SettingACME, &acme); err != nil {
			return nil, err
		}
		token = acme.CloudflareToken
	}
	return &nodeDomainZone{Domain: *d, Token: token}, nil
}

// suggestNodeDomain is a read-only draft operation. It owns its account locks,
// just like HTTPS diagnostics, so a slow client or DNS provider cannot stall
// account reset/renumbering. The result never reserves or creates a DNS record.
func (h *handlers) suggestNodeDomain(w http.ResponseWriter, r *http.Request) {
	r = r.WithContext(context.WithValue(r.Context(), subPresetAuthOnlyKey{}, true))
	authorized := false
	h.Store.Accounts.RLock()
	h.requireAdmin(func(http.ResponseWriter, *http.Request) { authorized = true })(w, r)
	h.Store.Accounts.RUnlock()
	if !authorized {
		return
	}
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Now().Add(5 * time.Second))
	body, readErr := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	_ = controller.SetReadDeadline(time.Time{})
	var in struct {
		Zone string `json:"zone"`
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if readErr != nil || dec.Decode(&in) != nil || dec.Decode(new(any)) != io.EOF {
		fail(w, 400, "bad json")
		return
	}
	zone, err := domain.NormalizeNodeDomain(in.Zone)
	// The longest possible prefix is eleven bytes plus its separating dot.
	if err != nil || zone == "" || len(zone) > 241 {
		fail(w, 400, "invalid domain suffix")
		return
	}
	var saved *nodeDomainZone
	var claimed map[string]bool
	authorized = false
	h.Store.Accounts.RLock()
	h.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		saved, err = h.suggestionZone(r.Context(), zone)
		if errors.Is(err, errAmbiguousSuggestionZone) {
			fail(w, 409, errAmbiguousSuggestionZone.Error())
			return
		}
		if errors.Is(err, store.ErrNotFound) {
			fail(w, 400, "select a registered domain")
			return
		}
		if err != nil {
			serverErr(w, err)
			return
		}
		claimed, err = h.nodeDomainClaims(r.Context())
		if err != nil {
			serverErr(w, err)
			return
		}
		authorized = true
	})(w, r)
	h.Store.Accounts.RUnlock()
	if !authorized {
		return
	}
	if !h.domainSuggestion.TryLock() {
		fail(w, 429, "domain suggestion is already running")
		return
	}
	defer h.domainSuggestion.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	var cf *dns.Cloudflare
	zoneID := ""
	reason := "manual_provider"
	if saved.Domain.Provider == "cloudflare" {
		reason = "missing_token"
		if saved.Token != "" {
			reason = "provider_unavailable"
			base := ""
			if h.DNS != nil {
				base = h.DNS.Base
			}
			cf = &dns.Cloudflare{Token: saved.Token, Base: base, Client: &http.Client{Timeout: 6 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
			zoneID, err = cf.ZoneID(ctx, zone)
			if err == nil && zoneID != "" {
				reason = ""
			}
		}
	}
	var result nodeDomainSuggestion
	found := false
	for range 12 {
		prefix, e := randomNodePrefix()
		if e != nil {
			serverErr(w, e)
			return
		}
		name := prefix + "." + zone
		if claimed[name] {
			continue
		}
		result = nodeDomainSuggestion{Prefix: prefix, Domain: name, DNSCheck: "unverified", DNSReason: reason}
		if reason == "" {
			exists, e := cf.NameExists(ctx, zoneID, name)
			if e != nil {
				result.DNSReason = "provider_unavailable"
			} else if exists {
				continue
			} else {
				result.DNSCheck = "checked"
			}
		}
		found = true
		break
	}
	// The exclusive account lock excludes admin mutations between reauthorization,
	// re-reading current name claims and returning the snapshot to this caller.
	h.Store.Accounts.Lock()
	defer h.Store.Accounts.Unlock()
	h.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		current, e := h.suggestionZone(r.Context(), zone)
		if errors.Is(e, store.ErrNotFound) || errors.Is(e, errAmbiguousSuggestionZone) {
			fail(w, 409, "domain settings changed; try again")
			return
		}
		if e != nil {
			serverErr(w, e)
			return
		}
		if *current != *saved {
			fail(w, 409, "domain settings changed; try again")
			return
		}
		if !found {
			fail(w, 409, "could not find an unused domain; try again")
			return
		}
		claims, e := h.nodeDomainClaims(r.Context())
		if e != nil {
			serverErr(w, e)
			return
		}
		if claims[result.Domain] {
			fail(w, 409, "domain was just claimed; try again")
			return
		}
		actual, e := h.suggestionDomainFor(r.Context(), result.Domain)
		if errors.Is(e, errAmbiguousSuggestionZone) {
			fail(w, 409, "domain settings changed; try again")
			return
		}
		if e != nil {
			serverErr(w, e)
			return
		}
		if actual == nil || actual.ID != saved.Domain.ID {
			fail(w, 409, "domain settings changed; try again")
			return
		}
		ok(w, result)
	})(w, r)
}

func randomNodePrefix() (string, error) {
	words := []string{"cedar", "lumen", "orbit", "maple", "coral", "birch", "aster", "comet", "delta", "ember", "grove", "haven", "nova", "ocean", "pine", "river", "solar", "stone", "terra", "willow"}
	const alphabet = "23456789abcdefghjkmnpqrstuvwxyz"
	pick := func(n int) (int, error) {
		v, e := rand.Int(rand.Reader, big.NewInt(int64(n)))
		if e != nil {
			return 0, e
		}
		return int(v.Int64()), nil
	}
	i, err := pick(len(words))
	if err != nil {
		return "", err
	}
	suffix := make([]byte, 4)
	for j := range suffix {
		k, e := pick(len(alphabet))
		if e != nil {
			return "", e
		}
		suffix[j] = alphabet[k]
	}
	return words[i] + "-" + string(suffix), nil
}

// Collect exact panel-owned host names, not broad certificate wildcards: a
// wildcard certificate is meant to be reused by new servers in this zone.
func (h *handlers) nodeDomainClaims(ctx context.Context) (map[string]bool, error) {
	names := map[string]bool{}
	add := func(v string) {
		if n, e := domain.NormalizeNodeDomain(v); e == nil && n != "" {
			names[n] = true
		}
	}
	addURL := func(v string) {
		if u, e := url.Parse(v); e == nil {
			add(u.Hostname())
		}
	}
	nodes, err := h.Store.ListNodes(ctx)
	if err != nil {
		return nil, err
	}
	for _, n := range nodes {
		add(n.Domain)
		ibs, e := h.Store.AllInboundsByNode(ctx, n.ID)
		if e != nil {
			return nil, e
		}
		for _, ib := range ibs {
			if tls := ib.Spec().TLS; tls != nil {
				add(tls.ServerName)
			}
		}
	}
	entries, err := h.Store.ListEntries(ctx)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		add(e.DisplayHost)
	}
	ingresses, err := h.Store.AllIngresses(ctx)
	if err != nil {
		return nil, err
	}
	for _, g := range ingresses {
		add(g.EntryDomain)
		add(g.EntryHost)
	}
	certs, err := h.Store.ListCertificates(ctx)
	if err != nil {
		return nil, err
	}
	for _, c := range certs {
		add(c.Domain)
		for _, n := range c.Names {
			add(n)
		}
	}
	zones, err := h.Store.ListDomains(ctx)
	if err != nil {
		return nil, err
	}
	for _, z := range zones {
		add(z.Name)
	}
	var sub service.SubscriptionSettings
	if err := h.Store.GetSetting(ctx, service.SettingSubscription, &sub); err != nil {
		return nil, err
	}
	for _, u := range sub.URLs {
		addURL(u)
	}
	var probe store.ProbeSettings
	if err := h.Store.GetSetting(ctx, store.SettingProbe, &probe); err != nil {
		return nil, err
	}
	for _, host := range probe.Hosts {
		add(host)
	}
	access, err := h.Store.AgentHTTPSSettings(ctx)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	addURL(access.URL)
	addURL(h.BaseURL)
	addURL(h.AgentURL)
	return names, nil
}
