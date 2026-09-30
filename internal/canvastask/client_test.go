package canvastask_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
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
		Image:  "https://img.example/cat.png",
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

// TestClientSubmitVideoSendsExtendedBody 覆盖 24 号票的扩展线体:尾帧、
// 参考列表与显式比例随提交上送;seconds 0(自动)不落线,维持厂商缺省。
func TestClientSubmitVideoSendsExtendedBody(t *testing.T) {
	g := &fakeGateway{status: http.StatusOK, body: `{"task_id":"vt_ext"}`}
	server := newGatewayServer(t, g)
	client := canvastask.NewClient(server.URL, "sk-service-key")

	if _, err := client.SubmitVideo(context.Background(), canvastask.VideoRequest{
		Model: "vid-m", Prompt: "p", Seconds: 0, Size: "720p", Ratio: "9:16",
		Image:     "https://img.example/first.png",
		LastImage: "https://img.example/last.png",
		References: []canvastask.VideoRefRequest{
			{URL: "https://img.example/style.png", Kind: "image"},
			{URL: "https://img.example/voice.mp3", Kind: "audio"},
		},
	}); err != nil {
		t.Fatalf("SubmitVideo: %v", err)
	}
	if g.requestBody["last_image"] != "https://img.example/last.png" ||
		g.requestBody["ratio"] != "9:16" || g.requestBody["size"] != "720p" {
		t.Errorf("body = %v, want last_image/ratio/size on the wire", g.requestBody)
	}
	refs, ok := g.requestBody["references"].([]any)
	if !ok || len(refs) != 2 {
		t.Fatalf("references = %v, want two entries", g.requestBody["references"])
	}
	if refs[0].(map[string]any)["kind"] != "image" || refs[1].(map[string]any)["kind"] != "audio" {
		t.Errorf("references = %v, want kinds carried", refs)
	}
	if _, present := g.requestBody["seconds"]; present {
		t.Errorf("seconds present on the wire for auto (0), want omitted")
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

// ---- 21 号票:EditImage 的 multipart 契约 ----

// refServer serves the reference bytes the canvas client fetches before
// building the edits multipart body.
func refServer(t *testing.T, png []byte) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(png)
	}))
	t.Cleanup(s.Close)
	return s
}

func TestClientEditImagePostsMultipartEdits(t *testing.T) {
	var gotPath, gotAuth, gotSource string
	var fields map[string][]string
	var files []struct {
		name    string
		file    string
		ctype   string
		content []byte
	}
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotSource = r.Header.Get("X-InfiniteChance-Source")
		r.ParseMultipartForm(32 << 20)
		fields = r.MultipartForm.Value
		for name, fhs := range r.MultipartForm.File {
			for _, fh := range fhs {
				f, _ := fh.Open()
				data, _ := io.ReadAll(f)
				f.Close()
				files = append(files, struct {
					name    string
					file    string
					ctype   string
					content []byte
				}{name, fh.Filename, fh.Header.Get("Content-Type"), data})
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"url":"https://img.example/edited.png"}]}`))
	}))
	t.Cleanup(gateway.Close)

	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x00, 0x01}
	refs := refServer(t, png)
	client := canvastask.NewClient(gateway.URL, "sk-service-key")
	res, err := client.EditImage(context.Background(), canvastask.EditRequest{
		Model: "img-m", Prompt: "把背景换成雪原", Size: "1792x1024",
		Images: []string{refs.URL + "/one.png", refs.URL + "/two.png"},
		Source: "canvas=7 task=ct_abc node=image-1-1",
	})
	if err != nil {
		t.Fatalf("EditImage: %v", err)
	}
	if res.URL != "https://img.example/edited.png" {
		t.Errorf("url = %q, want the delivered image", res.URL)
	}
	if gotPath != "/v1/images/edits" || gotAuth != "Bearer sk-service-key" ||
		gotSource != "canvas=7 task=ct_abc node=image-1-1" {
		t.Fatalf("request = (%s, %s, %s), want the edits endpoint with key and source", gotPath, gotAuth, gotSource)
	}
	// 文本字段:model/prompt/n=1 + size;多张参考图分节名 image[](OpenAI
	// 约定),文件名字节原样到达。
	if fields["model"][0] != "img-m" || fields["prompt"][0] != "把背景换成雪原" ||
		fields["n"][0] != "1" || fields["size"][0] != "1792x1024" {
		t.Errorf("fields = %v, want the generation facts", fields)
	}
	if len(files) != 2 {
		t.Fatalf("file parts = %d, want 2", len(files))
	}
	for i, p := range files {
		if p.name != "image[]" || p.file != fmt.Sprintf("reference-%d.png", i+1) || p.ctype != "image/png" {
			t.Errorf("part %d = (%s, %s, %s), want image[]/reference-%d.png/image/png", i, p.name, p.file, p.ctype, i+1)
		}
		if string(p.content) != string(png) {
			t.Errorf("part %d content drifted", i)
		}
	}
}

// 单张参考图的分节名是 image(OpenAI 单图约定);不带 size 时字段缺席。
func TestClientEditImageSingleRefUsesImageField(t *testing.T) {
	var fields map[string][]string
	var fileNames []string
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseMultipartForm(32 << 20)
		fields = r.MultipartForm.Value
		for name, fhs := range r.MultipartForm.File {
			for range fhs {
				fileNames = append(fileNames, name)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"url":"https://img.example/e.png"}]}`))
	}))
	t.Cleanup(gateway.Close)

	png := []byte{0x89, 'P', 'N', 'G'}
	refs := refServer(t, png)
	client := canvastask.NewClient(gateway.URL, "sk-service-key")
	if _, err := client.EditImage(context.Background(), canvastask.EditRequest{
		Model: "img-m", Prompt: "p",
		Images: []string{refs.URL + "/ref.png"},
	}); err != nil {
		t.Fatalf("EditImage: %v", err)
	}
	if fields["model"][0] != "img-m" || fields["n"][0] != "1" {
		t.Errorf("fields = %v, want model and n=1", fields)
	}
	if _, ok := fields["size"]; ok {
		t.Errorf("size field present, want it omitted when empty")
	}
	if len(fileNames) != 1 || fileNames[0] != "image" {
		t.Errorf("file parts = %v, want a single image part", fileNames)
	}
}

