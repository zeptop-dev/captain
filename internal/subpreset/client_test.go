package subpreset

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const revision = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), ContentLength: -1}
}

func jsonResponse(value any) *http.Response {
	b, _ := json.Marshal(value)
	return response(http.StatusOK, string(b))
}

func entry(name, body string) directoryEntry {
	return directoryEntry{Name: name, Path: configDir + name, Type: "file", SHA: gitBlobID([]byte(body)), Size: int64(len(body))}
}

// Each fixture is original test input, not a vendored upstream preset.
const examplePreset = "[custom]\nruleset=DIRECT,[]GEOIP,CN\nruleset=Proxy,[]FINAL\n"

func fixtureClient(t *testing.T, directory any, download func(*http.Request) (*http.Response, error)) *Client {
	t.Helper()
	c := NewClient()
	c.http.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Scheme != "https" || r.URL.User != nil || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Errorf("unexpected request method, scheme or credentials")
		}
		switch r.URL.String() {
		case apiRoot + "/commits/master":
			return jsonResponse(map[string]string{"sha": revision}), nil
		case apiRoot + "/contents/Clash/config?ref=" + revision:
			return jsonResponse(directory), nil
		default:
			if !strings.HasPrefix(r.URL.String(), rawRoot+revision+"/"+configDir) {
				t.Errorf("request escaped fixed snapshot endpoints")
				return nil, errors.New("unexpected request")
			}
			return download(r)
		}
	})
	return c
}

func TestFetchConsistentSnapshot(t *testing.T) {
	files := []map[string]any{}
	for _, name := range []string{"Zed.ini", "Alpha.ini"} {
		e := entry(name, examplePreset)
		files = append(files, map[string]any{"name": e.Name, "path": e.Path, "type": e.Type, "sha": e.SHA, "size": e.Size,
			// Upstream URLs are deliberately ignored; only locally constructed,
			// commit-pinned raw URLs may be requested.
			"download_url": "http://127.0.0.1/private", "url": "https://example.com/other"})
	}
	var count atomic.Int32
	c := fixtureClient(t, files, func(r *http.Request) (*http.Response, error) {
		count.Add(1)
		if r.URL.RawQuery != "" || r.URL.Fragment != "" {
			t.Error("raw URL contains query or fragment")
		}
		return response(http.StatusOK, examplePreset), nil
	})
	before := time.Now().Unix()
	s, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.Revision != revision || s.SyncedAt < before || s.SyncedAt > time.Now().Unix() || count.Load() != 2 {
		t.Fatalf("wrong snapshot metadata: %+v", s)
	}
	if got := []string{s.Files[0].Name, s.Files[1].Name}; !reflect.DeepEqual(got, []string{"Alpha.ini", "Zed.ini"}) {
		t.Fatalf("non-deterministic order: %v", got)
	}
	for _, f := range s.Files {
		if f.Content != examplePreset || f.SHA != gitBlobID([]byte(examplePreset)) || f.URL != rawRoot+revision+"/"+configDir+f.Name {
			t.Fatalf("incorrect file metadata: %+v", f)
		}
	}
}

func TestValidateDirectoryRejectsUnsafeEntries(t *testing.T) {
	tests := []struct {
		name   string
		change func(*directoryEntry)
	}{
		{"path traversal", func(e *directoryEntry) { e.Name = "../Other.ini"; e.Path = configDir + e.Name }},
		{"encoded traversal", func(e *directoryEntry) { e.Name = "%2e%2e.ini"; e.Path = configDir + e.Name }},
		{"wrong directory", func(e *directoryEntry) { e.Path = "Elsewhere/Valid.ini" }},
		{"symlink type", func(e *directoryEntry) { e.Type = "symlink" }},
		{"symlink target", func(e *directoryEntry) { e.Target = "../Elsewhere.ini" }},
		{"submodule", func(e *directoryEntry) { e.SubmoduleGitURL = "https://example.com/repository" }},
		{"directory", func(e *directoryEntry) { e.Type = "dir" }},
		{"invalid hash", func(e *directoryEntry) { e.SHA = "not-a-hash" }},
		{"negative size", func(e *directoryEntry) { e.Size = -1 }},
		{"newline", func(e *directoryEntry) { e.Name = "A\n.ini"; e.Path = configDir + e.Name }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := entry("Valid.ini", examplePreset)
			tt.change(&e)
			if _, err := validateDirectory([]directoryEntry{e}); !errors.Is(err, ErrInvalidSnapshot) {
				t.Fatalf("got %v", err)
			}
		})
	}
	for _, rows := range [][]directoryEntry{nil, {entry("Valid.ini", ""), entry("valid.ini", "")}} {
		if _, err := validateDirectory(rows); !errors.Is(err, ErrInvalidSnapshot) {
			t.Fatalf("got %v", err)
		}
	}
	rows := []directoryEntry{{Name: "README.md", Type: "file"}, entry("Valid.ini", "")}
	if got, err := validateDirectory(rows); err != nil || len(got) != 1 {
		t.Fatalf("non-preset files: %v, %v", got, err)
	}
}

