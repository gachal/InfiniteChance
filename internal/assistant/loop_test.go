package assistant

// 助手循环测试(91 号票验证点「SSE + 回执闭环」的循环层):工具草案 →
// 前端应用(测试里模拟回执)→ 续循环;拒绝路径如实喂回;回执超时中断。
// 全链 SSE(handler + store + fakeRelay)见 handler_test.go。

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gachal/InfiniteChance/internal/promptgen"
)

// scriptTurner 是逐轮脚本化的流式网关桩(照 promptgen agentloop_test 同族):
// 每轮消耗一条脚本,请求留档断言。
type scriptTurner struct {
	mu       sync.Mutex
	rounds   []promptgen.StreamTurn
	requests []promptgen.ChatRequest
}

func (s *scriptTurner) StreamChatTurn(_ context.Context, req promptgen.ChatRequest, _ func(string)) (promptgen.StreamTurn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, req)
	if len(s.requests) > len(s.rounds) {
		return promptgen.StreamTurn{}, context.DeadlineExceeded
	}
	return s.rounds[len(s.requests)-1], nil
}

func toolTurn(id, name string, args string) promptgen.StreamTurn {
	return promptgen.StreamTurn{ToolCalls: []promptgen.ToolCall{{ID: id, Name: name, Arguments: args}}, FinishReason: "tool_calls"}
}

func textTurn(text string) promptgen.StreamTurn {
	return promptgen.StreamTurn{Content: text, FinishReason: "stop"}
}

type recordedEvent struct {
	typeName string
	summary  string
	payload  json.RawMessage
	code     string
}

func collectEvents(t *testing.T, run func(emit func(promptgen.AgentEvent)) error) ([]recordedEvent, error) {
	t.Helper()
	var events []recordedEvent
	err := run(func(ev promptgen.AgentEvent) {
		events = append(events, recordedEvent{typeName: ev.Type, summary: ev.Summary, payload: ev.Payload, code: errCode(ev)})
	})
	return events, err
}

func errCode(ev promptgen.AgentEvent) string {
	if ev.Error != nil {
		return ev.Error.Code
	}
	return ""
}

// 主干:草案工具挂起 → 测试模拟前端应用(发回执,带新节点 id)→ 循环
// 续跑 → done。断言帧序、role=tool 帧带回执结果、done 摘要。
func TestAssistantLoopDraftReceiptClosesTheLoop(t *testing.T) {
	turner := &scriptTurner{rounds: []promptgen.StreamTurn{
		toolTurn("call_1", "create_nodes", `{"nodes":[{"type":"image","prompt":"主角立绘"}]}`),
		textTurn("已新建图片节点,节点 id 是 image-9。"),
	}}

	hub := NewHub()
	registry := NewRegistry(Deps{Hub: hub})

	// 模拟前端:读到 tool_call 后应用并发回执。
	go func() {
		for {
			time.Sleep(5 * time.Millisecond)
			hub.mu.Lock()
			_, pending := hub.pending[1]
			hub.mu.Unlock()
			if pending {
				_ = hub.Deliver(1, Receipt{CallID: "call_1", Status: ReceiptApplied,
					Result: []byte(`{"nodes":[{"index":0,"id":"image-9"}]}`)})
				return
			}
		}
	}()

	events, err := collectEvents(t, func(emit func(promptgen.AgentEvent)) error {
		return runAssistantLoop(context.Background(), turner, registry, hub, 1,
			promptgen.ChatRequest{Model: "chat-m", Source: "canvas=7 gen=assistant"},
			[]promptgen.ChatMessage{{Role: "user", Content: "帮我加一个主角立绘节点"}}, emit)
	})
	if err != nil {
		t.Fatalf("loop: %v", err)
	}

	// 帧序:delta(无,首轮无文本)→ tool_call → tool_result → delta → done。
	var types []string
	for _, ev := range events {
		types = append(types, ev.typeName)
	}
	if len(types) != 3 || types[0] != "tool_call" || types[1] != "tool_result" || types[2] != "done" {
		t.Fatalf("event types = %v, want [tool_call tool_result done]", types)
	}
	if events[1].summary != "已应用" || !strings.Contains(string(events[1].payload), "image-9") {
		t.Fatalf("tool_result = %q %s, want applied summary carrying the new node id", events[1].summary, events[1].payload)
	}
	done := events[2]
	if done.summary != "本轮工具调用:create_nodes(已应用)" {
		t.Errorf("done summary = %q, want the composed tool line", done.summary)
	}

	// 续请求形状:assistant(tool_calls) + tool 帧,tool 帧内容 = 回执反馈。
	if len(turner.requests) != 2 {
		t.Fatalf("relay calls = %d, want 2", len(turner.requests))
	}
	msgs := turner.requests[1].Conversation
	if len(msgs) != 3 || msgs[1].ToolCalls == nil {
		t.Fatalf("round-2 messages = %v, want assistant+tool frames", msgs)
	}
	toolFrame := msgs[2]
	if toolFrame.Role != "tool" || toolFrame.ToolCallID != "call_1" {
		t.Fatalf("tool frame = %+v, want call_1 binding", toolFrame)
	}
	if !strings.Contains(toolFrame.Content, "已应用") || !strings.Contains(toolFrame.Content, "image-9") {
		t.Errorf("tool feedback = %q, want the receipt result fed to the model", toolFrame.Content)
	}
}

