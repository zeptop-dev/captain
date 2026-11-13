package http

import (
	"context"
	"io"
	"net"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/agenthttps"
	"github.com/zeptop-dev/captain/internal/auth"
	"github.com/zeptop-dev/captain/internal/store"
)

type agentCheckFunc func(context.Context, string) agenthttps.AccessCheck

func (f agentCheckFunc) Check(ctx context.Context, url string) agenthttps.AccessCheck {
	return f(ctx, url)
}

func TestAgentHTTPSCheckAuthorizationAndConcurrentChanges(t *testing.T) {
	started, release := make(chan string, 4), make(chan struct{})
	check := agentCheckFunc(func(ctx context.Context, url string) agenthttps.AccessCheck {
		started <- url
		select {
		case <-release:
		case <-ctx.Done():
		}
		return agenthttps.AccessCheck{URL: url, Status: "ok", Perspective: "panel", CheckedAt: time.Now().UTC()}
	})
	r := newRigAt(t, "http://test", Options{AgentCheck: check})
	// The fixture agent is connected to the main API, not the replaceable TLS entry.
	if err := r.st.ObserveAgentConnection(context.Background(), r.nodeID, auth.SHA256Hex(r.agent.token), "https://main.example.com", true); err != nil {
		t.Fatal(err)
	}
	var releaseOnce sync.Once
	releaseAll := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseAll)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); r.web.AgentHTTPS().Close(context.Background()) })
	get := func() store.AgentHTTPSSettings {
		v, err := r.st.AgentHTTPSSettings(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	post := func(c *client, rev string) (int, []byte) {
		code, body, _ := c.do("POST", agentHTTPSPath+"/check", map[string]string{"revision": rev}, nil)
		return code, body
	}
	code, body := post(r.c, get().Revision)
	if code != 200 || mustJSON[agenthttps.AccessCheck](t, body).Status != "disabled" {
		t.Fatalf("disabled: %d %s", code, body)
	}
	if code, _, _ := r.c.do("POST", agentHTTPSPath+"/check", map[string]string{"revision": "0", "url": "https://other.example.com"}, nil); code != 400 {
		t.Fatal("request target accepted", code)
	}
	if code, _, _ := r.c.do("POST", agentHTTPSPath+"/check", map[string]string{"revision": strings.Repeat("x", 4097)}, nil); code != 400 {
		t.Fatal("oversized request accepted", code)
	}
	if code, _, _ := r.c.do("POST", agentHTTPSPath+"/check", map[string]string{"revision": "0"}, map[string]string{"Origin": "https://other.example.com"}); code != 403 {
		t.Fatal("cross origin diagnostic", code)
	}
	if code, _ := post(r.c, "stale"); code != 409 {
		t.Fatal(code)
	}
	if code, _ := post(&client{t: t, srv: r.srv}, "0"); code != 401 {
		t.Fatal(code)
	}
	chain, key := certimateChain(t)
	cert, err := store.ParseCertificate("example.com", chain, key)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.st.UpsertCertificate(ctx, cert); err != nil {
		t.Fatal(err)
	}
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := occupied.Addr().String()
	_, port, _ := net.SplitHostPort(addr)
	in := get()
	in.Enabled, in.URL, in.Listen, in.CertificateID = true, "https://example.com:"+port, addr, cert.ID
	if _, err = r.st.SaveAgentHTTPS(ctx, in); err != nil {
		t.Fatal(err)
	}
	if code, body = post(r.c, get().Revision); code != 200 || mustJSON[agenthttps.AccessCheck](t, body).Status != "not_ready" {
		t.Fatalf("unbound: %d %s", code, body)
	}
	occupied.Close()
	go r.web.AgentHTTPS().Run(ctx)
	waitAgentHTTPS(t, func() bool { return r.web.AgentHTTPS().Ready(get()) })
	token, record, err := r.st.CreateAPIToken(ctx, 1, "check", "full", nil, []string{"agent-https:write"})
	if err != nil {
		t.Fatal(err)
	}
	tokenClient := &client{t: t, srv: r.srv, token: token}
	finished := make(chan int, 1)
	start := func(c *client) {
		rev := get().Revision
		go func() { code, _ := post(c, rev); finished <- code }()
		select {
		case url := <-started:
			if url != get().URL {
				t.Fatal("checked unsaved URL")
			}
		case <-time.After(3 * time.Second):
			t.Fatal("check did not start")
		}
	}
	await := func(want int) {
		select {
		case code := <-finished:
			if code != want {
				t.Fatalf("got %d want %d", code, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("check did not finish")
		}
	}
	start(tokenClient)
	if code, _ = post(r.c, get().Revision); code != 429 {
		t.Fatal("concurrent check", code)
	}
	// Acquiring the exclusive account lock while I/O is blocked proves the
	// router and diagnostic handler both released it. Revoke before replying.
	locked := make(chan struct{})
	go func() {
		r.st.Accounts.Lock()
		defer r.st.Accounts.Unlock()
		if err := r.st.DeleteAPIToken(ctx, 1, record.ID); err != nil {
			t.Error(err)
		}
		close(locked)
	}()
	select {
	case <-locked:
	case <-time.After(3 * time.Second):
		releaseAll()
		t.Fatal("network held account lock")
	}
	release <- struct{}{}
	await(403)
	start(r.c)
	r.st.Accounts.Lock()
	in = get()
	in.Enabled = false
	_, err = r.st.SaveAgentHTTPS(ctx, in)
	r.st.Accounts.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	release <- struct{}{}
	await(409)
	select {
	case <-started:
		t.Fatal("unexpected probe")
	default:
	}
}

type agentSlowBody struct {
	started, release chan struct{}
	once             sync.Once
	io.Reader
}

func (b *agentSlowBody) Read(p []byte) (int, error) {
	b.once.Do(func() { close(b.started) })
	<-b.release
	return b.Reader.Read(p)
}

func TestAgentHTTPSCheckSlowBodyDoesNotHoldAccounts(t *testing.T) {
	r := newRig(t)
	body := &agentSlowBody{started: make(chan struct{}), release: make(chan struct{}), Reader: strings.NewReader(`{"revision":"0"}`)}
	req := httptest.NewRequest("POST", agentHTTPSPath+"/check", body)
	req.AddCookie(r.c.cookie)
	finished := make(chan struct{})
	go func() { r.web.Handler().ServeHTTP(httptest.NewRecorder(), req); close(finished) }()
	defer func() { close(body.release); <-finished }()
	select {
	case <-body.started:
	case <-time.After(3 * time.Second):
		t.Fatal("body not read")
	}
	if !r.st.Accounts.TryLock() {
		t.Fatal("slow body holds the account lock")
	}
	r.st.Accounts.Unlock()
}
