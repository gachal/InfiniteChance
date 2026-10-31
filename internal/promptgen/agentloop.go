package promptgen

// Agent 工具循环执行器(63 号票):逐轮带 tools 请求网关聊天面,模型发起
// tool_calls 就按序执行、以 role=tool 消息续请求,直至模型给出纯文本回答
// 或触 15 轮护栏(注入收尾指令强制总结并指引用户续跑)。事件按
// delta/tool_call/tool_result/done/error 五形状经 emit 推给 SSE 端点
// (ADR 0006 的事件回传契约);持久历史纪律由端点保证——循环中间帧只在
// 本函数的局部 messages 里,不入节点 data。

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// agentMaxToolRounds 是单次请求的工具轮上限(63 号票定案 6):触顶注入收尾
// 指令强制 LLM 总结已完成事项并指引用户续跑,保证请求必然有界收敛。
const agentMaxToolRounds = 15

// agentWrapupInstruction 是护栏触顶后的收尾指令(以 user 消息注入):收尾
// 轮不再带 tools,模型只能用文本总结收场。
const agentWrapupInstruction = "你已连续完成 15 轮工具调用,达到单次请求的上限。" +
	"请立即停止调用工具,总结本轮已完成的事项,并告诉用户如何继续(例如再次发送指令即可续跑)。"

