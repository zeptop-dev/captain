package clientcatalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"strings"
	"time"
)

// Source is injectable only through server construction (isolated tests), not
// through configuration or an HTTP request.
type Source interface {
	Check(context.Context, string) (*Release, bool, error)
	Download(context.Context, Asset, io.Writer) (string, error)
}
type GitHubSource struct{ client *http.Client }

func NewSource() *GitHubSource {
	tr := &http.Transport{Proxy: nil, DialContext: publicDial, ForceAttemptHTTP2: true, MaxIdleConns: 4, MaxConnsPerHost: 2, IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 20 * time.Second, MaxResponseHeaderBytes: 64 << 10}
	return &GitHubSource{client: &http.Client{Transport: tr, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !allowedSourceURL(r.URL) {
			return ErrUnavailable
		}
		r.Header.Del("Authorization")
		return nil
	}}}
}
func publicAddress(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	for _, cidr := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "64:ff9b::/96", "64:ff9b:1::/48", "2002::/16", "2001::/32"} {
		if netip.MustParsePrefix(cidr).Contains(ip) {
			return false
		}
	}
	return true
}
func publicDial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || port != "443" {
		return nil, ErrUnavailable
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return nil, ErrUnavailable
	}
	// Reject the complete answer set if any address is private. Pin dialing to
	// the checked addresses, so a second DNS answer cannot rebind the request.
	for _, ip := range ips {
		if !publicAddress(ip) {
			return nil, ErrUnavailable
		}
	}
	d := net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	for _, ip := range ips {
		c, e := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if e == nil {
			return c, nil
		}
	}
	return nil, ErrUnavailable
}
func allowedSourceURL(u *url.URL) bool {
	if u == nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	switch u.Hostname() {
	case "api.github.com", "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com":
		return true
	}
	return false
}
func (c *GitHubSource) get(ctx context.Context, raw string) (*http.Response, error) {
	u, e := url.Parse(raw)
	if e != nil || !allowedSourceURL(u) {
		return nil, ErrUnavailable
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if e != nil {
		return nil, ErrUnavailable
	}
	req.Header.Set("User-Agent", "Captain-client-catalog")
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, e := c.client.Do(req)
	if e != nil {
		return nil, ErrUnavailable
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, ErrUnavailable
	}
	return resp, nil
}
func (c *GitHubSource) json(ctx context.Context, raw string, out any) error {
	r, e := c.get(ctx, raw)
	if e != nil {
		return e
	}
	defer r.Body.Close()
	b, e := io.ReadAll(io.LimitReader(r.Body, (2<<20)+1))
	if e != nil || len(b) > 2<<20 {
		return ErrUnavailable
	}
	if json.Unmarshal(b, out) != nil {
		return ErrUnavailable
	}
	return nil
}
func (c *GitHubSource) Check(parent context.Context, id string) (*Release, bool, error) {
	p, ok := Find(id)
	if !ok || p.Repository == "" {
		return nil, false, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()
	var repo struct {
		FullName string `json:"full_name"`
		Archived bool   `json:"archived"`
	}
	if e := c.json(ctx, "https://api.github.com/repos/"+p.Repository, &repo); e != nil {
		return nil, false, e
	}
	if !strings.EqualFold(repo.FullName, p.Repository) {
		return nil, false, ErrUnavailable
	}
	var rel struct {
		Tag         string    `json:"tag_name"`
		URL         string    `json:"html_url"`
		Draft       bool      `json:"draft"`
		Prerelease  bool      `json:"prerelease"`
		PublishedAt time.Time `json:"published_at"`
		Assets      []struct {
			ID     int64  `json:"id"`
			Name   string `json:"name"`
			Size   int64  `json:"size"`
			URL    string `json:"browser_download_url"`
			Digest string `json:"digest"`
		} `json:"assets"`
	}
	if e := c.json(ctx, "https://api.github.com/repos/"+p.Repository+"/releases/latest", &rel); e != nil {
		return nil, false, e
	}
	if rel.Draft || rel.Prerelease || len(rel.Tag) < 1 || len(rel.Tag) > 128 || strings.ContainsAny(rel.Tag, "\r\n\x00") || rel.PublishedAt.IsZero() || len(rel.Assets) > 512 {
		return nil, false, ErrUnavailable
	}
	parsed, e := url.Parse(rel.URL)
	if e != nil || parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || !strings.HasPrefix(parsed.Path, "/"+p.Repository+"/releases/tag/") {
		return nil, false, ErrUnavailable
	}
	out := &Release{Version: rel.Tag, URL: rel.URL, PublishedAt: rel.PublishedAt, Assets: []Asset{}}
	seen := map[int64]bool{}
	for _, a := range rel.Assets {
		platform, arch := ClassifyAsset(p, a.Name)
		if platform == "" {
			continue
		}
		if a.ID <= 0 || seen[a.ID] || a.Size <= 0 || a.Size > MaxAssetBytes || !validAssetURL(p, a.URL, a.Name, rel.Tag) || !validDigest(a.Digest) {
			return nil, false, ErrUnavailable
		}
		seen[a.ID] = true
		typ := strings.TrimPrefix(strings.ToLower(path.Ext(a.Name)), ".")
		if strings.HasSuffix(strings.ToLower(a.Name), ".tar.gz") {
			typ = "tar.gz"
		}
		out.Assets = append(out.Assets, Asset{ID: a.ID, Name: a.Name, Platform: platform, Arch: arch, PackageType: typ, Size: a.Size, URL: a.URL, Version: rel.Tag, Digest: a.Digest})
	}
	if len(out.Assets) == 0 {
		return nil, false, ErrUnavailable
	}
	return out, repo.Archived, nil
}
func validDigest(d string) bool {
	if d == "" {
		return true
	}
	if !strings.HasPrefix(d, "sha256:") {
		return false
	}
	b, e := hex.DecodeString(strings.TrimPrefix(d, "sha256:"))
	return e == nil && len(b) == 32
}
func validAssetURL(p Project, raw, name, version string) bool {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	return u.Path == "/"+p.Repository+"/releases/download/"+version+"/"+name
}
func (c *GitHubSource) Download(parent context.Context, a Asset, w io.Writer) (string, error) {
	if a.Size <= 0 || a.Size > MaxAssetBytes || !ValidFilename(a.Name) || !validDigest(a.Digest) {
		return "", ErrInvalid
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Minute)
	defer cancel()
	r, e := c.get(ctx, a.URL)
	if e != nil {
		return "", e
	}
	defer r.Body.Close()
	if r.ContentLength > MaxAssetBytes || r.ContentLength >= 0 && r.ContentLength != a.Size {
		return "", ErrTooLarge
	}
	hash := sha256.New()
	n, e := io.Copy(io.MultiWriter(w, hash), io.LimitReader(r.Body, a.Size+1))
	if e != nil {
		return "", ErrUnavailable
	}
	if n != a.Size {
		return "", ErrUnavailable
	}
	digest := fmt.Sprintf("%x", hash.Sum(nil))
	if a.Digest != "" && !strings.EqualFold(a.Digest, "sha256:"+digest) {
		return "", errors.New("official package checksum did not match")
	}
	return digest, nil
}
