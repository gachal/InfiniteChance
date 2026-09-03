// Package promptgen implements the canvas's generate-prompt action (11 号票):
// canvas/server fills the admin-maintained template with the user's topic and
// relays it through the gateway's chat surface with the service-level key, so
// the spent tokens bill and land in the gateway's usage log like any direct
// chat call. The generated text is returned to the editor, which writes it
// into the current node or a new prompt node — the graph is never touched
// server-side, autosave stays the only writer.
package promptgen

import (
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

// ChatRequest is one prompt generation: the fully rendered message (template
// with the topic filled in) and the public chat model to run it on.
// Source is the canvas origin mark (X-InfiniteChance-Source 值).
type ChatRequest struct {
	Model   string
	Content string
	Source  string
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
	body := struct {
		Model    string        `json:"model"`
		Messages []chatMessage `json:"messages"`
		Stream   bool          `json:"stream"`
	}{
		Model:    req.Model,
		Messages: []chatMessage{{Role: "user", Content: req.Content}},
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

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
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
	summary := string(body)
	if len(summary) > 200 {
		summary = summary[:200]
	}
	return summary
}
