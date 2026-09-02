// Package relay is the gateway's core vertical slice: the OpenAI-compatible
// relay surface (/v1) that authenticates an API key, maps the public model
// name to a channel, forwards through a narrow vendor adaptor, and bills the
// key with the estimate → settle-or-refund flow, writing a usage-log row per
// relayed request. SSE streaming is ticket 05; multi-channel failover and
// circuit breaking are ticket 06.
package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gachal/InfiniteChance/internal/channel"
)

// Usage is the per-request token accounting an adaptor produces from the
// upstream response — the billing data's single entry point.
type Usage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
}

// UpstreamResponse is one raw upstream answer: status plus body. OK marks
// the 2xx range; anything else is treated as a failure to normalize.
type UpstreamResponse struct {
	Status int
	Body   []byte
	OK     bool
}

// Adaptor is the narrow vendor seam from the spec: build the upstream
// request (URL, auth header), execute it, normalize the response and produce
// the usage. All OpenAI-compatible upstreams share one adaptor; each new
// channel type adds an implementation here, nothing else in the relay
// changes.
type Adaptor interface {
	// ChatCompletions forwards one non-streaming chat request. payload is
	// the complete JSON body with the upstream model name already set.
	ChatCompletions(ctx context.Context, ch channel.Channel, payload []byte) (*UpstreamResponse, error)
	// Normalize converts a 2xx upstream chat-completion body into the
	// client-facing body (model rewritten to the public name) and extracts
	// the usage.
	Normalize(publicModel string, upstreamBody []byte) (clientBody []byte, usage Usage, err error)
	// ErrorSummary extracts a one-line human-readable message from a failed
	// upstream body, for the usage log and the client-facing error.
	ErrorSummary(body []byte) string
}

// maxUpstreamBody caps how much of an upstream response is read into
// memory; chat completions are far below this, a runaway upstream should
// fail instead of exhausting the gateway.
const maxUpstreamBody = 32 << 20

// openAIAdaptor relays OpenAI-compatible upstreams: the request is the
// client body with only the model name swapped (chat parameters pass
// through untouched — v1 全量透传), and the response passes back with the
// model rewritten to the public name.
type openAIAdaptor struct {
	Client *http.Client
}

// upstreamTimeout bounds one upstream chat request; without it a stalled
// vendor would hang the client's request until the client itself gives up.
// Generous because non-streaming completions can legitimately run minutes.
const upstreamTimeout = 5 * time.Minute

// NewOpenAIAdaptor builds the adaptor for channel.TypeOpenAI upstreams.
func NewOpenAIAdaptor() Adaptor {
	return &openAIAdaptor{Client: &http.Client{Timeout: upstreamTimeout}}
}

func (a *openAIAdaptor) ChatCompletions(ctx context.Context, ch channel.Channel, payload []byte) (*UpstreamResponse, error) {
	// BaseURL 含版本路径(如 https://api.openai.com/v1),已在录入时规范化。
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ch.BaseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+ch.APIKey)

	resp, err := a.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxUpstreamBody))
	if err != nil {
		return nil, err
	}
	return &UpstreamResponse{
		Status: resp.StatusCode,
		Body:   body,
		OK:     resp.StatusCode >= 200 && resp.StatusCode < 300,
	}, nil
}

func (a *openAIAdaptor) Normalize(publicModel string, upstreamBody []byte) ([]byte, Usage, error) {
	clientBody, err := rewriteModel(upstreamBody, publicModel)
	if err != nil {
		return nil, Usage{}, fmt.Errorf("upstream body is not a JSON object: %w", err)
	}

	// usage 解析失败按零用量计(账面少记不虚记);负数同。
	var envelope struct {
		Usage *Usage `json:"usage"`
	}
	_ = json.Unmarshal(upstreamBody, &envelope)
	var usage Usage
	if envelope.Usage != nil {
		usage = *envelope.Usage
	}
	if usage.PromptTokens < 0 {
		usage.PromptTokens = 0
	}
	if usage.CompletionTokens < 0 {
		usage.CompletionTokens = 0
	}
	return clientBody, usage, nil
}

func (a *openAIAdaptor) ErrorSummary(body []byte) string {
	// 宽进窄出:标准 OpenAI error object 取 message,否则截取原文。
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &e); err == nil && e.Error.Message != "" {
		return truncate(e.Error.Message, 512)
	}
	s := strings.TrimSpace(string(body))
	if s == "" {
		return "(empty upstream error body)"
	}
	return truncate(s, 512)
}

// truncate cuts to at most n runes so a Chinese error message can never be
// split mid-character into invalid UTF-8.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}
