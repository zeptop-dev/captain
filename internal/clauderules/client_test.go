package clauderules

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testRevision = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const testRules = "example.com\nfull:cdn.example.net\n"

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), ContentLength: -1}
}

func jsonResponse(value any) *http.Response {
	body, _ := json.Marshal(value)
	return response(http.StatusOK, string(body))
}

func metadata(body string) sourceFile {
	return sourceFile{Name: "anthropic", Path: sourcePath, Type: "file", SHA: gitBlobID([]byte(body)), Size: int64(len(body))}
}

func fixtureClient(t *testing.T, file any, download func(*http.Request) (*http.Response, error)) *Client {
	t.Helper()
	c := NewClient()
	c.http.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Scheme != "https" || r.URL.User != nil || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Proxy-Authorization") != "" {
			t.Error("unexpected method, scheme or credentials")
		}
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > fetchTimeout {
			t.Error("request lacks bounded deadline")
		}
		switch r.URL.String() {
		case apiRoot + "/commits/master":
			return jsonResponse(map[string]string{"sha": testRevision}), nil
		case apiRoot + "/contents/" + sourcePath + "?ref=" + testRevision:
			return jsonResponse(file), nil
		case rawRoot + testRevision + "/" + sourcePath:
			return download(r)
		default:
			t.Error("request escaped fixed snapshot URLs")
			return nil, errors.New("unexpected URL")
		}
	})
	return c
}

func TestFetchVerifiedSnapshotWithoutCredentials(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "fixture-secret")
	t.Setenv("GH_TOKEN", "fixture-secret")
	t.Setenv("HTTPS_PROXY", "https://user:fixture-secret@example.com")
	file := metadata(testRules)
	c := fixtureClient(t, map[string]any{
		"name": file.Name, "path": file.Path, "type": file.Type, "sha": file.SHA, "size": file.Size,
		// Remote metadata must not choose where the client connects.
		"download_url": "http://192.0.2.10/private", "url": "https://example.com/other",
	}, func(*http.Request) (*http.Response, error) { return response(200, testRules), nil })
	before := time.Now().Unix()
	snapshot, err := c.Fetch(context.Background())
	want := []Rule{{Type: "domain", Value: "example.com"}, {Type: "full", Value: "cdn.example.net"}}
	if err != nil || snapshot.Revision != testRevision || snapshot.SyncedAt < before || snapshot.SyncedAt > time.Now().Unix() || !reflect.DeepEqual(snapshot.Rules, want) {
		t.Fatalf("got %+v, %v", snapshot, err)
	}
	client := NewClient()
	transport := client.http.Transport.(*http.Transport)
	if transport.Proxy != nil || client.http.Jar != nil || client.http.Timeout <= 0 || client.http.CheckRedirect == nil {
		t.Fatal("client must not use environment proxy, cookies or unbounded requests")
	}
}

func TestFetchRejectsUnsafeMetadata(t *testing.T) {
	for _, tt := range []struct {
		name   string
		change func(*sourceFile)
		want   error
	}{
		{"wrong name", func(f *sourceFile) { f.Name = "other" }, ErrInvalidSnapshot},
		{"traversal", func(f *sourceFile) { f.Path = "data/../anthropic" }, ErrInvalidSnapshot},
		{"symlink", func(f *sourceFile) { f.Type = "symlink" }, ErrInvalidSnapshot},
		{"directory", func(f *sourceFile) { f.Type = "dir" }, ErrInvalidSnapshot},
		{"symlink target", func(f *sourceFile) { f.Target = "elsewhere" }, ErrInvalidSnapshot},
		{"submodule", func(f *sourceFile) { f.SubmoduleGitURL = "https://example.com/repository" }, ErrInvalidSnapshot},
		{"invalid blob ID", func(f *sourceFile) { f.SHA = "../master" }, ErrInvalidSnapshot},
		{"empty size", func(f *sourceFile) { f.Size = 0 }, ErrInvalidSnapshot},
		{"negative size", func(f *sourceFile) { f.Size = -1 }, ErrInvalidSnapshot},
		{"oversized file", func(f *sourceFile) { f.Size = maxFileBytes + 1 }, ErrTooLarge},
	} {
		t.Run(tt.name, func(t *testing.T) {
			file := metadata(testRules)
			tt.change(&file)
			c := fixtureClient(t, file, func(*http.Request) (*http.Response, error) {
				t.Fatal("invalid metadata must stop before content download")
				return nil, nil
			})
			assertFailure(t, c, tt.want)
		})
	}
}

