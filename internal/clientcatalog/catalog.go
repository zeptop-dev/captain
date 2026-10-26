// Package clientcatalog describes the fixed official download sources. It does
// not execute downloaded programs or turn a new upstream release into a public
// download without an administrator explicitly publishing it.
package clientcatalog

import (
	"errors"
	"net/url"
	"path"
	"regexp"
	"strings"
)

const MaxAssetBytes int64 = 1 << 30
const MaxCacheBytes int64 = 5 << 30
const MaxConcurrentDownloads = 1

var ErrInvalid = errors.New("invalid client catalog selection")
var ErrUnavailable = errors.New("official source unavailable")
var ErrTooLarge = errors.New("client package exceeds the download limit")
var ErrMissingCache = errors.New("selected cached package is missing; download it again")
var ErrAlreadyCached = errors.New("this package is already cached")
var ErrReferenced = errors.New("package is still published; unpublish it first")
var ErrBusy = errors.New("client downloads are busy")
var ErrConflict = errors.New("client catalog changed; reload before saving")

type PlatformLink struct {
	Platform    string `json:"platform"`
	URL         string `json:"url"`
	Kind        string `json:"kind"`
	Recommended bool   `json:"recommended"`
}
type Project struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Repository    string         `json:"repository,omitempty"`
	Website       string         `json:"website"`
	Format        string         `json:"format"`
	Configuration string         `json:"configuration"`
	Platforms     []PlatformLink `json:"platforms"`
}

// Catalog is deliberately curated. An admin may add external links through
// the legacy custom-client editor, but cannot make Captain fetch an arbitrary
// URL, Git repository, private host, or source code archive.
func Catalog() []Project {
	return []Project{
		{ID: "clash-verge-rev", Name: "Clash Verge Rev", Repository: "clash-verge-rev/clash-verge-rev", Website: "https://www.clashverge.dev/", Format: "clash", Configuration: "full", Platforms: []PlatformLink{{"windows", "https://www.clashverge.dev/install.html", "official", true}, {"macos", "https://www.clashverge.dev/install.html", "official", true}, {"linux", "https://www.clashverge.dev/install.html", "official", true}}},
		{ID: "v2rayn", Name: "v2rayN", Repository: "2dust/v2rayN", Website: "https://github.com/2dust/v2rayN", Format: "uri", Configuration: "nodes", Platforms: []PlatformLink{{"windows", "https://github.com/2dust/v2rayN/releases/latest", "official", false}, {"linux", "https://github.com/2dust/v2rayN/releases/latest", "official", false}}},
		{ID: "flclash", Name: "FlClash", Repository: "chen08209/FlClash", Website: "https://github.com/chen08209/FlClash", Format: "clash", Configuration: "full", Platforms: []PlatformLink{{"android", "https://github.com/chen08209/FlClash/releases/latest", "official", true}}},
		{ID: "v2rayng", Name: "v2rayNG", Repository: "2dust/v2rayNG", Website: "https://github.com/2dust/v2rayNG", Format: "uri", Configuration: "nodes", Platforms: []PlatformLink{{"android", "https://github.com/2dust/v2rayNG/releases/latest", "official", false}}},
		{ID: "stash", Name: "Stash", Website: "https://stash.ws/download", Format: "stash", Configuration: "full", Platforms: []PlatformLink{{"macos", "https://stash.ws/download", "official", false}, {"ios", "https://apps.apple.com/app/id1596063349", "store", true}}},
		{ID: "surge", Name: "Surge", Website: "https://nssurge.com/", Format: "surge", Configuration: "full", Platforms: []PlatformLink{{"macos", "https://nssurge.com/", "official", false}, {"ios", "https://apps.apple.com/app/id1442620678", "store", false}}},
		{ID: "shadowrocket", Name: "Shadowrocket", Website: "https://apps.apple.com/app/id932747118", Format: "uri", Configuration: "nodes", Platforms: []PlatformLink{{"ios", "https://apps.apple.com/app/id932747118", "store", false}}},
	}
}
func Find(id string) (Project, bool) {
	for _, p := range Catalog() {
		if p.ID == id {
			return p, true
		}
	}
	return Project{}, false
}
func (p Project) Supports(platform string) bool {
	for _, v := range p.Platforms {
		if v.Platform == platform {
			return true
		}
	}
	return false
}
func (p Project) Link(platform string) string {
	for _, v := range p.Platforms {
		if v.Platform == platform {
			return v.URL
		}
	}
	return ""
}

var safeFilename = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+() -]{0,199}$`)

func ValidFilename(name string) bool {
	return safeFilename.MatchString(name) && !strings.Contains(name, "..") && path.Base(name) == name
}
func ValidHTTPS(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.Fragment == "" && (u.Port() == "" || u.Port() == "443")
}

// ClassifyAsset only offers recognized distributable installers/packages.
// Signatures, checksums, source archives, and unknown names are never cached.
func ClassifyAsset(p Project, name string) (platform, arch string) {
	if !ValidFilename(name) {
		return "", ""
	}
	n := strings.ToLower(name)
	switch {
	case strings.HasSuffix(n, ".apk"):
		platform = "android"
	case strings.HasSuffix(n, ".exe") || strings.HasSuffix(n, ".msi") || strings.HasSuffix(n, ".msix") || strings.HasSuffix(n, ".zip") && strings.Contains(n, "windows"):
		platform = "windows"
	case strings.HasSuffix(n, ".dmg") || strings.HasSuffix(n, ".pkg"):
		platform = "macos"
	case strings.HasSuffix(n, ".deb") || strings.HasSuffix(n, ".rpm") || strings.HasSuffix(n, ".appimage") || strings.HasSuffix(n, ".zip") && strings.Contains(n, "linux") || strings.HasSuffix(n, ".tar.gz") && strings.Contains(n, "linux"):
		platform = "linux"
	default:
		return "", ""
	}
	if !p.Supports(platform) {
		return "", ""
	}
	switch {
	case strings.Contains(n, "arm64") || strings.Contains(n, "aarch64") || strings.Contains(n, "arm64-v8a"):
		arch = "arm64"
	case strings.Contains(n, "loong64"):
		arch = "loong64"
	case strings.Contains(n, "riscv64"):
		arch = "riscv64"
	case strings.Contains(n, "x86_64") || strings.Contains(n, "amd64") || strings.Contains(n, "x64") || strings.Contains(n, "-64.") || strings.Contains(n, "-64-"):
		arch = "x64"
	case strings.Contains(n, "armeabi") || strings.Contains(n, "armv7") || strings.Contains(n, "armhf"):
		arch = "armv7"
	case strings.Contains(n, "x86") || strings.Contains(n, "ia32") || strings.Contains(n, "i386") || strings.Contains(n, "-86.") || strings.Contains(n, "-86-"):
		arch = "x86"
	case strings.Contains(n, "universal"):
		arch = "universal"
	default:
		return "", ""
	}
	return
}
