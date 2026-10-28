// 音效生成的网关面(61 号票):同步 POST /v1/audio/sfx({model, text,
// seconds} → 直接回音效 URL),响应形状照 /v1/audio/speech ——
// {audio:{url}, usage:{seconds}},无异步三面。VOD sfx 上游虽是异步任务,
// 但在 vod adaptor 内同步等待(独立预算 3min + 5s 轮询,超时 504)。
// 计费 second 轨(61 号票定案 3):请求 seconds 预扣(单价 × 秒)→ 实结
// = 预扣定格(second 轨恒等结清,video.go 的 GetVideoTask succeeded 同款
// 先例;上游 Duration 真交付时长字段出现前不做多退少补)→ 失败全额退款,
// 「预扣 → 多退少补 → 失败退款」不变量以 actual=reserved 满足。
// 调度常规语义(不同于音色/台词合成的钉渠道,照配乐先例):按优先级/
// 权重选 sfx 能力渠道,上游临时失败(含同步等待超预算的 504)可换道,
// 预扣原封带往下一渠道;2xx 无产物 URL 按上游失败处理可换道(不虚交付)。
package relay

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/apierr"
	"github.com/gachal/InfiniteChance/internal/apikey"
	"github.com/gachal/InfiniteChance/internal/channel"
	"github.com/gachal/InfiniteChance/internal/pricing"
)

// 61 号票的契约上限:text 1–2000 runes(对齐配乐 prompt 先例,票面未给
// 限、集成方取一致值并写进 docs/api.md);seconds 整数 1–30(网关统一
// 域,模型域如 Kling [3,10] 交上游裁)。canvastask/handler.go 有同源副本,
// 改一边要同步另一边。
const (
	maxSfxTextRunes = 2000
	sfxMinSeconds   = int64(1)
	sfxMaxSeconds   = int64(30)
)

// sfxRequest is the submit body's gateway slice(61 号票三字段;loop 已删 —
// Kling/EL 上游均无循环参数,原 ElevenLabs 特性无处翻译)。
type sfxRequest struct {
	Model   string `json:"model"`
	Text    string `json:"text"`
	Seconds int64  `json:"seconds"`
}

// CreateSfx relays POST /v1/audio/sfx: validate → schedule sfx-capable
// candidates → price(second 轨)→ reserve(单价 × seconds)→ try channels
// in order → the first delivered URL settles(actual=reserved 定格)。
// 每次上游尝试在 adaptor 内同步等待终态,一次请求的总耗时上限约为
// 候选数 × sfx 预算。
func (h *Handlers) CreateSfx(c *gin.Context) {
	key, _ := apikey.KeyFrom(c)

	p := h.prepareSfx(c, key)
	if p == nil {
		return
	}

	ctx := c.Request.Context()
	// 账务与留痕使用脱离请求的 context:上游中途失败或客户端断开,已预扣
	// 的钱也必须退干净、失败 trail 必须落库(与聊天/生图/配乐同一纪律)。
	billing := context.WithoutCancel(ctx)
	p.started = time.Now()
	run := &failoverRunner{h: h, c: c, billing: billing, p: p, breaker: h.Breaker}

	for i, cand := range p.candidates {
		if !run.breaker.TryAcquire(cand.ID, time.Now()) {
			continue // 熔断中的渠道不占用尝试
		}
		at, err := p.attemptFor(cand)
		if err != nil {
			run.abortInternal(cand.ID, err)
			return
		}

		upstream, err := h.adaptorFor(at.ch).SfxSynthesis(ctx, at.ch, at.payload)
		if err == nil && upstream.OK {
			audioURL, perr := parseSpeechAudioURL(upstream.Body)
			if perr == nil {
				// 命中:second 轨恒等结清(实扣 = 预扣,差额恒零不动账),
				// 量列不落(usage 面 call/second 行 token 列恒 0)。
				run.breaker.RecordSuccess(at.ch.ID)
				run.settle(at, p.reserved, time.Since(p.started).Milliseconds(), nil)
				c.JSON(http.StatusOK, gin.H{
					"audio": gin.H{"url": audioURL},
					"usage": gin.H{"seconds": p.sfx.seconds},
				})
				return
			}
			// 2xx 却解不出产物地址:与上游失败同路 —— 可换道重试(不虚交付)。
			f := p.failure()
			f.normalizeErr = perr
			if !run.failed(at, f, i < len(p.candidates)-1) {
				return
			}
			continue
		}

		f := p.failure()
		if err != nil {
			f.transportErr = err
		} else {
			f.upstream = upstream
		}
		if !run.failed(at, f, i < len(p.candidates)-1) {
			return
		}
	}
	run.exhausted()
}

