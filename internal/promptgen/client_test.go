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

// TestGenerateChatSendsConversationVerbatim 验证多轮形态(29 号票):
// Conversation 非空时按原样作为 messages 上送,优先于单轮 Content。
func TestGenerateChatSendsConversationVerbatim(t *testing.T) {
	gateway := newFakeGateway(http.StatusOK, chatCompletion("改后的提示词"))
	server := newGatewayServer(t, gateway)
	client := promptgen.NewClient(server.URL, "sk-service-key")

	if _, err := client.GenerateChat(context.Background(), promptgen.ChatRequest{
		Model:  "chat-m",
		Source: "canvas=7 node=agent-1-1 gen=prompt",
		Conversation: []promptgen.ChatMessage{
			{Role: "user", Content: "为主题「赛博朋克城市」写提示词"},
			{Role: "assistant", Content: "a neon cyberpunk city"},
			{Role: "user", Content: "把色调改暖"},
		},
	}); err != nil {
		t.Fatalf("GenerateChat: %v", err)
	}

	messages, _ := gateway.last.Body["messages"].([]any)
	if len(messages) != 3 {
		t.Fatalf("messages = %v, want the three conversation turns", gateway.last.Body["messages"])
	}
	first, _ := messages[0].(map[string]any)
	if first["role"] != "user" || first["content"] != "为主题「赛博朋克城市」写提示词" {
		t.Errorf("messages[0] = %v, want the instruction turn", first)
	}
	last, _ := messages[2].(map[string]any)
	if last["role"] != "user" || last["content"] != "把色调改暖" {
		t.Errorf("messages[2] = %v, want the current input turn", last)
	}
}

// sseBody 拼一帧 data: 事件(与网关 clientFrame 同形:前缀 + 空格 + 载荷 + 空行)。
func sseBody(payload string) string {
	return "data: " + payload + "\n\n"
}

// TestStreamChatRelaysDeltasUntilDone 验证 31 号票流式主干:delta 逐帧回调、
// [DONE] 正常收尾、注释保活行被跳过。
func TestStreamChatRelaysDeltasUntilDone(t *testing.T) {
	gateway := newFakeGateway(http.StatusOK, nil)
	gateway.respondRaw(http.StatusOK,
		sseBody(`{"choices":[{"delta":{"content":"a neon "}}]}`)+
			": keep-alive\n\n"+
			sseBody(`{"choices":[{"delta":{"content":"cyberpunk city"}}]}`)+
			"data: [DONE]\n\n")
	server := newGatewayServer(t, gateway)
	client := promptgen.NewClient(server.URL, "sk-service-key")

	var got []string
	err := client.StreamChat(context.Background(), promptgen.ChatRequest{
		Model:  "chat-m",
		Source: "canvas=7 node=agent-1-1 gen=prompt",
		Conversation: []promptgen.ChatMessage{
			{Role: "user", Content: "为主题「赛博朋克城市」写提示词"},
		},
	}, func(delta string) { got = append(got, delta) })
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	if len(got) != 2 || got[0] != "a neon " || got[1] != "cyberpunk city" {
		t.Errorf("deltas = %q, want the two content deltas", got)
	}
	// 流式与同步同源:来源标记照带。
	if gateway.last.Source != "canvas=7 node=agent-1-1 gen=prompt" {
		t.Errorf("source = %q, want the canvas origin mark", gateway.last.Source)
	}
	body, _ := gateway.last.Body["stream"].(bool)
	if !body {
		t.Errorf("stream = %v, want true", gateway.last.Body["stream"])
	}
}

func TestStreamChatSurfacesPreStreamHTTPError(t *testing.T) {
	// 流打开前的失败与同步路径同答:额度不足等用户可行动的信息原样透出。
	gateway := newFakeGateway(http.StatusPaymentRequired, map[string]any{
		"error": map[string]any{"message": "余额不足 (insufficient quota)"},
	})
	server := newGatewayServer(t, gateway)
	client := promptgen.NewClient(server.URL, "sk-service-key")

	err := client.StreamChat(context.Background(), promptgen.ChatRequest{
		Model: "chat-m", Content: "问题",
	}, func(string) {})
	if err == nil || !strings.Contains(err.Error(), "余额不足") {
		t.Fatalf("err = %v, want the pre-stream gateway reason", err)
	}
}