// 拒绝路径:「先询问我」档下用户拒绝 → 回执如实告知 LLM,循环继续。
func TestAssistantLoopReceiptRejectFeedsModel(t *testing.T) {
	turner := &scriptTurner{rounds: []promptgen.StreamTurn{
		toolTurn("call_r", "delete_nodes", `{"ids":["image-1"]}`),
		textTurn("好的,已保留这些节点。"),
	}}
	hub := NewHub()
	registry := NewRegistry(Deps{Hub: hub})

	go func() {
		for {
			time.Sleep(5 * time.Millisecond)
			hub.mu.Lock()
			_, pending := hub.pending[1]
			hub.mu.Unlock()
			if pending {
				_ = hub.Deliver(1, Receipt{CallID: "call_r", Status: ReceiptRejected, Message: "这批还要用"})
				return
			}
		}
	}()

	events, err := collectEvents(t, func(emit func(promptgen.AgentEvent)) error {
		return runAssistantLoop(context.Background(), turner, registry, hub, 1,
			promptgen.ChatRequest{Model: "chat-m"},
			[]promptgen.ChatMessage{{Role: "user", Content: "把节点删掉"}}, emit)
	})
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	var result recordedEvent
	for _, ev := range events {
		if ev.typeName == "tool_result" {
			result = ev
		}
	}
	if !strings.Contains(result.summary, "用户拒绝了这批变更") || !strings.Contains(result.summary, "这批还要用") {
		t.Fatalf("tool_result summary = %q, want the honest rejection", result.summary)
	}
	toolFrame := turner.requests[1].Conversation[2]
	var feedback map[string]string
	if err := json.Unmarshal([]byte(toolFrame.Content), &feedback); err != nil {
		t.Fatalf("feedback not JSON: %v (%s)", err, toolFrame.Content)
	}
	if !strings.Contains(feedback["error"], "用户拒绝了这批变更") {
		t.Errorf("feedback = %v, want rejection fed back as the model-visible reason", feedback)
	}
}

// 应用失败路径:前端应用失败(如 EDGE_MATRIX 拒绝)→ 错误喂回模型,
// 循环继续由模型纠正。
func TestAssistantLoopReceiptFailureFeedsModel(t *testing.T) {
	turner := &scriptTurner{rounds: []promptgen.StreamTurn{
		toolTurn("call_f", "connect_nodes", `{"edges":[{"source":"a","target":"b"}]}`),
		textTurn("这条连线不允许,我换个方案。"),
	}}
	hub := NewHub()
	registry := NewRegistry(Deps{Hub: hub})

	go func() {
		for {
			time.Sleep(5 * time.Millisecond)
			hub.mu.Lock()
			_, pending := hub.pending[1]
			hub.mu.Unlock()
			if pending {
				_ = hub.Deliver(1, Receipt{CallID: "call_f", Status: ReceiptFailed, Message: "analysis 节点不能作为连线的终点"})
				return
			}
		}
	}()

	_, err := collectEvents(t, func(emit func(promptgen.AgentEvent)) error {
		return runAssistantLoop(context.Background(), turner, registry, hub, 1,
			promptgen.ChatRequest{Model: "chat-m"},
			[]promptgen.ChatMessage{{Role: "user", Content: "连一下"}}, emit)
	})
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	toolFrame := turner.requests[1].Conversation[2]
	if !strings.Contains(toolFrame.Content, "analysis 节点不能作为连线的终点") {
		t.Errorf("tool feedback = %q, want the failure reason verbatim", toolFrame.Content)
	}
}