// 参考图拉取失败与超限都要在任务行留下可读原因。
func TestClientEditImageReferenceFailures(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(dead.Close)

	client := canvastask.NewClient("http://gateway.invalid", "sk-service-key")
	_, err := client.EditImage(context.Background(), canvastask.EditRequest{
		Model: "img-m", Prompt: "p", Images: []string{dead.URL + "/gone.png"},
	})
	if err == nil || !strings.Contains(err.Error(), "拉取失败") {
		t.Fatalf("err = %v, want the fetch failure reason", err)
	}

	// 超限:响应体截断在 8MiB+1,客户端必须报上限而不是把胖参考图发出去。
	fat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(make([]byte, 9<<20))
	}))
	t.Cleanup(fat.Close)
	_, err = client.EditImage(context.Background(), canvastask.EditRequest{
		Model: "img-m", Prompt: "p", Images: []string{fat.URL + "/fat.png"},
	})
	if err == nil || !strings.Contains(err.Error(), "上限") {
		t.Fatalf("err = %v, want the size-cap reason", err)
	}
}

// 网关的 OpenAI 错误体原样进错误串,任务行因此拿到厂商原因。
func TestClientEditImageSurfacesGatewayErrors(t *testing.T) {
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"message":"size not supported by model"}}`))
	}))
	t.Cleanup(gateway.Close)

	png := []byte{0x89, 'P', 'N', 'G'}
	refs := refServer(t, png)
	client := canvastask.NewClient(gateway.URL, "sk-service-key")
	_, err := client.EditImage(context.Background(), canvastask.EditRequest{
		Model: "img-m", Prompt: "p", Images: []string{refs.URL + "/r.png"},
	})
	if err == nil || !strings.Contains(err.Error(), "size not supported by model") {
		t.Fatalf("err = %v, want the gateway's message", err)
	}
}

// 单张合规、合计超限:四张 8MiB 参考图各自过单张上限,但合计撞破
// 24MiB 组合上限 —— 必须在拨号网关之前就地失败,失败原因可读。
func TestClientEditImageRejectsCombinedReferenceSize(t *testing.T) {
	dialed := false
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		dialed = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(gateway.Close)

	fat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(make([]byte, 8<<20))
	}))
	t.Cleanup(fat.Close)

	client := canvastask.NewClient(gateway.URL, "sk-service-key")
	_, err := client.EditImage(context.Background(), canvastask.EditRequest{
		Model: "img-m", Prompt: "p",
		Images: []string{
			fat.URL + "/1.png", fat.URL + "/2.png", fat.URL + "/3.png", fat.URL + "/4.png",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "合计") {
		t.Fatalf("err = %v, want the combined size-cap reason", err)
	}
	if dialed {
		t.Errorf("gateway was dialed despite the combined cap")
	}
}