func TestStreamChatSurfacesMidStreamErrorFrame(t *testing.T) {
	gateway := newFakeGateway(http.StatusOK, nil)
	gateway.respondRaw(http.StatusOK,
		sseBody(`{"choices":[{"delta":{"content":"部分"}}]}`)+
			sseBody(`{"error":{"message":"上游中途失败"}}`))
	server := newGatewayServer(t, gateway)
	client := promptgen.NewClient(server.URL, "sk-service-key")

	var got []string
	err := client.StreamChat(context.Background(), promptgen.ChatRequest{
		Model: "chat-m", Content: "问题",
	}, func(d string) { got = append(got, d) })
	if err == nil || !strings.Contains(err.Error(), "上游中途失败") {
		t.Fatalf("err = %v, want the mid-stream error", err)
	}
	if len(got) != 1 || got[0] != "部分" {
		t.Errorf("deltas = %q, want the frames before the failure delivered", got)
	}
}

func TestStreamChatRejectsIncompleteOrEmptyStreams(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"eof without done", sseBody(`{"choices":[{"delta":{"content":"半句"}}]}`)},
		{"done without deltas", "data: [DONE]\n\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gateway := newFakeGateway(http.StatusOK, nil)
			gateway.respondRaw(http.StatusOK, tc.raw)
			server := newGatewayServer(t, gateway)
			client := promptgen.NewClient(server.URL, "sk-service-key")

			err := client.StreamChat(context.Background(), promptgen.ChatRequest{
				Model: "chat-m", Content: "问题",
			}, func(string) {})
			if err == nil {
				t.Fatal("err = nil, want a failure")
			}
		})
	}
}

func TestGenerateChatWithVideoSendsMultimodalContentParts(t *testing.T) {
	gateway := newFakeGateway(http.StatusOK, chatCompletion("提示词"))
	server := newGatewayServer(t, gateway)
	client := promptgen.NewClient(server.URL, "sk-service-key")

	if _, err := client.GenerateChat(context.Background(), promptgen.ChatRequest{
		Model:    "vl-m",
		Content:  "反推指令",
		VideoURL: "https://cdn.example.com/clip.mp4",
	}); err != nil {
		t.Fatalf("GenerateChat: %v", err)
	}

	messages, _ := gateway.last.Body["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("messages = %v, want exactly one", gateway.last.Body["messages"])
	}
	message, _ := messages[0].(map[string]any)
	if message["role"] != "user" {
		t.Errorf("role = %v, want user", message["role"])
	}
	parts, _ := message["content"].([]any)
	if len(parts) != 2 {
		t.Fatalf("content = %v, want video part ahead of text part", message["content"])
	}
	video, _ := parts[0].(map[string]any)
	if video["type"] != "video_url" {
		t.Errorf("part[0] = %v, want a video_url part", video)
	}
	videoRef, _ := video["video_url"].(map[string]any)
	if videoRef["url"] != "https://cdn.example.com/clip.mp4" {
		t.Errorf("video_url = %v, want the passed address", videoRef)
	}
	text, _ := parts[1].(map[string]any)
	if text["type"] != "text" || text["text"] != "反推指令" {
		t.Errorf("part[1] = %v, want the instruction as a text part", text)
	}
}