// 回执超时:挂起工具等满注入的短超时 → 循环以 receipt_timeout 错误中断
// (error 帧如实报告,无 done),挂起清理。
func TestAssistantLoopReceiptTimeoutAborts(t *testing.T) {
	turner := &scriptTurner{rounds: []promptgen.StreamTurn{
		toolTurn("call_t", "update_nodes", `{"nodes":[{"id":"a","prompt":"x"}]}`),
	}}
	hub := NewHub()
	hub.timeout = 30 * time.Millisecond
	registry := NewRegistry(Deps{Hub: hub})

	var gotCode string
	err := runAssistantLoop(context.Background(), turner, registry, hub, 1,
		promptgen.ChatRequest{Model: "chat-m"},
		[]promptgen.ChatMessage{{Role: "user", Content: "改"}}, func(ev promptgen.AgentEvent) {
			if ev.Type == "error" && ev.Error != nil {
				gotCode = ev.Error.Code
			}
		})
	if !strings.Contains(err.Error(), "回执等待超时") {
		t.Fatalf("err = %v, want the receipt timeout", err)
	}
	if gotCode != "receipt_timeout" {
		t.Errorf("error frame code = %q, want receipt_timeout", gotCode)
	}
	hub.mu.Lock()
	_, pending := hub.pending[1]
	hub.mu.Unlock()
	if pending {
		t.Error("pending call not cleared after timeout abort")
	}
	// 中断后没有第二轮 /v1。
	if len(turner.requests) != 1 {
		t.Errorf("relay calls = %d, want the loop to stop at round 1", len(turner.requests))
	}
}

// 形状错误的参数不挂起:草案校验失败直接以错误回执喂回,循环继续
// (模型可自行纠正),前端永远看不到坏卡。
func TestAssistantLoopMalformedArgsDoNotBlock(t *testing.T) {
	turner := &scriptTurner{rounds: []promptgen.StreamTurn{
		toolTurn("call_m", "create_nodes", `{"nodes":[{"type":"music"}]}`),
		textTurn("好的,我改用合法类型。"),
	}}
	hub := NewHub()
	registry := NewRegistry(Deps{Hub: hub})

	events, err := collectEvents(t, func(emit func(promptgen.AgentEvent)) error {
		return runAssistantLoop(context.Background(), turner, registry, hub, 1,
			promptgen.ChatRequest{Model: "chat-m"},
			[]promptgen.ChatMessage{{Role: "user", Content: "建一个"}}, emit)
	})
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	for _, ev := range events {
		if ev.typeName == "tool_result" && !strings.Contains(ev.summary, "不是合法的节点类型") {
			t.Errorf("tool_result = %q, want the validation reason", ev.summary)
		}
	}
	// 没有挂起登记(校验先行,失败不进等待)。
	hub.mu.Lock()
	_, pending := hub.pending[1]
	hub.mu.Unlock()
	if pending {
		t.Error("malformed call registered a pending receipt")
	}
}

// 同步工具(read_canvas 经 fakeCanvases / stub)在循环里照常同步完成,
// 不登记回执。
func TestAssistantLoopSyncToolRunsInline(t *testing.T) {
	turner := &scriptTurner{rounds: []promptgen.StreamTurn{
		toolTurn("call_s", "read_canvas", `{}`),
		textTurn("画布是空的。"),
	}}
	hub := NewHub()
	registry := NewRegistry(Deps{
		Hub:      hub,
		Canvases: fakeCanvases{graph: map[int64][]byte{7: []byte(`{"nodes":[],"edges":[]}`)}},
	})

	events, err := collectEvents(t, func(emit func(promptgen.AgentEvent)) error {
		ctx := promptgen.WithAgentScope(context.Background(), promptgen.AgentScope{CanvasID: 7, IPID: 3})
		return runAssistantLoop(ctx, turner, registry, hub, 1,
			promptgen.ChatRequest{Model: "chat-m"},
			[]promptgen.ChatMessage{{Role: "user", Content: "看看画布"}}, emit)
	})
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	for _, ev := range events {
		if ev.typeName == "tool_result" && !strings.Contains(ev.summary, "画布是空的") {
			t.Errorf("tool_result = %q, want the read_canvas summary", ev.summary)
		}
	}
	hub.mu.Lock()
	_, pending := hub.pending[1]
	hub.mu.Unlock()
	if pending {
		t.Error("sync tool registered a pending receipt")
	}
}
