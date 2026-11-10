package admin

import (
	"errors"
	"net/http"
	"strings"

	"github.com/zeptop-dev/captain/internal/mail"
)

func (h *handlers) getMailTemplates(w http.ResponseWriter, r *http.Request) {
	var d mail.Design
	if err := h.Store.GetSetting(r.Context(), mail.DesignKey, &d); err != nil {
		serverErr(w, err)
		return
	}
	defaults := map[string]map[string]mail.Template{}
	for _, l := range mail.Languages {
		defaults[l.Code] = mail.For(l.Code).Defaults()
	}
	ok(w, map[string]any{"design": d, "defaults": defaults, "variables": mail.Variables})
}
func (h *handlers) putMailTemplates(w http.ResponseWriter, r *http.Request) {
	var d mail.Design
	r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
	if !readJSON(w, r, &d) {
		return
	}
	if err := d.Validate(); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if err := h.Store.SetSetting(r.Context(), mail.DesignKey, d); err != nil {
		serverErr(w, err)
		return
	}
	if h.Mail != nil {
		h.Mail.Invalidate()
	}
	h.getMailTemplates(w, r)
}

var errMailLanguage = errors.New("unsupported mail language")

type mailSampleInput struct {
	Kind     string       `json:"kind"`
	Language string       `json:"language"`
	Design   *mail.Design `json:"design"`
}

func (h *handlers) sampleMail(r *http.Request, in mailSampleInput, to string) (mail.Message, error) {
	ms := h.Mail.Settings(r.Context())
	if in.Design != nil {
		if err := in.Design.Validate(); err != nil {
			return mail.Message{}, err
		}
		ms.Design = *in.Design
	}
	lang := in.Language
	if lang == "" {
		lang = ms.Language
	}
	if lang != "" && !mail.Supported(lang) {
		return mail.Message{}, errMailLanguage
	}
	return mail.Configured(ms, lang).Sample(in.Kind, h.SiteName, to, strings.TrimRight(h.BaseURL, "/")+"/portal/")
}
func (h *handlers) previewMail(w http.ResponseWriter, r *http.Request) {
	var in mailSampleInput
	r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
	if !readJSON(w, r, &in) {
		return
	}
	if h.Mail == nil {
		fail(w, 409, "mail disabled")
		return
	}
	m, err := h.sampleMail(r, in, "preview@example.com")
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	ok(w, map[string]string{"subject": m.Subject, "html": m.HTML})
}
