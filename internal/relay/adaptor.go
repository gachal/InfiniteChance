// Package relay is the gateway's core vertical slice: the OpenAI-compatible
// relay surface (/v1) that authenticates an API key, maps the public model
// name to a channel, forwards through a narrow vendor adaptor, and bills the
// key with the estimate → settle-or-refund flow, writing a usage-log row per
// relayed request. Buffered and SSE-streamed chat completions share that
// skeleton; multi-channel failover and circuit breaking are ticket 06.
package relay

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	// ChatCompletionsStream opens one streaming chat request. The adaptor
	// injects whatever the vendor needs to report usage (OpenAI-compat:
	// stream_options.include_usage — without it no chunk ever carries
	// usage and the settlement would have nothing to settle with). A
	// non-2xx answer is returned as a closed !OK stream with the buffered
	// error body; a transport error returns (nil, err).
	ChatCompletionsStream(ctx context.Context, ch channel.Channel, publicModel string, payload []byte) (*UpstreamStream, error)
	// Normalize converts a 2xx upstream chat-completion body into the
	// client-facing body (model rewritten to the public name) and extracts
	// the usage.
	Normalize(publicModel string, upstreamBody []byte) (clientBody []byte, usage Usage, err error)
	// ErrorSummary extracts a one-line human-readable message from a failed
	// upstream body, for the usage log and the client-facing error.
	ErrorSummary(body []byte) string
}

// maxStreamLine caps one SSE line (a single data chunk); chunks are well
// under it, a runaway upstream should fail instead of exhausting memory.
const maxStreamLine = 1 << 20

// UpstreamStream is one open streaming upstream conversation. A !OK stream
// carries the vendor's rejection (Status + buffered Body) and has nothing to
// iterate. An OK stream yields client-facing SSE frames: Next blocks for
// the next event, Close releases the upstream connection — the caller must
// always call Close exactly once.
type UpstreamStream struct {
	Status int
	OK     bool
	Body   []byte // !OK 时:缓冲的上游错误响应体

	scanner     *bufio.Scanner
	resp        *http.Response
	publicModel string
	// clientWantsUsage records whether the client's own request asked for
	// stream_options.include_usage; only then is the vendor's usage-only
	// chunk (empty choices, usage set) forwarded instead of swallowed.
	clientWantsUsage bool
	// pendingUsage holds the last usage the vendor reported until the next
	// Next call hands it to the handler alongside a frame or EOF.
	pendingUsage *Usage
	done         bool
}

// Next returns the next client-facing SSE frame — a complete `data: …` line
// (or upstream comment) plus the terminating blank line, ready to write —
// and any usage the vendor reported since the last call. Events are passed
// through one by one: payloads survive byte-for-byte except the model field,
// which is rewritten to the public name like the buffered path; a payload
// that does not parse as a JSON object is forwarded untouched. The
// billing-only usage-only chunk is swallowed unless the client asked for
// usage. io.EOF ends a finished stream; any other error aborts it (a client
// disconnect surfaces as an error wrapping context.Canceled). Next is not
// safe for concurrent use.
func (s *UpstreamStream) Next() (frame []byte, u *Usage, err error) {
	if !s.OK {
		return nil, nil, errors.New("relay: Next on a non-2xx upstream stream")
	}
	if s.done {
		return nil, s.takePendingUsage(), io.EOF
	}

	var data [][]byte // 当前事件已积累的 data 行
	emit := func() ([]byte, bool) {
		if len(data) == 0 {
			return nil, false
		}
		payload := bytes.Join(data, []byte("\n"))
		data = nil
		if usage, usageOnly := inspectChunk(payload); usage != nil {
			s.pendingUsage = usage
			if usageOnly && !s.clientWantsUsage {
				return nil, true // 只为记账而注入的块,客户端不背这个账
			}
		}
		return s.clientFrame(payload), true
	}

	for s.scanner.Scan() {
		line := s.scanner.Bytes()
		switch {
		case len(line) == 0: // 空行 = 事件边界
			if frame, ok := emit(); ok {
				return frame, s.takePendingUsage(), nil
			}
		case bytes.HasPrefix(line, dataPrefix):
			payload := bytes.TrimPrefix(bytes.TrimPrefix(line, dataPrefix), []byte(" "))
			data = append(data, payload)
			if bytes.Equal(payload, doneMarker) {
				s.done = true
			}
		default:
			// 注释行等非 data 行自成事件,原样转发(OpenAI 用注释保活);
			// 先冲刷已积累的 data 事件,保住上游的到达顺序。
			if frame, ok := emit(); ok {
				return frame, s.takePendingUsage(), nil
			}
			return append(append([]byte{}, line...), '\n', '\n'), s.takePendingUsage(), nil
		}
		if s.done && len(data) > 0 {
			// [DONE] 之后不再读:发完这一帧就收摊。
			if frame, ok := emit(); ok {
				return frame, s.takePendingUsage(), nil
			}
		}
	}

	// 流结束:未终止的尾事件也要发出(上游忘了结尾空行也算它的答案)。
	if frame, ok := emit(); ok {
		return frame, s.takePendingUsage(), nil
	}
	s.done = true
	scanErr := s.scanner.Err()
	if scanErr == nil {
		return nil, s.takePendingUsage(), io.EOF
	}
	if errors.Is(scanErr, context.Canceled) {
		return nil, s.takePendingUsage(), fmt.Errorf("relay: upstream stream canceled: %w", scanErr)
	}
	return nil, s.takePendingUsage(), fmt.Errorf("relay: upstream stream failed: %w", scanErr)
}

