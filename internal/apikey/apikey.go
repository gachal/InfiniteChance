// Package apikey implements the gateway's caller credentials: sk- keys with
// hashed storage, quota and expiry/revocation, the admin HTTP surface that
// issues and tops them up, and the RequireKey middleware the relay (/v1)
// surface mounts to reject unknown, revoked and expired keys uniformly.
package apikey

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"math"
	"time"
)

// ErrKeyNotFound reports a key that does not exist (unknown hash or id).
var ErrKeyNotFound = errors.New("apikey: not found")

const (
	// keyPrefix starts every issued key, matching OpenAI convention.
	keyPrefix = "sk-"
	// randomBytes is 30 bytes of entropy: base64url-encoded to 40 chars, so a
	// full key is sk- + 40 chars. 240 bits of entropy is far beyond guessing.
	randomBytes = 30
	// prefixRunes is the stored/displayed leading slice: sk- + 8 random chars,
	// enough for the admin to tell keys apart without learning the secret.
	prefixRunes = 11
)

// Quota is an integer count of micro-USD (1 USD = 1e6 micros): billing does
// all arithmetic in this unit — the conditional-decrement UPDATE in ticket 04
// relies on it — while the admin API converts to/from human USD at the edge.
const MicrosPerUSD = 1_000_000

// MaxTopUpUSD caps a single manual credit so a typo cannot mint unlimited
// balance; call it again to add more.
const MaxTopUpUSD = 1_000_000

// Ledger reasons recorded in the quota log. Ticket 04's billing appends
// pre-deduction/refund entries with its own reasons.
const (
	ReasonInitial     = "initial"
	ReasonManualTopUp = "manual_topup"
)

// Key statuses as reported to the admin.
const (
	StatusActive  = "active"
	StatusRevoked = "revoked"
	StatusExpired = "expired"
)

// Key is one issued credential. KeyHash is the only stored form of the
// secret (SHA-256 hex) — the full value exists solely in the create response.
type Key struct {
	ID          int64
	Name        string
	Prefix      string
	KeyHash     string
	QuotaMicros int64
	ExpiresAt   *time.Time // nil = 永不过期
	RevokedAt   *time.Time // nil = 未吊销
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Status derives the current state; expiry is inclusive (a key is valid
// strictly before its expires_at).
func (k Key) Status(now time.Time) string {
	switch {
	case k.RevokedAt != nil:
		return StatusRevoked
	case k.ExpiresAt != nil && !now.Before(*k.ExpiresAt):
		return StatusExpired
	default:
		return StatusActive
	}
}

// Generate mints a fresh full key: sk- + 40 base64url chars.
func Generate() (string, error) {
	buf := make([]byte, randomBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return keyPrefix + base64.RawURLEncoding.EncodeToString(buf), nil
}

// Hash is the at-rest form of a presented key. SHA-256 (not bcrypt) is
// deliberate: keys are high-entropy random secrets, not passwords, and the
// relay path does a lookup per request.
func Hash(presented string) string {
	sum := sha256.Sum256([]byte(presented))
	return hex.EncodeToString(sum[:])
}

// PrefixOf returns the display prefix of a full key.
func PrefixOf(full string) string {
	runes := []rune(full)
	if len(runes) > prefixRunes {
		runes = runes[:prefixRunes]
	}
	return string(runes)
}

// USDToMicros converts a human USD amount to quota micros, rounding to the
// nearest micro so float artifacts at the API edge never accumulate.
func USDToMicros(usd float64) int64 {
	return int64(math.Round(usd * MicrosPerUSD))
}

// MicrosToUSD converts quota micros back to human USD for the admin API.
func MicrosToUSD(micros int64) float64 {
	return float64(micros) / MicrosPerUSD
}

// QuotaEntry is one immutable quota ledger row: what changed, the balance
// right after it, and why — the traceable history behind every balance.
type QuotaEntry struct {
	ID            int64
	DeltaMicros   int64
	BalanceMicros int64
	Reason        string
	CreatedAt     time.Time
}

// Store persists keys and their quota ledger.
type Store interface {
	// Create stores a prepared key (hash/prefix/quota already set) and, when
	// QuotaMicros > 0, the matching initial ledger row. Returns the stored
	// row with ID and timestamps.
	Create(ctx context.Context, k Key) (Key, error)
	List(ctx context.Context) ([]Key, error)
	// ByID returns the key or ErrKeyNotFound.
	ByID(ctx context.Context, id int64) (Key, error)
	// ByHash looks a presented key up by its hash. It returns the row
	// regardless of revoked/expired state so callers can distinguish the
	// rejection reason; ErrKeyNotFound when unknown.
	ByHash(ctx context.Context, hash string) (Key, error)
	// Revoke stamps revoked_at (idempotent for already-revoked keys) and
	// returns the updated row, or ErrKeyNotFound.
	Revoke(ctx context.Context, id int64, at time.Time) (Key, error)
	// TopUp adds deltaMicros (> 0) and appends a ledger row with the new
	// balance, atomically. Returns the updated row or ErrKeyNotFound.
	TopUp(ctx context.Context, id int64, deltaMicros int64, reason string) (Key, error)
	// QuotaLog returns the key's ledger, newest first, at most limit rows.
	QuotaLog(ctx context.Context, keyID int64, limit int) ([]QuotaEntry, error)
}
