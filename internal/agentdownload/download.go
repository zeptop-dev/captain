// Package agentdownload serves a bounded cache of official bosun and managed
// core artifacts. Callers select catalog identities, never upstream URLs.
package agentdownload

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	nodedownload "github.com/zeptop-dev/bosun/pkg/agentdownload"
	"github.com/zeptop-dev/bosun/pkg/coremanifest"
	"github.com/zeptop-dev/bosun/pkg/selfupdate"
)

const (
	MinVersion  = nodedownload.MinVersion
	Prefix      = "/api/agent/downloads"
	maxMetadata = 2 << 20
	maxArtifact = 256 << 20
	maxCache    = 2 << 30
	cacheAge    = 7 * 24 * time.Hour
)

var ErrNotFound = errors.New("unsupported artifact")
var versionPattern = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// Service owns only this dedicated directory. HTTP is injectable for tests;
// even injected clients retain upstream and redirect host checks.
type Service struct {
	Dir      string
	HTTP     *http.Client
	mu       sync.Mutex
	metadata map[string]metadata
	flights  map[string]chan struct{}
	slots    chan struct{}
}

type metadata struct {
	Data []byte
	At   time.Time
	Err  error `json:"-"`
}
type File struct {
	Path           string `json:"-"`
	SHA256         string `json:"sha256"`
	Size           int64  `json:"size"`
	SourceVerified bool   `json:"source_verified"`
}

type Release struct {
	TagName     string         `json:"tag_name"`
	Name        string         `json:"name"`
	Body        string         `json:"body"`
	HTMLURL     string         `json:"html_url"`
	PublishedAt time.Time      `json:"published_at"`
	Draft       bool           `json:"draft"`
	Prerelease  bool           `json:"prerelease"`
	Assets      []ReleaseAsset `json:"assets"`
}
type ReleaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

func New(dir string) *Service {
	return &Service{Dir: dir, metadata: make(map[string]metadata), flights: make(map[string]chan struct{}), slots: make(chan struct{}, 2)}
}

func supportedVersion(v string) bool {
	return len(v) < 64 && versionPattern.MatchString(v) && !selfupdate.Newer(MinVersion, v)
}

func supportedAsset(n string) bool {
	return n == "SHA256SUMS" || n == "bosun-linux-amd64" || n == "bosun-linux-arm64"
}

func allowedURL(raw string) bool {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.Port() != "" {
		return false
	}
	switch u.Host {
	case "api.github.com", "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com", "github-releases.githubusercontent.com", "dl.nssurge.com":
		return true
	}
	return false
}

func (s *Service) get(ctx context.Context, raw string) (*http.Response, error) {
	if !allowedURL(raw) {
		return nil, fmt.Errorf("upstream URL is not allowed")
	}
	c := http.Client{Timeout: 10 * time.Minute}
	if s.HTTP != nil {
		c = *s.HTTP
	}
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !allowedURL(req.URL.String()) {
			return fmt.Errorf("upstream redirect is not allowed")
		}
		// Node credentials and private registry credentials are never upstream headers.
		req.Header.Del("Authorization")
		req.Header.Del("X-Bosun-Pair-Code")
		return nil
	}
	r, e := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if e != nil {
		return nil, e
	}
	r.Header.Set("User-Agent", "Captain-agent-downloads")
	r.Header.Set("Accept", "application/octet-stream, application/json")
	resp, e := c.Do(r)
	if e != nil {
		return nil, e
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("upstream returned HTTP %d", resp.StatusCode)
	}
	return resp, nil
}

