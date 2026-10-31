// Package promptgen implements the canvas's generate-prompt action (11 号票):
// canvas/server fills the admin-maintained template with the user's topic and
// relays it through the gateway's chat surface with the service-level key, so
// the spent tokens bill and land in the gateway's usage log like any direct
// chat call. The generated text is returned to the editor, which writes it
// into the current node or a new prompt node — the graph is never touched
// server-side, autosave stays the only writer.
package promptgen

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client calls the gateway's relay surface with the canvas's service-level
// key (CONTEXT.md 服务级 key:画布前端不持有任何网关或厂商密钥). Every call
// carries the canvas origin in X-InfiniteChance-Source so the gateway's
// usage log attributes canvas spend apart from direct key traffic.
type Client struct {
	BaseURL string // 网关根地址,如 http://localhost:8080
	Key     string // 服务级 API key(sk-…),完整值只在 canvas/server 进程内
	HTTP    *http.Client
}

// NewClient wires a client with a conservative backstop timeout — chat
// generations are the slowest of ordinary calls (reasoning models included),
// the handler sets no deadline of its own beyond the request context.
func NewClient(baseURL, key string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Key:     key,
		HTTP:    &http.Client{Timeout: 5 * time.Minute},
	}
}

// ChatMessage is one conversation turn (29 号票 Agent 多轮会话):role ∈
// user|assistant。纯文本轮 Content 即该轮文本;媒体轮(32 号票)置 Parts,
// content 走多模态分节数组、Content 被忽略。63 号票起另有两种循环中间帧:
// assistant 轮置 ToolCalls 表示发起的工具调用;role=tool 轮置 ToolCallID
// 回填执行结果(Content 是结果 JSON 文本)。中间帧只在循环内面上送,
// 不入节点 data 持久历史。
type ChatMessage struct {
	Role    string
	Content string
	// Parts 非空 = 多模态轮:content 上送为分节数组(媒体在前文本在后,
	// 17 号票形状),由 MediaTextParts 组装。
	Parts []ChatContentPart
	// ToolCalls 非空 = assistant 轮发起的工具调用(OpenAI tool_calls 形状)。
	ToolCalls []ToolCall
	// ToolCallID 非空 = role=tool 轮:本条结果对应哪一次调用。
	ToolCallID string
}

// ToolCall is one tool invocation the model asked for (63 号票):ID/Name
// 原样来自上游,Arguments 是调用参数的 JSON 文本(流式分片拼接后的全文,
// 交给工具 Execute 解析)。
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ChatRequest is one chat call through the gateway. Two shapes share the
// struct: the single-turn form (11/13/17 号票) carries Content as the
// instruction text with VideoURL / ImageURL attaching a multimodal content
// part ahead of the text; the multi-turn form (29 号票 Agent 会话) carries
// the complete Conversation (plain text) and takes precedence when non-empty.
// Source is the canvas origin mark (X-InfiniteChance-Source 值).
type ChatRequest struct {
	Model  string
	Source string
	// 单轮形态:指令文本与可选媒体分节。
	Content  string
	VideoURL string
	ImageURL string
	// 多轮形态:完整 messages 列表(纯文本),非空时优先于单轮形态。
	Conversation []ChatMessage
	// Tools 是暴露给模型的工具定义(63 号票 Agent 工具循环,OpenAI tools
	// 数组形状,每个元素是 {"type":"function","function":{…}} 的 JSON 原文,
	// 由工具注册表生成):网关聊天面对该字段零改动透传。空 = 不带 tools
	// 字段,请求与升级前逐字节一致。
	Tools []json.RawMessage
	// ToolChoice 透传 OpenAI tool_choice("auto"/"none"/"required");空 =
	// 不带该字段(OpenAI 缺省行为,等价 auto)。
	ToolChoice string
}

// ChatResult is the model's answer, whitespace-trimmed.
type ChatResult struct {
	Content string
}

// conversationOf picks the messages a chat call sends: the complete
// conversation when one is given (multi-turn Agent sessions), the
// single-turn shape otherwise.
func conversationOf(req ChatRequest) []chatMessage {
	messages := make([]chatMessage, 0, len(req.Conversation)+1)
	if len(req.Conversation) > 0 {
		for _, m := range req.Conversation {
			messages = append(messages, chatMessageOf(m))
		}
		return messages
	}
	return append(messages, userMessage(req))
}

