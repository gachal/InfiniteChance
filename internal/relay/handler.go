package relay

import (
	"context"
	"encoding/json"
	"errors"
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
	CodeStreamUnsupported   = "stream_unsupported"
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

// ChatCompletions relays one non-streaming chat request:
// validate → select channel → price → reserve → forward → settle-or-refund,
// with a usage-log row for every request that reached the upstream call.
func (h *Handlers) ChatCompletions(c *gin.Context) {
	key, _ := apikey.KeyFrom(c)
	ctx := c.Request.Context()

	raw, err := c.GetRawData()
	if err != nil {
		apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError, "The request body could not be read.")
		return
	}
	var req chatRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError, "The request body is not a valid JSON object.")
		return
	}
	if req.Model == "" {
		apierr.OpenAI(c, http.StatusBadRequest, CodeMissingModel, TypeInvalidRequestError, "You must provide a 'model' parameter.")
		return
	}
	if len(req.Messages) == 0 {
		apierr.OpenAI(c, http.StatusBadRequest, CodeMissingMessages, TypeInvalidRequestError, "'messages' must be a non-empty array.")
		return
	}
	if req.Stream {
		// 流式透传由 05 号票实现;先明确拒绝,不做半吊子转发。
		apierr.OpenAI(c, http.StatusBadRequest, CodeStreamUnsupported, TypeInvalidRequestError, "Streaming is not supported by this gateway build yet.")
		return
	}

	ch, err := selectChannel(ctx, h.Channels, req.Model)
	if errors.Is(err, errNoChannel) {
		apierr.OpenAI(c, http.StatusNotFound, CodeModelNotFound, TypeInvalidRequestError,
			"The model '"+req.Model+"' does not exist or no enabled channel serves it.")
		return
	}
	if err != nil {
		h.failInternal(c, err)
		return
	}
	price, err := h.Prices.ByModel(ctx, req.Model)
	if errors.Is(err, pricing.ErrNotFound) {
		apierr.OpenAI(c, http.StatusBadRequest, CodeModelNotPriced, TypeInvalidRequestError,
			"The model '"+req.Model+"' has no price configured; ask the administrator to add one.")
		return
	}
	if err != nil {
		h.failInternal(c, err)
		return
	}
	if price.Unit != pricing.UnitToken || price.Token == nil {
		apierr.OpenAI(c, http.StatusBadRequest, CodeModelNotPriced, TypeInvalidRequestError,
			"The model '"+req.Model+"' is not priced for token billing.")
		return
	}

	// 估算预扣。免费模型(单价 0)估算为 0,跳过预扣与后续账务。
	upstreamModel := ch.ModelMap[req.Model]
	payload, err := rewriteModel(raw, upstreamModel)
	if err != nil {
		apierr.OpenAI(c, http.StatusBadRequest, CodeInvalidRequest, TypeInvalidRequestError, "The request body is not a valid JSON object.")
		return
	}
	promptEst, completionEst := estimateTokens(req, len(raw))
	reserved := price.Token.ChargeMicros(promptEst, completionEst)
	if reserved > 0 {
		if _, err := h.Keys.Reserve(ctx, key.ID, reserved, apikey.ReasonEstimate); err != nil {
			h.reserveFailed(c, key, err)
			return
		}
	}

	// 账务与留痕使用脱离请求的 context:客户端在转发中途断开时,请求
	// context 随之取消,但已预扣的钱必须退、失败 trail 必须落库。
	billing := context.WithoutCancel(ctx)

	f := upstreamFail{
		key: key, ch: ch, publicModel: req.Model, upstreamModel: upstreamModel,
		unit: string(price.Unit), reserved: reserved, snapshot: priceSnapshot(price),
	}
	started := time.Now()
	upstream, err := h.adaptor().ChatCompletions(ctx, ch, payload)
	f.durationMS = time.Since(started).Milliseconds()

	if err != nil || !upstream.OK {
		f.upstream, f.transportErr = upstream, err
		h.failUpstream(c, billing, f)
		return
	}

	clientBody, used, err := h.adaptor().Normalize(req.Model, upstream.Body)
	if err != nil {
		// 2xx 但响应体不可用:与上游失败同路 —— 退款、留痕、明确报错。
		f.normalizeErr = err
		h.failUpstream(c, billing, f)
		return
	}

	// 结算多退少补:delta = 预扣 − 实际,正数退回、负数补扣;零差额不动账。
	actual := price.Token.ChargeMicros(used.PromptTokens, used.CompletionTokens)
	if delta := reserved - actual; delta != 0 {
		if _, err := h.Keys.Adjust(billing, key.ID, delta, apikey.ReasonSettle); err != nil {
			// 响应已完成,账务失败只记日志,不影响客户端。
			log.Printf("relay: settle key %d delta %d: %v", key.ID, delta, err)
		}
	}
	h.recordUsage(billing, usage.Log{
		KeyID: key.ID, ChannelID: ch.ID, ChannelName: ch.Name,
		PublicModel: req.Model, UpstreamModel: upstreamModel,
		Unit: string(price.Unit), PromptTokens: used.PromptTokens,
		CompletionTokens: used.CompletionTokens, DurationMS: f.durationMS,
		Status: usage.StatusSuccess, ChargeMicros: actual,
		PriceSnapshot: f.snapshot,
	})

	c.Data(http.StatusOK, "application/json; charset=utf-8", clientBody)
}

// upstreamFail collects everything the failure path needs, whichever of the
// three failure shapes produced it: transport error, non-2xx status, or an
// unusable 2xx body.
type upstreamFail struct {
	key           apikey.Key
	ch            channel.Channel
	publicModel   string
	upstreamModel string
	unit          string
	snapshot      []byte
	reserved      int64
	durationMS    int64
	upstream      *UpstreamResponse
	transportErr  error
	normalizeErr  error
}

// failUpstream refunds the whole reserve, leaves the failure trail in the
// usage log, and answers the client an OpenAI-shaped error: a non-2xx
// upstream answer passes its status through with the extracted message,
// everything else becomes 502. billing is a context detached from the
// request — the caller may already be gone by the time the upstream failed.
func (h *Handlers) failUpstream(c *gin.Context, billing context.Context, f upstreamFail) {
	if f.reserved > 0 {
		// 钱已预扣必须退回;退不回(行消失)是严重账务事故,记日志报警。
		if _, err := h.Keys.Adjust(billing, f.key.ID, f.reserved, apikey.ReasonRefund); err != nil {
			log.Printf("relay: refund key %d reserve %d: %v", f.key.ID, f.reserved, err)
		}
	}

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

	h.recordUsage(billing, usage.Log{
		KeyID: f.key.ID, ChannelID: f.ch.ID, ChannelName: f.ch.Name,
		PublicModel: f.publicModel, UpstreamModel: f.upstreamModel,
		Unit: f.unit, DurationMS: f.durationMS,
		Status: usage.StatusUpstreamError, ChargeMicros: 0,
		PriceSnapshot: f.snapshot, UpstreamError: summary,
	})

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
		apierr.OpenAI(c, http.StatusUnauthorized, "invalid_api_key", TypeInvalidRequestError, "Incorrect API key provided.")
	case errors.Is(err, apikey.ErrKeyNotActive):
		code := "key_revoked"
		if key.Status(time.Now()) == apikey.StatusExpired {
			code = "key_expired"
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
