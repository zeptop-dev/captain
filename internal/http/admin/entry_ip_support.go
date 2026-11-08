package admin

import (
	"errors"
	"net/http"
	"strings"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/bosun/pkg/subscription"
	"github.com/zeptop-dev/captain/internal/store"
)

// entryIPSupport previews only format capabilities and authorized global
// template conflicts. It does not resolve a hostname or test network access.
func (h *handlers) entryIPSupport(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	host, mode := q.Get("host"), q.Get("ip_version")
	if len(host) > 253 || subscription.ValidateIPVersion(host, mode, nil) != nil {
		fail(w, 400, "invalid connection IP policy")
		return
	}
	ib := spec.Inbound{Protocol: spec.Protocol(q.Get("protocol")), Cipher: "aes-128-gcm"}
	line := subscription.Line{Name: "IP policy preview", Host: host, Port: 443, Inbound: ib, IPVersion: mode}
	type support struct {
		Format    string `json:"format"`
		Supported bool   `json:"supported"`
		Reason    string `json:"reason"`
	}
	out := struct {
		Formats           []support `json:"formats"`
		TemplateConflicts []string  `json:"template_conflicts"`
		TemplatesChecked  bool      `json:"templates_checked"`
	}{Formats: []support{}, TemplateConflicts: []string{}}
	for _, format := range []string{"clash", "singbox", "surge", "stash", "surfboard", "loon", "qx", "egern", "uri", "wireguard"} {
		if format == "wireguard" && ib.Protocol != spec.WireGuard {
			continue
		}
		reason := subscription.IPVersionSupport(format, line)
		ok := reason == ""
		if ok && subscription.IPVersionUsesClientDefault(format, line) {
			reason = "client_native"
		}
		out.Formats = append(out.Formats, support{format, ok, reason})
	}
	// Knowing an entry does not grant read access to the operator's templates.
	path := "/api/admin/settings/sub-templates"
	canRead := allowed(userFrom(r).Role, http.MethodGet, path)
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer"))
	if canRead && strings.HasPrefix(token, "cap_") {
		owner, scope, err := h.Store.ReadAPIToken(r.Context(), token)
		if err != nil {
			serverErr(w, err)
			return
		}
		canRead = owner != nil && owner.IsAdmin() && owner.Status == "active" && store.TokenAllowsRequest(scope, http.MethodGet, path, "GET "+path)
	}
	if canRead {
		var templates store.SubTemplates
		if err := h.Store.GetSetting(r.Context(), store.SettingSubTemplates, &templates); err != nil {
			serverErr(w, err)
			return
		}
		out.TemplatesChecked = true
		// IPv6 admission is a document setting independent of the proxy protocol.
		// A supported placeholder prevents an unsupported protocol hiding a conflict.
		probe := line
		probe.Inbound.Protocol = spec.Shadowsocks
		for _, format := range []string{"clash", "surge"} {
			_, err := subscription.Pick(format, "").RenderWith([]subscription.Line{probe}, subscription.Account{}, templates[format])
			if errors.Is(err, subscription.ErrIPVersionTemplateConflict) {
				out.TemplateConflicts = append(out.TemplateConflicts, format)
			}
		}
	}
	ok(w, out)
}