func TestGenerateChatWithImageSendsImagePartAheadOfText(t *testing.T) {
	gateway := newFakeGateway(http.StatusOK, chatCompletion("分析报告"))
	server := newGatewayServer(t, gateway)
	client := promptgen.NewClient(server.URL, "sk-service-key")

	if _, err := client.GenerateChat(context.Background(), promptgen.ChatRequest{
		Model:    "vl-m",
		Content:  "分析指令",
		ImageURL: "https://cdn.example.com/pic.png",
	}); err != nil {
		t.Fatalf("GenerateChat: %v", err)
	}

	messages, _ := gateway.last.Body["messages"].([]any)
	message, _ := messages[0].(map[string]any)
	parts, _ := message["content"].([]any)
	if len(parts) != 2 {
		t.Fatalf("content = %v, want image part ahead of text part", message["content"])
	}
	image, _ := parts[0].(map[string]any)
	if image["type"] != "image_url" {
		t.Errorf("part[0] = %v, want an image_url part", image)
	}
	imageRef, _ := image["image_url"].(map[string]any)
	if imageRef["url"] != "https://cdn.example.com/pic.png" {
		t.Errorf("image_url = %v, want the passed address", imageRef)
	}
	text, _ := parts[1].(map[string]any)
	if text["type"] != "text" || text["text"] != "分析指令" {
		t.Errorf("part[1] = %v, want the instruction as a text part", text)
	}
}

