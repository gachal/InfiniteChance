// 音色定制与台词合成的网关面(56 号票):自定义契约,对齐 /v1/videos 先例
// —— 四端点请求体必带 channel_id 钉单渠道,**不换道**(音色绑定创建渠道的
// 工作空间,别家渠道必不可用,换道只会制造难懂错误)。校验链:渠道存在 →
// 启用 → audio 能力 → 模型映射命中,不满足各给明确 OpenAI error code;
// 熔断语义照「调度与熔断」:上游临时失败(传输错/限流/5xx/坏 2xx)记该渠道
// 熔断账并直接失败(预扣原退 + upstream_error 留痕透传),上游 4xx 透传不
// 记熔断账。计费两轨:enrollment 次轨照生图链(预扣 → 实结 → 失败退款),
// speech 字符轨预扣即实结(字符数服务端自算,见 audiochars.go);query/
// delete 是管理动作,不计费不落用量行。
package relay

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/apierr"
	"github.com/gachal/InfiniteChance/internal/apikey"
	"github.com/gachal/InfiniteChance/internal/channel"
	"github.com/gachal/InfiniteChance/internal/pricing"
	"github.com/gachal/InfiniteChance/internal/usage"
)

// enrollmentModel is the public scheduling and billing key of 音色定制
// (上游同名):渠道模型映射与价格行都用这个名字。
const enrollmentModel = "voice-enrollment"

// 上游档位与契约上限:voice_prompt ≤500 字(上游仅中英)、试听文本
// 15–200 字、prefix ≤10 位字母数字(缺省自动生成)、台词 ≤600 字符
// (上游 HTTP 单次上限未公布,取 qwen3-tts 同族保守值)。
const (
	voiceModeDesign = "design"
	voiceModeClone  = "clone"

	maxTargetModelRunes = pricing.ModelNameRunes
	maxVoicePromptRunes = 500
	minPreviewTextRunes = 15
	maxPreviewTextRunes = 200
	maxPrefixRunes      = 10
	maxSpeechTextRunes  = 600

	defaultPrefixRunes = 8
)

// errAudioInlineUnsupported marks the clone sample rejection that carries
// its own error code (audio_inline_unsupported,12 号票契约家族)。
var errAudioInlineUnsupported = errors.New("The 'audio' parameter must be an http(s) URL the vendor can fetch (inline data: URIs are not supported).")

// voiceRequest is the create body's gateway slice.
type voiceRequest struct {
	Model       string `json:"model"` // 恒 voice-enrollment(公开名)
	TargetModel string `json:"target_model"`
	Mode        string `json:"mode"` // design | clone
	ChannelID   int64  `json:"channel_id"`
	VoicePrompt string `json:"voice_prompt"`
	Audio       string `json:"audio"` // 仅 clone:http(s) 声音样本地址
	PreviewText string `json:"preview_text"`
	Prefix      string `json:"prefix"`
}

// speechRequest is the speech body's gateway slice.
type speechRequest struct {
	Model       string `json:"model"`
	Voice       string `json:"voice"` // voice_id 或系统音色名
	Text        string `json:"text"`
	Instruction string `json:"instruction"` // 可选自然语言演绎指令
	ChannelID   int64  `json:"channel_id"`
}

