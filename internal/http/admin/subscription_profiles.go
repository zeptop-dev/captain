package admin

import (
	"errors"
	"net/http"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/bosun/pkg/subdesign"
	"github.com/zeptop-dev/bosun/pkg/subscription"
	"github.com/zeptop-dev/captain/internal/service"
	"github.com/zeptop-dev/captain/internal/store"
)

func (h *handlers) registerSubscriptionProfiles(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/settings/sub-library/templates", h.requireAdmin(h.listNamedSubTemplates))
	mux.HandleFunc("POST /api/admin/settings/sub-library/templates/preview", h.requireAdmin(h.previewNamedSubTemplate))
	mux.HandleFunc("POST /api/admin/settings/sub-library/templates", h.requireAdmin(h.saveNamedSubTemplate))
	mux.HandleFunc("PUT /api/admin/settings/sub-library/templates/{id}", h.requireAdmin(h.saveNamedSubTemplate))
	mux.HandleFunc("DELETE /api/admin/settings/sub-library/templates/{id}", h.requireAdmin(h.deleteNamedSubTemplate))
	mux.HandleFunc("GET /api/admin/settings/sub-profiles", h.requireAdmin(h.listSubscriptionProfiles))
	mux.HandleFunc("POST /api/admin/settings/sub-profiles", h.requireAdmin(h.saveSubscriptionProfile))
	mux.HandleFunc("PUT /api/admin/settings/sub-profiles/{id}", h.requireAdmin(h.saveSubscriptionProfile))
	mux.HandleFunc("DELETE /api/admin/settings/sub-profiles/{id}", h.requireAdmin(h.deleteSubscriptionProfile))
	mux.HandleFunc("POST /api/admin/settings/sub-profiles/preview", h.requireAdmin(h.previewSubscriptionProfile))
	mux.HandleFunc("GET /api/admin/users/subscription-profiles", h.requireAdmin(h.subscriptionProfileChoices))
	mux.HandleFunc("GET /api/admin/users/{id}/subscription-profile", h.requireAdmin(h.getUserSubscriptionProfile))
	mux.HandleFunc("PUT /api/admin/users/{id}/subscription-profile", h.requireAdmin(h.setUserSubscriptionProfile))
}
func subscriptionProfileError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		fail(w, 404, "profile or template not found")
	case errors.Is(err, store.ErrProfileInUse):
		fail(w, 409, "remove assignments and default selection before deleting")
	case errors.Is(err, store.ErrSubscriptionProfile):
		fail(w, 400, "invalid profile, template or format binding")
	default:
		serverErr(w, err)
	}
}
func (h *handlers) listNamedSubTemplates(w http.ResponseWriter, r *http.Request) {
	v, err := h.Store.NamedSubTemplates(r.Context())
	if err != nil {
		serverErr(w, err)
		return
	}
	defaults := map[string]string{}
	for _, f := range subscription.TemplateNames() {
		defaults[f] = subscription.DefaultTemplate(f)
	}
	ok(w, map[string]any{"items": v, "defaults": defaults})
}
func (h *handlers) saveNamedSubTemplate(w http.ResponseWriter, r *http.Request) {
	var v store.NamedSubTemplate
	if !decode(r, &v) {
		fail(w, 400, "invalid template")
		return
	}
	v.ID = 0
	if r.Method == "PUT" {
		id, valid := pathID(r)
		if !valid {
			fail(w, 400, "invalid id")
			return
		}
		v.ID = id
	}
	if err := v.Validate(); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if err := h.Store.SaveNamedSubTemplate(r.Context(), &v); err != nil {
		subscriptionProfileError(w, err)
		return
	}
	ok(w, v)
}
func (h *handlers) deleteNamedSubTemplate(w http.ResponseWriter, r *http.Request) {
	if err := h.Store.DeleteNamedSubTemplate(r.Context(), idOf(r)); err != nil {
		subscriptionProfileError(w, err)
		return
	}
	ok(w, map[string]bool{"ok": true})
}
func (h *handlers) listSubscriptionProfiles(w http.ResponseWriter, r *http.Request) {
	v, err := h.Store.SubscriptionProfiles(r.Context())
	if err != nil {
		serverErr(w, err)
		return
	}
	ok(w, v)
}
func (h *handlers) saveSubscriptionProfile(w http.ResponseWriter, r *http.Request) {
	var p store.SubscriptionProfile
	if !decode(r, &p) {
		fail(w, 400, "invalid profile")
		return
	}
	p.ID = 0
	if r.Method == "PUT" {
		id, valid := pathID(r)
		if !valid {
			fail(w, 400, "invalid id")
			return
		}
		p.ID = id
	}
	if err := p.Validate(); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if err := h.Store.SaveSubscriptionProfile(r.Context(), &p); err != nil {
		subscriptionProfileError(w, err)
		return
	}
	ok(w, p)
}
func (h *handlers) deleteSubscriptionProfile(w http.ResponseWriter, r *http.Request) {
	if err := h.Store.DeleteSubscriptionProfile(r.Context(), idOf(r)); err != nil {
		subscriptionProfileError(w, err)
		return
	}
	ok(w, map[string]bool{"ok": true})
}
func (h *handlers) subscriptionProfileChoices(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.SubscriptionProfiles(r.Context())
	if err != nil {
		serverErr(w, err)
		return
	}
	out := []map[string]any{}
	for _, p := range list {
		out = append(out, map[string]any{"id": p.ID, "name": p.Name, "default": p.Default})
	}
	ok(w, out)
}
func (h *handlers) getUserSubscriptionProfile(w http.ResponseWriter, r *http.Request) {
	u, valid := h.customer(r)
	if !valid {
		fail(w, 404, "user not found")
		return
	}
	id, err := h.Store.AssignedSubscriptionProfile(r.Context(), u.ID)
	if err != nil {
		serverErr(w, err)
		return
	}
	effective, err := h.Store.SubscriptionProfileForUser(r.Context(), u.ID)
	if err != nil {
		serverErr(w, err)
		return
	}
	var label any
	if effective != nil {
		label = map[string]any{"id": effective.ID, "name": effective.Name, "page": effective.Settings.Page != nil && effective.Settings.Page.Enabled}
	}
	ok(w, map[string]any{"profile_id": id, "effective": label})
}
func (h *handlers) setUserSubscriptionProfile(w http.ResponseWriter, r *http.Request) {
	u, valid := h.customer(r)
	if !valid {
		fail(w, 404, "user not found")
		return
	}
	var in struct {
		ID *int64 `json:"profile_id"`
	}
	if !decode(r, &in) {
		fail(w, 400, "invalid profile")
		return
	}
	if in.ID != nil {
		if _, err := h.Store.SubscriptionProfile(r.Context(), *in.ID); err != nil {
			subscriptionProfileError(w, err)
			return
		}
	}
	if err := h.Store.AssignSubscriptionProfile(r.Context(), u.ID, in.ID); err != nil {
		serverErr(w, err)
		return
	}
	h.getUserSubscriptionProfile(w, r)
}
func (h *handlers) previewSubscriptionProfile(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Profile store.SubscriptionProfile `json:"profile"`
		UserID  int64                     `json:"user_id"`
		Format  string                    `json:"format"`
	}
	if !decode(r, &in) {
		fail(w, 400, "invalid preview")
		return
	}
	if err := in.Profile.Validate(); err != nil {
		fail(w, 400, err.Error())
		return
	}
	u, err := h.Store.UserByID(r.Context(), in.UserID)
	if err != nil {
		fail(w, 404, "user not found")
		return
	}
	svc := &service.Subscription{Store: h.Store}
	published, acct, err := svc.PublishedLinesWithProfile(r.Context(), u, time.Now(), &in.Profile)
	if errors.Is(err, service.ErrDisabled) {
		fail(w, 409, "user is banned")
		return
	}
	if err != nil && !errors.Is(err, service.ErrNoAccess) {
		serverErr(w, err)
		return
	}
	rd := subscription.Pick(in.Format, "")
	var body string
	if id := in.Profile.Templates[rd.Name()]; id > 0 {
		tpl, e := h.Store.NamedSubTemplate(r.Context(), id)
		if e != nil {
			subscriptionProfileError(w, e)
			return
		}
		if tpl.Format != rd.Name() {
			fail(w, 400, "template format mismatch")
			return
		}
		body = tpl.Body
	} else {
		var global store.SubTemplates
		if e := h.Store.GetSetting(r.Context(), store.SettingSubTemplates, &global); e != nil {
			serverErr(w, e)
			return
		}
		body = global[rd.Name()]
	}
	if rd.Name() == "qx" || rd.Name() == "loon" {
		body, err = subdesign.BindResourceURLs(body, h.BaseURL)
		if err != nil {
			fail(w, 400, "template contains invalid managed resources")
			return
		}
	}
	lines, err := service.ClientLines(published, rd.Name(), body)
	if err != nil {
		serverErr(w, err)
		return
	}
	rendered, err := rd.RenderWith(lines, acct, body)
	if errors.Is(err, subscription.ErrSurgeDNSHostname) {
		fail(w, 422, "Surge DNS protection requires IP-address proxy endpoints")
		return
	}
	if err != nil {
		fail(w, 400, "template cannot render this user")
		return
	}
	ok(w, map[string]any{"format": rd.Name(), "body": string(rendered), "lines": len(lines), "headers": in.Profile.Settings.Headers})
}

func (h *handlers) previewNamedSubTemplate(w http.ResponseWriter, r *http.Request) {
	var v store.NamedSubTemplate
	if !decode(r, &v) {
		fail(w, 400, "invalid template")
		return
	}
	if err := v.Validate(); err != nil {
		fail(w, 400, err.Error())
		return
	}
	sample := subscription.Line{Name: "Example", Host: "node.example.com", Port: 443, UUID: "00000000-0000-4000-8000-000000000001", UserID: 1, Inbound: spec.Inbound{Tag: "example", Protocol: spec.VLESS, Port: 443}}
	body := v.Body
	if v.Format == "qx" || v.Format == "loon" {
		var err error
		body, err = subdesign.BindResourceURLs(body, h.BaseURL)
		if err != nil {
			fail(w, 400, "template contains invalid managed resources")
			return
		}
	}
	b, err := subscription.Pick(v.Format, "").RenderWith([]subscription.Line{sample}, subscription.Account{}, body)
	if err != nil {
		fail(w, 400, "template cannot render")
		return
	}
	ok(w, map[string]string{"body": string(b)})
}
