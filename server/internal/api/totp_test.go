package api

import (
	"testing"
	"time"
)

// RFC 6238 appendix B test vectors (SHA-1, 8 digits) truncated to 6 digits.
func TestTOTPVectors(t *testing.T) {
	secret := b32.EncodeToString([]byte("12345678901234567890"))
	cases := map[int64]string{59: "287082", 1111111109: "081804", 1234567890: "005924", 2000000000: "279037"}
	for unix, want := range cases {
		got, err := totpCode(secret, time.Unix(unix, 0))
		if err != nil || got != want {
			t.Errorf("totp at %d = %s, %v; want %s", unix, got, err, want)
		}
	}
	now := time.Unix(1234567890, 0)
	if !validTOTP(secret, "005924", now) || !validTOTP(secret, "005924", now.Add(25*time.Second)) {
		t.Error("current and adjacent-step codes should validate")
	}
	if validTOTP(secret, "005924", now.Add(5*time.Minute)) || validTOTP(secret, "12345", now) {
		t.Error("stale or malformed codes must fail")
	}
}
