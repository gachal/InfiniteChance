package promptgen_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gachal/InfiniteChance/internal/promptgen"
)

// recordedRequest captures what the client actually put on the wire.
type recordedRequest struct {
	Method string
	Path   string
	Auth   string
	Source string
	Body   map[string]any
}

// fakeGateway is a programmable gateway relay stub: it answers with the
// configured status/body and records the last request it saw.
type fakeGateway struct {
	mu      sync.Mutex
	last    recordedRequest
	status  int
	body    any
	rawBody string
}

func newFakeGateway(status int, body any) *fakeGateway {
	return &fakeGateway{status: status, body: body}
}

// respondRaw answers with a body that is sent verbatim (non-JSON cases).
func (f *fakeGateway) respondRaw(status int, raw string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status, f.rawBody = status, raw
}

func (f *fakeGateway) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.last = recordedRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Auth:   r.Header.Get("Authorization"),
			Source: r.Header.Get("X-InfiniteChance-Source"),
			Body:   body,
		}
		status, rawBody := f.status, f.rawBody
		f.mu.Unlock()

		if rawBody != "" {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(rawBody))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(f.body)
	})
}

func chatCompletion(content string) map[string]any {
	return map[string]any{
		"id": "chatcmpl-x", "object": "chat.completion",
		"choices": []any{
			map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": content},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]any{
			"prompt_tokens": 12, "completion_tokens": 34, "total_tokens": 46,
		},
	}
}

func newGatewayServer(t *testing.T, g *fakeGateway) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(g.handler())
	t.Cleanup(server.Close)
	return server
}

func TestGenerateChatPostsChatCompletionsWithServiceKeyAndSource(t *testing.T) {
	gateway := newFakeGateway(http.StatusOK, chatCompletion("  a neon cyberpunk city  "))
	server := newGatewayServer(t, gateway)
	client := promptgen.NewClient(server.URL, "sk-service-key")

	result, err := client.GenerateChat(context.Background(), promptgen.ChatRequest{
		Model:   "chat-m",
		Content: "为主题「赛博朋克城市」写提示词",
		Source:  "canvas=7 node=prompt-1-1 gen=prompt",
	})
	if err != nil {
		t.Fatalf("GenerateChat: %v", err)
	}
	if result.Content != "a neon cyberpunk city" {
		t.Errorf("content = %q, want whitespace-trimmed answer", result.Content)
	}

	req := gateway.last
	if req.Method != http.MethodPost || req.Path != "/v1/chat/completions" {
		t.Errorf("request = %s %s, want POST /v1/chat/completions", req.Method, req.Path)
	}
	if req.Auth != "Bearer sk-service-key" {
		t.Errorf("authorization = %q, want the service key bearer", req.Auth)
	}
	if req.Source != "canvas=7 node=prompt-1-1 gen=prompt" {
		t.Errorf("source = %q, want the canvas origin mark", req.Source)
	}
	if req.Body["model"] != "chat-m" {
		t.Errorf("model = %v, want chat-m", req.Body["model"])
	}
	if req.Body["stream"] != false {
		t.Errorf("stream = %v, want false", req.Body["stream"])
	}
	messages, _ := req.Body["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("messages = %v, want exactly one", req.Body["messages"])
	}
	message, _ := messages[0].(map[string]any)
	if message["role"] != "user" || message["content"] != "为主题「赛博朋克城市」写提示词" {
		t.Errorf("message = %v, want the rendered template as one user message", message)
	}
}

func TestGenerateChatOmitsSourceHeaderWhenEmpty(t *testing.T) {
	gateway := newFakeGateway(http.StatusOK, chatCompletion("答案"))
	server := newGatewayServer(t, gateway)
	client := promptgen.NewClient(server.URL, "sk-service-key")

	if _, err := client.GenerateChat(context.Background(), promptgen.ChatRequest{
		Model: "chat-m", Content: "问题",
	}); err != nil {
		t.Fatalf("GenerateChat: %v", err)
	}
	if gateway.last.Source != "" {
		t.Errorf("source = %q, want no header", gateway.last.Source)
	}
}

func TestGenerateChatSurfacesOpenAIErrorMessage(t *testing.T) {
	gateway := newFakeGateway(http.StatusPaymentRequired, map[string]any{
		"error": map[string]any{
			"message": "余额不足 (insufficient quota)", "type": "insufficient_quota", "code": "insufficient_quota",
		},
	})
	server := newGatewayServer(t, gateway)
	client := promptgen.NewClient(server.URL, "sk-service-key")

	_, err := client.GenerateChat(context.Background(), promptgen.ChatRequest{
		Model: "chat-m", Content: "问题",
	})
	if err == nil {
		t.Fatal("err = nil, want the upstream reason")
	}
	if !strings.Contains(err.Error(), "余额不足") {
		t.Errorf("err = %v, want the upstream message", err)
	}
}

func TestGenerateChatSummarizesNonJSONFailureBody(t *testing.T) {
	gateway := newFakeGateway(http.StatusInternalServerError, nil)
	gateway.respondRaw(http.StatusInternalServerError, "<html>boom</html>")
	server := newGatewayServer(t, gateway)
	client := promptgen.NewClient(server.URL, "sk-service-key")

	_, err := client.GenerateChat(context.Background(), promptgen.ChatRequest{
		Model: "chat-m", Content: "问题",
	})
	if err == nil {
		t.Fatal("err = nil, want a failure")
	}
	if !strings.Contains(err.Error(), "gateway 500") || !strings.Contains(err.Error(), "<html>boom</html>") {
		t.Errorf("err = %v, want status plus raw-body summary", err)
	}
}

func TestGenerateChatRejectsEmptyOrMissingAnswers(t *testing.T) {
	cases := []struct {
		name string
		body any
	}{
		{"no choices", map[string]any{"choices": []any{}}},
		{"empty content", chatCompletion("   ")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gateway := newFakeGateway(http.StatusOK, tc.body)
			server := newGatewayServer(t, gateway)
			client := promptgen.NewClient(server.URL, "sk-service-key")

			_, err := client.GenerateChat(context.Background(), promptgen.ChatRequest{
				Model: "chat-m", Content: "问题",
			})
			if err == nil {
				t.Fatal("err = nil, want a failure")
			}
		})
	}
}

func TestGenerateChatRejectsUnparseableSuccessBody(t *testing.T) {
	gateway := newFakeGateway(http.StatusOK, nil)
	gateway.respondRaw(http.StatusOK, "not json at all")
	server := newGatewayServer(t, gateway)
	client := promptgen.NewClient(server.URL, "sk-service-key")

	_, err := client.GenerateChat(context.Background(), promptgen.ChatRequest{
		Model: "chat-m", Content: "问题",
	})
	if err == nil {
		t.Fatal("err = nil, want a failure")
	}
	if !strings.Contains(err.Error(), "not JSON") {
		t.Errorf("err = %v, want unparseable-body reason", err)
	}
}
