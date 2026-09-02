// Package pricing implements the gateway's dual-track model pricing: chat
// models bill by token with an upstream-cost multiplier (ratio track, this
// build), image/video models will bill per call in USD (call track, ticket
// 07 — the structure reserves it, the admin API rejects it for now). All
// amounts are integer micro-USD like the quota ledger, so billing arithmetic
// never touches floats; the admin API converts at the edge.
package pricing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"
	"unicode/utf8"
)

// Unit names a billing track. TokenTrack bills chat usage by token counts
// times a ratio; CallTrack (ticket 07) will bill per generated item in USD
// with size/duration factors.
type Unit string

const (
	UnitToken Unit = "token"
	UnitCall  Unit = "call"
)

// ErrNotFound reports a public model with no price row.
var ErrNotFound = errors.New("pricing: not found")

const (
	// ModelNameRunes matches the channel package's model-name bound so a
	// mapping key can always be priced.
	ModelNameRunes = 200
	// MaxUSDPerMTokens bounds a per-million-token price at $10,000 — a
	// typo shield, not a business rule.
	MaxUSDPerMTokens = 10_000
	// MaxRatio bounds the multiplier at ×1000 for the same reason.
	MaxRatio = 1_000
)

// ChargeMicros' division denominators: prices are per million tokens and
// ratios are in micro-units, so (tokens × micros-per-M × ratio-micros)
// divides by 1e6 × 1e6 to land on micro-USD.
const (
	mtokensDenom = 1_000_000
	ratioDenom   = 1_000_000
)

// TokenPrice is the token-track price of one public model. Costs are the
// upstream's micro-USD per million tokens; the key is charged
// upstream cost × ratio (RatioMicros = 1e6 means ×1.0, at cost).
type TokenPrice struct {
	InputMicrosPerMTokens  int64 `json:"input_micros_per_mtokens"`
	OutputMicrosPerMTokens int64 `json:"output_micros_per_mtokens"`
	RatioMicros            int64 `json:"ratio_micros"`
}

// Price is one public model's price row: the track discriminator plus
// exactly one track payload.
type Price struct {
	PublicModel string
	Unit        Unit
	Token       *TokenPrice // Unit == UnitToken 时非 nil
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ChargeMicros computes what a request costs the key: ceil of upstream cost
// times ratio, rounded up to whole micros so billed amounts never drift
// below true cost. Zero token counts or a free model yield 0. The multiply
// runs in big.Int — micros-per-M × ratio × token counts can overflow int64
// at legitimate upper bounds.
func (t TokenPrice) ChargeMicros(promptTokens, completionTokens int64) int64 {
	if promptTokens < 0 {
		promptTokens = 0
	}
	if completionTokens < 0 {
		completionTokens = 0
	}
	num := new(big.Int).Mul(big.NewInt(promptTokens), big.NewInt(t.InputMicrosPerMTokens))
	num.Add(num, new(big.Int).Mul(big.NewInt(completionTokens), big.NewInt(t.OutputMicrosPerMTokens)))
	num.Mul(num, big.NewInt(t.RatioMicros))

	den := new(big.Int).Mul(big.NewInt(mtokensDenom), big.NewInt(ratioDenom))
	q, rem := new(big.Int).QuoRem(num, den, new(big.Int))
	if rem.Sign() > 0 {
		q.Add(q, big.NewInt(1))
	}
	if !q.IsInt64() {
		return math.MaxInt64
	}
	return q.Int64()
}

// Snapshot renders the price as it applied to one request, for the usage
// log's 倍率快照: audits must be able to recompute the charge even after the
// price row changes later.
func (p Price) Snapshot() (json.RawMessage, error) {
	s := struct {
		Unit  Unit        `json:"unit"`
		Token *TokenPrice `json:"token,omitempty"`
		Call  any         `json:"call,omitempty"` // ticket 07 按次轨
	}{Unit: p.Unit}
	switch p.Unit {
	case UnitToken:
		s.Token = p.Token
	case UnitCall:
		return nil, fmt.Errorf("pricing: call track not implemented yet")
	}
	return json.Marshal(s)
}

// Normalize validates and canonicalizes a Price. It enforces the dual-track
// invariant: unit token ⇒ Token payload present; unit call is reserved for
// ticket 07 and rejected until then.
func (p Price) Normalize() (Price, error) {
	p.PublicModel = strings.TrimSpace(p.PublicModel)
	if p.PublicModel == "" {
		return p, fmt.Errorf("公开模型名不能为空")
	}
	if utf8.RuneCountInString(p.PublicModel) > ModelNameRunes {
		return p, fmt.Errorf("公开模型名最多 %d 个字符", ModelNameRunes)
	}
	switch p.Unit {
	case UnitToken:
		if p.Token == nil {
			return p, fmt.Errorf("token 计价需要输入/输出单价与倍率")
		}
		t := *p.Token
		if err := validatePerMTokens("输入单价", t.InputMicrosPerMTokens); err != nil {
			return p, err
		}
		if err := validatePerMTokens("输出单价", t.OutputMicrosPerMTokens); err != nil {
			return p, err
		}
		if t.RatioMicros < 0 || t.RatioMicros > MaxRatio*ratioDenom {
			return p, fmt.Errorf("倍率需在 0 到 %g 之间", float64(MaxRatio))
		}
		p.Token = &t
	case UnitCall:
		return p, fmt.Errorf("按次计价尚未开放(07 号票落地)")
	default:
		return p, fmt.Errorf("未知计价单位:%s", p.Unit)
	}
	return p, nil
}

func validatePerMTokens(label string, micros int64) error {
	if micros < 0 || micros > MaxUSDPerMTokens*mtokensDenom {
		return fmt.Errorf("%s需在 0 到 %d 美元/百万 token 之间", label, MaxUSDPerMTokens)
	}
	return nil
}

// Store persists public-model prices. The MySQL implementation backs the
// gateway; returned rows always carry timestamps.
type Store interface {
	List(ctx context.Context) ([]Price, error)
	// ByModel returns the price for a public model or ErrNotFound.
	ByModel(ctx context.Context, publicModel string) (Price, error)
	// Upsert creates or replaces the row for Price.PublicModel.
	Upsert(ctx context.Context, p Price) (Price, error)
	// Delete removes the row or reports ErrNotFound.
	Delete(ctx context.Context, publicModel string) error
}
