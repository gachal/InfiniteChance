package canvastask

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"path"
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

// maxEditRefBytes caps one fetched reference image (21 号票的图生图):
// 网关 /v1 整体请求体上限 32MiB,几张参考图连同表单余量必须落在其内;
// 8MiB 与交付图(b64 上限)同量级,超出在 worker 侧就报可读的原因。
const maxEditRefBytes = 8 << 20

// maxEditRefsTotalBytes caps the references' combined size: four 8MiB
// singles are individually legal but together bust the gateway's 32MiB
// request cap — fail here with a readable reason instead of a late 413
// a retry could never get past.
const maxEditRefsTotalBytes = 24 << 20

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
	return decodeImageDelivery(raw)
}

// decodeImageDelivery reads the OpenAI-shaped images body both relay
// endpoints answer with: the first url entry wins, a b64 entry wraps into a
// data: URI, an empty delivery is an error for the task row.
func decodeImageDelivery(raw []byte) (ImageResult, error) {
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

// ---- 图生图(21 号票):POST /v1/images/edits 的 multipart 契约 ----

// EditRequest is one image-to-image generation the worker submits. Images
// carries the already-resolved http(s) reference addresses; the client
// fetches each one and uploads the bytes as multipart file parts — the
// gateway's edits contract is a rebuilt multipart form, not a URL list.
type EditRequest struct {
	Model  string
	Prompt string
	Size   string
	Images []string
	Source string
}

// EditImage calls POST /v1/images/edits (n=1) with the reference images as
// file parts — 单张分节名 image,多张 image[](OpenAI 约定,中转按渠道原样
// 重建,VOD adaptor 对每个文件分节都转 Base64 参考). A reference that
// cannot be fetched or busts the size cap is an error carrying the reason
// for the task row.
func (c *Client) EditImage(ctx context.Context, req EditRequest) (ImageResult, error) {
	if len(req.Images) == 0 {
		return ImageResult{}, fmt.Errorf("image-to-image needs at least one reference image")
	}
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	fields := []struct{ name, value string }{
		{"model", req.Model},
		{"prompt", req.Prompt},
		{"n", "1"},
	}
	if req.Size != "" {
		fields = append(fields, struct{ name, value string }{"size", req.Size})
	}
	for _, f := range fields {
		if err := w.WriteField(f.name, f.value); err != nil {
			return ImageResult{}, err
		}
	}
	total := 0
	for i, ref := range req.Images {
		data, contentType, err := c.fetchImageRef(ctx, ref)
		if err != nil {
			return ImageResult{}, fmt.Errorf("参考图 %d: %w", i+1, err)
		}
		total += len(data)
		if total > maxEditRefsTotalBytes {
			return ImageResult{}, fmt.Errorf("参考图合计超过 %d MiB 上限", maxEditRefsTotalBytes>>20)
		}
		name := "image"
		if len(req.Images) > 1 {
			name = "image[]"
		}
		part, err := w.CreatePart(filePartHeader(name, refFileName(ref, i, contentType), contentType))
		if err != nil {
			return ImageResult{}, err
		}
		if _, err := part.Write(data); err != nil {
			return ImageResult{}, err
		}
	}
	if err := w.Close(); err != nil {
		return ImageResult{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.BaseURL+"/v1/images/edits", body)
	if err != nil {
		return ImageResult{}, err
	}
	httpReq.Header.Set("Content-Type", w.FormDataContentType())
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
	return decodeImageDelivery(raw)
}

// fetchImageRef downloads one reference image: a vendor original or our own
// stored object, both plain http(s) GETs with no auth. The byte cap keeps a
// fat reference from blowing the gateway's request limit; the content type
// is sanitized to an image mime (fallback png) so the vendor sees a sane
// part header.
func (c *Client) fetchImageRef(ctx context.Context, ref string) ([]byte, string, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, ref, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, "", fmt.Errorf("拉取失败(gateway %d)", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxEditRefBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) > maxEditRefBytes {
		return nil, "", fmt.Errorf("超过 %d MiB 上限", maxEditRefBytes>>20)
	}
	return data, sanitizeImageMime(resp.Header.Get("Content-Type")), nil
}

// sanitizeImageMime reduces a Content-Type header to a bare image mime the
// multipart part header can carry; anything non-image (or empty) falls back
// to png, the vendor's sniffing has the final word anyway.
func sanitizeImageMime(raw string) string {
	mime := strings.TrimSpace(strings.SplitN(raw, ";", 2)[0])
	if strings.HasPrefix(mime, "image/") {
		return mime
	}
	return "image/png"
}

// filePartHeader builds the multipart part header with an explicit file name
// and content type (CreateFormFile would force application/octet-stream).
func filePartHeader(field, filename, contentType string) textproto.MIMEHeader {
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition",
		fmt.Sprintf(`form-data; name="%s"; filename="%s"`, field, filename))
	h.Set("Content-Type", contentType)
	return h
}

// refFileName derives a part filename from the reference address's extension
// (query string stripped) when it carries a known image one, else from the
// sanitized mime — some vendors key their decoder on the filename.
func refFileName(ref string, i int, contentType string) string {
	base := path.Base(strings.SplitN(ref, "?", 2)[0])
	ext := strings.ToLower(path.Ext(base))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif":
	default:
		ext = mimeExt(contentType)
	}
	return fmt.Sprintf("reference-%d%s", i+1, ext)
}

// mimeExt maps the mimes sanitizeImageMime can emit onto file extensions.
func mimeExt(contentType string) string {
	switch contentType {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".png"
	}
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

// ---- 网关视频异步契约(08 号票;12 号票的图生视频从这里走)----

// VideoRequest is one video generation the worker submits (24 号票起支持
// 全模态参考)。Image 是首帧、LastImage 是尾帧(各自单个 http(s) 地址,
// 空串 = 无);References 是其余参考(参考图/参考视频/参考音频,kind
// 区分),对齐网关 /v1/videos 的契约扩展(25 号票定形,24 号票先上送 ——
// openai 形渠道对未知字段全量透传,上游不识别 4xx 透回)。Seconds 0 =
// 自动(不传,厂商缺省裁决);Size/Ratio 为档位串直传。
type VideoRequest struct {
	Model      string
	Prompt     string
	Seconds    int64
	Size       string
	Ratio      string
	Image      string
	LastImage  string
	References []VideoRefRequest
	Source     string
}

// VideoRefRequest is one generic reference on the wire: {url, kind},kind ∈
// image|video|audio。首尾帧不进这个列表(契约上是独立单串字段)。
type VideoRefRequest struct {
	URL  string `json:"url"`
	Kind string `json:"kind"`
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
// rejected submit, so nothing is owed on this path. The body carries only
// what the task row set: empty seconds/size/ratio/reference fields stay off
// the wire (seconds 缺省由网关补 5 计费、厂商缺省裁决时长).
func (c *Client) SubmitVideo(ctx context.Context, req VideoRequest) (VideoSubmitResult, error) {
	body := struct {
		Model      string            `json:"model"`
		Prompt     string            `json:"prompt"`
		Seconds    int64             `json:"seconds,omitempty"`
		Size       string            `json:"size,omitempty"`
		Ratio      string            `json:"ratio,omitempty"`
		Image      string            `json:"image,omitempty"`
		LastImage  string            `json:"last_image,omitempty"`
		References []VideoRefRequest `json:"references,omitempty"`
	}{
		Model: req.Model, Prompt: req.Prompt, Seconds: req.Seconds,
		Size: req.Size, Ratio: req.Ratio,
		Image: req.Image, LastImage: req.LastImage, References: req.References,
	}
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
