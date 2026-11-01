package agentdownload

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func digest(b string) string                                              { s := sha256.Sum256([]byte(b)); return hex.EncodeToString(s[:]) }
func response(r *http.Request, status int, b string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(b)), ContentLength: int64(len(b)), Request: r}
}
func testService(t *testing.T, fn transportFunc) *Service {
	t.Helper()
	s := New(t.TempDir())
	s.HTTP = &http.Client{Transport: fn}
	return s
}

func TestCatalogRejectsArbitrarySelectorsBeforeNetwork(t *testing.T) {
	s := testService(t, func(r *http.Request) (*http.Response, error) {
		t.Fatalf("unexpected upstream request %s", r.URL)
		return nil, nil
	})
	for _, v := range []string{"v0.87.0", "v0.88.0", "../../etc/passwd", "https://example.com", "v1.2.3?secret=1", "v01.2.3", "v0.89.0-beta.1"} {
		if _, e := s.Bosun(context.Background(), v, "bosun-linux-amd64"); e == nil {
			t.Fatal(v)
		}
	}
	for _, n := range []string{"../SHA256SUMS", "install.sh", "https://example.com", "bosun-darwin-arm64"} {
		if _, e := s.Bosun(context.Background(), MinVersion, n); e == nil {
			t.Fatal(n)
		}
	}
	for _, c := range []string{"unknown", "../../tmp", "https://example.com"} {
		if _, e := s.Core(context.Background(), c, "1.0.0", "linux", "amd64"); e == nil {
			t.Fatal(c)
		}
	}
	for _, raw := range []string{"http://github.com/x", "https://github.com.evil.example/x", "https://user:pw@github.com/x", "https://127.0.0.1/x", "https://github.com:443/x"} {
		if _, e := s.cached(context.Background(), raw, "", false, nil); e == nil {
			t.Fatal(raw)
		}
	}
}

func TestBosunChecksumBeforePublishingAndCacheRepair(t *testing.T) {
	const binary = "test native bosun artifact"
	var downloads atomic.Int32
	valid := true
	s := testService(t, func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("X-Bosun-Pair-Code") != "" {
			t.Fatal("credential sent upstream")
		}
		switch {
		case strings.Contains(r.URL.Path, "/releases/tags/"):
			rel := Release{TagName: MinVersion}
			for _, n := range []string{"bosun-linux-amd64", "bosun-linux-arm64", "SHA256SUMS"} {
				rel.Assets = append(rel.Assets, ReleaseAsset{Name: n, Size: 100, URL: "https://github.com/zeptop-dev/bosun/releases/download/" + MinVersion + "/" + n})
			}
			b, _ := json.Marshal(rel)
			return response(r, 200, string(b)), nil
		case strings.HasSuffix(r.URL.Path, "SHA256SUMS"):
			return response(r, 200, digest(binary)+"  bosun-linux-amd64\n"+digest(binary)+"  bosun-linux-arm64\n"), nil
		default:
			downloads.Add(1)
			b := binary
			if !valid {
				b = "corrupt"
			}
			return response(r, 200, b), nil
		}
	})
	valid = false
	if _, e := s.Bosun(context.Background(), MinVersion, "bosun-linux-amd64"); e == nil {
		t.Fatal("accepted checksum mismatch")
	}
	files, _ := filepath.Glob(filepath.Join(s.Dir, "*.bin"))
	if len(files) != 0 {
		t.Fatal("published bad artifact")
	}
	valid = true
	f, e := s.Bosun(context.Background(), MinVersion, "bosun-linux-amd64")
	if e != nil {
		t.Fatal(e)
	}
	if f.SHA256 != digest(binary) || !f.SourceVerified {
		t.Fatalf("bad evidence %+v", f)
	}
	if e = os.WriteFile(f.Path, []byte("disk corruption"), 0o600); e != nil {
		t.Fatal(e)
	}
	f, e = s.Bosun(context.Background(), MinVersion, "bosun-linux-amd64")
	if e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(f.Path)
	if string(b) != binary || downloads.Load() != 3 {
		t.Fatalf("cache was not repaired: %q, fetches %d", b, downloads.Load())
	}
	if _, e = s.Bosun(context.Background(), MinVersion, "bosun-linux-amd64"); e != nil || downloads.Load() != 3 {
		t.Fatalf("cache miss %v", e)
	}
}

func TestReleaseRejectsExternalAssetURL(t *testing.T) {
	s := testService(t, func(r *http.Request) (*http.Response, error) {
		return response(r, 200, `{"tag_name":"v0.89.0","assets":[{"name":"bosun-linux-amd64","size":12,"browser_download_url":"https://example.com/payload"}]}`), nil
	})
	if _, e := s.Release(context.Background(), "latest"); e == nil {
		t.Fatal("accepted external release artifact")
	}
}