// GenerateChat calls POST /v1/chat/completions (non-streaming — the editor
// waits for the whole text anyway) and returns choices[0].message.content.
// A gateway rejection (OpenAI error object) or an empty answer is an error
// carrying the reason back to the editor.
func (c *Client) GenerateChat(ctx context.Context, req ChatRequest) (ChatResult, error) {
	payload, err := json.Marshal(chatBody(req, false))
	if err != nil {
		return ChatResult{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.BaseURL+"/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return ChatResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.Key)
	if req.Source != "" {
		httpReq.Header.Set("X-InfiniteChance-Source", req.Source)
	}

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return ChatResult{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return ChatResult{}, fmt.Errorf("read gateway response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return ChatResult{}, fmt.Errorf("gateway %d: %s", resp.StatusCode, errorSummary(raw))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return ChatResult{}, fmt.Errorf("gateway response not JSON: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return ChatResult{}, fmt.Errorf("gateway delivered no choices")
	}
	content := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if content == "" {
		return ChatResult{}, fmt.Errorf("gateway delivered an empty message")
	}
	return ChatResult{Content: content}, nil
}

// maxResponseBytes caps how much of the answer body is read: prompts are
// text, tens of megabytes would mean something is wrong upstream.
const maxResponseBytes = 4 << 20

// maxSSELineBytes caps one SSE line: a delta frame is tiny, anything bigger
// means the peer is not the streaming shape we agreed on.
const maxSSELineBytes = 1 << 20

var (
	sseDataPrefix = []byte("data:")
	sseDoneMarker = []byte("[DONE]")
)

// chatBody assembles the POST /v1/chat/completions request body both call
// shapes share (63 号票:tools/tool_choice 按需并入,空值字段不出现在
// JSON 里,不带工具的请求与升级前逐字节一致)。
func chatBody(req ChatRequest, stream bool) chatCompletionBody {
	return chatCompletionBody{
		Model:      req.Model,
		Messages:   conversationOf(req),
		Stream:     stream,
		Tools:      req.Tools,
		ToolChoice: req.ToolChoice,
	}
}

// chatCompletionBody is the relay request body. Everything beyond these
// fields is out of scope for canvas callers (relay passes the rest of the
// raw body through untouched — we just never send any).
type chatCompletionBody struct {
	Model      string            `json:"model"`
	Messages   []chatMessage     `json:"messages"`
	Stream     bool              `json:"stream"`
	Tools      []json.RawMessage `json:"tools,omitempty"`
	ToolChoice string            `json:"tool_choice,omitempty"`
}

// StreamChat is the plain-text streaming variant (31 号票):deltas only, a
// round that produced no text at all is an error. The Agent tool loop (63
// 号票) goes through StreamChatTurn instead, which tolerates tool-only
// rounds.
func (c *Client) StreamChat(ctx context.Context, req ChatRequest, onDelta func(string)) error {
	turn, err := c.streamTurn(ctx, req, onDelta)
	if err != nil {
		return err
	}
	if turn.Content == "" && len(turn.ToolCalls) == 0 {
		return fmt.Errorf("gateway delivered an empty message")
	}
	return nil
}

// StreamTurn is one streamed completion, aggregated (63 号票):Content 是
// 全部文本增量的拼接,ToolCalls 是 tool_calls delta 按 index 键控聚合出的
// 调用清单(id/function.name/function.arguments 分片各自拼接),FinishReason
// 取流中最后一次非空的 choices[0].finish_reason。
type StreamTurn struct {
	Content      string
	ToolCalls    []ToolCall
	FinishReason string
}

// StreamChatTurn streams one completion and hands text deltas to onDelta as
// they arrive, aggregating tool_calls deltas along the way (63 号票 Agent
// 工具循环的每一轮):a gateway rejection before the stream opens is the same
// error GenerateChat returns; a failure after frames have flowed (error
// frame, EOF without [DONE]) is an error too — 已交的增量不作成功,由调用方
// 决定去留。
func (c *Client) StreamChatTurn(ctx context.Context, req ChatRequest, onDelta func(string)) (StreamTurn, error) {
	return c.streamTurn(ctx, req, onDelta)
}

// streamTurn is the shared streaming implementation: it opens the relay
// stream, relays text deltas, aggregates tool_calls deltas by index and
// captures the finish reason.
func (c *Client) streamTurn(ctx context.Context, req ChatRequest, onDelta func(string)) (StreamTurn, error) {
	payload, err := json.Marshal(chatBody(req, true))
	if err != nil {
		return StreamTurn{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.BaseURL+"/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return StreamTurn{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.Key)
	if req.Source != "" {
		httpReq.Header.Set("X-InfiniteChance-Source", req.Source)
	}

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return StreamTurn{}, err
	}
	defer resp.Body.Close()

	// 流打开前的失败照同步路径回答:网关已按聊天轨完成记账/退款。
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		if err != nil {
			return StreamTurn{}, fmt.Errorf("read gateway response: %w", err)
		}
		return StreamTurn{}, fmt.Errorf("gateway %d: %s", resp.StatusCode, errorSummary(raw))
	}

	turn := StreamTurn{}
	content := strings.Builder{}
	// tool_calls delta 按 index 键控聚合(63 号票):id/function.name 只在
	// 首片出现,arguments 逐片追加;聚合完成后按 index 序落位。
	type callAggregate struct {
		id, name string
		args     strings.Builder
	}
	calls := map[int]*callAggregate{}
	callOrder := []int{}

	scanner := bufio.NewScanner(io.LimitReader(resp.Body, maxResponseBytes))
	scanner.Buffer(make([]byte, 0, 64*1024), maxSSELineBytes)
	for scanner.Scan() {
		line := scanner.Bytes()
		if !bytes.HasPrefix(line, sseDataPrefix) {
			continue // 注释保活行与空行:无负载
		}
		payloadLine := bytes.TrimPrefix(bytes.TrimPrefix(line, sseDataPrefix), []byte(" "))
		if bytes.Equal(payloadLine, sseDoneMarker) {
			for _, i := range callOrder {
				agg := calls[i]
				turn.ToolCalls = append(turn.ToolCalls, ToolCall{ID: agg.id, Name: agg.name, Arguments: agg.args.String()})
			}
			turn.Content = content.String()
			return turn, nil
		}
		// 网关透传上游的任何 JSON 形状:只认 delta 与 error,其余跳过
		// (与同步路径只读 choices[0] 的宽容一致)。
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string         `json:"content"`
					ToolCalls []toolCallWire `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(payloadLine, &chunk) != nil {
			continue
		}
		if chunk.Error.Message != "" {
			return StreamTurn{}, fmt.Errorf("gateway stream error: %s", chunk.Error.Message)
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		if choice.FinishReason != "" {
			turn.FinishReason = choice.FinishReason
		}
		if choice.Delta.Content != "" {
			content.WriteString(choice.Delta.Content)
			onDelta(choice.Delta.Content)
		}
		for _, tc := range choice.Delta.ToolCalls {
			agg, ok := calls[tc.Index]
			if !ok {
				agg = &callAggregate{}
				calls[tc.Index] = agg
				callOrder = append(callOrder, tc.Index)
			}
			if tc.ID != "" {
				agg.id = tc.ID
			}
			if tc.Function.Name != "" {
				agg.name = tc.Function.Name
			}
			agg.args.WriteString(tc.Function.Arguments)
		}
	}
	if err := scanner.Err(); err != nil {
		return StreamTurn{}, fmt.Errorf("read gateway stream: %w", err)
	}
	// EOF 而无 [DONE]:上游断流,已交的增量不作成功。
	return StreamTurn{}, fmt.Errorf("gateway stream ended without a completion marker")
}

type chatMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
	// 63 号票循环中间帧:assistant 轮的发起调用 / tool 轮的结果回填;
	// 普通轮两字段为空、序列化后不出现。
	ToolCalls  []toolCallWire `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

// toolCallWire is the wire shape of one tool call (63 号票):请求侧按
// OpenAI assistant.tool_calls 形状带 id/type/function;流式 delta 侧则多
// 一个 index 作聚合键(Index 入 JSON 由 omitempty 折叠,请求侧恒 0 不出)。
type toolCallWire struct {
	Index    int    `json:"index,omitempty"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// chatMessageOf maps one conversation turn onto the wire, carrying the
// 63 号票 tool frames (assistant tool_calls / tool result) alongside the
// plain-text and multimodal shapes.
func chatMessageOf(m ChatMessage) chatMessage {
	wire := chatMessage{Role: m.Role, Content: messageContent(m)}
	if len(m.ToolCalls) > 0 {
		calls := make([]toolCallWire, 0, len(m.ToolCalls))
		for _, tc := range m.ToolCalls {
			var fn toolCallWire
			fn.ID = tc.ID
			fn.Type = "function"
			fn.Function.Name = tc.Name
			fn.Function.Arguments = tc.Arguments
			calls = append(calls, fn)
		}
		wire.ToolCalls = calls
	}
	wire.ToolCallID = m.ToolCallID
	return wire
}

// userMessage builds the single user message a generation sends: plain text
// for the template-driven shape, or a multimodal part array — media ahead of
// text — when a video or image reference rides along.
func userMessage(req ChatRequest) chatMessage {
	if req.VideoURL == "" && req.ImageURL == "" {
		return chatMessage{Role: "user", Content: req.Content}
	}
	parts := make([]ChatContentPart, 0, 2)
	if req.VideoURL != "" {
		parts = append(parts, ChatContentPart{Type: "video_url", VideoURL: &mediaURLPart{URL: req.VideoURL}})
	}
	if req.ImageURL != "" {
		parts = append(parts, ChatContentPart{Type: "image_url", ImageURL: &mediaURLPart{URL: req.ImageURL}})
	}
	parts = append(parts, ChatContentPart{Type: "text", Text: req.Content})
	return chatMessage{Role: "user", Content: parts}
}

// chatContentPart is one multimodal content part; the pointer shape keeps
// the part kinds on a single struct without emitting empty fields.
// Exported as ChatContentPart for handlers that assemble conversations
// (32 号票 Agent 历史媒体轮).
type ChatContentPart struct {
	Type     string        `json:"type"`
	Text     string        `json:"text,omitempty"`
	VideoURL *mediaURLPart `json:"video_url,omitempty"`
	ImageURL *mediaURLPart `json:"image_url,omitempty"`
}

// MediaKind names the two multimodal input kinds a conversation turn may
// carry (32 号票);the values match the asset kinds on the wire.
const (
	MediaKindImage = "image"
	MediaKindVideo = "video"
)

// MediaPart is one resolved media reference riding in a conversation turn:
// the handler resolves the editor's reference to the LLM-reachable address,
// MediaTextParts turns it into the wire part.
type MediaPart struct {
	Kind string // MediaKindImage | MediaKindVideo
	URL  string
}

// MediaTextParts builds a multimodal content array, media ahead of text
// (17 号票形状):each media part keeps the caller's order, the instruction
// text closes the array as a text part.
func MediaTextParts(media []MediaPart, text string) []ChatContentPart {
	parts := make([]ChatContentPart, 0, len(media)+1)
	for _, m := range media {
		if m.Kind == MediaKindVideo {
			parts = append(parts, ChatContentPart{Type: "video_url", VideoURL: &mediaURLPart{URL: m.URL}})
			continue
		}
		parts = append(parts, ChatContentPart{Type: "image_url", ImageURL: &mediaURLPart{URL: m.URL}})
	}
	parts = append(parts, ChatContentPart{Type: "text", Text: text})
	return parts
}

// messageContent picks the wire shape of one conversation turn: the part
// array when the turn carries media, the plain string otherwise.
func messageContent(m ChatMessage) any {
	if len(m.Parts) > 0 {
		return m.Parts
	}
	return m.Content
}

// mediaURLPart is the {url} payload video_url and image_url parts share.
type mediaURLPart struct {
	URL string `json:"url"`
}

// errorSummary picks the OpenAI error message out of a gateway failure body,
// falling back to a truncated raw body so the editor always has a reason.
func errorSummary(body []byte) string {
	var parsed struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil && parsed.Error.Message != "" {
		return parsed.Error.Message
	}
	return truncateRunes(string(body), 200)
}

// truncateRunes cuts to at most n runes so a Chinese error message can never
// be split mid-character into invalid UTF-8(与 relay 的摘要截断同规).
func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}
