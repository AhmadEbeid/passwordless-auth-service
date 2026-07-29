package auth_test

import (
	"errors"
	"testing"
	"time"

	"github.com/AhmadEbeid/passwordless-auth-service/internal/auth"
)

func TestNormalizePhone(t *testing.T) {
	const want = "+201012345678"
	valid := map[string]string{
		"leading zero trunk prefix": "01012345678",
		"pasted +20":                "+201012345678",
		"bare ten digits":           "1012345678",
		"spaced and dashed":         " 010-1234-5678 ",
	}
	for name, in := range valid {
		t.Run("valid/"+name, func(t *testing.T) {
			got, err := auth.NormalizePhone(in)
			if err != nil {
				t.Fatalf("NormalizePhone(%q) error = %v, want nil", in, err)
			}
			if got != want {
				t.Fatalf("NormalizePhone(%q) = %q, want %q", in, got, want)
			}
		})
	}

	invalid := map[string]string{
		"too short":       "0101234567",
		"too long":        "010123456789",
		"non +20 country": "+15551234567",
		"empty":           "",
		"non digits":      "010abc45678",
	}
	for name, in := range invalid {
		t.Run("invalid/"+name, func(t *testing.T) {
			if _, err := auth.NormalizePhone(in); !errors.Is(err, auth.ErrInvalidPhone) {
				t.Fatalf("NormalizePhone(%q) error = %v, want ErrInvalidPhone", in, err)
			}
		})
	}
}

var base = time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)

func TestVerificationIsExpired(t *testing.T) {
	v := &auth.Verification{ExpiresAt: base.Add(auth.OTPTTL)}
	if v.IsExpired(base.Add(5 * time.Minute)) {
		t.Fatal("challenge should not be expired 5 min in")
	}
	if !v.IsExpired(base.Add(auth.OTPTTL + time.Second)) {
		t.Fatal("challenge should be expired past its 10-min window")
	}
}

func TestVerificationRegisterFailureLocksOnFifth(t *testing.T) {
	v := &auth.Verification{Status: auth.StatusPending}
	for attempt := 1; attempt <= auth.MaxFailedAttempts-1; attempt++ {
		if locked := v.RegisterFailure(base); locked {
			t.Fatalf("attempt %d locked too early", attempt)
		}
		if got, want := v.RemainingAttempts(), auth.MaxFailedAttempts-attempt; got != want {
			t.Fatalf("after attempt %d RemainingAttempts = %d, want %d", attempt, got, want)
		}
	}

	if locked := v.RegisterFailure(base); !locked {
		t.Fatal("fifth wrong attempt should lock")
	}
	if v.Status != auth.StatusLocked {
		t.Fatalf("status = %q, want locked", v.Status)
	}
	if v.LockedUntil == nil || !v.LockedUntil.Equal(base.Add(auth.LockoutDuration)) {
		t.Fatalf("LockedUntil = %v, want %v", v.LockedUntil, base.Add(auth.LockoutDuration))
	}
	if !v.IsLocked(base) {
		t.Fatal("challenge should report locked")
	}
	if v.IsLocked(base.Add(auth.LockoutDuration + time.Second)) {
		t.Fatal("lock should lift after the lockout window")
	}
}

func TestVerificationResendReady(t *testing.T) {
	v := &auth.Verification{SentAt: base}
	if v.ResendReady(base.Add(30 * time.Second)) {
		t.Fatal("resend should not be ready inside the 60-s cooldown")
	}
	if !v.ResendReady(base.Add(auth.ResendCooldown)) {
		t.Fatal("resend should be ready once the cooldown elapses")
	}
	if want := base.Add(auth.ResendCooldown); !v.ResendAvailableAt().Equal(want) {
		t.Fatalf("ResendAvailableAt = %v, want %v", v.ResendAvailableAt(), want)
	}
}

func TestVerificationSendCapReached(t *testing.T) {
	if (&auth.Verification{SendsInWindow: auth.MaxSendsPerWindow - 1}).SendCapReached() {
		t.Fatal("cap should not be reached below the limit")
	}
	if !(&auth.Verification{SendsInWindow: auth.MaxSendsPerWindow}).SendCapReached() {
		t.Fatal("cap should be reached at the limit")
	}
}

func TestVerificationRegisterSend(t *testing.T) {
	v := &auth.Verification{}
	v.RegisterSend(base)
	if v.SendsInWindow != 1 {
		t.Fatalf("SendsInWindow = %d, want 1", v.SendsInWindow)
	}
	if !v.SentAt.Equal(base) {
		t.Fatalf("SentAt = %v, want %v", v.SentAt, base)
	}
	if !v.ExpiresAt.Equal(base.Add(auth.OTPTTL)) {
		t.Fatalf("ExpiresAt = %v, want %v", v.ExpiresAt, base.Add(auth.OTPTTL))
	}
}
