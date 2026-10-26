// 音色闭环的网关 client 面(57 号票):四端点的请求/响应形状以
// internal/relay/audio.go 为权威(56 号票),错位只会在 worker 轮询与
// 台账删除时才暴露,这里逐字段对齐。调用姿势与图片/视频同款:Bearer
// 服务级 key + X-InfiniteChance-Source 审计头;网关应答的拒绝一律
// *voice.UpstreamError(状态码与摘要原样保留),传输失败是普通 error。
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
	"github.com/gachal/InfiniteChance/internal/voice"
)

// enrollmentModel is the 音色定制 create 的公开调度与计费键(56 号票,
// 上游同名;relay 的 enrollmentModel 常量在此的镜像)。渠道模型映射里
// 必须有它,音色创建才挂得上。
const enrollmentModel = "voice-enrollment"

// VoiceCreateRequest is one 音色创建 submit:target_model 是音色绑定的
// 合成模型,mode 分流 design(clone 的 audio 必须已是厂商可拉取的
// http(s) 地址,提交 handler 就地解引用)。
type VoiceCreateRequest struct {
	TargetModel string
	Mode        string // design | clone
	ChannelID   int64
	VoicePrompt string // design 必填,≤500 字
	Audio       string // clone 必填,http(s) 声音样本地址
	PreviewText string // 15–200 字
	Source      string
}

// VoiceCreateResult carries the submit's facts: the upstream voice handle
// (先落任务行再轮询)与 Base64 试听音频(转存 canvases/ 档素材行)。
type VoiceCreateResult struct {
	VoiceID       string
	Status        string
	PreviewBase64 string
	PreviewFormat string
}

// SpeechRequest is one 台词合成 call;ChannelID 是音色所在渠道(钉单
// 渠道不换道),Voice 是上游 voice_id。
type SpeechRequest struct {
	Model       string
	Voice       string
	Text        string
	Instruction string
	ChannelID   int64
	Source      string
}

// SpeechResult carries the artifact's temporary address (约 24h,转存由
// worker 负责)与网关的服务端计费字符数(对账口径,存日志不作准)。
type SpeechResult struct {
	AudioURL string
	Chars    int64
}

// AudioModelCatalog is the /v1/audio/models 目录:字符轨配价且 audio 能力
// 渠道可服务的合成模型,价格随行走 PriceSummary(38 号票),canvas 的
// /audio-models 原样透传。
type AudioModelCatalog struct {
	Models []string
	Prices map[string]pricing.PriceSummary
}

