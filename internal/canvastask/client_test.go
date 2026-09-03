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
