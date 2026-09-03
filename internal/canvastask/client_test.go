package canvastask_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gachal/InfiniteChance/internal/canvastask"
)

// fakeGateway answers /v1/images/generations with the canned status and body
// while recording what the canvas server sent.
type fakeGateway struct {
	status int
	body   string

	path        string
	auth        string
	source      string
	requestBody map[string]any
}

func (g *fakeGateway) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		g.path = r.URL.Path
		g.auth = r.Header.Get("Authorization")
		g.source = r.Header.Get("X-InfiniteChance-Source")
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		g.requestBody = body
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(g.status)
		w.Write([]byte(g.body))
	}
}

func newGatewayServer(t *testing.T, g *fakeGateway) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(g.handler())
	t.Cleanup(s.Close)
	return s
}

func TestClientGenerateImageSendsServiceKeyAndSource(t *testing.T) {
	g := &fakeGateway{status: http.StatusOK, body: `{"created":1,"data":[{"url":"https://img.example/cat.png"}]}`}
	server := newGatewayServer(t, g)

	client := canvastask.NewClient(server.URL, "sk-service-key")
	res, err := client.GenerateImage(context.Background(), canvastask.ImageRequest{
		Model: "img-m", Prompt: "一只在月光下奔跑的猫", Size: "1024x1024",
		Source: "canvas=7 task=ct_abc node=image-1-1",
	})
	if err != nil {
		t.Fatalf("GenerateImage: %v", err)
	}
	if res.URL != "https://img.example/cat.png" {
		t.Errorf("url = %q, want the vendor's url", res.URL)
	}
	if g.path != "/v1/images/generations" {
		t.Errorf("path = %q, want /v1/images/generations", g.path)
	}
	if g.auth != "Bearer sk-service-key" {
		t.Errorf("auth = %q, want the service key", g.auth)
	}
	if g.source != "canvas=7 task=ct_abc node=image-1-1" {
		t.Errorf("source header = %q, want the canvas origin mark", g.source)
	}
	if g.requestBody["model"] != "img-m" || g.requestBody["prompt"] != "一只在月光下奔跑的猫" ||
		g.requestBody["size"] != "1024x1024" || g.requestBody["n"] != float64(1) {
		t.Errorf("request body = %v, want {model, prompt, size, n:1}", g.requestBody)
	}
}

func TestClientGenerateImageOmitsEmptySize(t *testing.T) {
	g := &fakeGateway{status: http.StatusOK, body: `{"data":[{"url":"https://img.example/x.png"}]}`}
	server := newGatewayServer(t, g)

	client := canvastask.NewClient(server.URL, "sk-service-key")
	if _, err := client.GenerateImage(context.Background(), canvastask.ImageRequest{
		Model: "img-m", Prompt: "星空", Source: "canvas=1",
	}); err != nil {
		t.Fatalf("GenerateImage: %v", err)
	}
	if _, ok := g.requestBody["size"]; ok {
		t.Errorf("size = %v, want the field omitted when unset", g.requestBody["size"])
	}
}

func TestClientGenerateImageWrapsBase64AsDataURI(t *testing.T) {
	// 真 PNG 魔数:客户端按字节嗅探 data URI 的 mime。
	png := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, make([]byte, 16)...)
	b64 := base64.StdEncoding.EncodeToString(png)
	g := &fakeGateway{status: http.StatusOK, body: `{"data":[{"b64_json":"` + b64 + `"}]}`}
	server := newGatewayServer(t, g)

	client := canvastask.NewClient(server.URL, "sk-service-key")
	res, err := client.GenerateImage(context.Background(), canvastask.ImageRequest{
		Model: "img-m", Prompt: "星空", Source: "canvas=1",
	})
	if err != nil {
		t.Fatalf("GenerateImage: %v", err)
	}
	if !strings.HasPrefix(res.URL, "data:image/png;base64,") {
		t.Errorf("url prefix = %q…, want a png data URI", res.URL[:min(40, len(res.URL))])
	}
	payload, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(res.URL, "data:image/png;base64,"))
	if err != nil || string(payload) != string(png) {
		t.Errorf("payload round-trip failed: %v", err)
	}
}

func TestClientGenerateImageSurfacesGatewayErrors(t *testing.T) {
	g := &fakeGateway{status: http.StatusTooManyRequests, body: `{"error":{"message":"insufficient quota","type":"insufficient_quota","code":"insufficient_quota"}}`}
	server := newGatewayServer(t, g)

	client := canvastask.NewClient(server.URL, "sk-service-key")
	_, err := client.GenerateImage(context.Background(), canvastask.ImageRequest{
		Model: "img-m", Prompt: "星空", Source: "canvas=1",
	})
	if err == nil {
		t.Fatalf("GenerateImage = nil error, want the gateway failure surfaced")
	}
	if !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "insufficient quota") {
		t.Errorf("error = %v, want status and the vendor's message", err)
	}
}

func TestClientGenerateImageRejectsEmptyDelivery(t *testing.T) {
	g := &fakeGateway{status: http.StatusOK, body: `{"created":1,"data":[]}`}
	server := newGatewayServer(t, g)

	client := canvastask.NewClient(server.URL, "sk-service-key")
	if _, err := client.GenerateImage(context.Background(), canvastask.ImageRequest{
		Model: "img-m", Prompt: "星空", Source: "canvas=1",
	}); err == nil || !strings.Contains(err.Error(), "no images") {
		t.Fatalf("error = %v, want an empty-delivery failure", err)
	}
}

