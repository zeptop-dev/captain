package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	nodedownload "github.com/zeptop-dev/bosun/pkg/agentdownload"
	"github.com/zeptop-dev/bosun/pkg/bootstrap"
	"github.com/zeptop-dev/bosun/pkg/coremanifest"
	"github.com/zeptop-dev/captain/internal/agentdownload"
	"github.com/zeptop-dev/captain/internal/auth"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
	"github.com/zeptop-dev/captain/internal/testdb"
)

type upstreamFunc func(*http.Request) (*http.Response, error)

func (f upstreamFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func fixture(t *testing.T) (*store.Store, *http.ServeMux, *agentdownload.Service) {
	t.Helper()
	conn := testdb.OpenAt(t, filepath.Join(t.TempDir(), "test.db"))
	st := store.New(conn)
	svc := agentdownload.New(t.TempDir())
	mux := http.NewServeMux()
	Register(mux, Deps{Store: st, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), BaseURL: "https://panel.example.com", AgentURL: "https://agent.example.com:24443", Downloads: svc})
	return st, mux, svc
}

func TestDownloadCredentialsSwitchFromPairToToken(t *testing.T) {
	st, mux, _ := fixture(t)
	ctx := context.Background()
	n := &domain.Node{Name: "node"}
	if e := st.CreateNode(ctx, n, "TEST-PAIR", time.Hour); e != nil {
		t.Fatal(e)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()
	tokenFile := filepath.Join(t.TempDir(), "token")
	c, e := nodedownload.New(srv.URL, tokenFile, "TEST-PAIR", true)
	if e != nil {
		t.Fatal(e)
	}
	get := func(client *http.Client, route string) int {
		t.Helper()
		r, e := client.Get(srv.URL + route)
		if e != nil {
			t.Fatal(e)
		}
		defer r.Body.Close()
		io.Copy(io.Discard, r.Body)
		return r.StatusCode
	}
	route := "/api/agent/downloads/bootstrap/install.sh"
	if status := get(srv.Client(), route); status != 401 {
		t.Fatalf("anonymous download %d", status)
	}
	if status := get(c.HTTP, route); status != 200 {
		t.Fatalf("valid pair download %d", status)
	}
	if _, e = st.RedeemPairCode(ctx, "TEST-PAIR", auth.SHA256Hex("new-node-token"), "node", MinVersionForTest, "linux/amd64"); e != nil {
		t.Fatal(e)
	}
	if status := get(c.HTTP, route); status != 401 {
		t.Fatalf("redeemed pair still works: %d", status)
	}
	if e = os.WriteFile(tokenFile, []byte("new-node-token"), 0o600); e != nil {
		t.Fatal(e)
	}
	if status := get(c.HTTP, route); status != 200 {
		t.Fatalf("paired token download %d", status)
	}
	if status := get(c.HTTP, "/api/agent/downloads/bootstrap/unknown"); status != 404 {
		t.Fatalf("unknown artifact %d", status)
	}
}

const MinVersionForTest = "v0.89.0"

func TestOpenWrtServiceDownloadRequiresCurrentCredentials(t *testing.T) {
	st, mux, _ := fixture(t)
	ctx := context.Background()
	n := &domain.Node{Name: "home-probe"}
	if err := st.CreateNode(ctx, n, "OPENWRT-PAIR", time.Hour); err != nil {
		t.Fatal(err)
	}
	want, ok := bootstrap.File("bosun.procd")
	if !ok || len(want) == 0 {
		t.Fatal("OpenWrt service is missing from the bootstrap distribution")
	}
	get := func(pair, token string, code int) {
		t.Helper()
		r := httptest.NewRequest("GET", "/api/agent/downloads/bootstrap/bosun.procd", nil)
		if pair != "" {
			r.Header.Set("X-Bosun-Pair-Code", pair)
		}
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != code {
			t.Fatalf("OpenWrt bootstrap status = %d, want %d", w.Code, code)
		}
		if code == http.StatusOK && w.Body.String() != string(want) {
			t.Fatal("OpenWrt service differs from the embedded artifact")
		}
		if w.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("authenticated bootstrap must not be cached publicly")
		}
	}
	get("", "", http.StatusUnauthorized)
	get("OPENWRT-PAIR", "", http.StatusOK)
	if _, err := st.RedeemPairCode(ctx, "OPENWRT-PAIR", auth.SHA256Hex("openwrt-node-token"), "home-probe", MinVersionForTest, "linux/arm64"); err != nil {
		t.Fatal(err)
	}
	get("OPENWRT-PAIR", "", http.StatusUnauthorized)
	get("", "openwrt-node-token", http.StatusOK)
	get("", "invalid-node-token", http.StatusUnauthorized)
}