func TestIncompleteReleaseCanRecoverWithoutWaitingForCacheExpiry(t *testing.T) {
	complete := false
	s := testService(t, func(r *http.Request) (*http.Response, error) {
		rel := Release{TagName: MinVersion}
		if complete {
			for _, n := range []string{"bosun-linux-amd64", "bosun-linux-arm64", "SHA256SUMS"} {
				rel.Assets = append(rel.Assets, ReleaseAsset{Name: n, Size: 1, URL: "https://github.com/zeptop-dev/bosun/releases/download/" + MinVersion + "/" + n})
			}
		}
		b, _ := json.Marshal(rel)
		return response(r, 200, string(b)), nil
	})
	if _, e := s.Release(context.Background(), MinVersion); e == nil {
		t.Fatal("accepted incomplete release")
	}
	complete = true
	if _, e := s.Release(context.Background(), MinVersion); e != nil {
		t.Fatalf("incomplete metadata poisoned immutable cache: %v", e)
	}
}

func TestConcurrentMetadataAndFilesAreSingleFlight(t *testing.T) {
	var count atomic.Int32
	s := testService(t, func(r *http.Request) (*http.Response, error) { count.Add(1); return response(r, 200, "content"), nil })
	for _, metadata := range []bool{true, false} {
		count.Store(0)
		var wg sync.WaitGroup
		start := make(chan struct{})
		for range 12 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				var e error
				if metadata {
					_, e = s.small(context.Background(), "https://api.github.com/test")
				} else {
					_, e = s.cached(context.Background(), "https://github.com/asset", digest("content"), true, nil)
				}
				if e != nil {
					t.Error(e)
				}
			}()
		}
		close(start)
		wg.Wait()
		if count.Load() != 1 {
			t.Fatalf("metadata=%v: upstream fetches=%d", metadata, count.Load())
		}
	}
}

func TestUpstreamRedirectCannotReachPrivateOrHTTP(t *testing.T) {
	for _, target := range []string{"http://github.com/file", "https://127.0.0.1/private", "https://example.com/payload"} {
		t.Run(target, func(t *testing.T) {
			var count int
			s := testService(t, func(r *http.Request) (*http.Response, error) {
				count++
				resp := response(r, 302, "")
				resp.Header.Set("Location", target)
				return resp, nil
			})
			if _, e := s.get(context.Background(), "https://github.com/file"); e == nil {
				t.Fatal("followed unsafe redirect")
			}
			if count != 1 {
				t.Fatal("unsafe redirect was requested")
			}
		})
	}
}

func TestCacheBoundAndChecksumParser(t *testing.T) {
	s := New(t.TempDir())
	for i := range 260 {
		p := filepath.Join(s.Dir, fmt.Sprintf("%03d.bin", i))
		if e := os.WriteFile(p, []byte("x"), 0o600); e != nil {
			t.Fatal(e)
		}
	}
	if e := s.prune(1, ""); e != nil {
		t.Fatal(e)
	}
	files, _ := filepath.Glob(filepath.Join(s.Dir, "*.bin"))
	if len(files) > 255 {
		t.Fatalf("unbounded cache: %d", len(files))
	}
	line := digest("x") + "  bosun-linux-amd64\n"
	if _, e := checksum([]byte(line+line), "bosun-linux-amd64"); e == nil {
		t.Fatal("accepted ambiguous digest")
	}
	if _, e := checksum([]byte("notsha  bosun-linux-amd64\n"), "bosun-linux-amd64"); e == nil {
		t.Fatal("accepted malformed digest")
	}
}

func TestImmutableMetadataSurvivesRestartAndCrashFilesArePruned(t *testing.T) {
	s := testService(t, func(r *http.Request) (*http.Response, error) { return response(r, 200, "checksum list"), nil })
	url := "https://github.com/zeptop-dev/bosun/releases/download/v0.89.0/SHA256SUMS"
	if _, e := s.small(context.Background(), url); e != nil {
		t.Fatal(e)
	}
	restarted := New(s.Dir)
	restarted.HTTP = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		t.Fatal("persisted immutable metadata should not need upstream")
		return nil, nil
	})}
	b, e := restarted.small(context.Background(), url)
	if e != nil || string(b) != "checksum list" {
		t.Fatalf("persistent metadata %q %v", b, e)
	}
	old := time.Now().Add(-25 * time.Hour)
	for _, name := range []string{".download-crashed", ".metadata-crashed", "orphan.bin.json"} {
		p := filepath.Join(s.Dir, name)
		if e = os.WriteFile(p, []byte("leftover"), 0o600); e != nil {
			t.Fatal(e)
		}
		if e = os.Chtimes(p, old, old); e != nil {
			t.Fatal(e)
		}
	}
	active := filepath.Join(s.Dir, ".download-active")
	if e = os.WriteFile(active, []byte("active"), 0o600); e != nil {
		t.Fatal(e)
	}
	if e = s.pruneAux(); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{".download-crashed", ".metadata-crashed", "orphan.bin.json"} {
		if _, e = os.Stat(filepath.Join(s.Dir, name)); !os.IsNotExist(e) {
			t.Fatalf("leftover kept %s", name)
		}
	}
	if _, e = os.Stat(active); e != nil {
		t.Fatal("removed active temporary download")
	}
}