func TestClientGenerateImageRejectsOversizedBase64(t *testing.T) {
	huge := strings.Repeat("QUFB", 4<<20) // ~16MB 的 base64,超出内联上限
	g := &fakeGateway{status: http.StatusOK, body: `{"data":[{"b64_json":"` + huge + `"}]}`}
	server := newGatewayServer(t, g)

	client := canvastask.NewClient(server.URL, "sk-service-key")
	if _, err := client.GenerateImage(context.Background(), canvastask.ImageRequest{
		Model: "img-m", Prompt: "星空", Source: "canvas=1",
	}); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("error = %v, want an oversized-payload failure", err)
	}
}

func TestClientSubmitVideoSendsContractBody(t *testing.T) {
	g := &fakeGateway{status: http.StatusOK, body: `{"task_id":"vt_abc123"}`}
	server := newGatewayServer(t, g)

	client := canvastask.NewClient(server.URL, "sk-service-key")
	res, err := client.SubmitVideo(context.Background(), canvastask.VideoRequest{
		Model: "vid-m", Prompt: "镜头缓缓推进", Seconds: 5,
		Image: "https://img.example/cat.png",
		Source: "canvas=7 task=ct_abc node=video-1-1",
	})
	if err != nil {
		t.Fatalf("SubmitVideo: %v", err)
	}
	if res.TaskID != "vt_abc123" {
		t.Fatalf("task id = %q, want the gateway's handle", res.TaskID)
	}
	if g.path != "/v1/videos/generations" {
		t.Errorf("path = %q, want the video submit endpoint", g.path)
	}
	if g.auth != "Bearer sk-service-key" || g.source != "canvas=7 task=ct_abc node=video-1-1" {
		t.Errorf("headers = %q / %q, want the service key and the canvas origin", g.auth, g.source)
	}
	if g.requestBody["model"] != "vid-m" || g.requestBody["prompt"] != "镜头缓缓推进" ||
		g.requestBody["seconds"] != float64(5) || g.requestBody["image"] != "https://img.example/cat.png" {
		t.Errorf("body = %v, want the image-to-video facts", g.requestBody)
	}
}

func TestClientSubmitVideoRejectsMissingTaskID(t *testing.T) {
	g := &fakeGateway{status: http.StatusOK, body: `{"created":true}`}
	server := newGatewayServer(t, g)
	client := canvastask.NewClient(server.URL, "sk-service-key")
	if _, err := client.SubmitVideo(context.Background(), canvastask.VideoRequest{Model: "vid-m", Prompt: "p", Seconds: 5}); err == nil {
		t.Fatalf("submit without task_id = nil error, want a protocol error")
	}
}

func TestClientPollVideoReadsStatusAndFacts(t *testing.T) {
	g := &fakeGateway{status: http.StatusOK,
		body: `{"task_id":"vt_abc123","status":"succeeded","video_url":"https://vid.example/cat.mp4"}`}
	server := newGatewayServer(t, g)
	client := canvastask.NewClient(server.URL, "sk-service-key")

	poll, err := client.PollVideo(context.Background(), "vt_abc123")
	if err != nil {
		t.Fatalf("PollVideo: %v", err)
	}
	if poll.Status != "succeeded" || poll.VideoURL != "https://vid.example/cat.mp4" {
		t.Fatalf("poll = %+v, want the terminal facts", poll)
	}
	if g.path != "/v1/videos/tasks/vt_abc123" {
		t.Errorf("path = %q, want the poll endpoint", g.path)
	}

	// 失败任务的错误消息透传给任务行。
	g.body = `{"task_id":"vt_abc123","status":"failed","error":{"message":"content policy"}}`
	poll, err = client.PollVideo(context.Background(), "vt_abc123")
	if err != nil {
		t.Fatalf("PollVideo failed task: %v", err)
	}
	if poll.Status != "failed" || poll.Error != "content policy" {
		t.Errorf("poll = %+v, want the failure reason", poll)
	}
}

func TestClientPollVideoSurfacesGatewayErrors(t *testing.T) {
	g := &fakeGateway{status: http.StatusBadGateway, body: `{"error":{"message":"upstream down"}}`}
	server := newGatewayServer(t, g)
	client := canvastask.NewClient(server.URL, "sk-service-key")
	if _, err := client.PollVideo(context.Background(), "vt_abc123"); err == nil || !strings.Contains(err.Error(), "upstream down") {
		t.Fatalf("poll on 502 = %v, want the gateway reason surfaced", err)
	}
}

func TestClientCancelVideoPostsCancelEndpoint(t *testing.T) {
	g := &fakeGateway{status: http.StatusOK, body: `{"task_id":"vt_abc123","status":"canceled"}`}
	server := newGatewayServer(t, g)
	client := canvastask.NewClient(server.URL, "sk-service-key")

	if err := client.CancelVideo(context.Background(), "vt_abc123"); err != nil {
		t.Fatalf("CancelVideo: %v", err)
	}
	if g.path != "/v1/videos/tasks/vt_abc123/cancel" {
		t.Errorf("path = %q, want the cancel endpoint", g.path)
	}

	// 非 2xx 是错误:取消路径上所有调用方都按尽力而为处理,这里只保证报告。
	g.status = http.StatusInternalServerError
	g.body = `{"error":{"message":"internal"}}`
	if err := client.CancelVideo(context.Background(), "vt_abc123"); err == nil {
		t.Fatalf("cancel on 500 = nil error, want one")
	}
}