// CreateVoice relays POST /v1/audio/voices: validate → pin the channel →
// price(enrollment 次轨)→ reserve → the single attempt → settle-or-refund.
func (h *Handlers) CreateVoice(c *gin.Context) {
	key, _ := apikey.KeyFrom(c)

	raw, err := c.GetRawData()
	if err != nil {
		refuseBody(c, err)
		return
	}
	var req voiceRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError, "The request body is not a valid JSON object.")
		return
	}
	if req.Model == "" {
		apierr.OpenAI(c, http.StatusBadRequest, CodeMissingModel, TypeInvalidRequestError, "You must provide a 'model' parameter.")
		return
	}
	if err := validateVoiceRequest(&req); err != nil {
		code := CodeInvalidRequest
		if errors.Is(err, errAudioInlineUnsupported) {
			code = CodeAudioInlineUnsupported
		}
		apierr.OpenAI(c, http.StatusBadRequest, code, TypeInvalidRequestError, err.Error())
		return
	}

	ctx := c.Request.Context()
	ch, ok := h.pinnedAudioChannel(c, ctx, req.ChannelID, req.Model)
	if !ok {
		return
	}
	price, err := h.priceFor(ctx, req.Model)
	if errors.Is(err, pricing.ErrNotFound) {
		apierr.OpenAI(c, http.StatusBadRequest, CodeModelNotPriced, TypeInvalidRequestError,
			"The model '"+req.Model+"' has no price configured; ask the administrator to add one.")
		return
	}
	if err != nil {
		h.failInternal(c, err)
		return
	}
	if price.Unit != pricing.UnitCall || price.Call == nil {
		apierr.OpenAI(c, http.StatusBadRequest, CodeModelNotPriced, TypeInvalidRequestError,
			"The model '"+req.Model+"' is not priced for per-call billing.")
		return
	}

	// 一次定制收一次费(size 空串、n=1);审计快照记下请求事实。
	reserved := price.Call.ChargeMicros("", 1)
	snapshot, err := price.CallSnapshot("", 1)
	if err != nil {
		// 轨道守卫已保证单位,这里只可能是编码失败;快照留空要留痕。
		log.Printf("relay: price snapshot for %s: %v", price.PublicModel, err)
		snapshot = nil
	}
	payload, err := voiceUpstreamBody(req, ch.ModelMap[req.Model])
	if err != nil {
		h.failInternal(c, err)
		return
	}
	p := &prepared{
		publicModel: req.Model, raw: payload,
		key: key, price: price, reserved: reserved, snapshot: snapshot,
		source: SourceFrom(c),
	}

	h.audioPinned(c, p, ch,
		func(ctx context.Context, at attempt) (*UpstreamResponse, error) {
			return h.adaptorFor(ch).VoicesCreate(ctx, ch, at.payload)
		},
		func(run *failoverRunner, at attempt, upstream *UpstreamResponse) {
			// 预扣即实结(一次一收,零差额无 settle 流水)。
			run.settle(at, p.reserved, time.Since(p.started).Milliseconds(), nil)
			c.Data(http.StatusOK, "application/json; charset=utf-8", upstream.Body)
		})
}

// QueryVoice relays POST /v1/audio/voices/{id}/query — the lifecycle poll
// (DEPLOYING → OK / UNDEPLOYED). 管理动作:不计费、不落用量行、不动熔断账。
func (h *Handlers) QueryVoice(c *gin.Context) {
	h.audioVoiceAction(c, "query_voice", func(ctx context.Context, ch channel.Channel, upstreamModel, voiceID string) (*UpstreamResponse, error) {
		return h.adaptorFor(ch).VoicesQuery(ctx, ch, upstreamModel, voiceID)
	}, func(body []byte) {
		c.Data(http.StatusOK, "application/json; charset=utf-8", body)
	})
}

// DeleteVoice relays DELETE /v1/audio/voices/{id} — the upstream delete.
// 管理动作,成功即 204。
func (h *Handlers) DeleteVoice(c *gin.Context) {
	h.audioVoiceAction(c, "delete_voice", func(ctx context.Context, ch channel.Channel, upstreamModel, voiceID string) (*UpstreamResponse, error) {
		return h.adaptorFor(ch).VoicesDelete(ctx, ch, upstreamModel, voiceID)
	}, func([]byte) {
		c.Status(http.StatusNoContent)
	})
}

// audioVoiceAction is the shared body of the two voice management actions:
// 钉渠道校验同 create,单次上游调用,失败透传摘要,成功交 deliver。
func (h *Handlers) audioVoiceAction(c *gin.Context, action string, call func(ctx context.Context, ch channel.Channel, upstreamModel, voiceID string) (*UpstreamResponse, error), deliver func(body []byte)) {
	var req struct {
		ChannelID int64 `json:"channel_id"`
	}
	raw, err := c.GetRawData()
	if err != nil {
		refuseBody(c, err)
		return
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &req); err != nil {
			apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError, "The request body is not a valid JSON object.")
			return
		}
	}

	ctx := c.Request.Context()
	voiceID := c.Param("id")
	ch, ok := h.pinnedAudioChannel(c, ctx, req.ChannelID, enrollmentModel)
	if !ok {
		return
	}
	upstream, err := call(ctx, ch, ch.ModelMap[enrollmentModel], voiceID)
	if err != nil {
		apierr.OpenAI(c, http.StatusBadGateway, CodeUpstreamError, CodeUpstreamError,
			"Upstream voice "+action+" failed: (transport error)")
		return
	}
	if !upstream.OK {
		apierr.OpenAI(c, upstream.Status, CodeUpstreamError, CodeUpstreamError,
			"Upstream voice "+action+" failed: "+h.adaptorFor(ch).ErrorSummary(upstream.Body))
		return
	}
	deliver(upstream.Body)
}

