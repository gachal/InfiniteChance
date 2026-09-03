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

func TestCallPriceChargeMicros(t *testing.T) {
	// $0.04/张,1024x1024 ×1.0、1792x1024 ×2.0(dall-e-3 标准价形状)。
	base := CallPrice{
		USDPerCallMicros: 40_000,
		SizeFactorMicros: map[string]int64{"1024x1024": 1_000_000, "1792x1024": 2_000_000},
	}
	if got := base.ChargeMicros("1024x1024", 1); got != 40_000 {
		t.Errorf("ChargeMicros 1024x1024 = %d, want 40000 micros ($0.04)", got)
	}
	if got := base.ChargeMicros("1792x1024", 1); got != 80_000 {
		t.Errorf("ChargeMicros 1792x1024 = %d, want 80000 (factor ×2)", got)
	}
	// 未配置系数的尺寸按 ×1.0 计:系数表只需列非默认档。
	if got := base.ChargeMicros("512x512", 1); got != 40_000 {
		t.Errorf("ChargeMicros unknown size = %d, want 40000 (default ×1.0)", got)
	}
	// 张数线性:n=3 × 系数2 → 240000。
	if got := base.ChargeMicros("1792x1024", 3); got != 240_000 {
		t.Errorf("ChargeMicros n=3 = %d, want 240000", got)
	}
	// 不足 1 微美元向上取整:单价 1 micro × 系数 1.5 → 每张 ⌈1.5⌉ = 2。
	tiny := CallPrice{USDPerCallMicros: 1, SizeFactorMicros: map[string]int64{"s": 1_500_000}}
	if got := tiny.ChargeMicros("s", 1); got != 2 {
		t.Errorf("ChargeMicros ceil = %d, want 2", got)
	}
	if got := tiny.ChargeMicros("s", 2); got != 4 {
		t.Errorf("ChargeMicros ceil×n = %d, want 4 (per-item ceil, then ×n)", got)
	}
	// 免费模型与张数防御:n<1 计 0,负系数表不劫持默认值。
	if got := base.ChargeMicros("1792x1024", 0); got != 0 {
		t.Errorf("ChargeMicros n=0 = %d, want 0", got)
	}
	if got := base.ChargeMicros("1792x1024", -5); got != 0 {
		t.Errorf("ChargeMicros n<0 = %d, want 0", got)
	}
	free := CallPrice{SizeFactorMicros: map[string]int64{"s": -3}}
	if got := free.FactorMicros("s"); got != 1_000_000 {
		t.Errorf("FactorMicros negative entry = %d, want the ×1.0 default", got)
	}

	// 大数值不溢出:$1000/张 × ×1000 系数 = $1e6/张,100 张 = 1e14 micros。
	huge := CallPrice{USDPerCallMicros: 1000 * 1_000_000, SizeFactorMicros: map[string]int64{"s": 1000 * 1_000_000}}
	if got := huge.ChargeMicros("s", MaxCallItems); got != 100_000_000_000_000 {
		t.Errorf("ChargeMicros huge = %d, want 1e14", got)
	}
	// 溢出钳制:结果超出 int64 时取 MaxInt64 而不是回绕。
	if got := huge.ChargeMicros("s", math.MaxInt64/1_000_000); got != math.MaxInt64 {
		t.Errorf("ChargeMicros overflow = %d, want MaxInt64 clamp", got)
	}
}

func TestCallPriceNormalize(t *testing.T) {
	valid := Price{
		PublicModel: " dall-e-3 ",
		Unit:        UnitCall,
		Call: &CallPrice{
			USDPerCallMicros: 40_000,
			SizeFactorMicros: map[string]int64{" 1024x1024 ": 1_000_000, "1792x1024": 2_000_000},
		},
	}
	got, err := valid.Normalize()
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if got.PublicModel != "dall-e-3" || got.Call.USDPerCallMicros != 40_000 {
		t.Fatalf("normalized = %+v, want trimmed model and intact price", got)
	}
	if got.Call.SizeFactorMicros["1024x1024"] != 1_000_000 {
		t.Errorf("factor keys = %v, want trimmed size key", got.Call.SizeFactorMicros)
	}

	for _, tc := range []struct {
		name  string
		price Price
	}{
		{"missing call payload", Price{PublicModel: "m", Unit: UnitCall}},
		{"token payload on call track", Price{PublicModel: "m", Unit: UnitCall, Call: &CallPrice{}, Token: &TokenPrice{}}},
		{"negative price", Price{PublicModel: "m", Unit: UnitCall, Call: &CallPrice{USDPerCallMicros: -1}}},
		{"price over $1000", Price{PublicModel: "m", Unit: UnitCall, Call: &CallPrice{USDPerCallMicros: 1001 * 1_000_000}}},
		{"negative factor", Price{PublicModel: "m", Unit: UnitCall, Call: &CallPrice{SizeFactorMicros: map[string]int64{"s": -1}}}},
		{"factor over 1000", Price{PublicModel: "m", Unit: UnitCall, Call: &CallPrice{SizeFactorMicros: map[string]int64{"s": 1001 * 1_000_000}}}},
		{"empty size key", Price{PublicModel: "m", Unit: UnitCall, Call: &CallPrice{SizeFactorMicros: map[string]int64{" ": 1_000_000}}}},
		{"token track with call payload", Price{PublicModel: "m", Unit: UnitToken, Token: &TokenPrice{}, Call: &CallPrice{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.price.Normalize(); err == nil {
				t.Fatalf("Normalize = nil error, want rejection")
			}
		})
	}
}

func TestCallSnapshots(t *testing.T) {
	p := Price{
		PublicModel: "dall-e-3",
		Unit:        UnitCall,
		Call:        &CallPrice{USDPerCallMicros: 40_000, SizeFactorMicros: map[string]int64{"1792x1024": 2_000_000}},
	}
	// 基础快照带 call 载荷。
	raw, err := p.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("snapshot not JSON: %v", err)
	}
	if got["unit"] != "call" {
		t.Errorf("snapshot unit = %v, want call", got["unit"])
	}
	call, ok := got["call"].(map[string]any)
	if !ok || call["usd_per_call_micros"] != float64(40_000) {
		t.Errorf("snapshot call payload = %v, want usd_per_call_micros 40000", got["call"])
	}
	// 审计快照额外记下请求事实(size、n):按次扣费没有它们无法重算。
	craw, err := p.CallSnapshot("1792x1024", 2)
	if err != nil {
		t.Fatalf("CallSnapshot: %v", err)
	}
	var cgot struct {
		Unit    string `json:"unit"`
		Request struct {
			Size string `json:"size"`
			N    int64  `json:"n"`
		} `json:"request"`
	}
	if err := json.Unmarshal(craw, &cgot); err != nil {
		t.Fatalf("call snapshot not JSON: %v", err)
	}
	if cgot.Unit != "call" || cgot.Request.Size != "1792x1024" || cgot.Request.N != 2 {
		t.Errorf("call snapshot = %s, want call track with request {1792x1024, 2}", craw)
	}
	// token 轨价格取按次审计快照是编程错误,必须报错。
	tp := Price{PublicModel: "m", Unit: UnitToken, Token: &TokenPrice{}}
	if _, err := tp.CallSnapshot("1024x1024", 1); err == nil {
		t.Errorf("CallSnapshot of token price = nil error, want failure")
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
	// 按次轨的快照形状归 TestCallSnapshots;token 轨快照不带 call 载荷。
	if _, present := got["call"]; present {
		t.Errorf("token snapshot carries a call payload: %v", got["call"])
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
