// Package pricing implements the gateway's dual-track model pricing: chat
// models bill by token with an upstream-cost multiplier (ratio track), image
// models bill per generated item in USD with a size coefficient (call track,
// ticket 07). All amounts are integer micro-USD like the quota ledger, so
// billing arithmetic never touches floats; the admin API converts at the edge.
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

	"github.com/gachal/InfiniteChance/internal/apikey"
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
	// MaxUSDPerCall bounds a per-item (per image) price at $1,000.
	MaxUSDPerCall = 1_000
	// MaxFactor bounds a size coefficient at ×1000.
	MaxFactor = 1_000
	// MaxSizeRunes bounds a size key (e.g. "1024x1024").
	MaxSizeRunes = 64
	// MaxCallItems bounds how many items one request may bill for — the
	// relay enforces it on the client's n, here it shields the arithmetic.
	MaxCallItems = 100
)

// ChargeMicros' division denominators: prices are per million tokens and
// ratios/factors are in micro-units, so (tokens × micros-per-M × ratio-micros)
// divides by 1e6 × 1e6 to land on micro-USD; per-item prices multiply by
// factor-micros and divide by 1e6.
const (
	mtokensDenom = 1_000_000
	ratioDenom   = 1_000_000
	factorDenom  = 1_000_000
)

// TokenPrice is the token-track price of one public model. Costs are the
// upstream's micro-USD per million tokens; the key is charged
// upstream cost × ratio (RatioMicros = 1e6 means ×1.0, at cost).
type TokenPrice struct {
	InputMicrosPerMTokens  int64 `json:"input_micros_per_mtokens"`
	OutputMicrosPerMTokens int64 `json:"output_micros_per_mtokens"`
	RatioMicros            int64 `json:"ratio_micros"`
}

// CallPrice is the call-track price of one public model: a USD unit price
// per generated item (一张图一次), multiplied by a per-size coefficient.
// Sizes without a configured entry bill at exactly ×1.0, so a coefficient
// table only needs the non-default sizes.
type CallPrice struct {
	USDPerCallMicros int64            `json:"usd_per_call_micros"`
	SizeFactorMicros map[string]int64 `json:"size_factor_micros,omitempty"` // 尺寸 → 系数(1e6 = ×1.0)
}

// FactorMicros returns the size's coefficient in micro-units (1e6 = ×1.0),
// defaulting to exactly ×1.0 for unconfigured sizes.
func (c CallPrice) FactorMicros(size string) int64 {
	if f, ok := c.SizeFactorMicros[size]; ok && f >= 0 {
		return f
	}
	return factorDenom
}

// ChargeMicros computes what n items of this size cost the key: per item
// ⌈unit price × size factor⌉ rounded up to whole micros, times n, so billed
// amounts never drift below true cost. n below 1 (or a free model) yields 0.
// The multiply runs in big.Int — unit price × factor × n can overflow int64
// at legitimate upper bounds.
func (c CallPrice) ChargeMicros(size string, n int64) int64 {
	if n < 1 {
		return 0
	}
	perItem := new(big.Int).Mul(big.NewInt(c.USDPerCallMicros), big.NewInt(c.FactorMicros(size)))
	q, rem := new(big.Int).QuoRem(perItem, big.NewInt(factorDenom), new(big.Int))
	if rem.Sign() > 0 {
		q.Add(q, big.NewInt(1))
	}
	q.Mul(q, big.NewInt(n))
	if !q.IsInt64() {
		return math.MaxInt64
	}
	return q.Int64()
}

// Price is one public model's price row: the track discriminator plus
// exactly one track payload.
type Price struct {
	PublicModel string
	Unit        Unit
	Token       *TokenPrice // Unit == UnitToken 时非 nil
	Call        *CallPrice  // Unit == UnitCall 时非 nil
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
// price row changes later. The call track should prefer CallSnapshot, which
// also records the request facts the charge derives from.
func (p Price) Snapshot() (json.RawMessage, error) {
	s := struct {
		Unit  Unit        `json:"unit"`
		Token *TokenPrice `json:"token,omitempty"`
		Call  *CallPrice  `json:"call,omitempty"`
	}{Unit: p.Unit}
	switch p.Unit {
	case UnitToken:
		s.Token = p.Token
	case UnitCall:
		s.Call = p.Call
	}
	return json.Marshal(s)
}

// CallSnapshot renders the call-track price plus the request facts the
// charge derives from — the requested size and item count n. Unlike the
// token track (usage comes from the vendor's report), a call-track charge
// cannot be recomputed from the row alone without them.
func (p Price) CallSnapshot(size string, n int64) (json.RawMessage, error) {
	if p.Unit != UnitCall {
		return nil, fmt.Errorf("pricing: call snapshot of a %s-track price", p.Unit)
	}
	s := struct {
		Unit    Unit       `json:"unit"`
		Call    *CallPrice `json:"call,omitempty"`
		Request struct {
			Size string `json:"size"`
			N    int64  `json:"n"`
		} `json:"request"`
	}{Unit: UnitCall, Call: p.Call}
	s.Request.Size = size
	s.Request.N = n
	return json.Marshal(s)
}

// Normalize validates and canonicalizes a Price. It enforces the dual-track
// invariant: the unit decides the payload, exactly one of the two may be
// present.
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
		if p.Call != nil {
			return p, fmt.Errorf("token 计价不能带按次价格")
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
		if p.Call == nil {
			return p, fmt.Errorf("按次计价需要 USD 单价")
		}
		if p.Token != nil {
			return p, fmt.Errorf("按次计价不能带 token 价格")
		}
		c := *p.Call
		if c.USDPerCallMicros < 0 || c.USDPerCallMicros > MaxUSDPerCall*apikey.MicrosPerUSD {
			return p, fmt.Errorf("按次单价需在 0 到 %d 美元/张之间", MaxUSDPerCall)
		}
		if len(c.SizeFactorMicros) > maxFactorEntries {
			return p, fmt.Errorf("尺寸系数最多 %d 条", maxFactorEntries)
		}
		factors := make(map[string]int64, len(c.SizeFactorMicros))
		for size, f := range c.SizeFactorMicros {
			size = strings.TrimSpace(size)
			if size == "" {
				return p, fmt.Errorf("尺寸系数的尺寸名不能为空")
			}
			if utf8.RuneCountInString(size) > MaxSizeRunes {
				return p, fmt.Errorf("尺寸名最多 %d 个字符", MaxSizeRunes)
			}
			if f < 0 || f > MaxFactor*factorDenom {
				return p, fmt.Errorf("尺寸系数需在 0 到 %g 之间", float64(MaxFactor))
			}
			factors[size] = f
		}
		c.SizeFactorMicros = factors
		p.Call = &c
	default:
		return p, fmt.Errorf("未知计价单位:%s", p.Unit)
	}
	return p, nil
}

// maxFactorEntries bounds the size-coefficient table like the channel
// package bounds model maps.
const maxFactorEntries = 100

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
