package promptgen_test

// 63 号票 Agent 工具循环 SSE 端点的全链测试:handler 装配真 promptgen.Client,
// 网关用 httptest 假中转(逐轮回放 OpenAI 流式 SSE 形状),断言浏览器侧
// 收到的事件序列、上行 tools 透传与断线中断语义。

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gachal/InfiniteChance/internal/promptgen"
)

// ---- 假网关中转 ----

// fakeRelay 是逐轮脚本化的聊天补全假中转:每个 POST /v1/chat/completions
// 消耗一条脚本(完整 SSE 文本,含 [DONE]),请求原文与来源头留档断言。
type fakeRelay struct {
	mu       sync.Mutex
	rounds   []string
	requests []recordedRequest
	statuses []int // 与 rounds 对齐的预流状态码(0 = 200)
}

func (f *fakeRelay) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		n := len(f.requests)
		f.requests = append(f.requests, recordedRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Auth:   r.Header.Get("Authorization"),
			Source: r.Header.Get("X-InfiniteChance-Source"),
			Body:   body,
		})
		script := ""
		status := 0
		if n < len(f.rounds) {
			script = f.rounds[n]
		}
		if n < len(f.statuses) {
			status = f.statuses[n]
		}
		f.mu.Unlock()

		if status != 0 && (status < 200 || status > 299) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"message":"余额不足 (insufficient quota)"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(script))
	})
}

func (f *fakeRelay) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func (f *fakeRelay) request(n int) recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[n]
}

// relayChunk 把一个流式块编码成一帧 SSE data 事件。
func relayChunk(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal relay chunk: %v", err)
	}
	return sseBody(string(raw))
}

// relayTextRound 拼一轮纯文本回答的 SSE 全文。
func relayTextRound(t *testing.T, text string) string {
	return relayChunk(t, map[string]any{
		"choices": []any{map[string]any{"delta": map[string]any{"content": text}}},
	}) + "data: [DONE]\n\n"
}

// relayToolRound 拼一轮工具调用的 SSE 全文:lead 文本帧(可为空)+ 
// tool_calls 参数劈两片(delta 分片聚合的真实形状)+ finish 收帧。
func relayToolRound(t *testing.T, lead, id, name string, args map[string]any) string {
	t.Helper()
	argsRaw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal tool args: %v", err)
	}
	argStr := string(argsRaw)
	half := len(argStr) / 2
	var b strings.Builder
	if lead != "" {
		b.WriteString(relayChunk(t, map[string]any{
			"choices": []any{map[string]any{"delta": map[string]any{"content": lead}}},
		}))
	}
	b.WriteString(relayChunk(t, map[string]any{
		"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{
			"index": 0, "id": id, "type": "function",
			"function": map[string]any{"name": name, "arguments": argStr[:half]},
		}}}}},
	}))
	b.WriteString(relayChunk(t, map[string]any{
		"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{
			"index":    0,
			"function": map[string]any{"arguments": argStr[half:]},
		}}}}},
	}))
	b.WriteString(relayChunk(t, map[string]any{
		"choices": []any{map[string]any{"delta": map[string]any{}, "finish_reason": "tool_calls"}},
	}))
	b.WriteString("data: [DONE]\n\n")
	return b.String()
}

// stubAgentTool 是端点测试里的注册工具桩(loop 单测里有同构桩,那边在
// 内部包,这边开箱复用不了)。
type stubAgentTool struct {
	name    string
	summary string
	payload string
	err     error

	mu       sync.Mutex
	gotArgs  []string
	execHook func()
}

func (t *stubAgentTool) Name() string               { return t.name }
func (t *stubAgentTool) Definition() json.RawMessage { return json.RawMessage(`{"type":"function","function":{"name":"` + t.name + `","description":"测试工具","parameters":{"type":"object"}}}`) }

func (t *stubAgentTool) Execute(_ context.Context, args json.RawMessage) (string, json.RawMessage, error) {
	t.mu.Lock()
	t.gotArgs = append(t.gotArgs, string(args))
	hook := t.execHook
	t.mu.Unlock()
	if hook != nil {
		hook()
	}
	if t.err != nil {
		return "", nil, t.err
	}
	return t.summary, json.RawMessage(t.payload), nil
}