func (s *Service) small(ctx context.Context, raw string) (data []byte, err error) {
	done, err := s.begin(ctx, "metadata:"+raw)
	if err != nil {
		return nil, err
	}
	defer done()
	s.mu.Lock()
	m, ok := s.metadata[raw]
	s.mu.Unlock()
	ttl := 20 * time.Minute
	// Tag metadata and checksums belong to immutable releases. Persist them so
	// a Captain restart does not require GitHub just to reuse a verified cache.
	if strings.Contains(raw, "/releases/tags/") || strings.HasSuffix(raw, "/SHA256SUMS") {
		ttl = cacheAge
	}
	key := sha256.Sum256([]byte(raw))
	metaPath := filepath.Join(s.Dir, hex.EncodeToString(key[:])+".meta")
	if !ok && s.Dir != "" {
		if f, e := os.Open(metaPath); e == nil {
			b, e := io.ReadAll(io.LimitReader(f, 3*maxMetadata+1))
			f.Close()
			if e == nil && len(b) <= 3*maxMetadata && json.Unmarshal(b, &m) == nil && len(m.Data) <= maxMetadata {
				ok = true
			}
		}
	}
	if m.Err != nil {
		ttl = 15 * time.Second
	}
	if ok && time.Since(m.At) < ttl {
		return append([]byte(nil), m.Data...), m.Err
	}
	defer func() {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if len(s.metadata) >= 64 {
			for k := range s.metadata {
				delete(s.metadata, k)
				break
			}
		}
		s.metadata[raw] = metadata{Data: append([]byte(nil), data...), At: time.Now(), Err: err}
		if err == nil && s.Dir != "" {
			if e := os.MkdirAll(s.Dir, 0o700); e == nil {
				if f, e := os.CreateTemp(s.Dir, ".metadata-*"); e == nil {
					b, _ := json.Marshal(s.metadata[raw])
					_, e = f.Write(b)
					ce := f.Close()
					if e == nil {
						e = ce
					}
					if e == nil {
						_ = os.Rename(f.Name(), metaPath)
					}
					_ = os.Remove(f.Name())
				}
				_ = s.pruneAux()
			}
		}
	}()
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	r, e := s.get(ctx, raw)
	if e != nil {
		return nil, e
	}
	defer r.Body.Close()
	b, e := io.ReadAll(io.LimitReader(r.Body, maxMetadata+1))
	if e != nil {
		return nil, e
	}
	if len(b) > maxMetadata {
		return nil, fmt.Errorf("upstream metadata exceeds limit")
	}
	return b, nil
}

func (s *Service) begin(ctx context.Context, key string) (func(), error) {
	for {
		s.mu.Lock()
		if done, ok := s.flights[key]; ok {
			s.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-done:
				continue
			}
		}
		done := make(chan struct{})
		s.flights[key] = done
		s.mu.Unlock()
		return func() { s.mu.Lock(); delete(s.flights, key); close(done); s.mu.Unlock() }, nil
	}
}

// A 200 response is not sufficient to publish immutable metadata. Incomplete
// release uploads and malformed checksum lists must be retriable once fixed.
func (s *Service) forgetMetadata(raw string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.metadata, raw)
	if s.Dir != "" {
		key := sha256.Sum256([]byte(raw))
		_ = os.Remove(filepath.Join(s.Dir, hex.EncodeToString(key[:])+".meta"))
	}
}

