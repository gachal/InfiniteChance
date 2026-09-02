package relay

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/apierr"
	"github.com/gachal/InfiniteChance/internal/apikey"
	"github.com/gachal/InfiniteChance/internal/channel"
	"github.com/gachal/InfiniteChance/internal/pricing"
	"github.com/gachal/InfiniteChance/internal/usage"
)

// Relay-surface error codes. Every rejection is an OpenAI error object so
// SDK clients branch on code the same way they do for vendor errors; the
// relay face speaks English like the vendor APIs.
const (
	CodeInvalidRequest      = "invalid_request"
	CodeMissingModel        = "missing_model"
	CodeMissingMessages     = "missing_messages"
	CodeModelNotFound       = "model_not_found"
	CodeModelNotPriced      = "model_not_priced"
	CodeModelUnavailable    = "model_unavailable"
	CodeInsufficientQuota   = "insufficient_quota"
	CodeUpstreamError       = "upstream_error"
	TypeInvalidRequestError = "invalid_request_error"
	TypeInsufficientQuota   = "insufficient_quota"
	TypeServerError         = "server_error"
)

// chatRequest is the slice of the OpenAI chat-completions body the gateway
// itself needs. Everything else rides through untouched.
type chatRequest struct {
	Model               string            `json:"model"`
	Stream              bool              `json:"stream"`
	MaxTokens           *int64            `json:"max_tokens"`
	MaxCompletionTokens *int64            `json:"max_completion_tokens"`
	Messages            []json.RawMessage `json:"messages"`
}

const (
	// defaultCompletionEstimate stands in for max_tokens when the client
	// omits both token caps; the settle true-ups after completion, so the
	// estimate only has to catch gross overspend.
	defaultCompletionEstimate = int64(1024)
	// maxEstimateTokens caps either side of the estimate so a bogus
	// max_tokens cannot overflow the arithmetic.
	maxEstimateTokens = int64(10_000_000)
	// bytesPerToken is a blended EN/CJK heuristic (~3 UTF-8 bytes/token)
	// for estimating prompt tokens without shipping a tokenizer.
	bytesPerToken = 3
)

// prepared is the outcome of the request prefix both paths share: body
// validated, candidates scheduled, price checked, estimate pre-deduction
// taken. raw is the client body verbatim (public model name) — each channel
// attempt rewrites the model field to its own upstream name.
type prepared struct {
	req        chatRequest
	key        apikey.Key
	price      pricing.Price
	raw        []byte
	reserved   int64
	snapshot   []byte
	started    time.Time         // 第一个上游尝试的起点;成败行的耗时都是请求总耗时
	candidates []channel.Channel // 加权排序后的换道候选,首选在前
}

// attempt binds one channel candidacy of a prepared request: the channel
// plus the upstream request derived from it.
type attempt struct {
	ch            channel.Channel
	upstreamModel string
	payload       []byte
}

// attemptFor derives one channel's upstream request from the prepared body.
// prepareChat already proved the body rewrites; only the model string
// differs per channel, so an error here is impossible short of a bug — the
// caller treats one as an internal failure with the reserve refunded.
func (p *prepared) attemptFor(ch channel.Channel) (attempt, error) {
	upstreamModel := ch.ModelMap[p.req.Model]
	payload, err := rewriteModel(p.raw, upstreamModel)
	if err != nil {
		return attempt{}, err
	}
	return attempt{ch: ch, upstreamModel: upstreamModel, payload: payload}, nil
}

// failure seeds the failure bundle with the pre-deduction the failure path
// must refund; the request identity stays on prepared.
func (p *prepared) failure() upstreamFailure {
	return upstreamFailure{reserved: p.reserved}
}

// logEntry seeds a usage-log row with the request identity and the attempt's
// channel; callers fill in the outcome (duration, status, tokens, charge,
// error summary).
func (p *prepared) logEntry(at attempt) usage.Log {
	return usage.Log{
		KeyID: p.key.ID, ChannelID: at.ch.ID, ChannelName: at.ch.Name,
		PublicModel: p.req.Model, UpstreamModel: at.upstreamModel,
		Unit: string(p.price.Unit), PriceSnapshot: p.snapshot,
	}
}