func (t *stubAgentTool) args() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.gotArgs...)
}

// ---- 断言辅助 ----

// sseFrames 切开响应原文:按 data: 前缀逐帧取出([DONE] 原样保留)。
func sseFrames(t *testing.T, raw string) []string {
	t.Helper()
	var frames []string
	for _, block := range strings.Split(raw, "\n\n") {
		payload := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(block), "data: "))
		if payload == "" || !strings.HasPrefix(strings.TrimSpace(block), "data: ") {
			continue
		}
		frames = append(frames, payload)
	}
	return frames
}

// frameMap 解析一帧 JSON([DONE] 返回 nil)。
func frameMap(t *testing.T, payload string) map[string]any {
	t.Helper()
	if payload == "[DONE]" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(payload), &m); err != nil {
		t.Fatalf("frame %q not JSON: %v", payload, err)
	}
	return m
}

// ---- 用例 ----

// 全链主干:tools 透传 + tool_calls 聚合 + 事件序列 + 续请求形状 + 来源
// 标记 gen=agent + 设定段流首帧。
func TestAgentStreamRunsToolLoopAndEmitsEvents(t *testing.T) {
	relay := &fakeRelay{rounds: []string{
		relayToolRound(t, "好的。", "call_1", "echo_tool", map[string]any{"name": "张三"}),
		relayTextRound(t, "完成了。"),
	}}
	relayServer := httptest.NewServer(relay.handler())
	t.Cleanup(relayServer.Close)

	tool := &stubAgentTool{name: "echo_tool", summary: "已创建 张三", payload: `{"draft":{"x":1}}`}
	registry := promptgen.NewAgentToolRegistry()
	registry.Register(tool)

	env := newHandlerEnv(t, func(p *envParams) {
		p.gateway = promptgen.NewClient(relayServer.URL, "sk-relay")
		p.agentTools = registry
	})

	res, raw := env.do(t, http.MethodPost, "/canvases/7/agent/stream", map[string]any{
		"node_id":       "agent-1-1",
		"topic":         "建一个角色",
		"model":         "chat-m",
		"character_ids": []int{11},
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.StatusCode, raw)
	}
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type = %q, want text/event-stream", ct)
	}

	frames := sseFrames(t, raw)
	// 帧序:character_section(注入了角色 11)→ delta → tool_call →
	// tool_result → delta → done → [DONE]。
	if len(frames) != 7 || frames[6] != "[DONE]" {
		t.Fatalf("frames = %v, want 7 frames ending with [DONE]", frames)
	}
	first := frameMap(t, frames[0])
	if !strings.Contains(first["character_section"].(string), "林小满") {
		t.Fatalf("first frame = %v, want the injected character section", first)
	}
	if d := frameMap(t, frames[1]); d["type"] != "delta" || d["text"] != "好的。" {
		t.Errorf("frames[1] = %v, want the round-1 text delta event", frames[1])
	}
	call := frameMap(t, frames[2])
	if call["type"] != "tool_call" || call["id"] != "call_1" || call["name"] != "echo_tool" {
		t.Errorf("frames[2] = %v, want the tool_call event", call)
	}
	if args, ok := call["args"].(map[string]any); !ok || args["name"] != "张三" {
		t.Errorf("tool_call args = %v, want the aggregated arguments", call["args"])
	}
	result := frameMap(t, frames[3])
	if result["type"] != "tool_result" || result["summary"] != "已创建 张三" {
		t.Errorf("frames[3] = %v, want the tool_result event with summary", result)
	}
	payload, ok := result["payload"].(map[string]any)
	if !ok || payload["draft"] == nil {
		t.Errorf("tool_result payload = %v, want the draft riding through", result["payload"])
	}
	if d := frameMap(t, frames[4]); d["type"] != "delta" || d["text"] != "完成了。" {
		t.Errorf("frames[4] = %v, want the round-2 text delta event", frames[4])
	}
	done := frameMap(t, frames[5])
	if done["type"] != "done" || done["text"] != "好的。\n\n完成了。" {
		t.Fatalf("frames[5] = %v, want the done event with all rounds' text", done)
	}
	if done["summary"] != "本轮工具调用:echo_tool(已创建 张三)" {
		t.Errorf("done summary = %v, want the composed tool line", done["summary"])
	}

	// 上行断言:两轮 /v1 调用,tools 透传、来源 gen=agent,续请求带工具帧。
	if relay.count() != 2 {
		t.Fatalf("relay calls = %d, want 2", relay.count())
	}
	firstReq := relay.request(0)
	if firstReq.Source != "canvas=7 node=agent-1-1 gen=agent" {
		t.Errorf("source = %q, want the agent origin mark", firstReq.Source)
	}
	if firstReq.Auth != "Bearer sk-relay" {
		t.Errorf("auth = %q, want the service key", firstReq.Auth)
	}
	tools, _ := firstReq.Body["tools"].([]any)
	if len(tools) != 1 || firstReq.Body["tool_choice"] != "auto" {
		t.Errorf("round-1 tools = %v / choice = %v, want definitions + auto", firstReq.Body["tools"], firstReq.Body["tool_choice"])
	}
	secondReq := relay.request(1)
	if len(secondReq.Body["tools"].([]any)) != 1 {
		t.Errorf("round-2 tools = %v, want definitions still present", secondReq.Body["tools"])
	}
	msgs, _ := secondReq.Body["messages"].([]any)
	// 66 号票起带工具的会话以「剧本导演」system 提示词开场:system + user +
	// assistant + tool。
	if len(msgs) != 4 {
		t.Fatalf("round-2 messages = %v, want system + user + assistant + tool", secondReq.Body["messages"])
	}
	if msgs[0].(map[string]any)["role"] != "system" {
		t.Errorf("messages[0] = %v, want the director system message first", msgs[0])
	}
	assistant, _ := msgs[2].(map[string]any)
	calls, _ := assistant["tool_calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("assistant frame = %v, want one tool_call", assistant)
	}
	toolTurn, _ := msgs[3].(map[string]any)
	if toolTurn["role"] != "tool" || toolTurn["tool_call_id"] != "call_1" {
		t.Errorf("tool frame = %v, want the result bound to call_1", toolTurn)
	}
	if got := tool.args(); len(got) != 1 || got[0] != `{"name":"张三"}` {
		t.Errorf("tool executed with args %v, want the raw call arguments", got)
	}
}