func TestDirectoryLimits(t *testing.T) {
	for _, tt := range []struct {
		name      string
		count     int
		size      int64
		extraRows int
	}{
		{"file count", maxFiles + 1, 0, 0},
		{"individual size", 1, maxFileBytes + 1, 0},
		{"total size", maxTotalBytes/maxFileBytes + 1, maxFileBytes, 0},
		{"metadata row count", 1, 0, maxDirectoryRows},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var rows []directoryEntry
			for i := 0; i < tt.count; i++ {
				e := entry(fmt.Sprintf("Preset%d.ini", i), "")
				e.Size = tt.size
				rows = append(rows, e)
			}
			for i := 0; i < tt.extraRows; i++ {
				rows = append(rows, directoryEntry{Name: "README.md"})
			}
			if _, err := validateDirectory(rows); !errors.Is(err, ErrTooLarge) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestFetchRejectsInconsistentOrBinaryContent(t *testing.T) {
	for _, tt := range []struct {
		name     string
		body     string
		expected string
	}{
		{"different revision", "[custoM]\nruleset=DIRECT,[]GEOIP,CN\nruleset=Proxy,[]FINAL\n", examplePreset},
		{"short body", "[custom]", examplePreset},
		{"binary", "[custom]\x00", "[custom]\x00"},
		{"invalid UTF-8", "[custom]\xff", "[custom]\xff"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := fixtureClient(t, []directoryEntry{entry("Valid.ini", tt.expected)}, func(*http.Request) (*http.Response, error) {
				return response(http.StatusOK, tt.body), nil
			})
			s, err := c.Fetch(context.Background())
			if !errors.Is(err, ErrInvalidSnapshot) || !reflect.DeepEqual(s, Snapshot{}) {
				t.Fatalf("got snapshot=%+v error=%v", s, err)
			}
		})
	}
}

func TestDownloadLimitsAndErrors(t *testing.T) {
	for _, tt := range []struct {
		name   string
		result func() (*http.Response, error)
		want   error
	}{
		{"actual body limit", func() (*http.Response, error) { return response(200, strings.Repeat("x", maxFileBytes+1)), nil }, ErrTooLarge},
		{"declared length limit", func() (*http.Response, error) {
			r := response(200, "")
			r.ContentLength = maxFileBytes + 1
			return r, nil
		}, ErrTooLarge},
		{"HTTP failure", func() (*http.Response, error) { return response(500, "upstream-sensitive-detail"), nil }, ErrUnavailable},
		{"transport failure", func() (*http.Response, error) { return nil, errors.New("proxy password upstream-sensitive-detail") }, ErrUnavailable},
		{"rate limit", func() (*http.Response, error) { return response(429, ""), nil }, ErrRateLimited},
		{"GitHub rate limit", func() (*http.Response, error) {
			r := response(403, "")
			r.Header.Set("X-RateLimit-Remaining", "0")
			return r, nil
		}, ErrRateLimited},
		{"timeout", func() (*http.Response, error) { return nil, context.DeadlineExceeded }, context.DeadlineExceeded},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := fixtureClient(t, []directoryEntry{entry("Valid.ini", examplePreset)}, func(*http.Request) (*http.Response, error) { return tt.result() })
			s, err := c.Fetch(context.Background())
			if !errors.Is(err, tt.want) || !reflect.DeepEqual(s, Snapshot{}) {
				t.Fatalf("got snapshot=%+v error=%v", s, err)
			}
			if strings.Contains(err.Error(), "sensitive-detail") {
				t.Fatal("upstream error details leaked")
			}
		})
	}
}

