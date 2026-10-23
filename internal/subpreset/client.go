// Package subpreset retrieves a consistent snapshot of the official ACL4SSR
// preset directory. It does not evaluate presets, follow their rule references,
// or persist data; callers publish a snapshot only after Fetch succeeds.
package subpreset

import (
	"bytes"
	"context"
	"crypto/sha1" // Git's blob object IDs use SHA-1, not an authentication digest.
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	// SourceURL and LicenseURL accompany imported presets. The upstream data is
	// CC BY-SA 4.0; it is fetched at runtime and is not embedded in this package.
	SourceURL  = "https://github.com/ACL4SSR/ACL4SSR"
	LicenseURL = SourceURL + "/blob/master/LICENCE"

	apiRoot   = "https://api.github.com/repos/ACL4SSR/ACL4SSR"
	rawRoot   = "https://raw.githubusercontent.com/ACL4SSR/ACL4SSR/"
	configDir = "Clash/config/"

	maxFiles         = 128
	maxDirectoryRows = 512
	maxFileBytes     = 256 << 10
	maxTotalBytes    = 4 << 20
	maxMetadataBytes = 2 << 20
	parallelRequests = 4
	fetchTimeout     = 60 * time.Second
)

var (
	// These errors deliberately contain no response body, URL error, or other
	// upstream-controlled detail. They may be returned to an administrator.
	ErrUnavailable     = errors.New("ACL4SSR preset source is unavailable")
	ErrRateLimited     = errors.New("ACL4SSR preset source rate limit reached; try again later")
	ErrInvalidSnapshot = errors.New("ACL4SSR preset source returned an invalid or inconsistent snapshot")
	ErrTooLarge        = errors.New("ACL4SSR preset snapshot exceeds the download limits")

	objectIDPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)
	fileNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,123}\.ini$`)
)

// File is one unmodified upstream preset. SHA is its Git blob object ID. URL
// names the fixed commit from which Content was retrieved, never a moving ref.
type File struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	SHA     string `json:"sha"`
	Content string `json:"content"`
}

// Snapshot contains all .ini files directly in the official preset directory.
// SyncedAt is the successful retrieval time as Unix seconds, not commit time.
type Snapshot struct {
	Revision string `json:"revision"`
	SyncedAt int64  `json:"synced_at"`
	Files    []File `json:"files"`
}

// Client downloads from fixed public upstream endpoints. It never reads a
// GitHub token, cookies, or environment proxy settings. There is intentionally
// no option accepting an upstream address from a request or saved setting.
// One Client may be used concurrently; the caller coordinates cache updates.
type Client struct {
	http *http.Client
}

func NewClient() *Client {
	return &Client{http: &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			Proxy:                  nil,
			DialContext:            (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:      true,
			MaxIdleConns:           8,
			MaxIdleConnsPerHost:    parallelRequests,
			MaxConnsPerHost:        parallelRequests,
			IdleConnTimeout:        90 * time.Second,
			TLSHandshakeTimeout:    10 * time.Second,
			ResponseHeaderTimeout:  15 * time.Second,
			MaxResponseHeaderBytes: 1 << 20,
		},
	}}
}

type directoryEntry struct {
	Name            string `json:"name"`
	Path            string `json:"path"`
	Type            string `json:"type"`
	SHA             string `json:"sha"`
	Size            int64  `json:"size"`
	Target          string `json:"target"`
	SubmoduleGitURL string `json:"submodule_git_url"`
}

// Fetch returns a complete snapshot or a zero Snapshot on any failure. All
// network work, including cancellation of active downloads, completes before
// returning. No cache or active subscription is changed here.
func (c *Client) Fetch(parent context.Context) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(parent, fetchTimeout)
	defer cancel()

	var commit struct {
		SHA string `json:"sha"`
	}
	if err := c.getJSON(ctx, apiRoot+"/commits/master", &commit); err != nil {
		return Snapshot{}, err
	}
	if !objectIDPattern.MatchString(commit.SHA) {
		return Snapshot{}, ErrInvalidSnapshot
	}

	var directory []directoryEntry
	if err := c.getJSON(ctx, apiRoot+"/contents/Clash/config?ref="+commit.SHA, &directory); err != nil {
		return Snapshot{}, err
	}
	entries, err := validateDirectory(directory)
	if err != nil {
		return Snapshot{}, err
	}

	files := make([]File, len(entries))
	jobs := make(chan int, len(entries))
	for i := range entries {
		jobs <- i
	}
	close(jobs)
	var firstError error
	var failed sync.Once
	var workers sync.WaitGroup
	for range min(parallelRequests, len(entries)) {
		workers.Go(func() {
			for i := range jobs {
				if ctx.Err() != nil {
					return
				}
				e := entries[i]
				address := rawRoot + commit.SHA + "/" + configDir + e.Name
				body, err := c.get(ctx, address, maxFileBytes)
				if err == nil && (int64(len(body)) != e.Size || gitBlobID(body) != e.SHA || !utf8.Valid(body) || bytes.IndexByte(body, 0) >= 0) {
					err = ErrInvalidSnapshot
				}
				if err != nil {
					failed.Do(func() { firstError = err; cancel() })
					return
				}
				files[i] = File{Name: e.Name, URL: address, SHA: e.SHA, Content: string(body)}
			}
		})
	}
	workers.Wait()
	if firstError != nil {
		return Snapshot{}, firstError
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Revision: commit.SHA, SyncedAt: time.Now().UTC().Unix(), Files: files}, nil
}

func validateDirectory(directory []directoryEntry) ([]directoryEntry, error) {
	if len(directory) > maxDirectoryRows {
		return nil, ErrTooLarge
	}
	entries := make([]directoryEntry, 0, len(directory))
	seen := make(map[string]bool)
	var total int64
	for _, e := range directory {
		if !strings.HasSuffix(strings.ToLower(e.Name), ".ini") {
			continue
		}
		key := strings.ToLower(e.Name)
		if !fileNamePattern.MatchString(e.Name) || e.Path != configDir+e.Name || e.Type != "file" || e.Target != "" || e.SubmoduleGitURL != "" || !objectIDPattern.MatchString(e.SHA) || seen[key] || e.Size < 0 {
			return nil, ErrInvalidSnapshot
		}
		if len(entries) == maxFiles || e.Size > maxFileBytes {
			return nil, ErrTooLarge
		}
		seen[key] = true
		total += e.Size
		if total > maxTotalBytes {
			return nil, ErrTooLarge
		}
		entries = append(entries, e)
	}
	if len(entries) == 0 {
		return nil, ErrInvalidSnapshot
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

func (c *Client) getJSON(ctx context.Context, address string, v any) error {
	body, err := c.get(ctx, address, maxMetadataBytes)
	if err != nil {
		return err
	}
	if json.Unmarshal(body, v) != nil {
		return ErrInvalidSnapshot
	}
	return nil
}

func (c *Client) get(ctx context.Context, address string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, ErrInvalidSnapshot
	}
	req.Header.Set("User-Agent", "Captain-ACL4SSR-Preset-Sync")
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, context.DeadlineExceeded
		}
		return nil, ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests || (resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0") {
		return nil, ErrRateLimited
	}
	if resp.StatusCode != http.StatusOK {
		return nil, ErrUnavailable
	}
	if resp.ContentLength > limit {
		return nil, ErrTooLarge
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrUnavailable
	}
	if int64(len(body)) > limit {
		return nil, ErrTooLarge
	}
	return body, nil
}

func gitBlobID(body []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(body))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}
