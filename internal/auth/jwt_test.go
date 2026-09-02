package auth_test

import (
	"strings"
	"testing"
	"time"

	"github.com/gachal/InfiniteChance/internal/auth"
)

func issue(t *testing.T, secret string, at time.Time) (string, time.Time) {
	t.Helper()
	issuer := auth.NewIssuer(secret, auth.SessionTTL)
	token, expiresAt, err := issuer.Issue("admin", at)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	return token, expiresAt
}

func TestIssueThenParseRoundTrip(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	token, expiresAt := issue(t, "secret-1", now)

	if !expiresAt.Equal(now.Add(auth.SessionTTL)) {
		t.Errorf("expiresAt = %v, want %v", expiresAt, now.Add(auth.SessionTTL))
	}

	issuer := auth.NewIssuer("secret-1", auth.SessionTTL)
	claims, err := issuer.Parse(token)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if claims.Username != "admin" {
		t.Errorf("username = %q, want %q", claims.Username, "admin")
	}
	if claims.Issuer != "infinitechance" {
		t.Errorf("issuer = %q, want %q", claims.Issuer, "infinitechance")
	}
	if claims.ExpiresAt == nil || !claims.ExpiresAt.Time.Equal(expiresAt) {
		t.Errorf("exp = %v, want %v", claims.ExpiresAt, expiresAt)
	}
}

func TestParseRejectsTokenFromOtherSecret(t *testing.T) {
	token, _ := issue(t, "secret-1", time.Now())

	issuer := auth.NewIssuer("secret-2", auth.SessionTTL)
	if _, err := issuer.Parse(token); err == nil {
		t.Fatal("Parse with a different secret should fail")
	}
}

func TestParseRejectsExpiredToken(t *testing.T) {
	issuedAt := time.Now().Add(-2 * auth.SessionTTL)
	token, _ := issue(t, "secret-1", issuedAt)

	issuer := auth.NewIssuer("secret-1", auth.SessionTTL)
	if _, err := issuer.Parse(token); err == nil {
		t.Fatal("Parse of an expired token should fail")
	}
}

func TestParseRejectsGarbageAndEmptyStrings(t *testing.T) {
	issuer := auth.NewIssuer("secret-1", auth.SessionTTL)
	for _, token := range []string{"", "not-a-jwt", "a.b.c", strings.Repeat("x", 64)} {
		if _, err := issuer.Parse(token); err == nil {
			t.Errorf("Parse(%q) should fail", token)
		}
	}
}

func TestParseRejectsNoneAlgorithm(t *testing.T) {
	// alg=none 的令牌即使签名段为空也不得通过校验。
	noneToken := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJzdWIiOiJhZG1pbiJ9."

	issuer := auth.NewIssuer("secret-1", auth.SessionTTL)
	if _, err := issuer.Parse(noneToken); err == nil {
		t.Fatal("Parse of an alg=none token should fail")
	}
}
