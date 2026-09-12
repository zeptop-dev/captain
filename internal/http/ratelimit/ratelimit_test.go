package ratelimit

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestLimiter(t *testing.T) {
	l := New()
	l.Max, l.Lock = 3, time.Minute
	for i := 0; i < 2; i++ {
		l.Fail("1.1.1.1")
	}
	if ok, _ := l.Allow("1.1.1.1"); !ok {
		t.Fatal("two failures should still be allowed")
	}
	l.Fail("1.1.1.1")
	if ok, wait := l.Allow("1.1.1.1"); ok || wait <= 0 {
		t.Fatalf("third failure should lock: %v %v", ok, wait)
	}
	if ok, _ := l.Allow("2.2.2.2"); !ok {
		t.Fatal("other addresses unaffected")
	}
	l.Reset("1.1.1.1")
	if ok, _ := l.Allow("1.1.1.1"); !ok {
		t.Fatal("reset should unlock")
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "172.18.0.3:4444"
	r.Header.Set("X-Forwarded-For", "203.0.113.9, 172.18.0.2")
	if got := ClientIP(r); got != "203.0.113.9" {
		t.Fatalf("proxy header should be trusted from a private address: %s", got)
	}
	r.RemoteAddr = "198.51.100.7:1"
	if got := ClientIP(r); got != "198.51.100.7" {
		t.Fatalf("header must be ignored from a public address: %s", got)
	}
}
