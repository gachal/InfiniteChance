package auth_test

import (
	"testing"

	"github.com/gachal/InfiniteChance/internal/auth"
)

func TestHashAndCheckPasswordRoundTrip(t *testing.T) {
	hash, err := auth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	// 哈希不得还原出明文。
	if len(hash) == 0 || hash == "correct horse battery staple" {
		t.Fatalf("hash = %q, want a bcrypt digest distinct from the input", hash)
	}
	if !auth.CheckPassword(hash, "correct horse battery staple") {
		t.Error("CheckPassword should accept the original password")
	}
	if auth.CheckPassword(hash, "wrong password") {
		t.Error("CheckPassword should reject a wrong password")
	}
}

func TestHashPasswordIsSaltedPerCall(t *testing.T) {
	first, err := auth.HashPassword("same-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	second, err := auth.HashPassword("same-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if first == second {
		t.Error("two hashes of the same password should differ (per-call salt)")
	}
}
