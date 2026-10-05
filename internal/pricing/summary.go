package pricing

// PriceSummary 是价格行的目录面投影(38 号票):人类人民币单位,供画布侧
// 目录端点(/image-models、/video-models、/prompt-models)随名单附带给前端
// 做模型价格展示与发送前预估。它不带任何校验力——计费永远读价格行本身。
type PriceSummary struct {
	Unit string `json:"unit"`

	// token 轨字段。
	Ratio                  float64            `json:"ratio,omitempty"`                  // 倍率(×1.0)
	InputCNYPerMTokens     float64            `json:"input_cny_per_mtokens,omitempty"`  // 元/百万输入 token
	OutputCNYPerMTokens    float64            `json:"output_cny_per_mtokens,omitempty"` // 元/百万输出 token
	SizeTokensPerSecond    map[string]float64 `json:"size_tokens_per_second,omitempty"` // 视频轨预扣估算表
	DefaultTokensPerSecond float64            `json:"default_tokens_per_second,omitempty"`

	// call/second 轨字段。
	CNYPerCall  float64            `json:"cny_per_call,omitempty"` // 元/张 或 元/秒
	SizeFactors map[string]float64 `json:"size_factors,omitempty"` // 尺寸/分辨率 → 系数(×1.0)
}

// Summary renders the price row as a catalog-facing summary in human CNY
// units, using the admin-edge conversions.
func (p Price) Summary() PriceSummary {
	s := PriceSummary{Unit: string(p.Unit)}
	if p.Token != nil {
		s.Ratio = MicrosToRatio(p.Token.RatioMicros)
		s.InputCNYPerMTokens = MicrosToCNYPerMTokens(p.Token.InputMicrosPerMTokens)
		s.OutputCNYPerMTokens = MicrosToCNYPerMTokens(p.Token.OutputMicrosPerMTokens)
		if len(p.Token.SizeTokensPerSecond) > 0 {
			s.SizeTokensPerSecond = p.Token.SizeTokensPerSecond
		}
		s.DefaultTokensPerSecond = p.Token.DefaultTokensPerSecond
	}
	if p.Call != nil {
		s.CNYPerCall = MicrosToCNY(p.Call.CNYPerCallMicros)
		if len(p.Call.SizeFactorMicros) > 0 {
			factors := make(map[string]float64, len(p.Call.SizeFactorMicros))
			for size, f := range p.Call.SizeFactorMicros {
				factors[size] = MicrosToFactor(f)
			}
			s.SizeFactors = factors
		}
	}
	return s
}
