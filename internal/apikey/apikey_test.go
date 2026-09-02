package apikey

import (
	"strings"
	"testing"
	"time"
)

func TestGenerateShapeAndUniqueness(t *testing.T) {
	first, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.HasPrefix(first, "sk-") {
		t.Errorf("key = %q, want the sk- prefix", first)
	}
	// sk- + 30 字节 base64url(40 字符)。
	if len(first) != 3+40 {
		t.Errorf("key length = %d, want 43", len(first))
	}
	if strings.ContainsAny(first[3:], "+/=") {
		t.Errorf("key = %q, want URL-safe base64 only", first)
	}

	second, err := Generate()
	if err != nil {
		t.Fatalf("Generate again: %v", err)
	}
	if first == second {
		t.Error("two Generate calls returned the same key")
	}
}

func TestHashIsDeterministicHexAndKeyedToInput(t *testing.T) {
	a1 := Hash("sk-aaa")
	a2 := Hash("sk-aaa")
	b := Hash("sk-bbb")
	if a1 != a2 {
		t.Error("Hash is not deterministic")
	}
	if a1 == b {
		t.Error("different keys hash equal")
	}
	if len(a1) != 64 {
		t.Errorf("hash length = %d, want 64 hex chars (sha256)", len(a1))
	}
}

func TestPrefixOf(t *testing.T) {
	full := "sk-abcdefghij-REST-IS-SECRET"
	if got := PrefixOf(full); got != "sk-abcdefgh" { // 11 runes: sk- + 8 chars
		t.Errorf("PrefixOf = %q, want sk-abcdefgh", got)
	}
	if got := PrefixOf("sk-short"); got != "sk-short" {
		t.Errorf("PrefixOf short = %q, want unchanged", got)
	}
}

func TestStatusMatrix(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour).Add(time.Minute)
	past := now.Add(-time.Minute)

	cases := []struct {
		name string
		key  Key
		want string
	}{
		{"no expiry, not revoked", Key{}, StatusActive},
		{"expires later", Key{ExpiresAt: &future}, StatusActive},
		{"expired a minute ago", Key{ExpiresAt: &past}, StatusExpired},
		{"expiry exactly now is expired", Key{ExpiresAt: &now}, StatusExpired},
		{"revoked wins over active", Key{RevokedAt: &past}, StatusRevoked},
		{"revoked wins over expired", Key{RevokedAt: &past, ExpiresAt: &past}, StatusRevoked},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.key.Status(now); got != tc.want {
				t.Errorf("Status = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestUSDConversions(t *testing.T) {
	cases := []struct {
		usd  float64
		want int64
	}{
		{10, 10_000_000},
		{0.1, 100_000},
		{1.25, 1_250_000},
		{0.000001, 1},
		{0.0000004, 0}, // rounds to zero — the handler must reject this
	}
	for _, tc := range cases {
		if got := USDToMicros(tc.usd); got != tc.want {
			t.Errorf("USDToMicros(%v) = %d, want %d", tc.usd, got, tc.want)
		}
	}
	if got := MicrosToUSD(10_000_000); got != 10 {
		t.Errorf("MicrosToUSD = %v, want 10", got)
	}
	if got := MicrosToUSD(150); got != 0.00015 {
		t.Errorf("MicrosToUSD(150) = %v, want 0.00015", got)
	}
}
