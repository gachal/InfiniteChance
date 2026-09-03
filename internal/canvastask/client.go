package canvastask

import (
	"bytes"
	"context"
	"encoding/base64"
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

// NewClient wires a client with a conservative default backstop timeout —
// the worker sets its own per-task deadline; this only guards a stuck call.
func NewClient(baseURL, key string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Key:     key,
		HTTP:    &http.Client{Timeout: 5 * time.Minute},
	}
}

// ImageRequest is one text-to-image generation the worker submits.
// Source is the canvas origin mark (X-InfiniteChance-Source 值).
type ImageRequest struct {
	Model  string
	Prompt string
	Size   string
	Source string
}

// ImageResult is one delivered artifact. URL is the vendor's http(s) URL,
// or a data: URI when the vendor answered base64 (对象存储转存为后续决策点).
type ImageResult struct {
	URL string
}

// maxB64Bytes caps inline base64 payloads: a data: URI is headed for the
// assets table and the node preview, both of which want megabytes, not
// tens of megabytes.
const maxB64Bytes = 9 << 20

// GenerateImage calls POST /v1/images/generations (n=1) and returns the
// first delivered image. A gateway rejection (OpenAI error object) or an
// empty delivery is an error carrying the reason for the task row.
func (c *Client) GenerateImage(ctx context.Context, req ImageRequest) (ImageResult, error) {
	body := struct {
		Model  string `json:"model"`
		Prompt string `json:"prompt"`
		N      int64  `json:"n"`
		Size   string `json:"size,omitempty"`
	}{Model: req.Model, Prompt: req.Prompt, N: 1, Size: req.Size}
	payload, err := json.Marshal(body)
	if err != nil {
		return ImageResult{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.BaseURL+"/v1/images/generations", bytes.NewReader(payload))
	if err != nil {
		return ImageResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.Key)
	if req.Source != "" {
		httpReq.Header.Set("X-InfiniteChance-Source", req.Source)
	}

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return ImageResult{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxB64Bytes*2))
	if err != nil {
		return ImageResult{}, fmt.Errorf("read gateway response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return ImageResult{}, fmt.Errorf("gateway %d: %s", resp.StatusCode, errorSummary(raw))
	}

	var parsed struct {
		Data []struct {
			URL     string `json:"url"`
			B64JSON string `json:"b64_json"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return ImageResult{}, fmt.Errorf("gateway response not JSON: %w", err)
	}
	for _, entry := range parsed.Data {
		if entry.URL != "" {
			return ImageResult{URL: entry.URL}, nil
		}
		if entry.B64JSON != "" {
			if len(entry.B64JSON) > maxB64Bytes {
				return ImageResult{}, fmt.Errorf("inline image too large (%d bytes of base64)", len(entry.B64JSON))
			}
			data, err := base64.StdEncoding.DecodeString(entry.B64JSON)
			if err != nil {
				continue // 坏的 b64 条目看下一条
			}
			return ImageResult{URL: dataURIFrom(data)}, nil
		}
	}
	return ImageResult{}, fmt.Errorf("gateway delivered no images")
}

// errorSummary picks the OpenAI error message out of a gateway failure body,
// falling back to a truncated raw body so the task row always has a reason.
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

// dataURIFrom wraps image bytes as a data: URI, sniffing the mime from the
// byte signature — data URIs carry no headers, the mime string is all the
// browser gets.
func dataURIFrom(data []byte) string {
	mime := "image/png"
	switch {
	case bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}):
		mime = "image/png"
	case bytes.HasPrefix(data, []byte{0xff, 0xd8, 0xff}):
		mime = "image/jpeg"
	case bytes.HasPrefix(data, []byte("GIF8")):
		mime = "image/gif"
	case bytes.HasPrefix(data, []byte("RIFF")) && len(data) > 12 && bytes.Equal(data[8:12], []byte("WEBP")):
		mime = "image/webp"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}