// CreateSpeech relays POST /v1/audio/speech: validate → pin the channel →
// price(字符轨)→ 服务端自算字符数预扣(预扣即实结)→ the single attempt
// → settle-or-refund。响应 {audio:{url}, usage:{chars}}(chars 为服务端
// 计费口径,供对账;上游产物是约 24h 临时地址,转存由调用方负责)。
func (h *Handlers) CreateSpeech(c *gin.Context) {
	key, _ := apikey.KeyFrom(c)

	raw, err := c.GetRawData()
	if err != nil {
		refuseBody(c, err)
		return
	}
	var req speechRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError, "The request body is not a valid JSON object.")
		return
	}
	if req.Model == "" {
		apierr.OpenAI(c, http.StatusBadRequest, CodeMissingModel, TypeInvalidRequestError, "You must provide a 'model' parameter.")
		return
	}
	if strings.TrimSpace(req.Voice) == "" {
		apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError, "You must provide a 'voice' parameter.")
		return
	}
	// 台词按原文计费与转发(不 trim):首尾空白也是要念出来的内容。
	if n := utf8.RuneCountInString(req.Text); n < 1 {
		apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError, "You must provide a 'text' parameter.")
		return
	} else if n > maxSpeechTextRunes {
		apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError,
			fmt.Sprintf("The 'text' parameter exceeds the %d-character limit.", maxSpeechTextRunes))
		return
	}

	ctx := c.Request.Context()
	ch, ok := h.pinnedAudioChannel(c, ctx, req.ChannelID, req.Model)
	if !ok {
		return
	}
	price, err := h.priceFor(ctx, req.Model)
	if errors.Is(err, pricing.ErrNotFound) {
		apierr.OpenAI(c, http.StatusBadRequest, CodeModelNotPriced, TypeInvalidRequestError,
			"The model '"+req.Model+"' has no price configured; ask the administrator to add one.")
		return
	}
	if err != nil {
		h.failInternal(c, err)
		return
	}
	if price.Unit != pricing.UnitCharacter || price.Character == nil {
		apierr.OpenAI(c, http.StatusBadRequest, CodeModelNotPriced, TypeInvalidRequestError,
			"The model '"+req.Model+"' is not priced for character billing.")
		return
	}

	// 计费字符数服务端自算(基数 = text + instruction 之和),预扣即实结。
	chars := billableChars(req.Text) + billableChars(req.Instruction)
	reserved := price.Character.ChargeMicros(chars)
	snapshot, err := price.CharacterSnapshot(chars)
	if err != nil {
		// 轨道守卫已保证单位,这里只可能是编码失败;快照留空要留痕。
		log.Printf("relay: price snapshot for %s: %v", price.PublicModel, err)
		snapshot = nil
	}
	payload, err := speechUpstreamBody(req, ch.ModelMap[req.Model])
	if err != nil {
		h.failInternal(c, err)
		return
	}
	p := &prepared{
		publicModel: req.Model, raw: payload,
		key: key, price: price, reserved: reserved, snapshot: snapshot,
		source: SourceFrom(c),
	}

	h.audioPinned(c, p, ch,
		func(ctx context.Context, at attempt) (*UpstreamResponse, error) {
			upstream, uerr := h.adaptorFor(ch).SpeechSynthesis(ctx, ch, at.payload)
			if uerr == nil && upstream.OK {
				// 2xx 却解不出产物地址:按上游失败收尾(退款留痕,不虚交付)。
				if _, perr := parseSpeechAudioURL(upstream.Body); perr != nil {
					return nil, perr
				}
			}
			return upstream, uerr
		},
		func(run *failoverRunner, at attempt, upstream *UpstreamResponse) {
			// 量列存服务端计费字符数;实结 = 预扣,零差额不动账。
			run.settle(at, p.reserved, time.Since(p.started).Milliseconds(), func(l *usage.Log) {
				l.CompletionTokens = chars
			})
			audioURL, _ := parseSpeechAudioURL(upstream.Body)
			c.JSON(http.StatusOK, gin.H{
				"audio": gin.H{"url": audioURL},
				"usage": gin.H{"chars": chars},
			})
		})
}

