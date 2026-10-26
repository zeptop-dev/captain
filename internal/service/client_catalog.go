package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	cc "github.com/zeptop-dev/captain/internal/clientcatalog"
	"github.com/zeptop-dev/captain/internal/store"
)

// ClientCatalog separates upstream discovery, cached bytes, and explicit
// public snapshots. It never executes or extracts a downloaded installer.
type ClientCatalog struct {
	Store    *store.Store
	Source   cc.Source
	Dir      string
	mu       sync.Mutex
	job      *cc.Job
	checking map[string]bool
}

func NewClientCatalog(st *store.Store, dir string, source cc.Source) *ClientCatalog {
	if source == nil {
		source = cc.NewSource()
	}
	return &ClientCatalog{Store: st, Dir: dir, Source: source, checking: map[string]bool{}}
}
func (m *ClientCatalog) ensure(ctx context.Context, id string) (cc.SavedClient, error) {
	s, e := m.Store.ClientCatalog(ctx, id)
	if errors.Is(e, store.ErrNotFound) {
		e = m.Store.MutateClientCatalog(ctx, id, func(*cc.SavedClient) error { return nil })
		if e == nil {
			s, e = m.Store.ClientCatalog(ctx, id)
		}
	}
	return s, e
}
func (m *ClientCatalog) Get(ctx context.Context, id string) (cc.Client, error) {
	p, ok := cc.Find(id)
	if !ok {
		return cc.Client{}, cc.ErrInvalid
	}
	s, e := m.ensure(ctx, id)
	if e != nil {
		return cc.Client{}, e
	}
	kind := "external"
	if p.Repository != "" {
		kind = "github"
	}
	cached := []cc.CachedAsset{}
	for _, a := range s.CachedAssets {
		if m.cacheReady(a) {
			cached = append(cached, a)
		}
	}
	return cc.Client{ID: id, Name: p.Name, Homepage: p.Website, Repo: p.Repository, Kind: kind, Format: p.Format, Revision: s.Revision, Platforms: s.Platforms, Release: s.Release, CheckedAt: s.CheckedAt, CheckError: s.CheckError, Archived: s.Archived, CachedAssets: cached, Published: s.Published}, nil
}
func (m *ClientCatalog) View(ctx context.Context) (cc.CatalogView, error) {
	// Completion must not overtake the cache snapshot: callers stop polling once
	// they see a complete job and must already see its downloaded package.
	m.mu.Lock()
	defer m.mu.Unlock()
	out := cc.CatalogView{Clients: []cc.Client{}, Storage: cc.Storage{MaxBytes: cc.MaxCacheBytes, MaxAssetBytes: cc.MaxAssetBytes}}
	for _, p := range cc.Catalog() {
		c, e := m.Get(ctx, p.ID)
		if e != nil {
			return out, e
		}
		out.Clients = append(out.Clients, c)
	}
	out.Storage.UsedBytes = m.diskUsage()
	if m.job != nil {
		j := *m.job
		out.Job = &j
	}
	return out, nil
}
func (m *ClientCatalog) diskUsage() int64 {
	root, e := os.OpenRoot(m.Dir)
	if e != nil {
		return 0
	}
	defer root.Close()
	f, e := root.Open(".")
	if e != nil {
		return 0
	}
	defer f.Close()
	entries, e := f.ReadDir(-1)
	if e != nil {
		return cc.MaxCacheBytes
	}
	var used int64
	for _, d := range entries {
		if i, e := d.Info(); e == nil && i.Mode().IsRegular() {
			used += i.Size()
		}
	}
	return used
}
func validHash(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && s == strings.ToLower(s)
}
func (m *ClientCatalog) openCache(a cc.CachedAsset) (*os.File, error) {
	if !validHash(a.SHA256) {
		return nil, store.ErrNotFound
	}
	root, e := os.OpenRoot(m.Dir)
	if e != nil {
		return nil, e
	}
	defer root.Close()
	f, e := root.Open(a.SHA256)
	if e != nil {
		return nil, e
	}
	i, e := f.Stat()
	if e != nil || !i.Mode().IsRegular() || i.Size() != a.Size {
		f.Close()
		return nil, store.ErrNotFound
	}
	return f, nil
}
func (m *ClientCatalog) cacheReady(a cc.CachedAsset) bool {
	f, e := m.openCache(a)
	if e != nil {
		return false
	}
	f.Close()
	return true
}

