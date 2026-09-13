package auth

import (
	"testing"
	"time"
)

func TestTOTPVector(t *testing.T) {
	// RFC 6238 test vector: secret "12345678901234567890" (base32 GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ), T=59 -> 287082 (SHA1, 8 digits 94287082 -> 6 digits 287082).
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	if code, _ := totpCode(secret, 59/30); code != "287082" {
		t.Fatalf("code %s", code)
	}
	at := time.Unix(59, 0)
	if !VerifyTOTP(secret, "287082", at) || !VerifyTOTP(secret, "287 082", at.Add(30*time.Second)) || VerifyTOTP(secret, "000000", at) || VerifyTOTP(secret, "28708", at) {
		t.Fatal("verify")
	}
	if s := NewTOTPSecret(); len(s) != 32 {
		t.Fatalf("secret %s", s)
	}
}
