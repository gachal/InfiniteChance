// Package auth implements the single-admin session layer shared by both
// services: bcrypt password hashing, HS256 JWT issue/parse, the MySQL
// account store, and the gin handlers/middleware for the /auth endpoints.
//
// The gateway issues tokens (init/login); canvas/server validates them with
// the same secret, so the two services share one account system.
package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// SessionTTL is how long an issued JWT stays valid. There is no refresh
// flow: an expired session means a fresh login.
const SessionTTL = 7 * 24 * time.Hour

// tokenIssuer is stamped into every claim and required back on parse, so
// tokens minted by other systems sharing the secret are rejected.
const tokenIssuer = "infinitechance"

// Claims is the JWT payload for an admin session.
type Claims struct {
	Username string `json:"username"`
	jwt.RegisteredClaims
}

// Issuer signs and validates HS256 session tokens with one shared secret.
type Issuer struct {
	secret []byte
	ttl    time.Duration
}

func NewIssuer(secret string, ttl time.Duration) *Issuer {
	return &Issuer{secret: []byte(secret), ttl: ttl}
}

// Issue mints a token for username valid from now until now+ttl, returning
// the signed string and the absolute expiry. now is truncated to whole
// seconds because the JWT exp claim is a NumericDate — callers comparing
// expiries across endpoints then see one value.
func (i *Issuer) Issue(username string, now time.Time) (string, time.Time, error) {
	now = now.Truncate(time.Second)
	expiresAt := now.Add(i.ttl)
	claims := Claims{
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    tokenIssuer,
			Subject:   username,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(i.secret)
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, expiresAt, nil
}

// Parse validates signature, algorithm, issuer and expiry, returning the
// claims of a token this service accepts.
func (i *Issuer) Parse(token string) (*Claims, error) {
	parsed, err := jwt.ParseWithClaims(
		token,
		&Claims{},
		func(*jwt.Token) (any, error) { return i.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(tokenIssuer),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, err
	}
	claims, ok := parsed.Claims.(*Claims)
	if !ok || !parsed.Valid {
		return nil, errors.New("auth: invalid token claims")
	}
	return claims, nil
}

// IsExpiredError reports whether err came from parsing a well-signed token
// whose exp has passed — distinct from garbage or foreign-secret tokens.
func IsExpiredError(err error) bool {
	return errors.Is(err, jwt.ErrTokenExpired)
}