// 15 轮护栏全链:中转恒回工具调用,第 16 轮(收尾)不带 tools 且最后一
// 条是收尾指令。
func TestAgentStreamGuardrailWrapUp(t *testing.T) {
	toolRound := relayToolRound(t, "", "call_loop", "echo_tool", map[string]any{"i": 1})
	relay := &fakeRelay{}
	for i := 0; i < 15; i++ {
		relay.rounds = append(relay.rounds, toolRound)
	}
	relay.rounds = append(relay.rounds, relayTextRound(t, "已完成 15 步,请继续。"))
	relayServer := httptest.NewServer(relay.handler())
	t.Cleanup(relayServer.Close)

	registry := promptgen.NewAgentToolRegistry()
	registry.Register(&stubAgentTool{name: "echo_tool", summary: "一步", payload: `null`})

	env := newHandlerEnv(t, func(p *envParams) {
		p.gateway = promptgen.NewClient(relayServer.URL, "sk-relay")
		p.agentTools = registry
	})

	_, raw := env.do(t, http.MethodPost, "/canvases/7/agent/stream", map[string]any{
		"node_id": "agent-1-1", "topic": "连续干 15 件事", "model": "chat-m",
	})
	frames := sseFrames(t, raw)
	doneSeen := false
	for _, payload := range frames {
		if m := frameMap(t, payload); m != nil && m["type"] == "done" {
			doneSeen = true
			if m["text"] != "已完成 15 步,请继续。" {
				t.Errorf("done.text = %v, want the wrap-up answer", m["text"])
			}
		}
	}
	if !doneSeen || frames[len(frames)-1] != "[DONE]" {
		t.Fatalf("frames tail = %v, want done + [DONE]", frames[max(0, len(frames)-3):])
	}
	if relay.count() != 16 {
		t.Fatalf("relay calls = %d, want 15 tool rounds + 1 wrap-up", relay.count())
	}
	for i := 0; i < 15; i++ {
		req := relay.request(i)
		if len(req.Body["tools"].([]any)) != 1 {
			t.Fatalf("round %d tools = %v, want definitions", i, req.Body["tools"])
		}
	}
	wrapup := relay.request(15)
	if _, has := wrapup.Body["tools"]; has {
		t.Errorf("wrap-up tools = %v, want the field absent", wrapup.Body["tools"])
	}
	msgs, _ := wrapup.Body["messages"].([]any)
	last, _ := msgs[len(msgs)-1].(map[string]any)
	if last["role"] != "user" || !strings.Contains(last["content"].(string), "15 轮工具调用") {
		t.Errorf("wrap-up last message = %v, want the injected wrap-up instruction", last)
	}
}

