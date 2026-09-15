package admin

import (
	"crypto/subtle"
	"encoding/json"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/auth"
	"net/http"
	"strings"

	"github.com/zeptop-dev/captain/internal/store"
)

// Uploaded certificates and the webhook a certificate manager (Certimate,
// acme.sh deploy hooks, ...) can call to deliver renewals. Pushed pairs
// reach the nodes whose inbounds use one of their names.

const settingCertHook = "cert_webhook"

type certHookSettings struct {
	Token string `json:"token"`
}

func (h *handlers) registerCertificates(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/certificates", h.requireAdmin(h.listCertificates))
	mux.HandleFunc("POST /api/admin/certificates", h.requireAdmin(h.uploadCertificate))
	mux.HandleFunc("POST /api/admin/certificates/issue", h.requireAdmin(h.issueCertificate))
	mux.HandleFunc("GET /api/admin/certificates/{id}", h.requireAdmin(h.certificateDetail))
	mux.HandleFunc("PATCH /api/admin/certificates/{id}", h.requireAdmin(h.updateCertificate))
	mux.HandleFunc("POST /api/admin/certificates/{id}/renew", h.requireAdmin(h.renewCertificate))
	mux.HandleFunc("DELETE /api/admin/certificates/{id}", h.requireAdmin(h.deleteCertificate))
	mux.HandleFunc("POST /api/admin/certificates/webhook-token", h.requireAdmin(h.rotateCertHookToken))
	mux.HandleFunc("POST /api/hooks/certificate", h.certificateWebhook)
}

func (h *handlers) certHook(r *http.Request) certHookSettings {
	var s certHookSettings
	_ = h.Store.GetSetting(r.Context(), settingCertHook, &s)
	return s
}

func (h *handlers) listCertificates(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.ListCertificates(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	deployed := h.deployments(r)
	out := make([]map[string]any, 0, len(list))
	for _, c := range list {
		c.CertPEM = ""
		out = append(out, map[string]any{"id": c.ID, "name": c.Name, "domain": c.Domain, "names": c.Names, "not_after": c.NotAfter, "source": c.Source, "issuer": c.Issuer,
			"renewals": c.Renewals, "last_error": c.LastError, "auto_renew": c.AutoRenew, "domain_id": c.DomainID, "created_at": c.CreatedAt, "updated_at": c.UpdatedAt, "nodes": deployed[c.ID]})
	}
	hook := h.certHook(r)
	url := ""
	if hook.Token != "" {
		url = strings.TrimRight(h.BaseURL, "/") + "/api/hooks/certificate?token=" + hook.Token
	}
	ok(w, map[string]any{"certificates": out, "webhook_url": url, "can_issue": h.Certs.Available()})
}

// deployments maps certificate id -> node names whose standard-TLS
// inbounds it covers (the same rule the agent state uses).
func (h *handlers) deployments(r *http.Request) map[int64][]string {
	ctx := r.Context()
	out := map[int64][]string{}
	certs, _ := h.Store.ListCertificates(ctx)
	nodes, _ := h.Store.ListNodes(ctx)
	for _, n := range nodes {
		ibs, _ := h.Store.AllInboundsByNode(ctx, n.ID)
		var names []string
		for _, ib := range ibs {
			if sp := ib.Spec(); sp.TLS != nil && sp.TLS.Mode == spec.TLSStandard && sp.TLS.ServerName != "" {
				names = append(names, sp.TLS.ServerName)
			}
		}
		for _, c := range certs {
			for _, name := range names {
				if c.Covers(name) {
					out[c.ID] = appendUnique(out[c.ID], n.Name)
					break
				}
			}
		}
	}
	for _, c := range certs {
		if out[c.ID] == nil {
			out[c.ID] = []string{}
		}
	}
	return out
}

func (h *handlers) issueCertificate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     string
		Names    []string
		DomainID *int64
	}
	if !decode(r, &in) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	if h.Certs == nil || !h.Certs.Available() {
		fail(w, http.StatusBadRequest, "certificate issuance is not available on this panel")
		return
	}
	c, err := h.Certs.Issue(r.Context(), in.Name, in.Names, in.DomainID)
	if err != nil {
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	c.CertPEM = ""
	ok(w, c)
}

