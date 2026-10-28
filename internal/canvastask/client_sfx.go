// 音效生成的网关 client 面(61 号票):请求/响应形状以 internal/relay/sfx.go
// 为权威(同步单面 POST /v1/audio/sfx,响应 {audio:{url}, usage:{seconds}}),
// 错位只会在 worker 运行时才暴露,这里逐字段对齐。调用姿势与音乐同款:
// Bearer 服务级 key + 审计头;网关应答的拒绝一律 *voice.UpstreamError
// (状态码与摘要原样保留),传输失败是普通 error。
package canvastask

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gachal/InfiniteChance/internal/pricing"
)

// SfxRequest is one 音效 call:Text 是音效描述(≤2000 字,网关与 canvas
// 双侧同源上限),Model 是公开模型名(调度换道由网关裁决,不钉渠道),
// Seconds 是期望时长(1–30,网关统一域)。
type SfxRequest struct {
	Model   string
	Text    string
	Seconds int64
	Source  string
}

// SfxResult carries the artifact's temporary address (VOD 临时 URL,7 天有效,
// 转存由 worker 负责)与网关回显的计费秒数(对账口径,存日志不作准)。
type SfxResult struct {
	AudioURL string
	Seconds  int64
}

// SfxModelCatalog is the /v1/audio/sfx-models 目录:sfx 能力渠道 ∩ 按秒配价
// 的音效模型,价格随行走 PriceSummary(38 号票;second 轨的 cny_per_call
// 字段语义 = 元/秒),canvas 的 /sfx-models 原样透传。
type SfxModelCatalog struct {
	Models []string
	Prices map[string]pricing.PriceSummary
}

// CreateSfx calls POST /v1/audio/sfx (synchronous — the gateway waits the
// vendor task out inside its own budget, second 轨预扣即实结或失败退款) and
// answers the artifact's temporary URL plus the echoed billed seconds. A
// gateway rejection (OpenAI error object) is *voice.UpstreamError.
func (c *Client) CreateSfx(ctx context.Context, req SfxRequest) (SfxResult, error) {
	body := struct {
		Model   string `json:"model"`
		Text    string `json:"text"`
		Seconds int64  `json:"seconds"`
	}{Model: req.Model, Text: req.Text, Seconds: req.Seconds}
	payload, err := json.Marshal(body)
	if err != nil {
		return SfxResult{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.BaseURL+"/v1/audio/sfx", bytes.NewReader(payload))
	if err != nil {
		return SfxResult{}, err
	}
	c.setHeaders(httpReq, req.Source)

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return SfxResult{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return SfxResult{}, fmt.Errorf("read gateway response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return SfxResult{}, upstreamRejection(resp.StatusCode, raw)
	}

	var parsed struct {
		Audio *struct {
			URL string `json:"url"`
		} `json:"audio"`
		Usage *struct {
			Seconds int64 `json:"seconds"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return SfxResult{}, fmt.Errorf("gateway response not JSON: %w", err)
	}
	url := ""
	if parsed.Audio != nil {
		url = strings.TrimSpace(parsed.Audio.URL)
	}
	if url == "" {
		return SfxResult{}, fmt.Errorf("gateway delivered no audio url")
	}
	var seconds int64
	if parsed.Usage != nil {
		seconds = parsed.Usage.Seconds
	}
	return SfxResult{AudioURL: url, Seconds: seconds}, nil
}

// ListSfxModels calls GET /v1/audio/sfx-models — the 按秒音效模型目录
// (sfx 能力渠道 ∩ second 配价,带价)。
func (c *Client) ListSfxModels(ctx context.Context) (SfxModelCatalog, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.BaseURL+"/v1/audio/sfx-models", nil)
	if err != nil {
		return SfxModelCatalog{}, err
	}
	c.setHeaders(httpReq, "")

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return SfxModelCatalog{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return SfxModelCatalog{}, fmt.Errorf("read gateway response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return SfxModelCatalog{}, upstreamRejection(resp.StatusCode, raw)
	}

	var parsed struct {
		Models []string                        `json:"models"`
		Prices map[string]pricing.PriceSummary `json:"prices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return SfxModelCatalog{}, fmt.Errorf("gateway response not JSON: %w", err)
	}
	if parsed.Models == nil {
		parsed.Models = []string{}
	}
	if parsed.Prices == nil {
		parsed.Prices = map[string]pricing.PriceSummary{}
	}
	return SfxModelCatalog{Models: parsed.Models, Prices: parsed.Prices}, nil
}
