package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TOTP per RFC 6238: SHA-1, 6 digits, 30-second steps, one step of drift
// either way — what Google Authenticator, 1Password and the rest expect.

// NewTOTPSecret returns a fresh base32 secret (20 random bytes).
func NewTOTPSecret() string {
	b := make([]byte, 20)
	_, _ = rand.Read(b)
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
}

// TOTPURI is the otpauth:// link authenticator apps scan.
func TOTPURI(issuer, account, secret string) string {
	return "otpauth://totp/" + url.PathEscape(issuer+":"+account) + "?secret=" + secret + "&issuer=" + url.QueryEscape(issuer) + "&algorithm=SHA1&digits=6&period=30"
}

func totpCode(secret string, step int64) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimRight(secret, "=")))
	if err != nil {
		return "", err
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", v%1000000), nil
}

// VerifyTOTP checks a 6-digit code against the secret at time now, allowing
// one step of clock drift on each side.
func VerifyTOTP(secret, code string, now time.Time) bool {
	code = strings.TrimSpace(strings.ReplaceAll(code, " ", ""))
	if len(code) != 6 {
		return false
	}
	if _, err := strconv.Atoi(code); err != nil {
		return false
	}
	_, ok := verifyTOTPStep(secret, code, now)
	return ok
}

// verifyTOTPStep is VerifyTOTP that also reports which time step the code
// belonged to (the current one or its drift neighbours).
func verifyTOTPStep(secret, code string, now time.Time) (int64, bool) {
	code = strings.TrimSpace(strings.ReplaceAll(code, " ", ""))
	if len(code) != 6 {
		return 0, false
	}
	if _, err := strconv.Atoi(code); err != nil {
		return 0, false
	}
	step := now.Unix() / 30
	for _, d := range []int64{0, -1, 1} {
		want, err := totpCode(secret, step+d)
		if err != nil {
			return 0, false
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return step + d, true
		}
	}
	return 0, false
}

var (
	totpMu   sync.Mutex
	totpUsed = map[string]int64{} // key -> last accepted step
)

// VerifyTOTPOnce is VerifyTOTP that refuses a code already accepted for
// key (a user id) within the same or an earlier step, so a sniffed code
// cannot be replayed inside its window.
func VerifyTOTPOnce(key, secret, code string, now time.Time) bool {
	matched, ok := verifyTOTPStep(secret, code, now)
	if !ok {
		return false
	}
	totpMu.Lock()
	defer totpMu.Unlock()
	// A code belongs to exactly one step; once that step's code was
	// accepted, that code (and any older one) is a replay. The next
	// step's code is a different code and stays valid.
	if last, used := totpUsed[key]; used && matched <= last {
		return false
	}
	totpUsed[key] = matched
	return true
}

// TOTPCode is the code valid at the given instant (for tests and setup checks).
func TOTPCode(secret string, now time.Time) string {
	c, _ := totpCode(secret, now.Unix()/30)
	return c
}
