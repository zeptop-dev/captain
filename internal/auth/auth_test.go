package auth

import (
	"testing"
	"time"
)

func TestPassword(t *testing.T) {
	h, err := HashPassword("s3cret")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(h, "s3cret") || VerifyPassword(h, "wrong") || VerifyPassword("garbage", "s3cret") {
		t.Fatal("verify mismatch")
	}
	if len(UUID()) != 36 || len(PairCode()) != 14 || Token(32) == Token(32) {
		t.Fatal("generators")
	}
}

func TestVerifyTOTPOnce(t *testing.T) {
	secret := NewTOTPSecret()
	now := time.Now()
	code := TOTPCode(secret, now)
	if !VerifyTOTPOnce("u1", secret, code, now) {
		t.Fatal("first use should pass")
	}
	if VerifyTOTPOnce("u1", secret, code, now) {
		t.Fatal("replay within the window should fail")
	}
	if !VerifyTOTPOnce("u2", secret, code, now) {
		t.Fatal("another key is independent")
	}
}
