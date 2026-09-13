package admin

import (
	"encoding/json"

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
	for i := range list {
		list[i].CertPEM = ""
	}
	hook := h.certHook(r)
	url := ""
	if hook.Token != "" {
		url = strings.TrimRight(h.BaseURL, "/") + "/api/hooks/certificate?token=" + hook.Token
	}
	ok(w, map[string]any{"certificates": list, "webhook_url": url})
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
	if hook.Token == "" || got == "" || got != hook.Token {
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
