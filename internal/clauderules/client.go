// Package clauderules retrieves the community Anthropic domain list at one
// verified Git revision. It neither publishes templates nor persists data;
// callers replace their cached snapshot only after Fetch succeeds.
package clauderules

import (
	"context"
	"crypto/sha1" // Git blob object IDs use SHA-1; this is not an authentication digest.
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"time"
)

const (
	// SourceURL identifies the community-maintained list, not an Anthropic
	// endorsement. The data is fetched at runtime under the upstream MIT license.
	SourceURL  = "https://github.com/v2fly/domain-list-community/blob/master/data/anthropic"
	LicenseURL = "https://github.com/v2fly/domain-list-community/blob/master/LICENSE"

	apiRoot    = "https://api.github.com/repos/v2fly/domain-list-community"
	rawRoot    = "https://raw.githubusercontent.com/v2fly/domain-list-community/"
	sourcePath = "data/anthropic"

	maxRules         = 256
	maxFileBytes     = 64 << 10
	maxMetadataBytes = 256 << 10
	fetchTimeout     = 60 * time.Second
)

var (
	// Errors contain no upstream response body, URL error, or credentials and
	// are safe to present to administrators.
	ErrUnavailable     = errors.New("Claude community rule source is unavailable")
	ErrRateLimited     = errors.New("Claude community rule source rate limit reached; try again later")
	ErrInvalidSnapshot = errors.New("Claude community rule source returned an invalid or inconsistent snapshot")
	ErrInvalidRules    = errors.New("Claude community rules contain invalid domains or unsupported syntax")
	ErrTooLarge        = errors.New("Claude community rules exceed the download or rule limits")

	objectIDPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)
)

// Rule preserves the upstream distinction between a domain suffix (domain)
// and a single exact hostname (full).
type Rule struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// Snapshot contains only community rules, without Captain's own additions.
// SyncedAt is the successful retrieval time, expressed as Unix seconds.
type Snapshot struct {
	Revision string `json:"revision"`
	SyncedAt int64  `json:"synced_at"`
	Rules    []Rule `json:"rules"`
}

// Client uses fixed, public HTTPS endpoints without environment proxies,
// credentials or redirects. It is safe for concurrent calls; callers coordinate
// cache publication. There is deliberately no configurable source URL.
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
			MaxIdleConnsPerHost:    2,
			MaxConnsPerHost:        2,
			IdleConnTimeout:        90 * time.Second,
			TLSHandshakeTimeout:    10 * time.Second,
			ResponseHeaderTimeout:  15 * time.Second,
			MaxResponseHeaderBytes: 64 << 10,
		},
	}}
}

type sourceFile struct {
	Name            string `json:"name"`
	Path            string `json:"path"`
	Type            string `json:"type"`
	SHA             string `json:"sha"`
	Size            int64  `json:"size"`
	Target          string `json:"target"`
	SubmoduleGitURL string `json:"submodule_git_url"`
}

// Fetch pins metadata and content to the same commit and verifies the Git blob
// digest before parsing. Every failure returns a zero Snapshot; partial data
// must never replace the caller's last successful snapshot.
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
	var file sourceFile
	if err := c.getJSON(ctx, apiRoot+"/contents/"+sourcePath+"?ref="+commit.SHA, &file); err != nil {
		return Snapshot{}, err
	}
	if file.Name != "anthropic" || file.Path != sourcePath || file.Type != "file" || file.Target != "" || file.SubmoduleGitURL != "" || !objectIDPattern.MatchString(file.SHA) || file.Size <= 0 {
		return Snapshot{}, ErrInvalidSnapshot
	}
	if file.Size > maxFileBytes {
		return Snapshot{}, ErrTooLarge
	}
	body, err := c.get(ctx, rawRoot+commit.SHA+"/"+sourcePath, maxFileBytes)
	if err != nil {
		return Snapshot{}, err
	}
	if int64(len(body)) != file.Size || gitBlobID(body) != file.SHA {
		return Snapshot{}, ErrInvalidSnapshot
	}
	rules, err := Parse(body)
	if err != nil {
		return Snapshot{}, err
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Revision: commit.SHA, SyncedAt: time.Now().UTC().Unix(), Rules: rules}, nil
}

func (c *Client) getJSON(ctx context.Context, address string, target any) error {
	body, err := c.get(ctx, address, maxMetadataBytes)
	if err != nil {
		return err
	}
	if json.Unmarshal(body, target) != nil {
		return ErrInvalidSnapshot
	}
	return nil
}

func (c *Client) get(ctx context.Context, address string, limit int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, ErrInvalidSnapshot
	}
	req.Header.Set("User-Agent", "Captain-Claude-Rules-Sync")
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, downloadError(ctx, err)
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
		return nil, downloadError(ctx, err)
	}
	if int64(len(body)) > limit {
		return nil, ErrTooLarge
	}
	return body, nil
}

func downloadError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return ErrUnavailable
}

func gitBlobID(body []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(body))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}
