package agenthttps

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestPoolNamedOriginsSharePortAndRetireIndependently(t *testing.T) {
	cs, release := configurations(t, 1)
	old := cs[0]
	old.URL = strings.Replace(old.URL, "agent.example.com", "old.example.com", 1)
	next := old
	next.URL = strings.Replace(old.URL, "old.example.com", "new.example.com", 1)
	next.CertificateID = 2
	a := certificate(t, "old.example.com", 1, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	b := certificate(t, "new.example.com", 2, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	pool := NewPool(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("agent")) }))
	defer pool.Close(context.Background())
	release()
	entries := []EntryMaterial{{Config: old, Certificate: a.cert, Key: a.key}, {Config: next, Certificate: b.cert, Key: b.key}}
	if err := pool.Apply(context.Background(), entries); err != nil {
		t.Fatal(err)
	}
	for _, cfg := range []Config{old, next} {
		tr := transport(cfg, a, b)
		res, body := fetch(t, tr, cfg, "")
		tr.CloseIdleConnections()
		if res.StatusCode != 200 || body != "agent" {
			t.Fatal(res.StatusCode, body)
		}
	}
	// Registered names do not permit cross-origin SNI/Host fronting.
	cross := transport(next, a, b)
	crossResult, _ := fetch(t, cross, next, "old.example.com")
	cross.CloseIdleConnections()
	if crossResult.StatusCode != http.StatusMisdirectedRequest {
		t.Fatal("cross-origin authority accepted", crossResult.StatusCode)
	}
	// Invalid replacement must not remove either current origin.
	bad := entries[1]
	bad.Certificate = a.cert
	bad.Key = a.key
	if err := pool.Apply(context.Background(), []EntryMaterial{bad}); err == nil {
		t.Fatal("accepted mismatched certificate")
	}
	tr := transport(old, a, b)
	res, _ := fetch(t, tr, old, "")
	tr.CloseIdleConnections()
	if res.StatusCode != 200 {
		t.Fatal("old entry was lost")
	}
	if err := pool.Apply(context.Background(), entries[1:]); err != nil {
		t.Fatal(err)
	}
	tr = transport(next, a, b)
	res, _ = fetch(t, tr, next, "old.example.com")
	tr.CloseIdleConnections()
	if res.StatusCode != http.StatusMisdirectedRequest {
		t.Fatal("retired authority still routed", res.StatusCode)
	}
	if pool.Status(next.URL).State != "listening" {
		t.Fatal("new origin stopped")
	}
}

func TestPoolPermanentCloseCancelsLongPolls(t *testing.T) {
	configs, release := configurations(t, 1)
	cfg := configs[0]
	release()
	now := time.Now()
	cert := certificate(t, "agent.example.com", 1, now.Add(-time.Hour), now.Add(time.Hour))
	entered, exited, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	p := NewPool(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done(); close(exited) }))
	if err := p.Apply(context.Background(), []EntryMaterial{{Config: cfg, Certificate: cert.cert, Key: cert.key}}); err != nil {
		t.Fatal(err)
	}
	tr := transport(cfg, cert)
	defer tr.CloseIdleConnections()
	go func() {
		defer close(done)
		response, err := (&http.Client{Transport: tr, Timeout: 3 * time.Second}).Get(cfg.URL)
		if err == nil {
			response.Body.Close()
		}
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("long poll did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := p.Close(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("long poll context not cancelled")
	}
	<-done
	if err := p.Apply(context.Background(), nil); err == nil {
		t.Fatal("closed pool reopened")
	}
}