// Release returns only supported native artifacts, with relative local URLs.
// Latest is resolved once to an immutable tag before any asset is downloaded.
func (s *Service) Release(ctx context.Context, version string) (*Release, error) {
	endpoint := "https://api.github.com/repos/zeptop-dev/bosun/releases/latest"
	if version != "latest" {
		if !supportedVersion(version) {
			return nil, ErrNotFound
		}
		endpoint = "https://api.github.com/repos/zeptop-dev/bosun/releases/tags/" + version
	}
	b, e := s.small(ctx, endpoint)
	if e != nil {
		return nil, e
	}
	valid := false
	defer func() {
		if !valid {
			s.forgetMetadata(endpoint)
		}
	}()
	var r Release
	if e = json.Unmarshal(b, &r); e != nil {
		return nil, fmt.Errorf("invalid release metadata")
	}
	if !supportedVersion(r.TagName) || r.Draft || r.Prerelease || (version != "latest" && r.TagName != version) {
		return nil, ErrNotFound
	}
	out := make([]ReleaseAsset, 0, 3)
	seen := map[string]bool{}
	for _, a := range r.Assets {
		if !supportedAsset(a.Name) {
			continue
		}
		expected := "https://github.com/zeptop-dev/bosun/releases/download/" + r.TagName + "/" + a.Name
		if seen[a.Name] || a.URL != expected || a.Size <= 0 || a.Size > maxArtifact {
			return nil, fmt.Errorf("invalid release artifact metadata")
		}
		seen[a.Name] = true
		a.URL = Prefix + "/bosun/" + r.TagName + "/" + a.Name
		out = append(out, a)
	}
	if !seen["SHA256SUMS"] || !seen["bosun-linux-amd64"] || !seen["bosun-linux-arm64"] {
		return nil, fmt.Errorf("release lacks required native artifacts")
	}
	r.Assets = out
	valid = true
	return &r, nil
}

func (s *Service) Releases(ctx context.Context) ([]Release, error) {
	b, e := s.small(ctx, "https://api.github.com/repos/zeptop-dev/bosun/releases?per_page=30")
	if e != nil {
		return nil, e
	}
	var releases []Release
	if e = json.Unmarshal(b, &releases); e != nil {
		s.forgetMetadata("https://api.github.com/repos/zeptop-dev/bosun/releases?per_page=30")
		return nil, fmt.Errorf("invalid release metadata")
	}
	out := []Release{}
	for _, r := range releases {
		if supportedVersion(r.TagName) && !r.Draft && !r.Prerelease {
			r.Assets = nil
			out = append(out, r)
		}
	}
	return out, nil
}

func checksum(b []byte, name string) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(b)))
	found := ""
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != name {
			continue
		}
		h := strings.ToLower(fields[0])
		decoded, e := hex.DecodeString(h)
		if e != nil || len(decoded) != 32 || found != "" {
			return "", fmt.Errorf("invalid checksum entry")
		}
		found = h
	}
	if scanner.Err() != nil || found == "" {
		return "", fmt.Errorf("checksum entry missing")
	}
	return found, nil
}

func (s *Service) Bosun(ctx context.Context, version, name string) (*File, error) {
	if !supportedVersion(version) || !supportedAsset(name) {
		return nil, ErrNotFound
	}
	if _, e := s.Release(ctx, version); e != nil {
		return nil, e
	}
	base := "https://github.com/zeptop-dev/bosun/releases/download/" + version + "/"
	// Even SHA256SUMS is buffered and validated before being served.
	sums, e := s.small(ctx, base+"SHA256SUMS")
	if e != nil {
		return nil, e
	}
	for _, n := range []string{"bosun-linux-amd64", "bosun-linux-arm64"} {
		if _, e = checksum(sums, n); e != nil {
			s.forgetMetadata(base + "SHA256SUMS")
			return nil, e
		}
	}
	if name == "SHA256SUMS" {
		return s.cacheBytes(ctx, base+name, sums, false)
	}
	want, e := checksum(sums, name)
	if e != nil {
		return nil, e
	}
	return s.cached(ctx, base+name, want, true, nil)
}

