package sub

import (
	"bytes"
	"html/template"
	"net/http"
	"net/url"
	"time"

	"github.com/zeptop-dev/bosun/pkg/subscription"
	"github.com/zeptop-dev/captain/internal/store"
)

var profilePage = template.Must(template.New("subscription").Parse(`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="referrer" content="no-referrer"><title>{{.Title}}</title><style>body{font:16px system-ui,sans-serif;background:#f5f6fa;color:#202330;margin:0;padding:24px}main{max-width:640px;margin:8vh auto;background:white;border:1px solid #e4e6ed;border-radius:20px;padding:32px}h1{margin-top:0;overflow-wrap:anywhere}p{line-height:1.6;white-space:pre-wrap;overflow-wrap:anywhere}nav{display:grid;grid-template-columns:repeat(auto-fit,minmax(160px,1fr));gap:12px}a{display:block;padding:14px;border-radius:10px;border:1px solid #e4e6ed;color:{{.Accent}};text-decoration:none}small{color:#667085}dl{display:grid;grid-template-columns:1fr 1fr;gap:8px}dd{text-align:right;margin:0}@media(prefers-color-scheme:dark){body{background:#161820;color:#f0f1f5}main{background:#20232d;border-color:#3a3e4b}a{border-color:#3a3e4b;color:#b8b4ff}small{color:#aeb5c4}}</style></head><body><main><h1>{{.Title}}</h1><p>{{.Description}}</p><dl><dt>{{.UsageLabel}}</dt><dd>{{.Usage}}</dd><dt>{{.ExpiryLabel}}</dt><dd>{{.Expiry}}</dd></dl><nav>{{range .Links}}<a href="{{.URL}}" rel="noreferrer">{{.Name}}</a>{{end}}</nav><p><small>{{.Hint}}</small></p></main></body></html>`))

func renderProfilePage(r *http.Request, p *store.ProfilePage, acct subscription.Account) ([]byte, error) {
	type link struct{ Name, URL string }
	links := []link{}
	for _, f := range []struct{ name, format string }{{"mihomo / Clash Meta", "clash"}, {"sing-box", "singbox"}, {"Surge", "surge"}, {"Surge VLESS (iOS 6+ / Mac 6.12+)", "surge-vless"}, {"Stash", "stash"}, {"Loon", "loon"}, {"Quantumult X", "qx"}, {"URI", "uri"}} {
		u := url.URL{Path: r.URL.Path}
		q := r.URL.Query()
		q.Set("client", f.format)
		u.RawQuery = q.Encode()
		links = append(links, link{f.name, u.String()})
	}
	title := p.Title
	if title == "" {
		title = "Subscription"
	}
	accent := p.Accent
	if accent == "" {
		accent = "#5046e5"
	}
	usage := bytesLabel(acct.Upload + acct.Download)
	if acct.Total > 0 {
		usage += " / " + bytesLabel(acct.Total)
	}
	expiry := "∞"
	if acct.Expire > 0 {
		expiry = time.Unix(acct.Expire, 0).UTC().Format("2006-01-02")
	}
	labels := profilePageLabels(r.Header.Get("Accept-Language"))
	data := map[string]any{"Title": title, "Description": p.Description, "Accent": accent, "Links": links, "Usage": usage, "Expiry": expiry, "UsageLabel": labels[0], "ExpiryLabel": labels[1], "Hint": labels[2]}
	var b bytes.Buffer
	err := profilePage.Execute(&b, data)
	return b.Bytes(), err
}
