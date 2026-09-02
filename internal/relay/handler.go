package relay

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
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

// prepared is the outcome of the request prefix both the buffered and the
// streaming path share: body validated, channel picked, price checked,
// estimate pre-deduction taken. payload is the client body with the
// upstream model name already swapped in.
type prepared struct {
	req           chatRequest
	key           apikey.Key
	ch            channel.Channel
	price         pricing.Price
	upstreamModel string
	payload       []byte
	reserved      int64
	snapshot      []byte
}

// failure seeds the failure bundle with the pre-deduction the failure path
// must refund; the request identity stays on prepared.
func (p *prepared) failure() upstreamFailure {
	return upstreamFailure{reserved: p.reserved}
}

// logEntry seeds a usage-log row with the request identity; callers fill in
// the outcome (duration, status, tokens, charge, error summary).
func (p *prepared) logEntry() usage.Log {
	return usage.Log{
		KeyID: p.key.ID, ChannelID: p.ch.ID, ChannelName: p.ch.Name,
		PublicModel: p.req.Model, UpstreamModel: p.upstreamModel,
		Unit: string(p.price.Unit), PriceSnapshot: p.snapshot,
	}
}

// settleSuccess fills the success outcome into a seeded entry, settles the
// ledger to the actual charge (多退少补:delta = 预扣 − 实际,正数退回、
// 负数补扣;零差额不动账) and records the row.
func (h *Handlers) settleSuccess(billing context.Context, p *prepared, used Usage, durationMS int64) {
	actual := p.price.Token.ChargeMicros(used.PromptTokens, used.CompletionTokens)
	entry := p.logEntry()
	entry.PromptTokens = used.PromptTokens
	entry.CompletionTokens = used.CompletionTokens
	entry.DurationMS = durationMS
	entry.Status = usage.StatusSuccess
	entry.ChargeMicros = actual
	h.adjustBalance(billing, p.key.ID, p.reserved-actual, apikey.ReasonSettle)
	h.recordUsage(billing, entry)
}

// ChatCompletions relays one chat request — buffered or streamed — through
// validate → select channel → price → reserve → forward → settle-or-refund,
// with a usage-log row for every request that reached the upstream call.
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

	f := p.failure()
	started := time.Now()
	upstream, err := h.adaptor().ChatCompletions(ctx, p.ch, p.payload)
	f.durationMS = time.Since(started).Milliseconds()

	if err != nil || !upstream.OK {
		f.upstream, f.transportErr = upstream, err
		h.failUpstream(c, billing, p, f)
		return
	}

	clientBody, used, err := h.adaptor().Normalize(p.req.Model, upstream.Body)
	if err != nil {
		// 2xx 但响应体不可用:与上游失败同路 —— 退款、留痕、明确报错。
		f.normalizeErr = err
		h.failUpstream(c, billing, p, f)
		return
	}

	// 结算多退少补,按实际用量落成功留痕。
	h.settleSuccess(billing, p, used, f.durationMS)

	c.Data(http.StatusOK, "application/json; charset=utf-8", clientBody)
}

// prepareChat reads and validates the client body, selects a channel,
// checks the price and takes the estimate pre-deduction — the shared prefix
// of both paths. On any rejection the client has already been answered an
// OpenAI error object and the result is nil.
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

	ch, err := selectChannel(ctx, h.Channels, req.Model)
	if errors.Is(err, errNoChannel) {
		apierr.OpenAI(c, http.StatusNotFound, CodeModelNotFound, TypeInvalidRequestError,
			"The model '"+req.Model+"' does not exist or no enabled channel serves it.")
		return nil
	}
	if err != nil {
		h.failInternal(c, err)
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

	// 估算预扣。免费模型(单价 0)估算为 0,跳过预扣与后续账务。
	upstreamModel := ch.ModelMap[req.Model]
	payload, err := rewriteModel(raw, upstreamModel)
	if err != nil {
		apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError, "The request body is not a valid JSON object.")
		return nil
	}
	promptEst, completionEst := estimateTokens(req, len(raw))
	reserved := price.Token.ChargeMicros(promptEst, completionEst)
	if reserved > 0 {
		if _, err := h.Keys.Reserve(ctx, key.ID, reserved, apikey.ReasonEstimate); err != nil {
			h.reserveFailed(c, key, err)
			return nil
		}
	}

	return &prepared{
		req: req, key: key, ch: ch, price: price,
		upstreamModel: upstreamModel, payload: payload,
		reserved: reserved, snapshot: priceSnapshot(price),
	}
}