// pinnedAudioChannel is the 钉渠道 validation chain shared by all four
// audio endpoints: 渠道存在 → 启用 → audio 能力 → 模型映射命中,每步一个
// 明确的 OpenAI error code。返回命中的渠道与它的上游模型名出口。
func (h *Handlers) pinnedAudioChannel(c *gin.Context, ctx context.Context, channelID int64, model string) (channel.Channel, bool) {
	if channelID <= 0 {
		apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError,
			"You must provide a 'channel_id' parameter.")
		return channel.Channel{}, false
	}
	ch, err := h.Channels.Get(ctx, channelID)
	if errors.Is(err, channel.ErrNotFound) {
		apierr.OpenAI(c, http.StatusNotFound, CodeChannelNotFound, TypeInvalidRequestError,
			fmt.Sprintf("No such channel: %d.", channelID))
		return channel.Channel{}, false
	}
	if err != nil {
		h.failInternal(c, err)
		return channel.Channel{}, false
	}
	if !ch.Enabled {
		apierr.OpenAI(c, http.StatusServiceUnavailable, CodeModelUnavailable, TypeServerError,
			"The channel serving this model is disabled.")
		return channel.Channel{}, false
	}
	if !ch.HasCapability(channel.CapAudio) {
		apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError,
			"The channel does not declare the audio capability.")
		return channel.Channel{}, false
	}
	if ch.ModelMap[model] == "" {
		apierr.OpenAI(c, http.StatusNotFound, CodeModelNotFound, TypeInvalidRequestError,
			"The model '"+model+"' is not mapped on channel '"+ch.Name+"'.")
		return channel.Channel{}, false
	}
	return ch, true
}

// audioPinned runs one pinned-channel audio attempt: acquire the breaker
// slot → reserve → dial → settle-or-fail. The breaker gate comes first so a
// circuit-open refusal never touches the ledger; a failed pre-deduction
// releases the slot. Failure is final(不换道):run.failed does the breaker
// bookkeeping, refunds whole and answers the client.
func (h *Handlers) audioPinned(c *gin.Context, p *prepared, ch channel.Channel, call func(ctx context.Context, at attempt) (*UpstreamResponse, error), deliver func(run *failoverRunner, at attempt, upstream *UpstreamResponse)) {
	ctx := c.Request.Context()
	// 账务与留痕使用脱离请求的 context:上游中途失败时,已预扣的钱必须
	// 退干净、失败 trail 必须落库(与聊天/生图/视频同一纪律)。
	billing := context.WithoutCancel(ctx)
	p.started = time.Now()
	run := &failoverRunner{h: h, c: c, billing: billing, p: p, breaker: h.Breaker}

	if !run.breaker.TryAcquire(ch.ID, time.Now()) {
		apierr.OpenAI(c, http.StatusServiceUnavailable, CodeModelUnavailable, TypeServerError,
			"The channel serving this model is circuit-open. Retry shortly.")
		return
	}
	if p.reserved > 0 {
		if _, err := h.Keys.Reserve(ctx, p.key.ID, p.reserved, apikey.ReasonEstimate); err != nil {
			run.breaker.Release(ch.ID)
			h.reserveFailed(c, p.key, err)
			return
		}
	}
	at := attempt{ch: ch, upstreamModel: ch.ModelMap[p.publicModel], payload: p.raw, contentType: "application/json"}

	upstream, err := call(ctx, at)
	if err == nil && upstream.OK {
		run.breaker.RecordSuccess(ch.ID)
		deliver(run, at, upstream)
		return
	}
	f := p.failure()
	if err != nil {
		f.transportErr = err
	} else {
		f.upstream = upstream
	}
	run.failed(at, f, false)
}