// settleSuccess fills the success outcome into a seeded entry, settles the
// ledger to the actual charge (多退少补:delta = 预扣 − 实际,正数退回、
// 负数补扣;零差额不动账) and records the row. The runner's retried trail —
// attempts failover abandoned before this one succeeded — goes into the
// row's upstream_error column: a success row with a non-empty column reads
// as "survived upstream errors", which is exactly what the audit trail
// wants to show.
func (r *failoverRunner) settleSuccess(at attempt, used Usage, durationMS int64) {
	p := r.p
	actual := p.price.Token.ChargeMicros(used.PromptTokens, used.CompletionTokens)
	entry := p.logEntry(at)
	entry.PromptTokens = used.PromptTokens
	entry.CompletionTokens = used.CompletionTokens
	entry.DurationMS = durationMS
	entry.Status = usage.StatusSuccess
	entry.ChargeMicros = actual
	entry.UpstreamError = upstreamErrorSummary(r.retried, "")
	r.h.adjustBalance(r.billing, p.key.ID, p.reserved-actual, apikey.ReasonSettle)
	r.h.recordUsage(r.billing, entry)
}

// ChatCompletions relays one chat request — buffered or streamed — through
// validate → schedule candidates → price → reserve → try channels in order
// → settle-or-refund, with a usage-log row for every request that reached
// at least one upstream call. A temporary upstream failure (06 号票) moves
// the request to the next candidate while the reserve stays put; only the
// attempt that decides the outcome bills or refunds.
func (h *Handlers) ChatCompletions(c *gin.Context) {
	key, _ := apikey.KeyFrom(c)

	p := h.prepareChat(c, key)
	if p == nil {
		return
	}
	if p.req.Stream {
		h.streamChat(c, p)
		return
	}

	ctx := c.Request.Context()
	// 账务与留痕使用脱离请求的 context:客户端在转发中途断开时,请求
	// context 随之取消,但已预扣的钱必须退、失败 trail 必须落库。
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

		upstream, err := h.adaptor().ChatCompletions(ctx, at.ch, at.payload)
		if err == nil && upstream.OK {
			clientBody, used, nerr := h.adaptor().Normalize(p.req.Model, upstream.Body)
			if nerr == nil {
				// 命中:结算多退少补,按实际用量落成功留痕。
				run.breaker.RecordSuccess(at.ch.ID)
				run.settleSuccess(at, used, time.Since(p.started).Milliseconds())
				c.Data(http.StatusOK, "application/json; charset=utf-8", clientBody)
				return
			}
			// 2xx 但响应体不可用:与上游失败同路 —— 可换道重试。
			f := p.failure()
			f.normalizeErr = nerr
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

// prepareChat reads and validates the client body, schedules the candidate
// channels, checks the price and takes the estimate pre-deduction — the
// shared prefix of both paths. On any rejection the client has already been
// answered an OpenAI error object and the result is nil.
func (h *Handlers) prepareChat(c *gin.Context, key apikey.Key) *prepared {
	ctx := c.Request.Context()

	raw, err := c.GetRawData()
	if err != nil {
		apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError, "The request body could not be read.")
		return nil
	}
	var req chatRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError, "The request body is not a valid JSON object.")
		return nil
	}
	if req.Model == "" {
		apierr.OpenAI(c, http.StatusBadRequest, CodeMissingModel, TypeInvalidRequestError, "You must provide a 'model' parameter.")
		return nil
	}
	if len(req.Messages) == 0 {
		apierr.OpenAI(c, http.StatusBadRequest, CodeMissingMessages, TypeInvalidRequestError, "'messages' must be a non-empty array.")
		return nil
	}

	channels, err := h.Channels.List(ctx)
	if err != nil {
		h.failInternal(c, err)
		return nil
	}
	candidates := weightedOrder(eligibleChannels(channels, req.Model), h.randIntn)
	if len(candidates) == 0 {
		apierr.OpenAI(c, http.StatusNotFound, CodeModelNotFound, TypeInvalidRequestError,
			"The model '"+req.Model+"' does not exist or no enabled channel serves it.")
		return nil
	}
	price, err := h.Prices.ByModel(ctx, req.Model)
	if errors.Is(err, pricing.ErrNotFound) {
		apierr.OpenAI(c, http.StatusBadRequest, CodeModelNotPriced, TypeInvalidRequestError,
			"The model '"+req.Model+"' has no price configured; ask the administrator to add one.")
		return nil
	}
	if err != nil {
		h.failInternal(c, err)
		return nil
	}
	if price.Unit != pricing.UnitToken || price.Token == nil {
		apierr.OpenAI(c, http.StatusBadRequest, CodeModelNotPriced, TypeInvalidRequestError,
			"The model '"+req.Model+"' is not priced for token billing.")
		return nil
	}

	// 先对首个候选重写一次模型名,证明 body 是可重写的 JSON 对象;
	// 逐渠道的真正重写在 attemptFor 里做(各渠道上游名不同)。
	if _, err := rewriteModel(raw, candidates[0].ModelMap[req.Model]); err != nil {
		apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError, "The request body is not a valid JSON object.")
		return nil
	}

	// 估算预扣。免费模型(单价 0)估算为 0,跳过预扣与后续账务。
	promptEst, completionEst := estimateTokens(req, len(raw))
	reserved := price.Token.ChargeMicros(promptEst, completionEst)
	if reserved > 0 {
		if _, err := h.Keys.Reserve(ctx, key.ID, reserved, apikey.ReasonEstimate); err != nil {
			h.reserveFailed(c, key, err)
			return nil
		}
	}

	return &prepared{
		req: req, key: key, price: price, raw: raw,
		reserved: reserved, snapshot: priceSnapshot(price),
		candidates: candidates,
	}
}