// 流前校验仍走 JSON 错误形状:topic 缺失 400,未配价模型 400,未配网关 503
// ——响应不是 event-stream。
func TestAgentStreamPreStreamValidationStaysJSON(t *testing.T) {
	relay := &fakeRelay{}
	relayServer := httptest.NewServer(relay.handler())
	t.Cleanup(relayServer.Close)

	env := newHandlerEnv(t, func(p *envParams) {
		p.gateway = promptgen.NewClient(relayServer.URL, "sk-relay")
	})

	cases := []struct {
		name string
		body map[string]any
		want int
		code string
	}{
		{"empty topic", map[string]any{"topic": "", "model": "chat-m"}, http.StatusBadRequest, "invalid_request"},
		{"unpriced model", map[string]any{"topic": "x", "model": "img-m"}, http.StatusBadRequest, "model_not_priced"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, raw := env.do(t, http.MethodPost, "/canvases/7/agent/stream", tc.body)
			if res.StatusCode != tc.want {
				t.Fatalf("status = %d, want %d (%s)", res.StatusCode, tc.want, raw)
			}
			if ct := res.Header.Get("Content-Type"); ct == "text/event-stream" {
				t.Fatalf("content-type = %q, want a plain JSON error before any stream", ct)
			}
			code, _ := errorBody(t, raw)
			if code != tc.code {
				t.Errorf("error code = %q, want %q", code, tc.code)
			}
		})
	}

	// 未配服务 key:装配缺口 503,同样是流前 JSON。
	noGateway := newHandlerEnv(t, func(p *envParams) { p.gateway = nil })
	res, raw := noGateway.do(t, http.MethodPost, "/canvases/7/agent/stream", map[string]any{
		"topic": "x", "model": "chat-m",
	})
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (%s)", res.StatusCode, raw)
	}
	if code, _ := errorBody(t, raw); code != "gateway_unconfigured" {
		t.Errorf("error code = %q, want gateway_unconfigured", code)
	}
}

// 流打开后上游失败:error 帧原样透出原因,无 done、无 [DONE]。
func TestAgentStreamSurfacesMidStreamFailure(t *testing.T) {
	relay := &fakeRelay{statuses: []int{http.StatusPaymentRequired}}
	relayServer := httptest.NewServer(relay.handler())
	t.Cleanup(relayServer.Close)

	env := newHandlerEnv(t, func(p *envParams) {
		p.gateway = promptgen.NewClient(relayServer.URL, "sk-relay")
		p.agentTools = promptgen.NewAgentToolRegistry()
	})

	_, raw := env.do(t, http.MethodPost, "/canvases/7/agent/stream", map[string]any{
		"node_id": "agent-1-1", "topic": "建角色", "model": "chat-m",
	})
	frames := sseFrames(t, raw)
	if len(frames) != 1 {
		t.Fatalf("frames = %v, want a single error frame", frames)
	}
	m := frameMap(t, frames[0])
	errObj, _ := m["error"].(map[string]any)
	if m["type"] != "error" || errObj == nil || errObj["code"] != "upstream_error" {
		t.Fatalf("frame = %v, want an upstream_error event", m)
	}
	if !strings.Contains(errObj["message"].(string), "余额不足") {
		t.Errorf("error message = %v, want the gateway reason verbatim", errObj["message"])
	}
}