// BeginCheck snapshots a reset-resistant incarnation before remote I/O.
func (m *ClientCatalog) BeginCheck(ctx context.Context, id string) (string, error) {
	p, ok := cc.Find(id)
	if !ok || p.Repository == "" {
		return "", cc.ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.checking[id] {
		return "", cc.ErrBusy
	}
	s, e := m.ensure(ctx, id)
	if e != nil {
		return "", e
	}
	m.checking[id] = true
	return s.Incarnation, nil
}
func (m *ClientCatalog) EndCheck(id string) { m.mu.Lock(); delete(m.checking, id); m.mu.Unlock() }
func (m *ClientCatalog) CommitCheck(ctx context.Context, id, incarnation string, release *cc.Release, archived bool, fetchErr error) error {
	return m.Store.MutateClientCatalog(ctx, id, func(s *cc.SavedClient) error {
		if s.Incarnation != incarnation {
			return cc.ErrConflict
		}
		now := time.Now().UTC()
		s.CheckedAt = &now
		if fetchErr != nil {
			s.CheckError = "Official version check failed; the previous snapshot was retained"
			return nil
		}
		if release == nil {
			return cc.ErrInvalid
		}
		s.Release = release
		s.Archived = &archived
		s.CheckError = ""
		return nil
	})
}
func findAsset(s *cc.SavedClient, id int64) (cc.Asset, bool) {
	for _, a := range s.RetainedAssets {
		if a.ID == id {
			return a, true
		}
	}
	if s.Release != nil {
		for _, a := range s.Release.Assets {
			if a.ID == id {
				return a, true
			}
		}
	}
	for _, a := range s.CachedAssets {
		if a.ID == id {
			return a.Asset, true
		}
	}
	// Retain the immutable official asset metadata in published download cards.
	for _, p := range s.Published {
		if p.Source == "official" {
			for _, d := range p.Downloads {
				if d.ID == id {
					return cc.Asset{ID: d.ID, Name: d.Label, Platform: p.Platform, Arch: d.Arch, PackageType: d.PackageType, Size: d.Size, URL: d.URL, Version: d.Version}, true
				}
			}
		}
	}
	return cc.Asset{}, false
}
func cachedAsset(s *cc.SavedClient, id int64) (cc.CachedAsset, bool) {
	for _, a := range s.CachedAssets {
		if a.ID == id {
			return a, true
		}
	}
	return cc.CachedAsset{}, false
}
func (m *ClientCatalog) Save(ctx context.Context, id string, revision int64, settings []cc.PlatformSettings) (cc.Client, error) {
	p, ok := cc.Find(id)
	if !ok {
		return cc.Client{}, cc.ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.Store.MutateClientCatalog(ctx, id, func(s *cc.SavedClient) error {
		if s.Revision != revision {
			return cc.ErrConflict
		}
		if len(settings) != len(p.Platforms) {
			return cc.ErrInvalid
		}
		seen := map[string]bool{}
		published := []cc.PublishedClient{}
		retained := []cc.Asset{}
		for _, v := range settings {
			if !p.Supports(v.Platform) || seen[v.Platform] || v.Sort < 0 || v.Sort > 10000 || len(v.AssetIDs) > 32 {
				return cc.ErrInvalid
			}
			seen[v.Platform] = true
			if v.Source != "official" && v.Source != "store" && v.Source != "cache" {
				return cc.ErrInvalid
			}
			if p.Repository != "" && v.Source == "store" || p.Repository == "" && v.Source == "cache" || p.Repository == "" && len(v.AssetIDs) > 0 {
				return cc.ErrInvalid
			}
			if v.Source == "store" {
				valid := false
				for _, l := range p.Platforms {
					if l.Platform == v.Platform && l.Kind == "store" {
						valid = true
					}
				}
				if !valid {
					return cc.ErrInvalid
				}
			}
			if v.Source == "cache" && len(v.AssetIDs) == 0 && (v.ShowHome || v.ShowPortal) {
				return cc.ErrInvalid
			}
			pub := cc.PublishedClient{ID: p.ID, Name: p.Name, Homepage: p.Website, Format: p.Format, Platform: v.Platform, Recommended: v.Recommended, Sort: v.Sort, Source: v.Source, Downloads: []cc.Download{}}
			ids := map[int64]bool{}
			mixedVersion := false
			for _, assetID := range v.AssetIDs {
				if assetID <= 0 || ids[assetID] {
					return cc.ErrInvalid
				}
				ids[assetID] = true
				a, ok := findAsset(s, assetID)
				if !ok {
					if !v.ShowHome && !v.ShowPortal {
						continue
					}
					return cc.ErrInvalid
				}
				if a.Platform != v.Platform {
					return cc.ErrInvalid
				}
				retained = append(retained, a)
				downloadURL := a.URL
				if v.Source == "cache" {
					cached, ok := cachedAsset(s, assetID)
					if !ok || !m.cacheReady(cached) {
						if !v.ShowHome && !v.ShowPortal {
							continue
						}
						return cc.ErrMissingCache
					}
					downloadURL = cacheURL(id, cached)
				}
				pub.Downloads = append(pub.Downloads, cc.Download{ID: a.ID, Label: a.Name, Arch: a.Arch, PackageType: a.PackageType, Size: a.Size, Version: a.Version, URL: downloadURL})
				if !mixedVersion {
					if pub.Version == "" {
						pub.Version = a.Version
					} else if pub.Version != a.Version {
						pub.Version = ""
						mixedVersion = true
					}
				}
			}
			if len(v.AssetIDs) == 0 && v.Source != "cache" {
				target := p.Link(v.Platform)
				if p.Repository != "" && s.Release != nil {
					target = s.Release.URL
					pub.Version = s.Release.Version
				}
				pub.Downloads = append(pub.Downloads, cc.Download{Label: p.Name, URL: target, Version: pub.Version})
			}
			published = append(published, pub)
		}
		s.RetainedAssets = retained
		s.Platforms = settings
		s.Published = published
		s.Revision++
		return nil
	})
	if e != nil {
		return cc.Client{}, e
	}
	return m.Get(ctx, id)
}
func cacheURL(id string, a cc.CachedAsset) string {
	return "/downloads/clients/" + id + "/" + fmt.Sprint(a.ID) + "/" + url.PathEscape(a.Name)
}
func (m *ClientCatalog) Published(ctx context.Context, home bool) ([]cc.PublishedClient, error) {
	out := []cc.PublishedClient{}
	for _, p := range cc.Catalog() {
		s, e := m.Store.ClientCatalog(ctx, p.ID)
		if errors.Is(e, store.ErrNotFound) {
			continue
		}
		if e != nil {
			return nil, e
		}
		for _, pub := range s.Published {
			enabled := false
			for _, v := range s.Platforms {
				if v.Platform == pub.Platform {
					enabled = home && v.ShowHome || !home && v.ShowPortal
					break
				}
			}
			if !enabled {
				continue
			}
			if pub.Source == "cache" {
				downloads := []cc.Download{}
				for _, d := range pub.Downloads {
					if a, ok := cachedAsset(&s, d.ID); ok && m.cacheReady(a) {
						downloads = append(downloads, d)
					}
				}
				pub.Downloads = downloads
				if len(downloads) == 0 {
					continue
				}
			}
			out = append(out, pub)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Sort != out[j].Sort {
			return out[i].Sort < out[j].Sort
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// DownloadAuthorization is called under the caller's short Accounts lock. It
// resolves visibility afresh for every GET/HEAD, including range requests.
func (m *ClientCatalog) DownloadAuthorization(ctx context.Context, id string, assetID int64, name string, portal bool) (*os.File, cc.CachedAsset, error) {
	s, e := m.Store.ClientCatalog(ctx, id)
	if e != nil {
		return nil, cc.CachedAsset{}, store.ErrNotFound
	}
	a, ok := cachedAsset(&s, assetID)
	if !ok || a.Name != name {
		return nil, a, store.ErrNotFound
	}
	allowed := false
	for _, v := range s.Platforms {
		if v.Source == "cache" && slices.Contains(v.AssetIDs, assetID) && (v.ShowHome || portal && v.ShowPortal) {
			allowed = true
		}
	}
	if !allowed {
		return nil, a, store.ErrNotFound
	}
	f, e := m.openCache(a)
	if e != nil {
		return nil, a, store.ErrNotFound
	}
	return f, a, nil
}
func (m *ClientCatalog) DeleteCache(ctx context.Context, id string, assetID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.job != nil && m.job.State == "downloading" {
		return cc.ErrBusy
	}
	var sha string
	e := m.Store.MutateClientCatalog(ctx, id, func(s *cc.SavedClient) error {
		for _, v := range s.Platforms {
			if v.Source == "cache" && slices.Contains(v.AssetIDs, assetID) && (v.ShowHome || v.ShowPortal) {
				return cc.ErrReferenced
			}
		}
		found := false
		kept := []cc.CachedAsset{}
		for _, a := range s.CachedAssets {
			if a.ID == assetID {
				sha = a.SHA256
				found = true
			} else {
				kept = append(kept, a)
			}
		}
		if !found {
			return store.ErrNotFound
		}
		s.CachedAssets = kept
		return nil
	})
	if e != nil {
		return e
	}
	// A digest can be shared by multiple upstream asset IDs. Remove only after
	// all database references disappear; existing open downloads remain valid.
	used := false
	for _, p := range cc.Catalog() {
		s, e := m.Store.ClientCatalog(ctx, p.ID)
		if e != nil && !errors.Is(e, store.ErrNotFound) {
			return e
		}
		for _, a := range s.CachedAssets {
			if a.SHA256 == sha {
				used = true
			}
		}
	}
	if !used && validHash(sha) {
		if root, e := os.OpenRoot(m.Dir); e == nil {
			defer root.Close()
			if e = root.Remove(sha); e != nil && !os.IsNotExist(e) {
				return e
			}
		}
	}
	return nil
}

// PrepareDownload reserves the sole global transfer before returning HTTP 202.
// The supplied finalize callback reauthorizes the original session/token under
// Accounts exclusively; it must call commit only if that identity is current.
func (m *ClientCatalog) PrepareDownload(ctx context.Context, id string, assetID int64) (cc.Job, func(func(func() error) error), error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.job != nil && m.job.State == "downloading" {
		return cc.Job{}, nil, cc.ErrBusy
	}
	p, ok := cc.Find(id)
	if !ok || p.Repository == "" {
		return cc.Job{}, nil, cc.ErrInvalid
	}
	s, e := m.ensure(ctx, id)
	if e != nil {
		return cc.Job{}, nil, e
	}
	a, ok := findAsset(&s, assetID)
	if !ok || a.Size <= 0 || a.Size > cc.MaxAssetBytes {
		return cc.Job{}, nil, cc.ErrInvalid
	}
	if ca, ok := cachedAsset(&s, assetID); ok && m.cacheReady(ca) {
		return cc.Job{}, nil, cc.ErrAlreadyCached
	}
	b := make([]byte, 16)
	if _, e = rand.Read(b); e != nil {
		return cc.Job{}, nil, e
	}
	j := cc.Job{ID: hex.EncodeToString(b), ClientID: id, AssetID: assetID, State: "downloading", TotalBytes: a.Size}
	m.job = &j
	run := func(finalize func(func() error) error) { m.runDownload(j, a, s.Incarnation, finalize) }
	return j, run, nil
}
func (m *ClientCatalog) setJob(id, state, msg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.job != nil && m.job.ID == id {
		m.job.State = state
		m.job.Error = msg
	}
}

type progressWriter struct {
	w   io.Writer
	m   *ClientCatalog
	job string
}

func (p progressWriter) Write(b []byte) (int, error) {
	n, e := p.w.Write(b)
	p.m.mu.Lock()
	if p.m.job != nil && p.m.job.ID == p.job {
		p.m.job.ReceivedBytes += int64(n)
	}
	p.m.mu.Unlock()
	return n, e
}
func (m *ClientCatalog) runDownload(j cc.Job, a cc.Asset, incarnation string, finalize func(func() error) error) {
	failure := func(msg string) { m.setJob(j.ID, "failed", msg) }
	if m.Dir == "" {
		failure("Package cache is unavailable")
		return
	}
	if e := os.MkdirAll(m.Dir, 0700); e != nil {
		failure("Package cache is unavailable")
		return
	}
	root, e := os.OpenRoot(m.Dir)
	if e != nil {
		failure("Package cache is unavailable")
		return
	}
	defer root.Close()
	// Partial and orphan files can remain after a crash or database-only reset.
	// The single transfer reservation excludes other completion/deletion work.
	// Only delete known cache names absent from every persisted reference.
	keep := map[string]bool{}
	for _, p := range cc.Catalog() {
		state, err := m.Store.ClientCatalog(context.Background(), p.ID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			failure("Cannot read package cache inventory")
			return
		}
		for _, cached := range state.CachedAssets {
			keep[cached.SHA256] = true
		}
	}
	if f, e := root.Open("."); e == nil {
		entries, _ := f.ReadDir(-1)
		f.Close()
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".part-") || validHash(entry.Name()) && !keep[entry.Name()] {
				_ = root.Remove(entry.Name())
			}
		}
	}
	if m.diskUsage() > cc.MaxCacheBytes-a.Size {
		failure("Package cache quota exceeded; remove unused packages first")
		return
	}
	temp := ".part-" + j.ID
	f, e := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		failure("Cannot create package cache file")
		return
	}
	defer root.Remove(temp)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	sha, e := m.Source.Download(ctx, a, progressWriter{w: &boundedPackageWriter{w: f, remaining: a.Size + 1}, m: m, job: j.ID})
	// Source implementations must respect metadata too: verify bytes and digest
	// locally even when a test source or future provider is injected.
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil || !validHash(sha) {
		failure("Package download or checksum verification failed; the current publication was retained")
		return
	}
	verify, e := root.Open(temp)
	if e != nil {
		failure("Cannot verify package cache file")
		return
	}
	digest, n, e := hashReader(verify)
	verify.Close()
	if e != nil || n != a.Size || digest != sha || a.Digest != "" && !strings.EqualFold(a.Digest, "sha256:"+sha) {
		failure("Package checksum or size verification failed")
		return
	}
	verified := "local_sha256"
	if a.Digest != "" {
		verified = "upstream_digest"
	}
	cached := cc.CachedAsset{Asset: a, SHA256: sha, VerifiedBy: verified, CachedAt: time.Now().UTC()}
	e = finalize(func() error {
		current, err := m.Store.ClientCatalog(context.Background(), j.ClientID)
		if err != nil || current.Incarnation != incarnation {
			return cc.ErrConflict
		}
		if err = root.Rename(temp, sha); err != nil {
			return err
		}
		return m.Store.MutateClientCatalog(context.Background(), j.ClientID, func(s *cc.SavedClient) error {
			if s.Incarnation != incarnation {
				return cc.ErrConflict
			}
			kept := []cc.CachedAsset{}
			for _, c := range s.CachedAssets {
				if c.ID != a.ID {
					kept = append(kept, c)
				}
			}
			s.CachedAssets = append(kept, cached)
			return nil
		})
	})
	if e != nil {
		failure("Download was not committed because authorization or catalog state changed")
		return
	}
	m.setJob(j.ID, "complete", "")
}

// CacheDirectory returns the private directory used outside the static root.
func CacheDirectory(dataDir string) string {
	if dataDir == "" {
		return ""
	}
	return filepath.Join(dataDir, "client-packages")
}

func hashReader(r io.Reader) (string, int64, error) {
	h := sha256.New()
	n, e := io.Copy(h, r)
	return fmt.Sprintf("%x", h.Sum(nil)), n, e
}

type boundedPackageWriter struct {
	w         io.Writer
	remaining int64
}

func (w *boundedPackageWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.remaining {
		return 0, cc.ErrTooLarge
	}
	n, e := w.w.Write(p)
	w.remaining -= int64(n)
	return n, e
}
