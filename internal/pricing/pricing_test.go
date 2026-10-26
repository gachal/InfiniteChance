package pricing

import (
	"encoding/json"
	"fmt"
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
		CNYPerCallMicros: 40_000,
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
	tiny := CallPrice{CNYPerCallMicros: 1, SizeFactorMicros: map[string]int64{"s": 1_500_000}}
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
	huge := CallPrice{CNYPerCallMicros: 1000 * 1_000_000, SizeFactorMicros: map[string]int64{"s": 1000 * 1_000_000}}
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
			CNYPerCallMicros: 40_000,
			SizeFactorMicros: map[string]int64{" 1024x1024 ": 1_000_000, "1792x1024": 2_000_000},
		},
	}
	got, err := valid.Normalize()
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if got.PublicModel != "dall-e-3" || got.Call.CNYPerCallMicros != 40_000 {
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
		{"negative price", Price{PublicModel: "m", Unit: UnitCall, Call: &CallPrice{CNYPerCallMicros: -1}}},
		{"price over $1000", Price{PublicModel: "m", Unit: UnitCall, Call: &CallPrice{CNYPerCallMicros: 1001 * 1_000_000}}},
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
		Call:        &CallPrice{CNYPerCallMicros: 40_000, SizeFactorMicros: map[string]int64{"1792x1024": 2_000_000}},
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
	if !ok || call["cny_per_call_micros"] != float64(40_000) {
		t.Errorf("snapshot call payload = %v, want cny_per_call_micros 40000", got["call"])
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

// TestEstimateVideoTokens pins the token track's submit-time guess (25 号票):
// the size's configured per-second rate wins, an absent or unconfigured size
// falls to the default scalar, and a price with no table at all (every chat
// token row) estimates 0. Rounding is up — 预扣不低估.
func TestEstimateVideoTokens(t *testing.T) {
	p := TokenPrice{
		SizeTokensPerSecond:    map[string]float64{"720p": 24_000, "1080p": 53_665.5},
		DefaultTokensPerSecond: 12_000,
	}
	for _, tc := range []struct {
		size    string
		seconds int64
		want    int64
	}{
		{"720p", 5, 120_000},
		{"1080p", 5, 268_328}, // 53665.5 × 5 = 268327.5 → ceil
		{"480p", 5, 60_000},   // 档位未配 → default 12000
		{"", 10, 120_000},     // size 未传 → default
		{"720p", 0, 0},
		{"720p", -3, 0},
	} {
		if got := p.EstimateVideoTokens(tc.size, tc.seconds); got != tc.want {
			t.Errorf("EstimateVideoTokens(%q, %d) = %d, want %d", tc.size, tc.seconds, got, tc.want)
		}
	}
	if got := (TokenPrice{}).EstimateVideoTokens("720p", 5); got != 0 {
		t.Errorf("empty table estimates %d, want 0", got)
	}
	if got := (TokenPrice{DefaultTokensPerSecond: 99.9}).EstimateVideoTokens("720p", 3); got != 300 {
		t.Errorf("fractional default estimates %d, want 300 (ceil)", got)
	}
}

// TestTokenPriceNormalizeVideoTable pins the conversion table's validation
// (25 号票): entry count, blank/oversized tier names, out-of-bound rates,
// and the default scalar's bound. Zero rates fold away — an empty table
// never reaches the store.
func TestTokenPriceNormalizeVideoTable(t *testing.T) {
	base := func(table map[string]float64) Price {
		return Price{PublicModel: "seedance", Unit: UnitToken, Token: &TokenPrice{
			InputMicrosPerMTokens: 0, OutputMicrosPerMTokens: 2_000_000, RatioMicros: 1_000_000,
			SizeTokensPerSecond: table,
		}}
	}
	if _, err := base(map[string]float64{"720p": 24_000}).Normalize(); err != nil {
		t.Fatalf("valid table rejected: %v", err)
	}
	oversized := map[string]float64{}
	for i := range 101 {
		oversized[fmt.Sprintf("tier%d", i)] = 100
	}
	blank := base(map[string]float64{"  ": 100})
	badTier := base(map[string]float64{strings.Repeat("t", 65): 100})
	negative := base(map[string]float64{"720p": -1})
	huge := base(map[string]float64{"720p": MaxTokensPerSecond + 0.5})
	for _, tc := range []struct {
		name  string
		price Price
	}{
		{"too many entries", Price{PublicModel: "m", Unit: UnitToken, Token: &TokenPrice{SizeTokensPerSecond: oversized}}},
		{"blank tier name", blank},
		{"tier name too long", badTier},
		{"negative rate", negative},
		{"rate over shield", huge},
		{"default over shield", Price{PublicModel: "m", Unit: UnitToken, Token: &TokenPrice{DefaultTokensPerSecond: MaxTokensPerSecond + 1}}},
		{"negative default", Price{PublicModel: "m", Unit: UnitToken, Token: &TokenPrice{DefaultTokensPerSecond: -1}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.price.Normalize(); err == nil {
				t.Fatal("Normalize = nil error, want rejection")
			}
		})
	}

	// 零折算与未配同义:折叠掉,空表不落库。
	folded, err := base(map[string]float64{"720p": 24_000, "480p": 0}).Normalize()
	if err != nil {
		t.Fatalf("fold: %v", err)
	}
	if _, kept := folded.Token.SizeTokensPerSecond["480p"]; kept {
		t.Errorf("zero rate kept: %v", folded.Token.SizeTokensPerSecond)
	}
	if len(folded.Token.SizeTokensPerSecond) != 1 {
		t.Errorf("folded table = %v, want only 720p", folded.Token.SizeTokensPerSecond)
	}
}

// TestCharacterPriceChargeMicros pins the character track's arithmetic
// (56 号票): ⌈chars × 单价 / 10000⌉,向上取整不低估,big.Int 防溢出。
// 1.4 元/万字符(qwen-audio-3.1-tts-plus 档)是活价。
func TestCharacterPriceChargeMicros(t *testing.T) {
	plus := CharacterPrice{CNYPer10KCharsMicros: 1_400_000} // 1.4 元/万字符
	// 10000 字符 × 1.4 元 = 1_400_000 micros,整除不动。
	if got := plus.ChargeMicros(10_000); got != 1_400_000 {
		t.Errorf("ChargeMicros(10000) = %d, want 1400000", got)
	}
	// 不足一万字符按比例且向上取整:6000 × 1.4 元/10000 = 0.84 元 = 840000。
	if got := plus.ChargeMicros(6_000); got != 840_000 {
		t.Errorf("ChargeMicros(6000) = %d, want 840000", got)
	}
	// 不能整除的余数向上取整:1 字符 × 1.4 元 / 10000 = 140 micros 整;
	// 3 字符 = 420;1 微不下的尾数必须进位 —— 单价 1 micro 时
	// 9999 × 1 / 10000 = 0.9999 → ⌈⌉ = 1。
	if got := plus.ChargeMicros(3); got != 420 {
		t.Errorf("ChargeMicros(3) = %d, want 420", got)
	}
	tiny := CharacterPrice{CNYPer10KCharsMicros: 1}
	if got := tiny.ChargeMicros(9_999); got != 1 {
		t.Errorf("ChargeMicros ceil = %d, want 1 (尾数进位不低估)", got)
	}
	// 免费模型与防御:0 单价计 0,chars<1 计 0。
	if got := (CharacterPrice{}).ChargeMicros(50_000); got != 0 {
		t.Errorf("free model = %d, want 0", got)
	}
	if got := plus.ChargeMicros(0); got != 0 {
		t.Errorf("zero chars = %d, want 0", got)
	}
	if got := plus.ChargeMicros(-7); got != 0 {
		t.Errorf("negative chars = %d, want 0", got)
	}
	// 大数值不溢出:1000 元/万字符 × 1e13 字符,分子 1e22 超出 int64,
	// 商 1e18 仍在范围内。
	huge := CharacterPrice{CNYPer10KCharsMicros: 1000 * 1_000_000}
	if got := huge.ChargeMicros(10_000_000_000_000); got != 100_000_000_000_000_000_0 {
		t.Errorf("ChargeMicros huge = %d, want 1e18", got)
	}
	if got := huge.ChargeMicros(math.MaxInt64); got != math.MaxInt64 {
		t.Errorf("ChargeMicros overflow = %d, want MaxInt64 clamp", got)
	}
}

// TestCharacterPriceNormalize pins the character track's Normalize branch:
// 恰好一个 Character 载荷,单价护盾 [0, MaxCNYPer10KChars] 元。
func TestCharacterPriceNormalize(t *testing.T) {
	valid := Price{PublicModel: " qwen-audio-3.1-tts-plus ", Unit: UnitCharacter,
		Character: &CharacterPrice{CNYPer10KCharsMicros: 1_400_000}}
	got, err := valid.Normalize()
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if got.PublicModel != "qwen-audio-3.1-tts-plus" || got.Character.CNYPer10KCharsMicros != 1_400_000 {
		t.Fatalf("normalized = %+v, want trimmed model and intact price", got)
	}

	for _, tc := range []struct {
		name  string
		price Price
	}{
		{"missing character payload", Price{PublicModel: "m", Unit: UnitCharacter}},
		{"token payload on character track", Price{PublicModel: "m", Unit: UnitCharacter, Character: &CharacterPrice{}, Token: &TokenPrice{}}},
		{"call payload on character track", Price{PublicModel: "m", Unit: UnitCharacter, Character: &CharacterPrice{}, Call: &CallPrice{}}},
		{"negative price", Price{PublicModel: "m", Unit: UnitCharacter, Character: &CharacterPrice{CNYPer10KCharsMicros: -1}}},
		{"price over shield", Price{PublicModel: "m", Unit: UnitCharacter, Character: &CharacterPrice{CNYPer10KCharsMicros: (MaxCNYPer10KChars + 1) * 1_000_000}}},
		{"token track with character payload", Price{PublicModel: "m", Unit: UnitToken, Token: &TokenPrice{}, Character: &CharacterPrice{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.price.Normalize(); err == nil {
				t.Fatalf("Normalize = nil error, want rejection")
			}
		})
	}
}

// TestCharacterSnapshots pins both snapshot shapes: Snapshot() 带 character
// 载荷,CharacterSnapshot() 额外记请求事实(chars)供审计重算;字符轨
// 价格取按次快照或按次价格取字符快照都是编程错误。
func TestCharacterSnapshots(t *testing.T) {
	p := Price{PublicModel: "qwen-audio-3.1-tts-plus", Unit: UnitCharacter,
		Character: &CharacterPrice{CNYPer10KCharsMicros: 1_400_000}}
	raw, err := p.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("snapshot not JSON: %v", err)
	}
	if got["unit"] != "character" {
		t.Errorf("snapshot unit = %v, want character", got["unit"])
	}
	ch, ok := got["character"].(map[string]any)
	if !ok || ch["cny_per_10k_chars_micros"] != float64(1_400_000) {
		t.Errorf("snapshot character payload = %v, want cny_per_10k_chars_micros 1400000", got["character"])
	}

	craw, err := p.CharacterSnapshot(1234)
	if err != nil {
		t.Fatalf("CharacterSnapshot: %v", err)
	}
	var cgot struct {
		Unit      string `json:"unit"`
		Character *struct {
			CNYPer10KCharsMicros int64 `json:"cny_per_10k_chars_micros"`
		} `json:"character"`
		Request struct {
			Chars int64 `json:"chars"`
		} `json:"request"`
	}
	if err := json.Unmarshal(craw, &cgot); err != nil {
		t.Fatalf("character snapshot not JSON: %v (%s)", err, craw)
	}
	if cgot.Unit != "character" || cgot.Request.Chars != 1234 || cgot.Character == nil ||
		cgot.Character.CNYPer10KCharsMicros != 1_400_000 {
		t.Errorf("character snapshot = %s, want character track with request {chars:1234}", craw)
	}

	tp := Price{PublicModel: "m", Unit: UnitToken, Token: &TokenPrice{}}
	if _, err := tp.CharacterSnapshot(10); err == nil {
		t.Errorf("CharacterSnapshot of token price = nil error, want failure")
	}
	if _, err := p.CallSnapshot("1024x1024", 1); err == nil {
		t.Errorf("CallSnapshot of character price = nil error, want failure")
	}
}