// 未知工具:tool_result 显式报告,模型收到错误回执后继续,会话正常收尾。
func TestAgentStreamUnknownToolReportedAndLoopContinues(t *testing.T) {
	relay := &fakeRelay{rounds: []string{
		relayToolRound(t, "", "call_x", "no_such_tool", map[string]any{"a": 1}),
		relayTextRound(t, "那我直接回答。"),
	}}
	relayServer := httptest.NewServer(relay.handler())
	t.Cleanup(relayServer.Close)

	env := newHandlerEnv(t, func(p *envParams) {
		p.gateway = promptgen.NewClient(relayServer.URL, "sk-relay")
		p.agentTools = promptgen.NewAgentToolRegistry()
	})

	_, raw := env.do(t, http.MethodPost, "/canvases/7/agent/stream", map[string]any{
		"node_id": "agent-1-1", "topic": "建角色", "model": "chat-m",
	})
	var sawUnknown, sawDone bool
	for _, payload := range sseFrames(t, raw) {
		m := frameMap(t, payload)
		if m == nil {
			continue
		}
		if m["type"] == "tool_result" {
			sawUnknown = strings.Contains(m["summary"].(string), "未知")
		}
		if m["type"] == "done" {
			sawDone = m["text"] == "那我直接回答。"
		}
	}
	if !sawUnknown {
		t.Error("no explicit unknown-tool tool_result frame")
	}
	if !sawDone {
		t.Error("no done frame after the unknown tool round")
	}
	msgs, _ := relay.request(1).Body["messages"].([]any)
	toolTurn, _ := msgs[len(msgs)-1].(map[string]any)
	content, _ := toolTurn["content"].(string)
	if toolTurn["role"] != "tool" || !strings.Contains(content, "unknown tool") {
		t.Errorf("tool frame = %v, want the unknown-tool error fed back", toolTurn)
	}
}

// 断线中断:客户端取消后循环不再发起下一轮 /v1 调用,已流出内容保留
// (无 done、无 [DONE])。
func TestAgentStreamAbortsOnClientDisconnect(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	tool := &stubAgentTool{name: "block_tool", summary: "s", payload: `null`}
	tool.execHook = func() {
		close(entered)
		<-release
	}
	registry := promptgen.NewAgentToolRegistry()
	registry.Register(tool)

	relay := &fakeRelay{rounds: []string{
		relayToolRound(t, "", "call_1", "block_tool", map[string]any{"x": 1}),
	}}
	relayServer := httptest.NewServer(relay.handler())
	t.Cleanup(relayServer.Close)

	env := newHandlerEnv(t, func(p *envParams) {
		p.gateway = promptgen.NewClient(relayServer.URL, "sk-relay")
		p.agentTools = registry
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	body := strings.NewReader(`{"node_id":"agent-1-1","topic":"建角色","model":"chat-m"}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, env.server.URL+"/canvases/7/agent/stream", body)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := env.server.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer res.Body.Close()

	// 逐帧读到 tool_call(此刻工具已挂起),然后断线。
	reader := bufio.NewReader(res.Body)
	sawToolCall := false
	for !sawToolCall {
		line, rErr := reader.ReadString('\n')
		if rErr != nil {
			t.Fatalf("read stream before tool_call: %v", rErr)
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
		if strings.HasPrefix(line, "data: ") && strings.Contains(payload, "tool_call") {
			sawToolCall = true
		}
	}
	cancel()
	close(release)

	// 读到 EOF:服务端处理完(循环中断、handler 返回)连接才关。
	if _, err := io.ReadAll(reader); err != nil {
		t.Logf("tail read: %v", err)
	}
	if relay.count() != 1 {
		t.Errorf("relay calls = %d, want the loop to stop before round 2", relay.count())
	}
}