// streamChat relays one streaming chat request: after the shared prepare,
// candidates are tried in scheduling order — but only failures before a
// stream opens (transport error, non-2xx rejection) can move to the next
// channel, because an open stream has already committed the response head.
// From the open stream on, the upstream SSE body is pumped to the client
// frame by frame, no reassembly; the pump loop exits on upstream EOF,
// upstream failure or a client disconnect (the request context cancels the
// upstream call, and a failed write means the client is gone) — the books
// close out either way on a context detached from the request.
func (h *Handlers) streamChat(c *gin.Context, p *prepared) {
	ctx := c.Request.Context()
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

		stream, err := h.adaptor().ChatCompletionsStream(ctx, at.ch, p.req.Model, at.payload)
		if err == nil && stream.OK {
			// 流已开:响应头随之发出,此后不再换道;熔断的成败记账延到
			// 流收尾(pumpStream)—— 打开流本身不算交付。
			run.pumpStream(at, stream)
			return
		}

		f := p.failure()
		if err != nil {
			f.transportErr = err
		} else {
			// 上游在流开始前就拒绝:读干错误体,按非流式失败同路处理。
			f.upstream = &UpstreamResponse{Status: stream.Status, Body: stream.Body}
			stream.Close()
		}
		if !run.failed(at, f, i < len(p.candidates)-1) {
			return
		}
	}
	run.exhausted()
}

