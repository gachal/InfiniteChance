package promptgen

// 63 号票 Agent 工具循环的执行器单测:白盒直调 runAgentLoop,网关用脚本化
// 桩(每轮回放配置好的 StreamTurn),工具用可编程桩。SSE 端到端(真
// Client + httptest 假网关)在 agentstream_test.go。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// ---- fakes ----

// scriptTurner 按轮次脚本回放:第 n 轮调用 rounds(n) 取得该轮的增量与
// 轮次结果;全部请求原文记入 requests 供断言。
type scriptTurner struct {
	mu       sync.Mutex
	requests []ChatRequest
	rounds   func(n int, req ChatRequest, onDelta func(string)) (StreamTurn, error)
}

func (s *scriptTurner) StreamChatTurn(_ context.Context, req ChatRequest, onDelta func(string)) (StreamTurn, error) {
	s.mu.Lock()
	n := len(s.requests)
	s.requests = append(s.requests, req)
	s.mu.Unlock()
	return s.rounds(n, req, onDelta)
}

func (s *scriptTurner) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

// stubTool 是工具注册表桩:定义固定,Execute 行为可编程。
type stubTool struct {
	name    string
	def     string
	summary string
	payload string
	err     error

	mu       sync.Mutex
	gotArgs  []string
	execHook func() // Execute 前回调(断线测试用来挂起)
}

func (t *stubTool) Name() string { return t.name }

func (t *stubTool) Definition() json.RawMessage { return json.RawMessage(t.def) }

func (t *stubTool) Execute(_ context.Context, args json.RawMessage) (string, json.RawMessage, error) {
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

func (t *stubTool) args() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.gotArgs...)
}

// textTurn / toolTurn 构造轮次结果。
func textTurn(text string) StreamTurn {
	return StreamTurn{Content: text, FinishReason: "stop"}
}

func toolTurn(id, name, args string) StreamTurn {
	return StreamTurn{
		ToolCalls:    []ToolCall{{ID: id, Name: name, Arguments: args}},
		FinishReason: "tool_calls",
	}
}

// collectEvents 把 emit 事件收进切片。
func collectEvents() (func(AgentEvent), *[]AgentEvent) {
	var mu sync.Mutex
	events := &[]AgentEvent{}
	return func(ev AgentEvent) {
		mu.Lock()
		defer mu.Unlock()
		*events = append(*events, ev)
	}, events
}

func (ev AgentEvent) typeName() string { return ev.Type }

// loopMessages 是各测试共用的会话起点:一条 user 指令。
func loopMessages() []ChatMessage {
	return []ChatMessage{{Role: "user", Content: "建一个角色"}}
}

const stubToolDef = `{"type":"function","function":{"name":"echo_tool","description":"测试工具","parameters":{"type":"object","properties":{"name":{"type":"string"}}}}}`

// ---- 用例 ----

// 纯文本回答:一轮流式补全,delta 逐帧透出,done 带全文与空摘要,不再
// 发起第二轮。
func TestAgentLoopEmitsDoneOnPlainAnswer(t *testing.T) {
	turner := &scriptTurner{rounds: func(n int, _ ChatRequest, onDelta func(string)) (StreamTurn, error) {
		if n != 0 {
			return StreamTurn{}, fmt.Errorf("unexpected round %d", n)
		}
		onDelta("你好")
		onDelta("!")
		return textTurn("你好!"), nil
	}}
	emit, events := collectEvents()

	err := runAgentLoop(context.Background(), turner, NewAgentToolRegistry(), ChatRequest{Model: "chat-m"}, loopMessages(), emit)
	if err != nil {
		t.Fatalf("runAgentLoop: %v", err)
	}
	if turner.count() != 1 {
		t.Fatalf("gateway calls = %d, want exactly one round", turner.count())
	}
	got := *events
	if len(got) != 3 {
		t.Fatalf("events = %d, want 2 delta + 1 done", len(got))
	}
	if got[0].typeName() != "delta" || got[0].Text != "你好" || got[1].typeName() != "delta" || got[1].Text != "!" {
		t.Errorf("delta events = %+v / %+v, want the two text deltas", got[0], got[1])
	}
	done := got[2]
	if done.typeName() != "done" || done.Text != "你好!" || done.Summary != "" {
		t.Errorf("done = %+v, want full text with empty summary", done)
	}
}