// validateVoiceRequest enforces the create contract: mode 互斥必填
// (design → voice_prompt,clone → audio 且仅 http(s))、长度上限、prefix
// 形状(缺省自动生成 ≤10 位字母数字)。
func validateVoiceRequest(req *voiceRequest) error {
	req.TargetModel = strings.TrimSpace(req.TargetModel)
	if req.TargetModel == "" {
		return errors.New("You must provide a 'target_model' parameter.")
	}
	if utf8.RuneCountInString(req.TargetModel) > maxTargetModelRunes {
		return fmt.Errorf("The 'target_model' parameter is too long (max %d).", maxTargetModelRunes)
	}

	req.Mode = strings.TrimSpace(req.Mode)
	req.VoicePrompt = strings.TrimSpace(req.VoicePrompt)
	req.Audio = strings.TrimSpace(req.Audio)
	req.PreviewText = strings.TrimSpace(req.PreviewText)
	switch req.Mode {
	case voiceModeDesign:
		if req.VoicePrompt == "" {
			return errors.New("'voice_prompt' is required in design mode.")
		}
		if utf8.RuneCountInString(req.VoicePrompt) > maxVoicePromptRunes {
			return fmt.Errorf("The 'voice_prompt' parameter exceeds the %d-character limit.", maxVoicePromptRunes)
		}
		if req.Audio != "" {
			return errors.New("'audio' only applies to clone mode.")
		}
	case voiceModeClone:
		if req.Audio == "" {
			return errors.New("'audio' is required in clone mode.")
		}
		if !strings.HasPrefix(req.Audio, "http://") && !strings.HasPrefix(req.Audio, "https://") {
			return errAudioInlineUnsupported
		}
		if req.VoicePrompt != "" {
			return errors.New("'voice_prompt' only applies to design mode.")
		}
	default:
		return errors.New("'mode' must be 'design' or 'clone'.")
	}

	if n := utf8.RuneCountInString(req.PreviewText); n < minPreviewTextRunes || n > maxPreviewTextRunes {
		return fmt.Errorf("The 'preview_text' parameter must be %d–%d characters.", minPreviewTextRunes, maxPreviewTextRunes)
	}
	req.Prefix = strings.TrimSpace(req.Prefix)
	if req.Prefix == "" {
		req.Prefix = randomPrefix(defaultPrefixRunes)
		return nil
	}
	if n := utf8.RuneCountInString(req.Prefix); n > maxPrefixRunes {
		return fmt.Errorf("The 'prefix' parameter must be at most %d alphanumerics.", maxPrefixRunes)
	}
	for _, r := range req.Prefix {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return errors.New("The 'prefix' parameter must be alphanumerics only.")
		}
	}
	return nil
}

// prefixAlphabet is the auto-generated prefix's charset (字母数字).
const prefixAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// randomPrefix draws n alphanumerics from crypto/rand — the upstream
// voice_id's readable discriminator when the client omits prefix.
func randomPrefix(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// 熵源缺失是系统级故障:回退到时间基前缀,请求不该因此失败。
		return fmt.Sprintf("t%x", time.Now().UnixNano())[:n]
	}
	for i := range b {
		b[i] = prefixAlphabet[int(b[i])%len(prefixAlphabet)]
	}
	return string(b)
}

// voiceUpstreamBody renders the create body the adaptor translates: the
// gateway fields minus channel_id, with the channel's upstream model name.
func voiceUpstreamBody(req voiceRequest, upstreamModel string) ([]byte, error) {
	body := map[string]any{
		"model":        upstreamModel,
		"mode":         req.Mode,
		"target_model": req.TargetModel,
	}
	if req.VoicePrompt != "" {
		body["voice_prompt"] = req.VoicePrompt
	}
	if req.Audio != "" {
		body["audio"] = req.Audio
	}
	if req.PreviewText != "" {
		body["preview_text"] = req.PreviewText
	}
	if req.Prefix != "" {
		body["prefix"] = req.Prefix
	}
	return json.Marshal(body)
}

// speechUpstreamBody renders the speech body the adaptor translates: the
// gateway fields minus channel_id, with the channel's upstream model name.
func speechUpstreamBody(req speechRequest, upstreamModel string) ([]byte, error) {
	return json.Marshal(map[string]any{
		"model":       upstreamModel,
		"voice":       req.Voice,
		"text":        req.Text,
		"instruction": req.Instruction,
	})
}

// parseSpeechAudioURL extracts the temporary artifact URL from a normalized
// speech body ({"audio":{"url":…}}) — the deliverability precondition the
// adaptor's 502 synthesis guarantees on the OK path.
func parseSpeechAudioURL(body []byte) (string, error) {
	var r struct {
		Audio *struct {
			URL string `json:"url"`
		} `json:"audio"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("upstream speech body is not a JSON object: %w", err)
	}
	if r.Audio == nil || strings.TrimSpace(r.Audio.URL) == "" {
		return "", errors.New("upstream speech body carries no audio.url")
	}
	return strings.TrimSpace(r.Audio.URL), nil
}