func TestFetchRejectsChangedOrUnsupportedContent(t *testing.T) {
	for _, tt := range []struct {
		name, body, expected string
		want                 error
	}{
		{"forged blob", "changed.com\nfull:cdn.example.net\n", testRules, ErrInvalidSnapshot},
		{"truncated content", "example.com\n", testRules, ErrInvalidSnapshot},
		{"unsupported syntax", "example.com\ninclude:other\n", "example.com\ninclude:other\n", ErrInvalidRules},
		{"invalid UTF8", "example.com\xff", "example.com\xff", ErrInvalidRules},
		{"binary content", "example.com\x00", "example.com\x00", ErrInvalidRules},
		{"empty effective list", "# only comment\n", "# only comment\n", ErrInvalidRules},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := fixtureClient(t, metadata(tt.expected), func(*http.Request) (*http.Response, error) { return response(200, tt.body), nil })
			assertFailure(t, c, tt.want)
		})
	}
}

func TestFetchMetadataErrors(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		want       error
	}{
		{"invalid revision", `{"sha":"../master"}`, ErrInvalidSnapshot},
		{"uppercase revision", `{"sha":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}`, ErrInvalidSnapshot},
		{"null metadata", `null`, ErrInvalidSnapshot},
		{"malformed JSON", `{"sha":`, ErrInvalidSnapshot},
		{"metadata body limit", strings.Repeat(" ", maxMetadataBytes+1), ErrTooLarge},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := NewClient()
			c.http.Transport = transportFunc(func(*http.Request) (*http.Response, error) { return response(200, tt.body), nil })
			assertFailure(t, c, tt.want)
		})
	}
}

func TestFetchDownloadFailures(t *testing.T) {
	for _, tt := range []struct {
		name   string
		result func() (*http.Response, error)
		want   error
	}{
		{"body limit", func() (*http.Response, error) { return response(200, strings.Repeat("x", maxFileBytes+1)), nil }, ErrTooLarge},
		{"declared length limit", func() (*http.Response, error) {
			r := response(200, "")
			r.ContentLength = maxFileBytes + 1
			return r, nil
		}, ErrTooLarge},
		{"HTTP error", func() (*http.Response, error) { return response(500, "upstream-private-detail"), nil }, ErrUnavailable},
		{"network error", func() (*http.Response, error) { return nil, errors.New("upstream-private-detail") }, ErrUnavailable},
		{"429 rate limit", func() (*http.Response, error) { return response(429, "upstream-private-detail"), nil }, ErrRateLimited},
		{"403 rate limit", func() (*http.Response, error) {
			r := response(403, "upstream-private-detail")
			r.Header.Set("X-RateLimit-Remaining", "0")
			return r, nil
		}, ErrRateLimited},
		{"deadline", func() (*http.Response, error) { return nil, context.DeadlineExceeded }, context.DeadlineExceeded},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := fixtureClient(t, metadata(testRules), func(*http.Request) (*http.Response, error) { return tt.result() })
			assertFailure(t, c, tt.want)
		})
	}
}

func TestFetchDoesNotFollowRedirects(t *testing.T) {
	c := NewClient()
	var requests atomic.Int32
	c.http.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		r := response(http.StatusFound, "")
		r.Header.Set("Location", "http://192.0.2.10/private")
		return r, nil
	})
	assertFailure(t, c, ErrUnavailable)
	if requests.Load() != 1 {
		t.Fatal("client followed a redirect")
	}
}

func TestFetchCancellationAndParentDeadline(t *testing.T) {
	for _, alreadyCanceled := range []bool{true, false} {
		ctx, cancel := context.WithCancel(context.Background())
		c := NewClient()
		if alreadyCanceled {
			cancel()
		}
		c.http.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
			if alreadyCanceled {
				t.Error("canceled fetch performed network work")
			}
			cancel()
			<-r.Context().Done()
			return nil, r.Context().Err()
		})
		snapshot, err := c.Fetch(ctx)
		cancel()
		if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(snapshot, Snapshot{}) {
			t.Fatalf("cancellation returned %+v, %v", snapshot, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c := NewClient()
	c.http.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > time.Second {
			t.Error("parent deadline was extended")
		}
		return nil, context.DeadlineExceeded
	})
	if _, err := c.Fetch(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

func assertFailure(t *testing.T, c *Client, expected error) {
	t.Helper()
	snapshot, err := c.Fetch(context.Background())
	if !errors.Is(err, expected) || !reflect.DeepEqual(snapshot, Snapshot{}) {
		t.Fatalf("got snapshot=%+v, error=%v; want %v", snapshot, err, expected)
	}
	if strings.Contains(err.Error(), "upstream-private-detail") {
		t.Fatal("upstream details leaked into error")
	}
}

func TestLiveFetch(t *testing.T) {
	if os.Getenv("CAPTAIN_TEST_CLAUDE_RULES_LIVE") != "1" {
		t.Skip("explicit opt-in required for upstream network access")
	}
	snapshot, err := NewClient().Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("verified revision %s, %d community rules", snapshot.Revision, len(snapshot.Rules))
}