func TestDownloadRevalidatesPairAfterUpstreamWait(t *testing.T) {
	st, mux, svc := fixture(t)
	ctx := context.Background()
	n := &domain.Node{Name: "node"}
	if e := st.CreateNode(ctx, n, "WAIT-PAIR", time.Hour); e != nil {
		t.Fatal(e)
	}
	started, release := make(chan struct{}), make(chan struct{})
	svc.HTTP = &http.Client{Transport: upstreamFunc(func(r *http.Request) (*http.Response, error) {
		close(started)
		<-release
		rel := agentdownload.Release{TagName: MinVersionForTest}
		for _, a := range []string{"bosun-linux-amd64", "bosun-linux-arm64", "SHA256SUMS"} {
			rel.Assets = append(rel.Assets, agentdownload.ReleaseAsset{Name: a, URL: "https://github.com/zeptop-dev/bosun/releases/download/" + MinVersionForTest + "/" + a, Size: 20})
		}
		b, _ := json.Marshal(rel)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(b))), Request: r}, nil
	})}
	r := httptest.NewRequest("GET", "/api/agent/downloads/repos/zeptop-dev/bosun/releases/latest", nil)
	r.Header.Set("X-Bosun-Pair-Code", "WAIT-PAIR")
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { mux.ServeHTTP(w, r); close(done) }()
	<-started
	// A real account writer must be able to acquire the lock while the network waits.
	locked := make(chan struct{})
	go func() { st.Accounts.Lock(); st.Accounts.Unlock(); close(locked) }()
	select {
	case <-locked:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("network held account lock")
	}
	if _, e := st.RedeemPairCode(ctx, "WAIT-PAIR", auth.SHA256Hex("paired"), "node", MinVersionForTest, "linux/amd64"); e != nil {
		close(release)
		t.Fatal(e)
	}
	close(release)
	<-done
	if w.Code != 401 {
		t.Fatalf("stale pre-pair ticket received metadata: %d %s", w.Code, w.Body.String())
	}
}

func TestInstallUsesEffectiveEndpointAndNoCredentialRedirect(t *testing.T) {
	st, mux, _ := fixture(t)
	n := &domain.Node{Name: "node", AgentURL: "https://edge.example.com:24443", DownloadSource: "captain"}
	if e := st.CreateNode(context.Background(), n, "BOOT-PAIR", time.Hour); e != nil {
		t.Fatal(e)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/agent/install.sh?pair=BOOT-PAIR", nil))
	b := w.Body.String()
	for _, want := range []string{"https://edge.example.com:24443/api/agent/downloads/bootstrap/install.sh", "--captain 'https://edge.example.com:24443'", "--download-source captain", "--max-redirs 0", "--proto '=https'", "X-Bosun-Pair-Code: BOOT-PAIR"} {
		if !strings.Contains(b, want) {
			t.Fatalf("missing %q: %s", want, b)
		}
	}
	if strings.Contains(b, "https://panel.example.com") || strings.Contains(b, "githubusercontent.com") {
		t.Fatalf("installation bypassed endpoint: %s", b)
	}
	if got := shellQuote("a'$(touch /tmp/example)"); got != "'a'\"'\"'$(touch /tmp/example)'" {
		t.Fatalf("unsafe shell quoting %s", got)
	}
}

func TestNodeClientConsumesCaptainCoreManifestAndCachedBytes(t *testing.T) {
	st, mux, svc := fixture(t)
	n := &domain.Node{Name: "node"}
	if e := st.CreateNode(context.Background(), n, "CORE-PAIR", time.Hour); e != nil {
		t.Fatal(e)
	}
	// Exercise a catalog entry without an upstream checksum. Captain must not
	// turn its computed content digest into a claim of independent provenance.
	var rel coremanifest.Release
	for _, r := range coremanifest.Manifest {
		if a, ok := r.Assets["linux/amd64"]; ok && a.SHA256 == "" && a.SumsURL == "" {
			rel = r
			break
		}
	}
	if rel.Core == "" {
		t.Skip("every current catalog entry has upstream verification")
	}
	const body = "test archive bytes"
	var calls atomic.Int32
	svc.HTTP = &http.Client{Transport: upstreamFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.String() != rel.Assets["linux/amd64"].URL {
			t.Fatalf("wrong catalog URL %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)), Request: r}, nil
	})}
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c, e := nodedownload.New(srv.URL, filepath.Join(t.TempDir(), "token"), "CORE-PAIR", true)
	if e != nil {
		t.Fatal(e)
	}
	m, e := c.CoreManifest(context.Background(), rel.Core, rel.Version, "linux/amd64")
	if e != nil {
		t.Fatal(e)
	}
	h := sha256.Sum256([]byte(body))
	if m.SHA256 != hex.EncodeToString(h[:]) || m.SourceVerified {
		t.Fatalf("unexpected manifest %+v", m)
	}
	r, e := c.HTTP.Get(m.URL)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Body.Close()
	b, e := io.ReadAll(r.Body)
	if e != nil || r.StatusCode != 200 || string(b) != body || calls.Load() != 1 {
		t.Fatalf("download status=%d body=%q fetches=%d error=%v", r.StatusCode, b, calls.Load(), e)
	}
}