// CreateVoice calls POST /v1/audio/voices. The gateway pins the channel
// (不换道)and bills one enrollment; a rejection is *voice.UpstreamError.
func (c *Client) CreateVoice(ctx context.Context, req VoiceCreateRequest) (VoiceCreateResult, error) {
	body := struct {
		Model       string `json:"model"`
		TargetModel string `json:"target_model"`
		Mode        string `json:"mode"`
		ChannelID   int64  `json:"channel_id"`
		VoicePrompt string `json:"voice_prompt,omitempty"`
		Audio       string `json:"audio,omitempty"`
		PreviewText string `json:"preview_text"`
	}{
		Model: enrollmentModel, TargetModel: req.TargetModel, Mode: req.Mode,
		ChannelID: req.ChannelID, VoicePrompt: req.VoicePrompt,
		Audio: req.Audio, PreviewText: req.PreviewText,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return VoiceCreateResult{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.BaseURL+"/v1/audio/voices", bytes.NewReader(payload))
	if err != nil {
		return VoiceCreateResult{}, err
	}
	c.setHeaders(httpReq, req.Source)

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return VoiceCreateResult{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxB64Bytes*2))
	if err != nil {
		return VoiceCreateResult{}, fmt.Errorf("read gateway response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return VoiceCreateResult{}, upstreamRejection(resp.StatusCode, raw)
	}

	var parsed struct {
		VoiceID      string `json:"voice_id"`
		Status       string `json:"status"`
		PreviewAudio *struct {
			Data   string `json:"data"`
			Format string `json:"format"`
		} `json:"preview_audio"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return VoiceCreateResult{}, fmt.Errorf("gateway response not JSON: %w", err)
	}
	if strings.TrimSpace(parsed.VoiceID) == "" {
		return VoiceCreateResult{}, fmt.Errorf("gateway accepted the submit but returned no voice_id")
	}
	result := VoiceCreateResult{
		VoiceID: strings.TrimSpace(parsed.VoiceID),
		Status:  strings.TrimSpace(parsed.Status),
	}
	if parsed.PreviewAudio != nil {
		result.PreviewBase64 = strings.TrimSpace(parsed.PreviewAudio.Data)
		result.PreviewFormat = strings.TrimSpace(parsed.PreviewAudio.Format)
	}
	return result, nil
}

// QueryVoice calls POST /v1/audio/voices/{id}/query and answers the
// upstream lifecycle status (DEPLOYING / OK / UNDEPLOYED). The query is a
// free management action on the gateway side; a gateway-answered rejection
// (voice not found / channel gone / circuit open) arrives as
// *voice.UpstreamError so callers can tell permanent from transient.
func (c *Client) QueryVoice(ctx context.Context, channelID int64, voiceID, source string) (string, error) {
	payload, err := json.Marshal(struct {
		ChannelID int64 `json:"channel_id"`
	}{ChannelID: channelID})
	if err != nil {
		return "", err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.BaseURL+"/v1/audio/voices/"+voiceID+"/query", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	c.setHeaders(httpReq, source)

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read gateway response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", upstreamRejection(resp.StatusCode, raw)
	}
	var parsed struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("gateway response not JSON: %w", err)
	}
	status := strings.TrimSpace(parsed.Status)
	if status == "" {
		return "", fmt.Errorf("gateway voice query body carries no status")
	}
	return status, nil
}

// DeleteVoice calls DELETE /v1/audio/voices/{id} (204 on success). The
// voices handler passes gateway-answered rejections through untouched.
func (c *Client) DeleteVoice(ctx context.Context, channelID int64, voiceID, source string) error {
	payload, err := json.Marshal(struct {
		ChannelID int64 `json:"channel_id"`
	}{ChannelID: channelID})
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		c.BaseURL+"/v1/audio/voices/"+voiceID, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	c.setHeaders(httpReq, source)

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read gateway response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return upstreamRejection(resp.StatusCode, raw)
	}
	return nil
}

// SynthesizeSpeech calls POST /v1/audio/speech (synchronous, 字符轨计费
// 在网关侧预扣即实结) and answers the artifact's temporary URL plus the
// server-side billable character count.
func (c *Client) SynthesizeSpeech(ctx context.Context, req SpeechRequest) (SpeechResult, error) {
	body := struct {
		Model       string `json:"model"`
		Voice       string `json:"voice"`
		Text        string `json:"text"`
		Instruction string `json:"instruction,omitempty"`
		ChannelID   int64  `json:"channel_id"`
	}{
		Model: req.Model, Voice: req.Voice, Text: req.Text,
		Instruction: req.Instruction, ChannelID: req.ChannelID,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return SpeechResult{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.BaseURL+"/v1/audio/speech", bytes.NewReader(payload))
	if err != nil {
		return SpeechResult{}, err
	}
	c.setHeaders(httpReq, req.Source)

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return SpeechResult{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxB64Bytes*2))
	if err != nil {
		return SpeechResult{}, fmt.Errorf("read gateway response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return SpeechResult{}, upstreamRejection(resp.StatusCode, raw)
	}

	var parsed struct {
		Audio *struct {
			URL string `json:"url"`
		} `json:"audio"`
		Usage *struct {
			Chars int64 `json:"chars"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return SpeechResult{}, fmt.Errorf("gateway response not JSON: %w", err)
	}
	url := ""
	if parsed.Audio != nil {
		url = strings.TrimSpace(parsed.Audio.URL)
	}
	if url == "" {
		return SpeechResult{}, fmt.Errorf("gateway delivered no audio url")
	}
	var chars int64
	if parsed.Usage != nil {
		chars = parsed.Usage.Chars
	}
	return SpeechResult{AudioURL: url, Chars: chars}, nil
}

// ListAudioModels calls GET /v1/audio/models — the 字符轨合成模型目录
// (audio 能力渠道 ∩ 配价模型,带价)。
func (c *Client) ListAudioModels(ctx context.Context) (AudioModelCatalog, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.BaseURL+"/v1/audio/models", nil)
	if err != nil {
		return AudioModelCatalog{}, err
	}
	c.setHeaders(httpReq, "")

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return AudioModelCatalog{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return AudioModelCatalog{}, fmt.Errorf("read gateway response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return AudioModelCatalog{}, upstreamRejection(resp.StatusCode, raw)
	}

	var parsed struct {
		Models []string                        `json:"models"`
		Prices map[string]pricing.PriceSummary `json:"prices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return AudioModelCatalog{}, fmt.Errorf("gateway response not JSON: %w", err)
	}
	if parsed.Models == nil {
		parsed.Models = []string{}
	}
	if parsed.Prices == nil {
		parsed.Prices = map[string]pricing.PriceSummary{}
	}
	return AudioModelCatalog{Models: parsed.Models, Prices: parsed.Prices}, nil
}

// upstreamRejection wraps one gateway-answered failure body into
// *voice.UpstreamError, keeping the status and the OpenAI error message
// (or a truncated raw body) for pass-through and task-row reasons.
func upstreamRejection(status int, body []byte) error {
	return &voice.UpstreamError{Status: status, Message: errorSummary(body)}
}

// Client 满足音色闭环消费方的两个窄切片:台账删除面(voice.UpstreamRemover)
// 与角色绑定实时校准(character 经 voice.UpstreamQuerier)。wiring 按接口
// 断言装配,签名漂移在这里编译期暴露,不在运行期静默落 nil。
var (
	_ voice.UpstreamRemover = (*Client)(nil)
	_ voice.UpstreamQuerier = (*Client)(nil)
)