// 一轮工具调用:tool_call 事件 → 工具执行 → tool_result 事件(带 payload)→
// 以 assistant tool_calls 帧 + role=tool 帧续请求 → 文本轮 done,摘要行组装。
func TestAgentLoopRunsToolThenFinalAnswer(t *testing.T) {
	tool := &stubTool{name: "echo_tool", def: stubToolDef, summary: "已创建 张三", payload: `{"draft":{"x":1}}`}
	registry := NewAgentToolRegistry()
	registry.Register(tool)

	turner := &scriptTurner{rounds: func(n int, _ ChatRequest, onDelta func(string)) (StreamTurn, error) {
		switch n {
		case 0:
			onDelta("让我先建角色。")
			return toolTurn("call_1", "echo_tool", `{"name":"张三"}`), nil
		case 1:
			onDelta("完成了。")
			return textTurn("完成了。"), nil
		default:
			return StreamTurn{}, fmt.Errorf("unexpected round %d", n)
		}
	}}
	emit, events := collectEvents()

	err := runAgentLoop(context.Background(), turner, registry, ChatRequest{Model: "chat-m", Source: "canvas=7 gen=agent"}, loopMessages(), emit)
	if err != nil {
		t.Fatalf("runAgentLoop: %v", err)
	}
	if got := tool.args(); len(got) != 1 || got[0] != `{"name":"张三"}` {
		t.Errorf("tool args = %v, want the raw call arguments", got)
	}

	got := *events
	var toolCall, toolResult, done *AgentEvent
	for i := range got {
		switch got[i].Type {
		case "tool_call":
			toolCall = &got[i]
		case "tool_result":
			toolResult = &got[i]
		case "done":
			done = &got[i]
		}
	}
	if toolCall == nil || toolCall.ID != "call_1" || toolCall.Name != "echo_tool" {
		t.Fatalf("tool_call event = %+v, want the invocation surfaced", toolCall)
	}
	if string(toolCall.Args) != `{"name":"张三"}` {
		t.Errorf("tool_call.args = %s, want the parsed arguments", toolCall.Args)
	}
	if toolResult == nil || toolResult.Summary != "已创建 张三" || string(toolResult.Payload) != `{"draft":{"x":1}}` {
		t.Fatalf("tool_result event = %+v, want summary and payload surfaced", toolResult)
	}
	if done == nil || done.Text != "完成了。" {
		t.Fatalf("done event = %+v, want the final answer", done)
	}
	if done.Summary != "本轮工具调用:echo_tool(已创建 张三)" {
		t.Errorf("done.summary = %q, want the composed tool line", done.Summary)
	}

	// 续请求的形状:tools 照带,messages 追加 assistant 工具帧与 tool 结果帧。
	if turner.count() != 2 {
		t.Fatalf("gateway calls = %d, want 2", turner.count())
	}
	second := turner.requests[1]
	if len(second.Tools) != 1 || second.ToolChoice != "auto" {
		t.Fatalf("second round tools = %v / choice = %q, want the definitions riding along", second.Tools, second.ToolChoice)
	}
	msgs := second.Conversation
	if len(msgs) != 3 {
		t.Fatalf("second round messages = %d, want user + assistant + tool", len(msgs))
	}
	if msgs[1].Role != "assistant" || len(msgs[1].ToolCalls) != 1 || msgs[1].ToolCalls[0].ID != "call_1" {
		t.Errorf("assistant frame = %+v, want the tool_calls turn", msgs[1])
	}
	if msgs[2].Role != "tool" || msgs[2].ToolCallID != "call_1" {
		t.Errorf("tool frame = %+v, want the result turn bound to call_1", msgs[2])
	}
	var toolContent map[string]any
	if err := json.Unmarshal([]byte(msgs[2].Content), &toolContent); err != nil {
		t.Fatalf("tool frame content not JSON: %v (%q)", err, msgs[2].Content)
	}
	if toolContent["summary"] != "已创建 张三" || toolContent["payload"] == nil {
		t.Errorf("tool frame content = %v, want summary + payload fed back", toolContent)
	}
}