// streamChat relays one streaming chat request: after the shared prepare,
// the upstream SSE body is pumped to the client frame by frame, no
// reassembly. The pump loop exits on upstream EOF, upstream failure or a
// client disconnect (the request context cancels the upstream call, and a
// failed write means the client is gone) — the books close out either way
// on a context detached from the request.
func (h *Handlers) streamChat(c *gin.Context, p *prepared) {
	ctx := c.Request.Context()
	billing := context.WithoutCancel(ctx)

	f := p.failure()
	started := time.Now()
	stream, err := h.adaptor().ChatCompletionsStream(ctx, p.ch, p.req.Model, p.payload)
	f.durationMS = time.Since(started).Milliseconds()

	if err != nil || !stream.OK {
		// 上游在流开始前就失败:响应头未动,与非流式失败同路。
		if err != nil {
			f.transportErr = err
		} else {
			f.upstream = &UpstreamResponse{Status: stream.Status, Body: stream.Body}
			stream.Close()
		}
		h.failUpstream(c, billing, p, f)
		return
	}
	defer stream.Close()

	w := c.Writer
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
	f.durationMS = time.Since(started).Milliseconds()

	// finishEntry records an unbilled outcome: refund the whole reserve and
	// trail with the given status and error summary.
	finishEntry := func(status, summary string) {
		entry := p.logEntry()
		entry.DurationMS = f.durationMS
		entry.Status = status
		entry.UpstreamError = summary
		h.adjustBalance(billing, p.key.ID, p.reserved, apikey.ReasonRefund)
		h.recordUsage(billing, entry)
	}
	switch {
	case reported != nil:
		// 实际用量已报:照常按实结算。用量块在流尾,拿到它即视同服务已
		// 交付 —— 之后才发生的断开或上游故障不改账,报了多少结多少。
		h.settleSuccess(billing, p, *reported, f.durationMS)
	case streamErr == nil && !clientGone:
		// 流完整走完却没有可计用量(上游不守 include_usage 约定):
		// 少记不虚记 —— 全额退款,成功流零扣费留痕。
		finishEntry(usage.StatusSuccess, "")
	default:
		// 流被中断:客户端断开(写失败或请求取消)或上游故障。
		// 用量未知 → 全额退款 + 失败留痕,摘要列区分原因。
		finishEntry(usage.StatusUpstreamError, streamAbortSummary(clientGone, streamErr))
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

// failUpstream refunds the whole reserve, leaves the failure trail in the
// usage log, and answers the client an OpenAI-shaped error: a non-2xx
// upstream answer passes its status through with the extracted message,
// everything else becomes 502. billing is a context detached from the
// request — the caller may already be gone by the time the upstream failed.
func (h *Handlers) failUpstream(c *gin.Context, billing context.Context, p *prepared, f upstreamFailure) {
	// 钱已预扣必须退回;退不回(行消失)是严重账务事故,记日志报警。
	h.adjustBalance(billing, p.key.ID, f.reserved, apikey.ReasonRefund)

	var summary string
	var status int
	switch {
	case f.transportErr != nil:
		summary = f.transportErr.Error()
		status = http.StatusBadGateway
	case f.normalizeErr != nil:
		summary = f.normalizeErr.Error()
		status = http.StatusBadGateway
	default:
		summary = h.adaptor().ErrorSummary(f.upstream.Body)
		status = f.upstream.Status
	}

	entry := p.logEntry()
	entry.DurationMS = f.durationMS
	entry.Status = usage.StatusUpstreamError
	entry.ChargeMicros = 0
	entry.UpstreamError = summary
	h.recordUsage(billing, entry)

	apierr.OpenAI(c, status, CodeUpstreamError, CodeUpstreamError, "Upstream request failed: "+summary)
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

// selectChannel picks the channel serving a public model: enabled channels
// whose model map contains the name, best priority first (Store.List order),
// ties by lowest id. Ticket 06 replaces this with weighted scheduling and
// failover. errNoChannel is a clean "nobody serves this model"; any other
// error is an infrastructure failure the caller reports as 500.
var errNoChannel = errors.New("relay: no enabled channel serves the model")

func selectChannel(ctx context.Context, store channel.Store, publicModel string) (channel.Channel, error) {
	channels, err := store.List(ctx)
	if err != nil {
		return channel.Channel{}, err
	}
	for _, ch := range channels {
		if !ch.Enabled {
			continue
		}
		if _, ok := ch.ModelMap[publicModel]; ok {
			return ch, nil
		}
	}
	return channel.Channel{}, errNoChannel
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
