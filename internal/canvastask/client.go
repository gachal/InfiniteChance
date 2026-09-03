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

// ---- 网关视频异步契约(08 号票;12 号票的图生视频从这里走)----

// VideoRequest is one image-to-video generation the worker submits. Image is
// the reference picture's http(s) address; empty means plain text-to-video.
type VideoRequest struct {
	Model   string
	Prompt  string
	Seconds int64
	Image   string
	Source  string
}

// VideoSubmitResult carries the gateway's task handle (vt_…): the worker
// persists it on the task row and polls the gateway with it.
type VideoSubmitResult struct {
	TaskID string
}

// VideoPoll is one gateway task poll. Status is the gateway's external
// five-state machine (queued/running/succeeded/failed/canceled); VideoURL
// and Error carry the terminal facts — a succeeded poll without a URL is a
// protocol violation the worker reports as a failure.
type VideoPoll struct {
	Status   string
	VideoURL string
	Error    string
}

// SubmitVideo calls POST /v1/videos/generations and returns the task handle.
// A gateway rejection (OpenAI error object) is an error carrying the reason
// for the task row — the gateway refunds its own pre-deduction on any
// rejected submit, so nothing is owed on this path.
func (c *Client) SubmitVideo(ctx context.Context, req VideoRequest) (VideoSubmitResult, error) {
	body := struct {
		Model   string `json:"model"`
		Prompt  string `json:"prompt"`
		Seconds int64  `json:"seconds"`
		Image   string `json:"image,omitempty"`
	}{Model: req.Model, Prompt: req.Prompt, Seconds: req.Seconds, Image: req.Image}
	payload, err := json.Marshal(body)
	if err != nil {
		return VideoSubmitResult{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.BaseURL+"/v1/videos/generations", bytes.NewReader(payload))
	if err != nil {
		return VideoSubmitResult{}, err
	}
	c.setHeaders(httpReq, req.Source)

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return VideoSubmitResult{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return VideoSubmitResult{}, fmt.Errorf("read gateway response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return VideoSubmitResult{}, fmt.Errorf("gateway %d: %s", resp.StatusCode, errorSummary(raw))
	}

	var parsed struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return VideoSubmitResult{}, fmt.Errorf("gateway response not JSON: %w", err)
	}
	if strings.TrimSpace(parsed.TaskID) == "" {
		return VideoSubmitResult{}, fmt.Errorf("gateway accepted the submit but returned no task_id")
	}
	return VideoSubmitResult{TaskID: strings.TrimSpace(parsed.TaskID)}, nil
}

// PollVideo calls GET /v1/videos/tasks/{id}. Transient gateway failures are
// errors — the worker keeps polling; only the gateway's own status words
// advance the task.
func (c *Client) PollVideo(ctx context.Context, taskID string) (VideoPoll, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.BaseURL+"/v1/videos/tasks/"+taskID, nil)
	if err != nil {
		return VideoPoll{}, err
	}
	c.setHeaders(httpReq, "")

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return VideoPoll{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return VideoPoll{}, fmt.Errorf("read gateway response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return VideoPoll{}, fmt.Errorf("gateway %d: %s", resp.StatusCode, errorSummary(raw))
	}

	var parsed struct {
		Status   string `json:"status"`
		VideoURL string `json:"video_url"`
		Error    *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return VideoPoll{}, fmt.Errorf("gateway response not JSON: %w", err)
	}
	status := strings.TrimSpace(parsed.Status)
	if status == "" {
		return VideoPoll{}, fmt.Errorf("gateway task body carries no status")
	}
	poll := VideoPoll{Status: status, VideoURL: strings.TrimSpace(parsed.VideoURL)}
	if parsed.Error != nil {
		poll.Error = strings.TrimSpace(parsed.Error.Message)
	}
	return poll, nil
}

// CancelVideo calls POST /v1/videos/tasks/{id}/cancel. The gateway cancels
// active tasks locally and refunds regardless of whether the vendor itself
// stopped, so a 2xx closes the books; any other answer is reported to the
// caller (cancel is best-effort on every path that invokes it).
func (c *Client) CancelVideo(ctx context.Context, taskID string) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.BaseURL+"/v1/videos/tasks/"+taskID+"/cancel", nil)
	if err != nil {
		return err
	}
	c.setHeaders(httpReq, "")

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read gateway response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("gateway %d: %s", resp.StatusCode, errorSummary(raw))
	}
	return nil
}

func (c *Client) setHeaders(req *http.Request, source string) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Key)
	if source != "" {
		req.Header.Set("X-InfiniteChance-Source", source)
	}
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