// 15 轮护栏:第 15 个工具轮执行完注入收尾指令,收尾轮不带 tools、强制
// 纯文本总结,该轮文本即 done。
func TestAgentLoopGuardrailWrapUpAt15ToolRounds(t *testing.T) {
	tool := &stubTool{name: "echo_tool", def: stubToolDef, summary: "完成一步", payload: `null`}
	registry := NewAgentToolRegistry()
	registry.Register(tool)

	turner := &scriptTurner{rounds: func(n int, _ ChatRequest, onDelta func(string)) (StreamTurn, error) {
		if n < agentMaxToolRounds {
			return toolTurn(fmt.Sprintf("call_%d", n), "echo_tool", "{}"), nil
		}
		onDelta("已完成 15 步,请继续。")
		return textTurn("已完成 15 步,请继续。"), nil
	}}
	emit, events := collectEvents()

	err := runAgentLoop(context.Background(), turner, registry, ChatRequest{Model: "chat-m"}, loopMessages(), emit)
	if err != nil {
		t.Fatalf("runAgentLoop: %v", err)
	}
	if turner.count() != agentMaxToolRounds+1 {
		t.Fatalf("gateway calls = %d, want 15 tool rounds + 1 wrap-up", turner.count())
	}
	// 前 15 轮都带 tools;收尾轮不带。
	for i, req := range turner.requests {
		if i < agentMaxToolRounds && len(req.Tools) != 1 {
			t.Fatalf("round %d tools = %v, want definitions present", i, req.Tools)
		}
		if i == agentMaxToolRounds {
			if len(req.Tools) != 0 || req.ToolChoice != "" {
				t.Fatalf("wrap-up round carries tools = %v / choice = %q, want none", req.Tools, req.ToolChoice)
			}
			msgs := req.Conversation
			last := msgs[len(msgs)-1]
			if last.Role != "user" || last.Content != agentWrapupInstruction {
				t.Fatalf("wrap-up round last message = %+v, want the injected wrap-up instruction", last)
			}
		}
	}
	toolCalls, toolResults := 0, 0
	for _, ev := range *events {
		switch ev.Type {
		case "tool_call":
			toolCalls++
		case "tool_result":
			toolResults++
		case "done":
			if ev.Text != "已完成 15 步,请继续。" {
				t.Errorf("done.text = %q, want the wrap-up answer", ev.Text)
			}
		}
	}
	if toolCalls != agentMaxToolRounds || toolResults != agentMaxToolRounds {
		t.Errorf("tool events = %d calls / %d results, want 15 each", toolCalls, toolResults)
	}
}

// 未知工具:不炸会话——tool_result 显式报告失败,模型收到错误回执后可改
// 调已注册工具。
func TestAgentLoopUnknownToolInformsModelAndContinues(t *testing.T) {
	registry := NewAgentToolRegistry()
	turner := &scriptTurner{rounds: func(n int, _ ChatRequest, onDelta func(string)) (StreamTurn, error) {
		switch n {
		case 0:
			return toolTurn("call_x", "no_such_tool", `{"a":1}`), nil
		case 1:
			return textTurn("那我直接回答。"), nil
		default:
			return StreamTurn{}, fmt.Errorf("unexpected round %d", n)
		}
	}}
	emit, events := collectEvents()

	if err := runAgentLoop(context.Background(), turner, registry, ChatRequest{Model: "chat-m"}, loopMessages(), emit); err != nil {
		t.Fatalf("runAgentLoop: %v", err)
	}
	var toolResult *AgentEvent
	for i := range *events {
		if (*events)[i].Type == "tool_result" {
			toolResult = &(*events)[i]
		}
	}
	if toolResult == nil || toolResult.Name != "no_such_tool" || !strings.Contains(toolResult.Summary, "未知") {
		t.Fatalf("tool_result = %+v, want an explicit unknown-tool report", toolResult)
	}
	msgs := turner.requests[1].Conversation
	last := msgs[len(msgs)-1]
	if last.Role != "tool" || !strings.Contains(last.Content, "unknown tool") {
		t.Errorf("tool frame = %+v, want the unknown-tool error fed back to the model", last)
	}
}

// 工具执行失败:tool_result 报告失败原因,循环继续(模型可自行纠正)。
func TestAgentLoopToolFailureFeedsErrorBackAndContinues(t *testing.T) {
	tool := &stubTool{name: "echo_tool", def: stubToolDef, err: errors.New("库存不足")}
	registry := NewAgentToolRegistry()
	registry.Register(tool)
	turner := &scriptTurner{rounds: func(n int, _ ChatRequest, onDelta func(string)) (StreamTurn, error) {
		switch n {
		case 0:
			return toolTurn("call_1", "echo_tool", "{}"), nil
		case 1:
			return textTurn("库存不足,换一个吧。"), nil
		default:
			return StreamTurn{}, fmt.Errorf("unexpected round %d", n)
		}
	}}
	emit, events := collectEvents()

	if err := runAgentLoop(context.Background(), turner, registry, ChatRequest{Model: "chat-m"}, loopMessages(), emit); err != nil {
		t.Fatalf("runAgentLoop: %v", err)
	}
	var toolResult *AgentEvent
	for i := range *events {
		if (*events)[i].Type == "tool_result" {
			toolResult = &(*events)[i]
		}
	}
	if toolResult == nil || !strings.Contains(toolResult.Summary, "库存不足") {
		t.Fatalf("tool_result = %+v, want the failure surfaced in the summary", toolResult)
	}
	msgs := turner.requests[1].Conversation
	last := msgs[len(msgs)-1]
	var content map[string]any
	if err := json.Unmarshal([]byte(last.Content), &content); err != nil || content["error"] != "库存不足" {
		t.Errorf("tool frame content = %v, want the error fed back", last.Content)
	}
}

