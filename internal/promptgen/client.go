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

// ChatMessage is one plain-text conversation turn (29 号票 Agent 多轮会话):
// role ∈ user|assistant,content 即该轮文本。
type ChatMessage struct {
	Role    string
	Content string
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
}

// ChatResult is the model's answer, whitespace-trimmed.
type ChatResult struct {
	Content string
}

// GenerateChat calls POST /v1/chat/completions (non-streaming — the editor
// waits for the whole text anyway) and returns choices[0].message.content.
// A gateway rejection (OpenAI error object) or an empty answer is an error
// carrying the reason back to the editor.
func (c *Client) GenerateChat(ctx context.Context, req ChatRequest) (ChatResult, error) {
	messages := make([]chatMessage, 0, len(req.Conversation)+1)
	if len(req.Conversation) > 0 {
		for _, m := range req.Conversation {
			messages = append(messages, chatMessage{Role: m.Role, Content: m.Content})
		}
	} else {
		messages = append(messages, userMessage(req))
	}
	body := struct {
		Model    string        `json:"model"`
		Messages []chatMessage `json:"messages"`
		Stream   bool          `json:"stream"`
	}{
		Model:    req.Model,
		Messages: messages,
		Stream:   false,
	}
	payload, err := json.Marshal(body)
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

// StreamChat calls POST /v1/chat/completions with stream=true and hands each
// delta (choices[0].delta.content) to onDelta as it arrives (31 号票 Agent
// 流式):the gateway relays upstream frames one by one (usage-only chunks are
// swallowed there — this client never asks for include_usage), so the text
// lands in the editor while the model is still writing. Accumulating the
// full text is the caller's job. A gateway rejection before the stream opens
// is the same error GenerateChat returns; a failure after frames have flowed
// (error frame, EOF without [DONE], zero deltas) is an error too — the
// caller decides what to keep of the partial text.
func (c *Client) StreamChat(ctx context.Context, req ChatRequest, onDelta func(string)) error {
	messages := make([]chatMessage, 0, len(req.Conversation)+1)
	if len(req.Conversation) > 0 {
		for _, m := range req.Conversation {
			messages = append(messages, chatMessage{Role: m.Role, Content: m.Content})
		}
	} else {
		messages = append(messages, userMessage(req))
	}
	payload, err := json.Marshal(struct {
		Model    string        `json:"model"`
		Messages []chatMessage `json:"messages"`
		Stream   bool          `json:"stream"`
	}{
		Model:    req.Model,
		Messages: messages,
		Stream:   true,
	})
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.BaseURL+"/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.Key)
	if req.Source != "" {
		httpReq.Header.Set("X-InfiniteChance-Source", req.Source)
	}

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// 流打开前的失败照同步路径回答:网关已按聊天轨完成记账/退款。
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		if err != nil {
			return fmt.Errorf("read gateway response: %w", err)
		}
		return fmt.Errorf("gateway %d: %s", resp.StatusCode, errorSummary(raw))
	}

	sawDelta := false
	scanner := bufio.NewScanner(io.LimitReader(resp.Body, maxResponseBytes))
	scanner.Buffer(make([]byte, 0, 64*1024), maxSSELineBytes)
	for scanner.Scan() {
		line := scanner.Bytes()
		if !bytes.HasPrefix(line, sseDataPrefix) {
			continue // 注释保活行与空行:无负载
		}
		payloadLine := bytes.TrimPrefix(bytes.TrimPrefix(line, sseDataPrefix), []byte(" "))
		if bytes.Equal(payloadLine, sseDoneMarker) {
			if !sawDelta {
				return fmt.Errorf("gateway delivered an empty message")
			}
			return nil
		}
		// 网关透传上游的任何 JSON 形状:只认 delta 与 error,其余跳过
		// (与同步路径只读 choices[0] 的宽容一致)。
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(payloadLine, &chunk) != nil {
			continue
		}
		if chunk.Error.Message != "" {
			return fmt.Errorf("gateway stream error: %s", chunk.Error.Message)
		}
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			sawDelta = true
			onDelta(chunk.Choices[0].Delta.Content)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read gateway stream: %w", err)
	}
	// EOF 而无 [DONE]:上游断流,已交的增量不作成功。
	return fmt.Errorf("gateway stream ended without a completion marker")
}

type chatMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// userMessage builds the single user message a generation sends: plain text
// for the template-driven shape, or a multimodal part array — media ahead of
// text — when a video or image reference rides along.
func userMessage(req ChatRequest) chatMessage {
	if req.VideoURL == "" && req.ImageURL == "" {
		return chatMessage{Role: "user", Content: req.Content}
	}
	parts := make([]chatContentPart, 0, 2)
	if req.VideoURL != "" {
		parts = append(parts, chatContentPart{Type: "video_url", VideoURL: &mediaURLPart{URL: req.VideoURL}})
	}
	if req.ImageURL != "" {
		parts = append(parts, chatContentPart{Type: "image_url", ImageURL: &mediaURLPart{URL: req.ImageURL}})
	}
	parts = append(parts, chatContentPart{Type: "text", Text: req.Content})
	return chatMessage{Role: "user", Content: parts}
}

// chatContentPart is one multimodal content part; the pointer shape keeps
// the part kinds on a single struct without emitting empty fields.
type chatContentPart struct {
	Type     string        `json:"type"`
	Text     string        `json:"text,omitempty"`
	VideoURL *mediaURLPart `json:"video_url,omitempty"`
	ImageURL *mediaURLPart `json:"image_url,omitempty"`
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
