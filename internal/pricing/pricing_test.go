package pricing

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestTokenPriceChargeMicros(t *testing.T) {
	// in $1.25/M, out $10/M, ratio ×1: (1000×1.25 + 2000×10)/1e6 USD.
	base := TokenPrice{
		InputMicrosPerMTokens:  1_250_000,
		OutputMicrosPerMTokens: 10_000_000,
		RatioMicros:            1_000_000,
	}
	if got := base.ChargeMicros(1000, 2000); got != 21_250 {
		t.Errorf("ChargeMicros(1000,2000) = %d, want 21250 micros ($0.02125)", got)
	}

	// 不足 1 微美元向上取整:任何正成本至少记 1 micro,账面永不低估。
	tiny := TokenPrice{InputMicrosPerMTokens: 1, OutputMicrosPerMTokens: 1, RatioMicros: 1_000_000}
	if got := tiny.ChargeMicros(1, 0); got != 1 {
		t.Errorf("ChargeMicros ceil = %d, want 1", got)
	}

	// 倍率生效:×1.5 → 21250 × 1.5 = 31875。
	x15 := base
	x15.RatioMicros = 1_500_000
	if got := x15.ChargeMicros(1000, 2000); got != 31_875 {
		t.Errorf("ChargeMicros ratio 1.5 = %d, want 31875", got)
	}

	// 免费模型(单价为 0)计 0。
	free := TokenPrice{}
	if got := free.ChargeMicros(1000, 1000); got != 0 {
		t.Errorf("ChargeMicros free = %d, want 0", got)
	}
	if got := base.ChargeMicros(0, 0); got != 0 {
		t.Errorf("ChargeMicros zero tokens = %d, want 0", got)
	}

	// 负数防御:上游 usage 异常时按 0 计。
	if got := base.ChargeMicros(-5, -5); got != 0 {
		t.Errorf("ChargeMicros negative = %d, want 0", got)
	}

	// 大数值不溢出:10M tokens × $10000/M × ×1000 仍给出 int64 内的正确值。
	huge := TokenPrice{
		InputMicrosPerMTokens:  10_000 * 1_000_000,
		OutputMicrosPerMTokens: 10_000 * 1_000_000,
		RatioMicros:            1000 * 1_000_000,
	}
	got := huge.ChargeMicros(10_000_000, 0)
	// (1e7 × 1e10 × 1e9) / 1e12 = 1e14 micros。
	if got != 100_000_000_000_000 {
		t.Errorf("ChargeMicros huge = %d, want 1e14", got)
	}

	// 溢出钳制:结果超出 int64 时取 MaxInt64 而不是回绕。
	if got := huge.ChargeMicros(math.MaxInt64/1_000_000, 0); got != math.MaxInt64 {
		t.Errorf("ChargeMicros overflow = %d, want MaxInt64 clamp", got)
	}
}

func TestPriceNormalize(t *testing.T) {
	valid := Price{
		PublicModel: "  deepseek-chat ",
		Unit:        UnitToken,
		Token:       &TokenPrice{InputMicrosPerMTokens: 1, OutputMicrosPerMTokens: 2, RatioMicros: 1_000_000},
	}
	got, err := valid.Normalize()
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if got.PublicModel != "deepseek-chat" {
		t.Errorf("model = %q, want trimmed", got.PublicModel)
	}

	for _, tc := range []struct {
		name  string
		price Price
	}{
		{"empty model", Price{Unit: UnitToken, Token: &TokenPrice{}}},
		{"missing token payload", Price{PublicModel: "m", Unit: UnitToken}},
		{"negative input", Price{PublicModel: "m", Unit: UnitToken, Token: &TokenPrice{InputMicrosPerMTokens: -1}}},
		{"negative ratio", Price{PublicModel: "m", Unit: UnitToken, Token: &TokenPrice{RatioMicros: -1}}},
		{"ratio over 1000", Price{PublicModel: "m", Unit: UnitToken, Token: &TokenPrice{RatioMicros: 1001 * 1_000_000}}},
		{"call track not open", Price{PublicModel: "m", Unit: UnitCall}},
		{"unknown unit", Price{PublicModel: "m", Unit: "bogus"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.price.Normalize(); err == nil {
				t.Fatalf("Normalize = nil error, want rejection")
			}
		})
	}
}

func TestPriceSnapshotJSON(t *testing.T) {
	p := Price{
		PublicModel: "deepseek-chat",
		Unit:        UnitToken,
		Token:       &TokenPrice{InputMicrosPerMTokens: 440_000, OutputMicrosPerMTokens: 1_320_000, RatioMicros: 1_200_000},
	}
	raw, err := p.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("snapshot not JSON: %v", err)
	}
	if got["unit"] != "token" {
		t.Errorf("snapshot unit = %v, want token", got["unit"])
	}
	token, ok := got["token"].(map[string]any)
	if !ok || token["ratio_micros"] != float64(1_200_000) {
		t.Errorf("snapshot token payload = %v, want ratio_micros 1200000", got["token"])
	}

	call := p
	call.Unit = UnitCall
	if _, err := call.Snapshot(); err == nil {
		t.Errorf("Snapshot call track = nil error, want rejection")
	}
}

func TestPriceModelNameBound(t *testing.T) {
	long := Price{
		PublicModel: strings.Repeat("模", ModelNameRunes+1),
		Unit:        UnitToken,
		Token:       &TokenPrice{},
	}
	if _, err := long.Normalize(); err == nil {
		t.Errorf("over-long model name accepted")
	}
}