func (s *Service) Core(ctx context.Context, core, version, goos, arch string) (*File, error) {
	if goos != "linux" || (arch != "amd64" && arch != "arm64" && arch != "amd64-musl" && arch != "arm64-musl") {
		return nil, ErrNotFound
	}
	// Resolve by exact stored version: aliases must not change the download identity.
	var selected *coremanifest.Release
	for _, r := range coremanifest.Manifest {
		if r.Core == core && r.Version == version {
			copy := r
			selected = &copy
			break
		}
	}
	if core == "geocheck" {
		r := coremanifest.DiagnosticRelease()
		if r.Version == version {
			selected = &r
		}
	}
	if core == "nexttrace" {
		r := coremanifest.RouteToolRelease()
		if r.Version == version {
			selected = &r
		}
	}
	if selected == nil {
		return nil, ErrNotFound
	}
	a, ok := selected.Assets[goos+"/"+arch]
	if !ok {
		return nil, ErrNotFound
	}
	want := strings.ToLower(a.SHA256)
	if want == "" && a.SumsURL != "" {
		b, e := s.small(ctx, a.SumsURL)
		if e != nil {
			return nil, e
		}
		u, e := url.Parse(a.URL)
		if e != nil {
			return nil, e
		}
		want, e = checksum(b, path.Base(u.Path))
		if e != nil {
			s.forgetMetadata(a.SumsURL)
			return nil, e
		}
	}
	return s.cached(ctx, a.URL, want, want != "", nil)
}

func (s *Service) cacheBytes(ctx context.Context, key string, b []byte, verified bool) (*File, error) {
	h := sha256.Sum256(b)
	return s.cached(ctx, key, hex.EncodeToString(h[:]), verified, b)
}

func hashFile(p string) (string, int64, error) {
	f, e := os.Open(p)
	if e != nil {
		return "", 0, e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Size() > maxArtifact {
		return "", 0, fmt.Errorf("invalid cache file")
	}
	h := sha256.New()
	n, e := io.Copy(h, io.LimitReader(f, maxArtifact+1))
	return hex.EncodeToString(h.Sum(nil)), n, e
}

func (s *Service) cached(ctx context.Context, raw, want string, verified bool, data []byte) (*File, error) {
	if !allowedURL(raw) {
		return nil, fmt.Errorf("upstream URL is not allowed")
	}
	if want != "" {
		b, e := hex.DecodeString(want)
		if e != nil || len(b) != 32 {
			return nil, fmt.Errorf("invalid expected checksum")
		}
	}
	keyHash := sha256.Sum256([]byte(raw + "\n" + want))
	key := hex.EncodeToString(keyHash[:])
	p := filepath.Join(s.Dir, key+".bin")
	done, e := s.begin(ctx, key)
	if e != nil {
		return nil, e
	}
	defer done()
	var existing File
	if b, e := os.ReadFile(p + ".json"); e == nil && json.Unmarshal(b, &existing) == nil {
		if st, e := os.Lstat(p); e == nil && st.Mode().IsRegular() && time.Since(st.ModTime()) < cacheAge {
			h, n, e := hashFile(p)
			if e == nil && h == existing.SHA256 && n == existing.Size && (want == "" || h == want) {
				existing.Path = p
				existing.SourceVerified = verified
				return &existing, nil
			}
		}
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if s.Dir == "" {
		return nil, fmt.Errorf("artifact cache is unavailable")
	}
	if e := os.MkdirAll(s.Dir, 0o700); e != nil {
		return nil, e
	}
	f, e := os.CreateTemp(s.Dir, ".download-*")
	if e != nil {
		return nil, e
	}
	defer os.Remove(f.Name())
	defer f.Close()
	var body io.ReadCloser
	if data != nil {
		body = io.NopCloser(strings.NewReader(string(data)))
	} else {
		r, e := s.get(ctx, raw)
		if e != nil {
			return nil, e
		}
		if r.ContentLength > maxArtifact {
			r.Body.Close()
			return nil, fmt.Errorf("artifact exceeds size limit")
		}
		body = r.Body
	}
	defer body.Close()
	h := sha256.New()
	n, e := io.Copy(io.MultiWriter(f, h), io.LimitReader(body, maxArtifact+1))
	if e != nil {
		return nil, e
	}
	if n == 0 || n > maxArtifact {
		return nil, fmt.Errorf("artifact exceeds size limit or is empty")
	}
	got := hex.EncodeToString(h.Sum(nil))
	if want != "" && got != want {
		return nil, fmt.Errorf("artifact checksum mismatch")
	}
	if e = f.Sync(); e != nil {
		return nil, e
	}
	if e = f.Close(); e != nil {
		return nil, e
	}
	result := &File{Path: p, SHA256: got, Size: n, SourceVerified: verified}
	encoded, _ := json.Marshal(result)
	s.mu.Lock()
	defer s.mu.Unlock()
	if e = s.prune(n, p); e != nil {
		return nil, e
	}
	if e = os.Rename(f.Name(), p); e != nil {
		return nil, e
	}
	meta, e := os.CreateTemp(s.Dir, ".metadata-*")
	if e != nil {
		os.Remove(p)
		return nil, e
	}
	defer os.Remove(meta.Name())
	_, e = meta.Write(encoded)
	ce := meta.Close()
	if e == nil {
		e = ce
	}
	if e == nil {
		e = os.Rename(meta.Name(), p+".json")
	}
	if e != nil {
		os.Remove(p)
		return nil, e
	}
	return result, nil
}

// prune runs under mu before publishing each file. Active readers keep their
// open descriptors; a removed file is safely refetched on the next request.
func (s *Service) prune(incoming int64, keep string) error {
	if e := s.pruneAux(); e != nil {
		return e
	}
	entries, e := os.ReadDir(s.Dir)
	if e != nil {
		return e
	}
	type item struct {
		p    string
		size int64
		at   time.Time
	}
	var files []item
	var total int64
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".bin") {
			continue
		}
		p := filepath.Join(s.Dir, entry.Name())
		st, e := entry.Info()
		if e != nil {
			return e
		}
		if !st.Mode().IsRegular() {
			continue
		}
		if p == keep || time.Since(st.ModTime()) >= cacheAge {
			if e = os.Remove(p); e != nil {
				return e
			}
			_ = os.Remove(p + ".json")
			continue
		}
		files = append(files, item{p, st.Size(), st.ModTime()})
		total += st.Size()
	}
	sort.Slice(files, func(i, j int) bool { return files[i].at.Before(files[j].at) })
	for len(files) > 0 && (total+incoming > maxCache || len(files) >= 256) {
		v := files[0]
		if e = os.Remove(v.p); e != nil {
			return e
		}
		_ = os.Remove(v.p + ".json")
		total -= v.size
		files = files[1:]
	}
	return nil
}

