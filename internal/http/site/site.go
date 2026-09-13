// Package site serves the public landing page data: the admin-edited copy,
// node locations for the globe and a few counts.
package site

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/zeptop-dev/captain/internal/store"
)

// Settings is the landing page content, stored under SettingSite.
type Settings struct {
	Name        string     `json:"name"`
	Tagline     string     `json:"tagline"`
	Description string     `json:"description"`
	Features    []Feature  `json:"features"`
	Locations   []Location `json:"locations"`
	Hub         *Location  `json:"hub,omitempty"`
	FAQ         []FAQ      `json:"faq"`
	Links       Links      `json:"links"`
	ShowPlans   bool       `json:"show_plans"`
	// Theme is shared by the landing page and the portal.
	Theme Theme `json:"theme"`
	// InjectHead / InjectBody are raw HTML the operator adds to every page
	// (analytics, chat widgets). Admin-only input; served verbatim.
	InjectHead string `json:"inject_head"`
	InjectBody string `json:"inject_body"`
}

// Theme is the operator-chosen look: a Mantine palette name for the
// primary colour, a radius token and a colour scheme.
type Theme struct {
	Primary     string `json:"primary"`      // cyan (default), blue, indigo, violet, grape, pink, red, orange, yellow, lime, green, teal
	Radius      string `json:"radius"`       // xs, sm, md, lg (default), xl
	Scheme      string `json:"scheme"`       // portal: "light" (default), "dark", "auto"
	SiteScheme  string `json:"site_scheme"`  // landing: "dark" (default), "light", "auto"
	FontFamily  string `json:"font_family"`  // optional CSS font stack
	PortalTitle string `json:"portal_title"` // brand text in the portal header; default = site name
}

// Feature is one selling point.
type Feature struct {
	Title string `json:"title"`
	Text  string `json:"text"`
	Icon  string `json:"icon,omitempty"` // bolt, shield, world, devices, refresh, headset
}

// Location is a point on the globe.
type Location struct {
	Name string  `json:"name"`
	Lat  float64 `json:"lat"`
	Lng  float64 `json:"lng"`
	Tag  string  `json:"tag,omitempty"`
}

// FAQ is one question.
type FAQ struct {
	Q string `json:"q"`
	A string `json:"a"`
}

// Links are the footer/header links.
type Links struct {
	Telegram string `json:"telegram,omitempty"`
	TOS      string `json:"tos,omitempty"`
	Download string `json:"download,omitempty"`
	GitHub   string `json:"github,omitempty"`
}

// SettingSite is the settings key.
const SettingSite = "site"

// Deps wires the handler.
type Deps struct {
	Store        *store.Store
	SiteName     string // config site_name, the default for Settings.Name
	Registration bool
}

// Defaults returns the page shown before an admin edits anything.
func Defaults(name string) Settings {
	return Settings{
		Name: name, Tagline: "稳定、快速、省心的网络加速", Description: "多地区节点，自动优选线路，一条订阅链接覆盖所有设备。",
		Features: []Feature{
			{Title: "多协议", Text: "VLESS、Hysteria2、mieru 等主流协议，客户端一键导入。", Icon: "bolt"},
			{Title: "全球节点", Text: "覆盖多个地区，按需选择延迟最低的线路。", Icon: "world"},
			{Title: "隐私优先", Text: "不记录访问内容，仅统计流量用量。", Icon: "shield"},
			{Title: "多设备", Text: "手机、电脑、路由器共用一个订阅。", Icon: "devices"},
			{Title: "自动更新", Text: "节点变动自动同步到订阅，无需手动操作。", Icon: "refresh"},
			{Title: "随时支持", Text: "遇到问题随时联系我们。", Icon: "headset"},
		},
		Locations: []Location{{Name: "Tokyo", Lat: 35.68, Lng: 139.69}, {Name: "Hong Kong", Lat: 22.32, Lng: 114.17}, {Name: "Los Angeles", Lat: 34.05, Lng: -118.24}},
		Hub:       &Location{Name: "Shanghai", Lat: 31.23, Lng: 121.47},
		FAQ:       []FAQ{{Q: "支持哪些客户端？", A: "Clash 系、sing-box、Shadowrocket、Surge、v2rayN 等，导入订阅链接即可。"}},
		ShowPlans: true,
	}
}

// Register mounts GET /api/site.
func Register(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("GET /api/site", func(w http.ResponseWriter, r *http.Request) {
		s := Defaults(d.SiteName)
		_ = d.Store.GetSetting(r.Context(), SettingSite, &s)
		if s.Name == "" {
			s.Name = d.SiteName
		}
		if s.Features == nil {
			s.Features = []Feature{}
		}
		if s.Locations == nil {
			s.Locations = []Location{}
		}
		if s.FAQ == nil {
			s.FAQ = []FAQ{}
		}
		out := map[string]any{
			"name": s.Name, "tagline": s.Tagline, "description": s.Description, "features": s.Features, "locations": s.Locations,
			"hub": s.Hub, "faq": s.FAQ, "links": s.Links, "show_plans": s.ShowPlans, "registration": d.Registration,
			"stats": stats(r.Context(), d.Store, len(s.Locations)),
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=60")
		_ = json.NewEncoder(w).Encode(out)
	})
}

func stats(ctx context.Context, st *store.Store, locations int) map[string]int {
	nodes, _ := st.ListNodes(ctx)
	online := 0
	now := time.Now()
	for _, n := range nodes {
		if n.LastSeenAt != nil && now.Sub(*n.LastSeenAt) < 3*time.Minute {
			online++
		}
	}
	return map[string]int{"nodes": len(nodes), "online": online, "locations": locations}
}