// prepareSfx is the request prefix: validate, schedule sfx-capable
// candidates, require a second-track price and take the pre-deduction
// (单价 × seconds;审计快照记下请求事实)。On any rejection the client has
// already been answered an OpenAI error object and the result is nil.
func (h *Handlers) prepareSfx(c *gin.Context, key apikey.Key) *prepared {
	ctx := c.Request.Context()

	raw, err := c.GetRawData()
	if err != nil {
		refuseBody(c, err)
		return nil
	}
	var req sfxRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError, "The request body is not a valid JSON object.")
		return nil
	}
	if req.Model == "" {
		apierr.OpenAI(c, http.StatusBadRequest, CodeMissingModel, TypeInvalidRequestError, "You must provide a 'model' parameter.")
		return nil
	}
	// 描述按 trim 后计长(音效的空格不是内容,与配乐 prompt 的「原文计费」
	// 不同 —— 音效按秒计费,文本不进账,trim 只为校验口径统一)。
	text := strings.TrimSpace(req.Text)
	if n := utf8.RuneCountInString(text); n < 1 {
		apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError, "You must provide a 'text' parameter.")
		return nil
	} else if n > maxSfxTextRunes {
		apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError,
			"The 'text' parameter exceeds the "+strconv.Itoa(maxSfxTextRunes)+"-character limit.")
		return nil
	}
	if req.Seconds < sfxMinSeconds || req.Seconds > sfxMaxSeconds {
		apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError,
			"The 'seconds' parameter must be an integer between "+strconv.FormatInt(sfxMinSeconds, 10)+" and "+strconv.FormatInt(sfxMaxSeconds, 10)+".")
		return nil
	}

	channels, err := h.listChannels(ctx)
	if err != nil {
		h.failInternal(c, err)
		return nil
	}
	candidates := weightedOrder(eligibleChannels(channels, req.Model, channel.CapSfx), h.randIntn)
	if len(candidates) == 0 {
		apierr.OpenAI(c, http.StatusNotFound, CodeModelNotFound, TypeInvalidRequestError,
			"The model '"+req.Model+"' does not exist or no sfx-capable channel serves it.")
		return nil
	}
	price, err := h.priceFor(ctx, req.Model)
	if errors.Is(err, pricing.ErrNotFound) {
		apierr.OpenAI(c, http.StatusBadRequest, CodeModelNotPriced, TypeInvalidRequestError,
			"The model '"+req.Model+"' has no price configured; ask the administrator to add one.")
		return nil
	}
	if err != nil {
		h.failInternal(c, err)
		return nil
	}
	if price.Unit != pricing.UnitSecond || price.Call == nil {
		apierr.OpenAI(c, http.StatusBadRequest, CodeModelNotPriced, TypeInvalidRequestError,
			"The model '"+req.Model+"' is not priced for per-second sfx billing.")
		return nil
	}

	// 先对首个候选重写一次模型名,证明 body 是可重写的 JSON 对象;
	// 逐渠道的真正重写在 attemptFor 里做(各渠道上游名不同)。
	if _, err := rewriteModel(raw, candidates[0].ModelMap[req.Model]); err != nil {
		apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError, "The request body is not a valid JSON object.")
		return nil
	}

	// 预扣:second 轨「单价 × 秒」,size 空串(音效无分辨率档);审计快照
	// 记下请求事实。
	reserved := price.Call.ChargeMicros("", req.Seconds)
	snapshot, err := price.CallSnapshot("", req.Seconds)
	if err != nil {
		// 轨道守卫已保证单位,这里只可能是编码失败;快照留空要留痕。
		log.Printf("relay: price snapshot for %s: %v", price.PublicModel, err)
		snapshot = nil
	}
	if reserved > 0 {
		if _, err := h.Keys.Reserve(ctx, key.ID, reserved, apikey.ReasonEstimate); err != nil {
			h.reserveFailed(c, key, err)
			return nil
		}
	}

	return &prepared{
		publicModel: req.Model,
		raw:         raw,
		key:         key, price: price,
		reserved: reserved, snapshot: snapshot,
		candidates: candidates,
		source:     SourceFrom(c),
		sfx:        &sfxPrep{seconds: req.Seconds},
	}
}