// A terminated process may leave temporary downloads or sidecars behind. Their
// age exceeds every download deadline before cleanup; active files are retained.
func (s *Service) pruneAux() error {
	entries, e := os.ReadDir(s.Dir)
	if e != nil {
		return e
	}
	type item struct {
		p  string
		at time.Time
	}
	var metas []item
	for _, entry := range entries {
		name := entry.Name()
		p := filepath.Join(s.Dir, name)
		st, e := entry.Info()
		if e != nil {
			return e
		}
		if !st.Mode().IsRegular() {
			continue
		}
		remove := false
		switch {
		case strings.HasPrefix(name, ".download-") || strings.HasPrefix(name, ".metadata-"):
			remove = time.Since(st.ModTime()) > 24*time.Hour
		case strings.HasSuffix(name, ".bin.json"):
			_, err := os.Lstat(strings.TrimSuffix(p, ".json"))
			remove = os.IsNotExist(err)
		case strings.HasSuffix(name, ".meta"):
			remove = time.Since(st.ModTime()) > cacheAge
			if !remove {
				metas = append(metas, item{p, st.ModTime()})
			}
		}
		if remove {
			if e = os.Remove(p); e != nil {
				return e
			}
		}
	}
	sort.Slice(metas, func(i, j int) bool { return metas[i].at.Before(metas[j].at) })
	for len(metas) > 64 {
		if e = os.Remove(metas[0].p); e != nil {
			return e
		}
		metas = metas[1:]
	}
	return nil
}
