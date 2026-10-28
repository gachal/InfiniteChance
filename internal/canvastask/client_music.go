// 配乐闭环的网关 client 面(60 号票):两端的请求/响应形状以
// internal/relay/music.go 为权威,错位只会在 worker 轮询时才暴露,这里
// 逐字段对齐。调用姿势与视频同款:Bearer 服务级 key + 审计头;网关应答
// 的拒绝一律 *voice.UpstreamError(状态码与摘要原样保留),传输失败是
// 普通 error。无取消面 —— worker 的超时收尾靠网关侧计费时限兜底。
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

// MusicRequest is one 配乐/歌曲 submit:Prompt 是风格描述(≤2000 字,网关与
// MPS 的契约上限),Model 是公开模型名(调度换道由网关裁决,不钉渠道);
// Instrumental/Lyrics 是 62 号票的歌曲事实 —— Instrumental 为 nil 时网关按
// 缺省 true(纯音乐,60 号票行为不变),false 即人声歌曲;Lyrics 非空才带
// (网关互斥校验:instrumental=true 携带歌词 400)。
type MusicRequest struct {
	Model        string
	Prompt       string
	Source       string
	Lyrics       string
	Instrumental *bool
}

// MusicSubmitResult carries the gateway's task handle (mt_…): the worker
// persists it on the task row and polls the gateway with it.
type MusicSubmitResult struct {
	TaskID string
}

// MusicPoll is one gateway task poll. Status is the gateway's external
// five-state machine (queued/running/succeeded/failed/canceled); AudioURL
// and Error carry the terminal facts — a succeeded poll without a URL is a
// protocol violation the worker reports as a failure.
type MusicPoll struct {
	Status   string
	AudioURL string
	Error    string
}

// MusicModelCatalog is the /v1/audio/music-models 目录:music 能力渠道 ∩
// 按次配价的配乐模型,价格随行走 PriceSummary(38 号票),canvas 的
// /music-models 原样透传。
type MusicModelCatalog struct {
	Models []string
	Prices map[string]pricing.PriceSummary
}

// SubmitMusic calls POST /v1/audio/music and returns the task handle. A
// gateway rejection (OpenAI error object) is an error carrying the reason
// for the task row — the gateway refunds its own pre-deduction on any
// rejected submit, so nothing is owed on this path.
func (c *Client) SubmitMusic(ctx context.Context, req MusicRequest) (MusicSubmitResult, error) {
	payload, err := json.Marshal(struct {
		Model        string `json:"model"`
		Prompt       string `json:"prompt"`
		Lyrics       string `json:"lyrics,omitempty"`
		Instrumental *bool  `json:"instrumental,omitempty"`
	}{Model: req.Model, Prompt: req.Prompt, Lyrics: req.Lyrics, Instrumental: req.Instrumental})
	if err != nil {
		return MusicSubmitResult{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.BaseURL+"/v1/audio/music", bytes.NewReader(payload))
	if err != nil {
		return MusicSubmitResult{}, err
	}
	c.setHeaders(httpReq, req.Source)

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return MusicSubmitResult{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return MusicSubmitResult{}, fmt.Errorf("read gateway response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return MusicSubmitResult{}, fmt.Errorf("gateway %d: %s", resp.StatusCode, errorSummary(raw))
	}
	var parsed struct {
		ID     string `json:"id"`
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return MusicSubmitResult{}, fmt.Errorf("gateway response not JSON: %w", err)
	}
	id := strings.TrimSpace(parsed.ID)
	if id == "" {
		id = strings.TrimSpace(parsed.TaskID)
	}
	if id == "" {
		return MusicSubmitResult{}, fmt.Errorf("gateway accepted the submit but returned no task id")
	}
	return MusicSubmitResult{TaskID: id}, nil
}

// PollMusic calls GET /v1/audio/music/tasks/{id}. Transient gateway
// failures are errors — the worker keeps polling; only the gateway's own
// status words advance the task.
func (c *Client) PollMusic(ctx context.Context, taskID string) (MusicPoll, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.BaseURL+"/v1/audio/music/tasks/"+taskID, nil)
	if err != nil {
		return MusicPoll{}, err
	}
	c.setHeaders(httpReq, "")

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return MusicPoll{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return MusicPoll{}, fmt.Errorf("read gateway response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return MusicPoll{}, fmt.Errorf("gateway %d: %s", resp.StatusCode, errorSummary(raw))
	}
	var parsed struct {
		Status string `json:"status"`
		Audio  *struct {
			URL string `json:"url"`
		} `json:"audio"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return MusicPoll{}, fmt.Errorf("gateway response not JSON: %w", err)
	}
	status := strings.TrimSpace(parsed.Status)
	if status == "" {
		return MusicPoll{}, fmt.Errorf("gateway task body carries no status")
	}
	poll := MusicPoll{Status: status}
	if parsed.Audio != nil {
		poll.AudioURL = strings.TrimSpace(parsed.Audio.URL)
	}
	if parsed.Error != nil {
		poll.Error = strings.TrimSpace(parsed.Error.Message)
	}
	return poll, nil
}

// ListMusicModels calls GET /v1/audio/music-models — the 按次配乐模型目录
// (music 能力渠道 ∩ call 配价,带价)。
func (c *Client) ListMusicModels(ctx context.Context) (MusicModelCatalog, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.BaseURL+"/v1/audio/music-models", nil)
	if err != nil {
		return MusicModelCatalog{}, err
	}
	c.setHeaders(httpReq, "")

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return MusicModelCatalog{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return MusicModelCatalog{}, fmt.Errorf("read gateway response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return MusicModelCatalog{}, upstreamRejection(resp.StatusCode, raw)
	}
	var parsed struct {
		Models []string                        `json:"models"`
		Prices map[string]pricing.PriceSummary `json:"prices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return MusicModelCatalog{}, fmt.Errorf("gateway response not JSON: %w", err)
	}
	if parsed.Models == nil {
		parsed.Models = []string{}
	}
	if parsed.Prices == nil {
		parsed.Prices = map[string]pricing.PriceSummary{}
	}
	return MusicModelCatalog{Models: parsed.Models, Prices: parsed.Prices}, nil
}