// Close releases the upstream connection. Safe to call on a !OK stream;
// idempotent.
func (s *UpstreamStream) Close() error {
	if s.resp == nil {
		return nil
	}
	err := s.resp.Body.Close()
	s.resp = nil
	return err
}

func (s *UpstreamStream) takePendingUsage() *Usage {
	u := s.pendingUsage
	s.pendingUsage = nil
	return u
}

var (
	dataPrefix = []byte("data:")
	doneMarker = []byte("[DONE]")
)

// clientFrame renders one payload as the client-facing `data: …` frame:
// the model field rewritten to the public name when the payload is a JSON
// object, verbatim otherwise.
func (s *UpstreamStream) clientFrame(payload []byte) []byte {
	out := payload
	if rewritten, err := rewriteModel(payload, s.publicModel); err == nil {
		out = rewritten
	}
	frame := make([]byte, 0, len(out)+len(dataPrefix)+3)
	frame = append(frame, dataPrefix...)
	frame = append(frame, ' ')
	frame = append(frame, out...)
	return append(frame, '\n', '\n')
}

// inspectChunk extracts what billing needs from one chunk payload: the
// vendor-reported usage, and whether the chunk is a usage-only chunk
// (usage set, no choices) — the shape of the trailing chunk OpenAI sends
// when include_usage is on.
func inspectChunk(payload []byte) (u *Usage, usageOnly bool) {
	var chunk struct {
		Choices *json.RawMessage `json:"choices"`
		Usage   *Usage           `json:"usage"`
	}
	if err := json.Unmarshal(payload, &chunk); err != nil {
		return nil, false
	}
	if chunk.Usage == nil {
		return nil, false
	}
	u = &Usage{
		PromptTokens:     max(chunk.Usage.PromptTokens, 0),
		CompletionTokens: max(chunk.Usage.CompletionTokens, 0),
	}
	usageOnly = chunk.Choices == nil || bytes.Equal(*chunk.Choices, []byte("[]"))
	return u, usageOnly
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
	req, err := a.newUpstreamRequest(ctx, ch, payload)
	if err != nil {
		return nil, err
	}

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

// newUpstreamRequest builds the upstream POST both the buffered and the
// streaming call share: same URL, same auth headers, body verbatim.
func (a *openAIAdaptor) newUpstreamRequest(ctx context.Context, ch channel.Channel, payload []byte) (*http.Request, error) {
	// BaseURL 含版本路径(如 https://api.openai.com/v1),已在录入时规范化。
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ch.BaseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+ch.APIKey)
	return req, nil
}

// ChatCompletionsStream opens the streaming variant of the same upstream
// call. Before forwarding, the payload gets stream_options.include_usage
// forced on: the settlement bills the vendor's reported usage, and a stream
// without it reports none. The client's own stream_options fields ride
// along untouched; only whether the client itself asked for usage is
// remembered, because that decides whether the usage-only chunk is
// forwarded or kept as the billing side-channel.
func (a *openAIAdaptor) ChatCompletionsStream(ctx context.Context, ch channel.Channel, publicModel string, payload []byte) (*UpstreamStream, error) {
	payload, clientWantsUsage, err := ensureUsageReporting(payload)
	if err != nil {
		return nil, fmt.Errorf("stream payload is not a JSON object: %w", err)
	}
	req, err := a.newUpstreamRequest(ctx, ch, payload)
	if err != nil {
		return nil, err
	}

	resp, err := a.Client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// 上游在流开始前就拒绝:读干错误体,按非流式失败同路处理。
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxUpstreamBody))
		resp.Body.Close()
		return &UpstreamStream{Status: resp.StatusCode, Body: body}, nil
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), maxStreamLine)
	return &UpstreamStream{
		Status:           resp.StatusCode,
		OK:               true,
		scanner:          scanner,
		resp:             resp,
		publicModel:      publicModel,
		clientWantsUsage: clientWantsUsage,
	}, nil
}

// ensureUsageReporting returns payload with stream_options.include_usage
// set to true, and whether the client had asked for it themselves. Unknown
// stream_options fields the client sent survive the rewrite (全量透传).
func ensureUsageReporting(payload []byte) ([]byte, bool, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return nil, false, err
	}
	clientAsked := false
	if raw, ok := fields["stream_options"]; ok {
		var opts struct {
			IncludeUsage *bool `json:"include_usage"`
		}
		if err := json.Unmarshal(raw, &opts); err == nil && opts.IncludeUsage != nil {
			clientAsked = *opts.IncludeUsage
		}
	}
	// include_usage 恒置真(记账需要);clientAsked 只影响转发与否。
	if _, ok := fields["stream_options"]; !ok {
		fields["stream_options"] = json.RawMessage(`{}`)
	}
	var opts map[string]json.RawMessage
	if err := json.Unmarshal(fields["stream_options"], &opts); err != nil || opts == nil {
		opts = map[string]json.RawMessage{}
	}
	opts["include_usage"] = json.RawMessage("true")
	raw, err := json.Marshal(opts)
	if err != nil {
		return nil, clientAsked, err
	}
	fields["stream_options"] = raw
	out, err := json.Marshal(fields)
	return out, clientAsked, err
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
	usage.PromptTokens = max(usage.PromptTokens, 0)
	usage.CompletionTokens = max(usage.CompletionTokens, 0)
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