func (h *handlers) renewCertificate(w http.ResponseWriter, r *http.Request) {
	if h.Certs == nil {
		fail(w, http.StatusBadRequest, "certificate issuance is not available on this panel")
		return
	}
	c, err := h.Certs.Renew(r.Context(), idOf(r))
	if err != nil {
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	c.CertPEM = ""
	ok(w, c)
}

func (h *handlers) updateCertificate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name      string
		AutoRenew *bool
	}
	if !decode(r, &in) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	c, err := h.Store.CertificateByID(r.Context(), idOf(r))
	if err != nil {
		fail(w, http.StatusNotFound, "certificate not found")
		return
	}
	if strings.TrimSpace(in.Name) != "" {
		c.Name = strings.TrimSpace(in.Name)
	}
	if in.AutoRenew != nil {
		c.AutoRenew = *in.AutoRenew
	}
	if err := h.Store.UpdateCertificateMeta(r.Context(), c.ID, c.Name, c.AutoRenew); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	c.CertPEM = ""
	ok(w, c)
}

// certificateDetail includes the public certificate chain (never the key)
// and the nodes using it.
func (h *handlers) certificateDetail(w http.ResponseWriter, r *http.Request) {
	c, err := h.Store.CertificateByID(r.Context(), idOf(r))
	if err != nil {
		fail(w, http.StatusNotFound, "certificate not found")
		return
	}
	ok(w, map[string]any{"certificate": c, "nodes": h.deployments(r)[c.ID]})
}

func (h *handlers) uploadCertificate(w http.ResponseWriter, r *http.Request) {
	var in struct{ Domain, CertPEM, KeyPEM string }
	if !decode(r, &in) || in.CertPEM == "" || in.KeyPEM == "" {
		fail(w, http.StatusBadRequest, "cert_pem and key_pem are required")
		return
	}
	c, err := store.ParseCertificate(in.Domain, in.CertPEM, in.KeyPEM)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	c.Source = "upload"
	if err := h.Store.UpsertCertificate(r.Context(), c); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	c.CertPEM = ""
	ok(w, c)
}

func (h *handlers) deleteCertificate(w http.ResponseWriter, r *http.Request) {
	if err := h.Store.DeleteCertificate(r.Context(), idOf(r)); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]bool{"ok": true})
}

func (h *handlers) rotateCertHookToken(w http.ResponseWriter, r *http.Request) {
	s := certHookSettings{Token: auth.Token(24)}
	if err := h.Store.SetSetting(r.Context(), settingCertHook, s); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]string{"webhook_url": strings.TrimRight(h.BaseURL, "/") + "/api/hooks/certificate?token=" + s.Token})
}

// certificateWebhook accepts a renewed pair. The body is JSON with lenient
// key names so Certimate's template, acme.sh scripts and hand-written
// curls all fit:
//
//	{"domain": "*.example.com", "certificate": "<fullchain pem>", "private_key": "<key pem>"}
//
// The token comes from ?token= or a Bearer header.
func (h *handlers) certificateWebhook(w http.ResponseWriter, r *http.Request) {
	hook := h.certHook(r)
	got := r.URL.Query().Get("token")
	if got == "" {
		got = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	}
	if hook.Token == "" || got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(hook.Token)) != 1 {
		fail(w, http.StatusForbidden, "bad token")
		return
	}
	var raw map[string]any
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&raw); err != nil {
		fail(w, http.StatusBadRequest, "json body required")
		return
	}
	pick := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := raw[k]; ok {
				switch x := v.(type) {
				case string:
					if x != "" {
						return x
					}
				case []any:
					if len(x) > 0 {
						if s, ok := x[0].(string); ok {
							return s
						}
					}
				}
			}
		}
		return ""
	}
	domain := pick("domain", "domains", "name", "common_name", "commonName", "subject")
	if i := strings.IndexAny(domain, ",; "); i > 0 {
		domain = domain[:i]
	}
	certPEM := pick("certificate", "cert", "fullchain", "cert_pem", "certificate_pem", "certificatePem", "fullchain_pem")
	keyPEM := pick("private_key", "privateKey", "key", "key_pem", "private_key_pem", "privateKeyPem")
	c, err := store.ParseCertificate(domain, certPEM, keyPEM)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	c.Source = "webhook"
	if err := h.Store.UpsertCertificate(r.Context(), c); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if h.Log != nil {
		h.Log.Info("certificate received via webhook", "domain", c.Domain, "not_after", c.NotAfter)
	}
	ok(w, map[string]any{"domain": c.Domain, "names": c.Names, "not_after": c.NotAfter})
}