// AgentEvent is one SSE event of the Agent tool loop (ADR 0006 事件协议,
// 前后端契约):delta/tool_call/tool_result/done/error 五形状,字段按类型
// 各取所需(omitempty 折叠)。
type AgentEvent struct {
	Type string `json:"type"` // delta | tool_call | tool_result | done | error
	// delta:文本增量;done:最终回答全文(整个循环各轮文本按序拼接)。
	Text string `json:"text,omitempty"`
	// tool_call / tool_result:该次调用的 id 与工具名。
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	// tool_call:调用参数(模型 arguments 的 JSON 原文,解析失败则缺省)。
	Args json.RawMessage `json:"args,omitempty"`
	// tool_result:人读摘要与结构化 payload(写图类 = 前端应用草案,
	// ADR 0006;payload 为 null 表示无前端可应用物)。
	Summary string          `json:"summary,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
	// error:流中失败形状,与网关 OpenAI error object 同族。
	Error *AgentEventError `json:"error,omitempty"`
}

// AgentEventError is the error payload of an error event.
type AgentEventError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// AgentStreamTurner is the slice of the gateway client the loop needs: one
// streamed chat completion per round, text deltas relayed as they arrive.
type AgentStreamTurner interface {
	StreamChatTurn(ctx context.Context, req ChatRequest, onDelta func(string)) (StreamTurn, error)
}

// runAgentLoop drives one Agent session (63 号票):messages 是会话起点
// (首条指令 + 历史 + 本轮输入,由端点按既有规则渲染),base 携带模型与
// 来源标记(每轮 /v1 调用各自独立计费)。事件经 emit 推出;成功时已推
// done、返回 nil。ctx 取消(浏览器断线)= 循环中断,返回 ctx 错误且不再
// 推事件——已流出内容保留在客户端。
func runAgentLoop(
	ctx context.Context,
	gateway AgentStreamTurner,
	registry *AgentToolRegistry,
	base ChatRequest,
	messages []ChatMessage,
	emit func(AgentEvent),
) error {
	var tools []json.RawMessage
	if registry != nil {
		tools = registry.Definitions()
	}

	var texts []string      // 各轮文本(中间轮的解说 + 最终回答),done 按序拼接
	var toolLine []string   // 工具调用摘要(人读),done.summary 组装用
	wrapup := false         // 护栏触顶:下一轮进入收尾(不带 tools,只收文本)

	for round := 1; ; round++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		req := base
		req.Conversation = messages
		if len(tools) > 0 && !wrapup {
			req.Tools = tools
			req.ToolChoice = "auto"
		}
		turn, err := gateway.StreamChatTurn(ctx, req, func(delta string) {
			emit(AgentEvent{Type: "delta", Text: delta})
		})
		if err != nil {
			emit(AgentEvent{Type: "error", Error: &AgentEventError{Code: "upstream_error", Message: err.Error()}})
			return err
		}
		if turn.Content != "" {
			texts = append(texts, turn.Content)
		}
		// 纯文本轮(或收尾轮)= 会话完成。
		if len(turn.ToolCalls) == 0 || wrapup {
			emit(AgentEvent{Type: "done", Text: strings.Join(texts, "\n\n"), Summary: composeToolSummary(toolLine)})
			return nil
		}

		// 工具轮:assistant 帧(带 tool_calls)入会话,按序执行并回填。
		messages = append(messages, ChatMessage{Role: "assistant", Content: turn.Content, ToolCalls: turn.ToolCalls})
		for _, tc := range turn.ToolCalls {
			// 调用开始即播报(前端「正在创建角色…」式过程展示的触发帧);
			// 残缺参数不上 SSE,args 缺省由前端按无参展示。
			callArgs := json.RawMessage(tc.Arguments)
			if !json.Valid(callArgs) {
				callArgs = nil
			}
			emit(AgentEvent{Type: "tool_call", ID: tc.ID, Name: tc.Name, Args: callArgs})
			summary, payload, result := executeToolCall(ctx, registry, tc)
			if ctx.Err() != nil {
				// 断线发生在工具执行中:整个循环就此中断,已流出内容保留。
				return ctx.Err()
			}
			emit(AgentEvent{Type: "tool_result", ID: tc.ID, Name: tc.Name, Summary: summary, Payload: payload})
			messages = append(messages, ChatMessage{Role: "tool", ToolCallID: tc.ID, Content: result})
			toolLine = append(toolLine, toolLineItem(tc.Name, summary))
		}

		// 护栏:第 15 个工具轮执行完即注入收尾指令,下一轮强制纯文本收场。
		if round >= agentMaxToolRounds {
			wrapup = true
			messages = append(messages, ChatMessage{Role: "user", Content: agentWrapupInstruction})
		}
	}
}

// executeToolCall runs one invocation (63 号票):未知工具与执行失败都不炸
// 会话——结果以错误回执(feed 的 JSON)喂回模型,模型可自行纠正;摘要
// 面向人,如实描述发生了什么。返回 (summary, payload, 喂回模型的结果文本)。
func executeToolCall(ctx context.Context, registry *AgentToolRegistry, tc ToolCall) (string, json.RawMessage, string) {
	if registry == nil {
		return unknownToolSummary(tc.Name), nil, toolErrorFeedback("unknown tool: " + tc.Name)
	}
	tool, ok := registry.Lookup(tc.Name)
	if !ok {
		return unknownToolSummary(tc.Name), nil, toolErrorFeedback("unknown tool: " + tc.Name)
	}
	args := json.RawMessage(tc.Arguments)
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	summary, payload, err := tool.Execute(ctx, args)
	if err != nil {
		failure := "执行失败:" + err.Error()
		return failure, nil, toolErrorFeedback(err.Error())
	}
	return summary, payload, toolResultFeedback(summary, payload)
}

// unknownToolSummary 是未注册工具的 tool_result 摘要(前端过程展示可见)。
func unknownToolSummary(name string) string {
	return "未知工具 " + name + ",已告知模型不可用"
}

// toolErrorFeedback is the tool-turn content when the invocation failed:
// a JSON error object the model can read and react to.
func toolErrorFeedback(message string) string {
	raw, _ := json.Marshal(map[string]string{"error": message})
	return string(raw)
}

// toolResultFeedback is the tool-turn content on success: the same summary
// and payload the browser saw, so the model reasons over what was done.
func toolResultFeedback(summary string, payload json.RawMessage) string {
	feedback := map[string]any{"summary": summary}
	if len(payload) > 0 {
		feedback["payload"] = json.RawMessage(payload)
	}
	raw, err := json.Marshal(feedback)
	if err != nil {
		return `{"error":"tool result not serializable"}`
	}
	return string(raw)
}

// toolLineItem renders one entry of the persisted summary line.
func toolLineItem(name, summary string) string {
	if summary == "" {
		return name
	}
	return fmt.Sprintf("%s(%s)", name, summary)
}

// composeToolSummary folds the per-tool entries into the done summary line
// (持久纪律:assistant 轮 = 最终回答 + 工具调用摘要行);无工具轮为空。
func composeToolSummary(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return "本轮工具调用:" + strings.Join(items, ";")
}