// pumpStream relays one open upstream SSE stream to the client frame by
// frame, no reassembly (05 号票语义原样):每帧 data 负载语义保真,用量
// 专用块只入账不转发(除非客户端自己点了 include_usage)。账务在脱离
// 请求的 context 上收尾:拿到用量照常结算;流被中断或上游始终未报用量
// 则全额退款并留痕。熔断记账同样在流收尾时一次结清 —— 打开流本身不记
// 成功,否则一个只会开流就断的渠道永远攒不起连续失败:完整交付或已报
// 用量记成功;上游中途失败且尚无可计用量记一次失败;客户端主动断开不
// 记渠道的账。
func (r *failoverRunner) pumpStream(at attempt, stream *UpstreamStream) {
	h, p := r.h, r.p
	defer stream.Close()

	w := r.c.Writer
	header := w.Header()
	header.Set("Content-Type", "text/event-stream; charset=utf-8")
	header.Set("Cache-Control", "no-cache")
	header.Set("X-Accel-Buffering", "no") // 提醒反向代理别攒缓冲
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	var reported *Usage
	var streamErr error
	clientGone := false
	for {
		frame, u, err := stream.Next()
		if u != nil {
			reported = u
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				streamErr = err
			}
			break
		}
		if frame == nil {
			continue
		}
		if _, werr := w.Write(frame); werr != nil {
			clientGone = true
			break
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
	durationMS := time.Since(p.started).Milliseconds()

	// finishEntry records an unbilled outcome: refund the whole reserve and
	// trail with the given status and error summary.
	finishEntry := func(status, summary string) {
		entry := p.logEntry(at)
		entry.DurationMS = durationMS
		entry.Status = status
		entry.UpstreamError = upstreamErrorSummary(r.retried, summary)
		h.adjustBalance(r.billing, p.key.ID, p.reserved, apikey.ReasonRefund)
		h.recordUsage(r.billing, entry)
	}
	switch {
	case reported != nil:
		// 实际用量已报:照常按实结算。用量块在流尾,拿到它即视同服务已
		// 交付 —— 之后才发生的断开或上游故障不改账,报了多少结多少。
		r.breaker.RecordSuccess(at.ch.ID)
		r.settleSuccess(at, *reported, durationMS)
	case streamErr == nil && !clientGone:
		// 流完整走完却没有可计用量(上游不守 include_usage 约定):
		// 少记不虚记 —— 全额退款,成功流零扣费留痕;渠道交付完整,记成功。
		r.breaker.RecordSuccess(at.ch.ID)
		finishEntry(usage.StatusSuccess, "")
	default:
		// 流被中断:客户端断开(写失败或请求取消)或上游故障。
		// 用量未知 → 全额退款 + 失败留痕,摘要列区分原因。熔断账:上游
		// 故障记一次失败;客户端主动断开不是渠道的错,放回探测位。
		finishEntry(usage.StatusUpstreamError, streamAbortSummary(clientGone, streamErr))
		if streamErr != nil && !errors.Is(streamErr, context.Canceled) {
			r.breaker.RecordFailure(at.ch.ID, time.Now())
		} else {
			r.breaker.Release(at.ch.ID)
		}
	}
}

// streamAbortSummary explains why a stream ended without a reportable
// completion, for the usage log's error column.
func streamAbortSummary(clientGone bool, streamErr error) string {
	switch {
	case clientGone:
		return "client disconnected mid-stream"
	case errors.Is(streamErr, context.Canceled):
		return "client disconnected mid-stream: " + truncate(streamErr.Error(), 512)
	default:
		return "upstream stream failed: " + truncate(streamErr.Error(), 512)
	}
}

// failoverRunner carries one request's failover state across channel
// attempts: the breaker gate, the summaries of attempts abandoned along the
// way, the last real failure (the close-out when every remaining candidate
// turns out to be circuit-open), and the shared failure close-outs.
type failoverRunner struct {
	h           *Handlers
	c           *gin.Context
	billing     context.Context
	p           *prepared
	breaker     *channel.Breaker
	retried     []string         // 已换道放弃的尝试摘要:"'渠道名': 失败摘要"
	lastAttempt attempt          // 与 lastFailure 同置:最后一次真实失败
	lastFailure *upstreamFailure // nil = 尚未真正拨过任何上游
}

// failed resolves one failed upstream attempt: it records the breaker
// outcome and reports whether the request may move on to the next
// candidate. When it reports false the books are closed and the client has
// been answered. more tells it whether any candidate is left to try.
func (r *failoverRunner) failed(at attempt, f upstreamFailure, more bool) bool {
	f.durationMS = time.Since(r.p.started).Milliseconds()
	retryable := f.retryable()
	if retryable {
		r.breaker.RecordFailure(at.ch.ID, time.Now())
	} else {
		// 客户端或请求侧的问题(请求体被上游拒绝、厂商密钥失效、客户端
		// 主动断开):不记渠道的账,放回探测位。
		r.breaker.Release(at.ch.ID)
	}
	if retryable && r.c.Request.Context().Err() == nil && more {
		r.retried = append(r.retried, "'"+at.ch.Name+"': "+f.summary(r.h.adaptor()))
		r.lastAttempt, r.lastFailure = at, &f
		return true
	}
	r.failUpstream(at, f)
	return false
}

// exhausted closes out a request that ran out of candidates. Two shapes:
// some upstream was really dialed and only the remaining candidates turned
// out to be circuit-open — then the last real failure is the deciding
// outcome and gets the ordinary failure close-out (refund, trail, and the
// client hears the true cause); nothing was dialed at all (pure all-open) —
// the reserve is refunded whole, no usage row is written, and the client
// learns the model exists but is momentarily unservable.
func (r *failoverRunner) exhausted() {
	if r.lastFailure != nil {
		// 被存根的尝试已计入 retried 尾部;它现在就是终局,从换道史里
		// 取回,免得同一失败在摘要里出现两遍。
		f := *r.lastFailure
		r.retried = r.retried[:len(r.retried)-1]
		r.failUpstream(r.lastAttempt, f)
		return
	}
	r.h.adjustBalance(r.billing, r.p.key.ID, r.p.reserved, apikey.ReasonRefund)
	apierr.OpenAI(r.c, http.StatusServiceUnavailable, CodeModelUnavailable, TypeServerError,
		"The model '"+r.p.req.Model+"' is temporarily unavailable: every channel serving it is circuit-open. Retry shortly.")
}

// abortInternal closes out an impossible attempt-derivation failure: the
// breaker slot goes back untouched, the reserve is refunded so the account
// cannot leak, and the request answers 500. prepareChat's rewrite proof
// makes this unreachable in practice.
func (r *failoverRunner) abortInternal(channelID int64, err error) {
	r.breaker.Release(channelID)
	r.h.adjustBalance(r.billing, r.p.key.ID, r.p.reserved, apikey.ReasonRefund)
	r.h.failInternal(r.c, err)
}

// adjustBalance applies a signed ledger delta on a context detached from the
// request: by then the response may already be gone, so a failed adjustment
// can only scream into the log. Zero deltas change nothing by Store contract.
func (h *Handlers) adjustBalance(billing context.Context, keyID, delta int64, reason string) {
	if delta == 0 {
		return
	}
	if _, err := h.Keys.Adjust(billing, keyID, delta, reason); err != nil {
		log.Printf("relay: adjust key %d delta %d (%s): %v", keyID, delta, reason, err)
	}
}

// upstreamFailure collects the failure details for whichever of the three
// failure shapes aborted the upstream call: transport error, non-2xx
// status, or an unusable 2xx body. The request identity stays on prepared.
type upstreamFailure struct {
	reserved     int64
	durationMS   int64
	upstream     *UpstreamResponse
	transportErr error
	normalizeErr error
}

// retryable reports whether another channel could plausibly answer what
// this one failed at: transport errors (unless the client itself canceled),
// rate limits, server-side upstream failures, and an unparseable 2xx body.
// A client-caused upstream 4xx — a bad request, a dead vendor key, an
// unknown upstream model — is answered as-is: the request, not the channel,
// is the problem, and hammering every other channel with it helps nobody.
func (f upstreamFailure) retryable() bool {
	switch {
	case f.transportErr != nil:
		return !errors.Is(f.transportErr, context.Canceled)
	case f.normalizeErr != nil:
		return true
	default:
		return f.upstream.Status == http.StatusTooManyRequests || f.upstream.Status >= 500
	}
}

// summary renders the failure for the usage log and the client error: the
// transport message, the normalize message, or the vendor's own error text.
func (f upstreamFailure) summary(a Adaptor) string {
	switch {
	case f.transportErr != nil:
		return f.transportErr.Error()
	case f.normalizeErr != nil:
		return f.normalizeErr.Error()
	default:
		return a.ErrorSummary(f.upstream.Body)
	}
}

// status maps the failure to the client-facing HTTP status: a non-2xx
// upstream answer passes its status through, everything else becomes 502.
func (f upstreamFailure) status() int {
	if f.upstream != nil && f.transportErr == nil && f.normalizeErr == nil {
		return f.upstream.Status
	}
	return http.StatusBadGateway
}

// upstreamErrorSummary renders the request's upstream-error history for the
// usage log: attempts failover abandoned first, then the deciding outcome's
// own summary. Without retries it is the plain summary, unchanged from the
// single-channel shape.
func upstreamErrorSummary(retried []string, final string) string {
	if len(retried) == 0 {
		return final
	}
	return strings.Join(append(append([]string(nil), retried...), final), "; ")
}

// failUpstream refunds the whole reserve, leaves the failure trail in the
// usage log, and answers the client an OpenAI-shaped error carrying only
// the deciding attempt's summary — the retried-away attempts belong to the
// audit trail, not to the client (换道对客户端无感). billing is a context
// detached from the request — the caller may already be gone by the time
// the upstream failed.
func (r *failoverRunner) failUpstream(at attempt, f upstreamFailure) {
	h, p := r.h, r.p
	// 钱已预扣必须退回;退不回(行消失)是严重账务事故,记日志报警。
	h.adjustBalance(r.billing, p.key.ID, f.reserved, apikey.ReasonRefund)

	summary := f.summary(h.adaptor())
	entry := p.logEntry(at)
	entry.DurationMS = f.durationMS
	entry.Status = usage.StatusUpstreamError
	entry.ChargeMicros = 0
	entry.UpstreamError = upstreamErrorSummary(r.retried, summary)
	h.recordUsage(r.billing, entry)

	apierr.OpenAI(r.c, f.status(), CodeUpstreamError, CodeUpstreamError, "Upstream request failed: "+summary)
}

// reserveFailed maps a failed pre-deduction to the relay error surface: the
// OpenAI-shaped 429 for an empty balance, the unified 401s for a key that
// died between the middleware and billing, 500 otherwise.
func (h *Handlers) reserveFailed(c *gin.Context, key apikey.Key, err error) {
	switch {
	case errors.Is(err, apikey.ErrInsufficientQuota):
		apierr.OpenAI(c, http.StatusTooManyRequests, CodeInsufficientQuota, TypeInsufficientQuota,
			"Insufficient quota: the estimated cost of this request exceeds the remaining balance. Top up the key and retry.")
	case errors.Is(err, apikey.ErrKeyNotFound):
		apierr.OpenAI(c, http.StatusUnauthorized, apikey.CodeInvalidAPIKey, TypeInvalidRequestError, "Incorrect API key provided.")
	case errors.Is(err, apikey.ErrKeyNotActive):
		code := apikey.CodeKeyRevoked
		if key.Status(time.Now()) == apikey.StatusExpired {
			code = apikey.CodeKeyExpired
		}
		apierr.OpenAI(c, http.StatusUnauthorized, code, TypeInvalidRequestError, "This API key is no longer active.")
	default:
		h.failInternal(c, err)
	}
}

// rewriteModel returns the JSON object body with only the model field
// replaced — request direction public→upstream name, response direction
// back. Other fields survive as raw JSON; re-marshalling may reorder keys
// but never alters values (高级字段透传).
func rewriteModel(body []byte, model string) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(model)
	if err != nil {
		return nil, err
	}
	fields["model"] = raw
	return json.Marshal(fields)
}