// 上游中途失败:error 事件 + 返回错误,不再续轮。
func TestAgentLoopSurfacesUpstreamFailure(t *testing.T) {
	turner := &scriptTurner{rounds: func(n int, _ ChatRequest, _ func(string)) (StreamTurn, error) {
		return StreamTurn{}, errors.New("gateway 502: 上游断了")
	}}
	emit, events := collectEvents()

	err := runAgentLoop(context.Background(), turner, NewAgentToolRegistry(), ChatRequest{Model: "chat-m"}, loopMessages(), emit)
	if err == nil {
		t.Fatal("err = nil, want the gateway failure")
	}
	var errorEvent *AgentEvent
	for i := range *events {
		if (*events)[i].Type == "error" {
			errorEvent = &(*events)[i]
		}
	}
	if errorEvent == nil || errorEvent.Error == nil || errorEvent.Error.Code != "upstream_error" {
		t.Fatalf("error event = %+v, want upstream_error with the reason", errorEvent)
	}
	if !strings.Contains(errorEvent.Error.Message, "上游断了") {
		t.Errorf("error message = %q, want the gateway reason", errorEvent.Error.Message)
	}
}

// 断线中断:ctx 取消后循环在下一轮前停下,不再发起网关调用,已发出事件
// 保留、无 done。
func TestAgentLoopAbortsWhenContextCanceled(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	tool := &stubTool{name: "block_tool", def: stubToolDef, summary: "s", payload: `null`}
	tool.execHook = func() {
		close(entered)
		<-release
	}
	registry := NewAgentToolRegistry()
	registry.Register(tool)

	turner := &scriptTurner{rounds: func(n int, _ ChatRequest, _ func(string)) (StreamTurn, error) {
		return toolTurn("call_1", "block_tool", "{}"), nil
	}}
	emit, events := collectEvents()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runAgentLoop(ctx, turner, registry, ChatRequest{Model: "chat-m"}, loopMessages(), emit)
	}()

	<-entered // 工具已在执行:此刻断线
	cancel()
	close(release)

	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if turner.count() != 1 {
		t.Errorf("gateway calls = %d, want the loop to stop before round 2", turner.count())
	}
	for _, ev := range *events {
		if ev.Type == "done" {
			t.Errorf("done event emitted on a canceled run: %+v", ev)
		}
	}
}

// 工具定义的注册序即暴露序(决定 LLM 看到的 tools 数组顺序)。
func TestAgentToolRegistryDefinitionsInRegistrationOrder(t *testing.T) {
	registry := NewAgentToolRegistry()
	registry.Register(&stubTool{name: "b_tool", def: `{"type":"function","function":{"name":"b_tool"}}`})
	registry.Register(&stubTool{name: "a_tool", def: `{"type":"function","function":{"name":"a_tool"}}`})

	defs := registry.Definitions()
	if len(defs) != 2 {
		t.Fatalf("definitions = %d, want 2", len(defs))
	}
	var names []string
	for _, d := range defs {
		var parsed struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		}
		if err := json.Unmarshal(d, &parsed); err != nil {
			t.Fatalf("definition %s not JSON: %v", d, err)
		}
		names = append(names, parsed.Function.Name)
	}
	if names[0] != "b_tool" || names[1] != "a_tool" {
		t.Errorf("order = %v, want registration order", names)
	}
	if _, ok := registry.Lookup("a_tool"); !ok {
		t.Error("lookup a_tool failed")
	}
	if _, ok := registry.Lookup("missing"); ok {
		t.Error("lookup missing unexpectedly succeeded")
	}
	// 空注册表不带任何定义。
	if empty := NewAgentToolRegistry().Definitions(); len(empty) != 0 {
		t.Errorf("empty registry definitions = %v, want none", empty)
	}
}
