package auth

import "testing"

func TestPassword(t *testing.T) {
	h, err := HashPassword("s3cret")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(h, "s3cret") || VerifyPassword(h, "wrong") || VerifyPassword("garbage", "s3cret") {
		t.Fatal("verify mismatch")
	}
	if len(UUID()) != 36 || len(PairCode()) != 9 || Token(32) == Token(32) {
		t.Fatal("generators")
	}
}