// TestGenerateChatConversationMediaTurnSendsPartArray 验证 32 号票会话
// 媒体轮:Parts 非空时 content 上送为分节数组(媒体在前、文本在后),
// 纯文本轮保持字符串 content 不变。
func TestGenerateChatConversationMediaTurnSendsPartArray(t *testing.T) {
	gateway := newFakeGateway(http.StatusOK, chatCompletion("改后的提示词"))
	server := newGatewayServer(t, gateway)
	client := promptgen.NewClient(server.URL, "sk-service-key")

	if _, err := client.GenerateChat(context.Background(), promptgen.ChatRequest{
		Model:  "chat-m",
		Source: "canvas=7 node=agent-1-1 gen=prompt",
		Conversation: []promptgen.ChatMessage{
			{Role: "user", Content: "指令"},
			{Role: "user", Parts: promptgen.MediaTextParts([]promptgen.MediaPart{
				{Kind: promptgen.MediaKindImage, URL: "https://cdn.example.com/a.png"},
				{Kind: promptgen.MediaKindVideo, URL: "https://cdn.example.com/b.mp4"},
			}, "分析这段素材")},
			{Role: "assistant", Content: "上一轮提示词"},
		},
	}); err != nil {
		t.Fatalf("GenerateChat: %v", err)
	}

	messages, _ := gateway.last.Body["messages"].([]any)
	if len(messages) != 3 {
		t.Fatalf("messages = %v, want three turns", gateway.last.Body["messages"])
	}
	// 纯文本轮:content 是字符串。
	first, _ := messages[0].(map[string]any)
	if _, isStr := first["content"].(string); !isStr {
		t.Errorf("messages[0].content = %v, want a plain string", first["content"])
	}
	// 媒体轮:content 是分节数组,图片在前、视频其次、文本收尾。
	media, _ := messages[1].(map[string]any)
	parts, _ := media["content"].([]any)
	if len(parts) != 3 {
		t.Fatalf("media turn content = %v, want three parts (image, video, text)", media["content"])
	}
	img, _ := parts[0].(map[string]any)
	if img["type"] != "image_url" {
		t.Errorf("part[0] = %v, want an image_url part", img)
	}
	vid, _ := parts[1].(map[string]any)
	if vid["type"] != "video_url" {
		t.Errorf("part[1] = %v, want a video_url part", vid)
	}
	text, _ := parts[2].(map[string]any)
	if text["type"] != "text" || text["text"] != "分析这段素材" {
		t.Errorf("part[2] = %v, want the text part closing the array", text)
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

// ---- 63 号票:Agent 工具循环的客户端扩容 ----

// toolCallSSE 拼一帧带 tool_calls delta 的流式块:调用方给原始 JSON 片段。
func toolCallSSE(deltaJSON string) string {
	return sseBody(`{"choices":[{"delta":{"tool_calls":[` + deltaJSON + `]}}]}`)
}

// TestStreamChatTurnAggregatesToolCallDeltas 验证 63 号票下行聚合:tool_calls
// delta 按 index 键控,id/function.name/function.arguments 分片拼接,文本
// delta 照旧逐帧回调,finish_reason 取流中最后一次非空值。
func TestStreamChatTurnAggregatesToolCallDeltas(t *testing.T) {
	gateway := newFakeGateway(http.StatusOK, nil)
	gateway.respondRaw(http.StatusOK,
		sseBody(`{"choices":[{"delta":{"role":"assistant","content":"让我先建角色。"}}]}`)+
			toolCallSSE(`{"index":0,"id":"call_1","type":"function","function":{"name":"create_character","arguments":"{\"na"}}`)+
			toolCallSSE(`{"index":0,"function":{"arguments":"me\":\"\u5f20\u4e09\"}"}}`)+
			toolCallSSE(`{"index":1,"id":"call_2","type":"function","function":{"name":"list_assets","arguments":"{}"}}`)+
			sseBody(`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`)+
			"data: [DONE]\n\n")
	server := newGatewayServer(t, gateway)
	client := promptgen.NewClient(server.URL, "sk-service-key")

	var got []string
	turn, err := client.StreamChatTurn(context.Background(), promptgen.ChatRequest{
		Model: "chat-m", Content: "建一个角色",
	}, func(delta string) { got = append(got, delta) })
	if err != nil {
		t.Fatalf("StreamChatTurn: %v", err)
	}
	if len(got) != 1 || got[0] != "让我先建角色。" {
		t.Errorf("deltas = %q, want the content delta delivered as it arrived", got)
	}
	if turn.Content != "让我先建角色。" {
		t.Errorf("content = %q, want the accumulated text", turn.Content)
	}
	if turn.FinishReason != "tool_calls" {
		t.Errorf("finish_reason = %q, want tool_calls", turn.FinishReason)
	}
	want := []promptgen.ToolCall{
		{ID: "call_1", Name: "create_character", Arguments: `{"name":"张三"}`},
		{ID: "call_2", Name: "list_assets", Arguments: `{}`},
	}
	if len(turn.ToolCalls) != len(want) {
		t.Fatalf("tool_calls = %v, want %d calls", turn.ToolCalls, len(want))
	}
	for i, tc := range turn.ToolCalls {
		if tc != want[i] {
			t.Errorf("tool_calls[%d] = %+v, want %+v", i, tc, want[i])
		}
	}
}

// TestStreamChatTurnPassesToolsThrough 验证 63 号票上行透传:Tools/ToolChoice
// 进请求体;不带工具的请求保持不带这两个字段。
func TestStreamChatTurnPassesToolsThrough(t *testing.T) {
	gateway := newFakeGateway(http.StatusOK, nil)
	gateway.respondRaw(http.StatusOK,
		sseBody(`{"choices":[{"delta":{"content":"好"}}]}`)+"data: [DONE]\n\n")
	server := newGatewayServer(t, gateway)
	client := promptgen.NewClient(server.URL, "sk-service-key")

	definitions := []json.RawMessage{
		json.RawMessage(`{"type":"function","function":{"name":"create_character","description":"建角色","parameters":{"type":"object"}}}`),
	}
	if _, err := client.StreamChatTurn(context.Background(), promptgen.ChatRequest{
		Model:      "chat-m",
		Content:    "建一个角色",
		Tools:      definitions,
		ToolChoice: "auto",
	}, func(string) {}); err != nil {
		t.Fatalf("StreamChatTurn: %v", err)
	}

	tools, ok := gateway.last.Body["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %v, want the definition riding through untouched", gateway.last.Body["tools"])
	}
	tool, _ := tools[0].(map[string]any)
	if tool["type"] != "function" {
		t.Errorf("tools[0].type = %v, want function", tool["type"])
	}
	if gateway.last.Body["tool_choice"] != "auto" {
		t.Errorf("tool_choice = %v, want auto", gateway.last.Body["tool_choice"])
	}

	// 对照:不带工具的请求不带 tools/tool_choice 字段(与升级前逐字节一致)。
	gateway.respondRaw(http.StatusOK,
		sseBody(`{"choices":[{"delta":{"content":"好"}}]}`)+"data: [DONE]\n\n")
	if _, err := client.StreamChatTurn(context.Background(), promptgen.ChatRequest{
		Model: "chat-m", Content: "问题",
	}, func(string) {}); err != nil {
		t.Fatalf("StreamChatTurn plain: %v", err)
	}
	if _, has := gateway.last.Body["tools"]; has {
		t.Errorf("tools = %v, want the field absent", gateway.last.Body["tools"])
	}
	if _, has := gateway.last.Body["tool_choice"]; has {
		t.Errorf("tool_choice = %v, want the field absent", gateway.last.Body["tool_choice"])
	}
}

// TestStreamChatTurnPureToolCallStreamIsValid 验证纯工具轮(无文本 delta)
// 不是空消息:聚合出 tool_calls 即合法收尾。
func TestStreamChatTurnPureToolCallStreamIsValid(t *testing.T) {
	gateway := newFakeGateway(http.StatusOK, nil)
	gateway.respondRaw(http.StatusOK,
		toolCallSSE(`{"index":0,"id":"call_1","type":"function","function":{"name":"t","arguments":"{}"}}`)+
			sseBody(`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`)+
			"data: [DONE]\n\n")
	server := newGatewayServer(t, gateway)
	client := promptgen.NewClient(server.URL, "sk-service-key")

	turn, err := client.StreamChatTurn(context.Background(), promptgen.ChatRequest{
		Model: "chat-m", Content: "问题",
	}, func(string) {})
	if err != nil {
		t.Fatalf("StreamChatTurn: %v", err)
	}
	if len(turn.ToolCalls) != 1 || turn.ToolCalls[0].Name != "t" {
		t.Errorf("tool_calls = %v, want the single call", turn.ToolCalls)
	}
}

// TestGenerateChatSerializesAgentToolTurns 验证循环中间帧的上行序列化:
// assistant 轮的 tool_calls 与 role=tool 轮的 tool_call_id 按形状进 messages。
func TestGenerateChatSerializesAgentToolTurns(t *testing.T) {
	gateway := newFakeGateway(http.StatusOK, chatCompletion("完成了"))
	server := newGatewayServer(t, gateway)
	client := promptgen.NewClient(server.URL, "sk-service-key")

	if _, err := client.GenerateChat(context.Background(), promptgen.ChatRequest{
		Model: "chat-m",
		Conversation: []promptgen.ChatMessage{
			{Role: "user", Content: "建角色"},
			{Role: "assistant", Content: "让我先建角色。", ToolCalls: []promptgen.ToolCall{
				{ID: "call_1", Name: "create_character", Arguments: `{"name":"张三"}`},
			}},
			{Role: "tool", ToolCallID: "call_1", Content: `{"summary":"已创建 张三"}`},
		},
	}); err != nil {
		t.Fatalf("GenerateChat: %v", err)
	}

	messages, _ := gateway.last.Body["messages"].([]any)
	if len(messages) != 3 {
		t.Fatalf("messages = %v, want three turns", gateway.last.Body["messages"])
	}
	assistant, _ := messages[1].(map[string]any)
	calls, ok := assistant["tool_calls"].([]any)
	if !ok || len(calls) != 1 {
		t.Fatalf("assistant tool_calls = %v, want one call", assistant["tool_calls"])
	}
	call, _ := calls[0].(map[string]any)
	if call["id"] != "call_1" || call["type"] != "function" {
		t.Errorf("tool_call = %v, want id/type shape", call)
	}
	fn, _ := call["function"].(map[string]any)
	if fn["name"] != "create_character" || fn["arguments"] != `{"name":"张三"}` {
		t.Errorf("tool_call.function = %v, want name/arguments verbatim", fn)
	}
	toolTurn, _ := messages[2].(map[string]any)
	if toolTurn["role"] != "tool" || toolTurn["tool_call_id"] != "call_1" {
		t.Errorf("tool turn = %v, want role/tool_call_id shape", toolTurn)
	}
	if toolTurn["content"] != `{"summary":"已创建 张三"}` {
		t.Errorf("tool turn content = %v, want the result JSON", toolTurn["content"])
	}
}