// estimateTokens produces the pre-deduction's token guess: prompt tokens
// from body size (bytesPerToken heuristic) plus the completion budget
// (max_completion_tokens, else max_tokens, else the default).
func estimateTokens(req chatRequest, bodyBytes int) (prompt, completion int64) {
	prompt = clampEstimate(int64(bodyBytes) / bytesPerToken)
	completion = defaultCompletionEstimate
	switch {
	case req.MaxCompletionTokens != nil:
		completion = *req.MaxCompletionTokens
	case req.MaxTokens != nil:
		completion = *req.MaxTokens
	}
	return prompt, clampEstimate(completion)
}

func clampEstimate(tokens int64) int64 {
	if tokens < 0 {
		return 0
	}
	if tokens > maxEstimateTokens {
		return maxEstimateTokens
	}
	return tokens
}

// priceSnapshot renders the price in force for this request. Snapshot only
// errors on the call track, which the token-track guard above already
// rejects — a nil here would break the audit trail, so it screams in the log.
func priceSnapshot(price pricing.Price) []byte {
	snapshot, err := price.Snapshot()
	if err != nil {
		log.Printf("relay: price snapshot for %s: %v", price.PublicModel, err)
		return nil
	}
	return snapshot
}

func (h *Handlers) recordUsage(ctx context.Context, entry usage.Log) {
	if _, err := h.Usage.Insert(ctx, entry); err != nil {
		// 用量日志是审计唯一依据,写失败必须留下服务端痕迹。
		log.Printf("relay: record usage %+v: %v", entry, err)
	}
}

func (h *Handlers) failInternal(c *gin.Context, err error) {
	log.Printf("relay: %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
	apierr.OpenAI(c, http.StatusInternalServerError, "internal_error", TypeServerError, "The gateway hit an internal error.")
}