func TestMetadataErrors(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		want       error
	}{
		{"invalid SHA", `{"sha":"../master"}`, ErrInvalidSnapshot},
		{"invalid JSON", `{"sha":`, ErrInvalidSnapshot},
		{"oversized metadata", strings.Repeat(" ", maxMetadataBytes+1), ErrTooLarge},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := NewClient()
			c.http.Transport = transportFunc(func(*http.Request) (*http.Response, error) { return response(200, tt.body), nil })
			if s, err := c.Fetch(context.Background()); !errors.Is(err, tt.want) || !reflect.DeepEqual(s, Snapshot{}) {
				t.Fatalf("got snapshot=%+v error=%v", s, err)
			}
		})
	}
}

func TestFetchRefusesRedirect(t *testing.T) {
	c := NewClient()
	var count atomic.Int32
	c.http.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		count.Add(1)
		r := response(http.StatusFound, "")
		r.Header.Set("Location", "http://127.0.0.1/private")
		return r, nil
	})
	if _, err := c.Fetch(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("got %v", err)
	}
	if count.Load() != 1 {
		t.Fatal("followed an upstream redirect")
	}
}

func TestDownloadConcurrencyAndCancellation(t *testing.T) {
	var entries []directoryEntry
	for i := 0; i < 12; i++ {
		entries = append(entries, entry(fmt.Sprintf("Preset%d.ini", i), examplePreset))
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{}, len(entries))
	var active, peak, count atomic.Int32
	c := fixtureClient(t, entries, func(r *http.Request) (*http.Response, error) {
		count.Add(1)
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		started <- struct{}{}
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	finished := make(chan error, 1)
	go func() { _, err := c.Fetch(ctx); finished <- err }()
	for range parallelRequests {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("downloads did not start")
		}
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation did not finish")
	}
	if active.Load() != 0 || peak.Load() != parallelRequests || count.Load() != parallelRequests {
		t.Fatalf("workers escaped bounds: active=%d peak=%d count=%d", active.Load(), peak.Load(), count.Load())
	}
}

func TestOneFailureCancelsOtherDownloads(t *testing.T) {
	var entries []directoryEntry
	for i := 0; i < 8; i++ {
		entries = append(entries, entry(fmt.Sprintf("Preset%d.ini", i), examplePreset))
	}
	var active atomic.Int32
	ready := sync.WaitGroup{}
	ready.Add(parallelRequests)
	c := fixtureClient(t, entries, func(r *http.Request) (*http.Response, error) {
		active.Add(1)
		defer active.Add(-1)
		ready.Done()
		ready.Wait()
		if strings.HasSuffix(r.URL.Path, "Preset0.ini") {
			return response(503, ""), nil
		}
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	s, err := c.Fetch(ctx)
	if !errors.Is(err, ErrUnavailable) || !reflect.DeepEqual(s, Snapshot{}) || active.Load() != 0 {
		t.Fatalf("partial snapshot or workers remained: %+v %v %d", s, err, active.Load())
	}
}

func TestClientIgnoresEnvironmentProxy(t *testing.T) {
	c := NewClient()
	tr, ok := c.http.Transport.(*http.Transport)
	if !ok || tr.Proxy != nil || c.http.Jar != nil || c.http.Timeout <= 0 || c.http.CheckRedirect == nil || tr.MaxConnsPerHost != parallelRequests {
		t.Fatal("client lacks fixed transport protections")
	}
}

// The ordinary suite uses only synthetic responses. This opt-in check verifies
// the live fixed endpoints without persisting or printing any upstream content.
func TestLiveOfficialSnapshot(t *testing.T) {
	if os.Getenv("CAPTAIN_ACL4SSR_LIVE") != "1" {
		t.Skip("set CAPTAIN_ACL4SSR_LIVE=1 to check the official preset snapshot")
	}
	s, err := NewClient().Fetch(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var total int
	for _, f := range s.Files {
		total += len(f.Content)
	}
	t.Logf("retrieved %d presets at %s (%d bytes)", len(s.Files), s.Revision, total)
}
