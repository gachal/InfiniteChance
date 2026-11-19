package assistant

// 画布助手工具循环(91 号票):与 promptgen.runAgentLoop 同族的事件循环
// (delta/tool_call/tool_result/done/error 五形状,SSE 帧契约不变),差异
// 只在工具执行一段 —— 写图草案工具在 executeToolCall 处挂起等前端回执,
// 回执内容作 role=tool 帧喂回模型续跑。为什么复制改造而不是改公共循环:
// runAgentLoop 的形状与断线语义被 Agent 节点流使用,画布助手的「停轮等
// 回执 + 超时中断」语义不宜回流公共路径(笔记 02 坑 1 的定形决策)。
//
// 持久纪律由端点保证:循环中间帧(assistant tool_calls / role=tool)只在
// 本函数的局部 messages 里,不入会话持久历史。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/gachal/InfiniteChance/internal/promptgen"
)

// assistantMaxToolRounds 与收尾指令照 Agent 循环护栏同规(63 号票定案 15
// 轮,触顶注入收尾指令强制纯文本收场)。轮上限真引用 promptgen.AgentMaxToolRounds
// (91 号票评审修复:此前本地复制同值常量、注释却称「单点演进」失实),
// 改公共常量两边同时生效。
const assistantMaxToolRounds = promptgen.AgentMaxToolRounds

// assistantWrapupInstruction 是护栏触顶后的收尾指令(照公共循环文案语义,
// 面向画布助手口吻微调)。轮数同样从公共常量拼入。
var assistantWrapupInstruction = fmt.Sprintf(
	"你已连续完成 %d 轮工具调用,达到单次请求的上限。"+
		"请立即停止调用工具,总结本轮已完成与待用户确认的事项,并告诉用户如何继续。",
	promptgen.AgentMaxToolRounds,
)

// SystemPrompt 是画布助手的「剧本导演」位系统提示词:开场注入,与用户
// 消息正交。mode 段向模型声明当前授权档,行为不越档。
func SystemPrompt(mode string) string {
	modeLine := "当前授权档:先询问我 —— 一切写操作(建/改/删节点、连线、触发任务)都要等用户在前端变更卡片上批准后才真正应用,批准与否以工具回执为准。"
	if mode == ModeFull {
		modeLine = "当前授权档:完全授权 —— 写操作会由前端自动应用、生成任务自动提交(真实消耗额度),请保持克制,大手笔操作前仍向用户说明。"
	}
	return "你是「画布助手」,在用户当前的画布上协助创作:读写画布节点与连线、查阅 IP 资产库与剧本原文、按用户指令提交生成任务。\n" +
		"工作纪律:\n" +
		"- 所有画布修改都通过工具以草案提交,由前端应用;不要在文本里声称已经直接修改了画布。\n" +
		"- 批量操作优先(一次 create_nodes 建一批),参数给完整、给具体;新建节点的 id 以后续回执给出的为准。\n" +
		"- 修改节点时先用 read_canvas 检查相连的下游节点,主动提示连带影响,由用户决定是否一起改;系统不做自动级联。\n" +
		"- 用户拒绝某批变更时,不要原样重提;先弄清原因再给新方案。\n" +
		"- 长文正文用 read_source 分段精读,不要凭摘要臆测内容。\n" +
		"- " + modeLine
}

// runAssistantLoop drives one assistant turn (91 号票)。messages 是拼好的
// 请求起点(system + 窗口内历史 + 本轮 user),base 携带模型与来源标记
// (每轮 /v1 调用各自计费,来源 canvas={id} gen=assistant)。事件经 emit
// 推出;成功时已推 done、返回 nil。回执等待超时 = error 帧(receipt_timeout)
// + 返回错误中断;ctx 取消(断线)= 中断不再推事件。
func runAssistantLoop(
	ctx context.Context,
	gateway promptgen.AgentStreamTurner,
	registry *promptgen.AgentToolRegistry,
	hub *Hub,
	sessionID int64,
	base promptgen.ChatRequest,
	messages []promptgen.ChatMessage,
	emit func(promptgen.AgentEvent),
) error {
	var tools []json.RawMessage
	if registry != nil {
		tools = registry.Definitions()
	}

	var texts []string
	var toolLine []string
	wrapup := false

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
			emit(promptgen.AgentEvent{Type: "delta", Text: delta})
		})
		if err != nil {
			emit(promptgen.AgentEvent{Type: "error", Error: &promptgen.AgentEventError{Code: "upstream_error", Message: err.Error()}})
			return err
		}
		if turn.Content != "" {
			texts = append(texts, turn.Content)
		}
		if len(turn.ToolCalls) == 0 || wrapup {
			emit(promptgen.AgentEvent{Type: "done", Text: strings.Join(texts, "\n\n"), Summary: composeToolSummary(toolLine)})
			return nil
		}

		messages = append(messages, promptgen.ChatMessage{Role: "assistant", Content: turn.Content, ToolCalls: turn.ToolCalls})
		for _, tc := range turn.ToolCalls {
			callArgs := json.RawMessage(tc.Arguments)
			if !json.Valid(callArgs) {
				callArgs = nil
			}
			emit(promptgen.AgentEvent{Type: "tool_call", ID: tc.ID, Name: tc.Name, Args: callArgs})

			// 草案工具在挂起前登记在途调用(回执按 session+call_id 匹配);
			// 同步工具不登记(前端对它们不发回执)。
			tool, lookup := registry.Lookup(tc.Name)
			isDraft := false
			if lookup {
				_, isDraft = tool.(draftTool)
			}
			if isDraft {
				hub.Begin(sessionID, tc.ID)
			}

			outcome := runToolCall(ctx, registry, hub, sessionID, tc)
			if isDraft {
				// 校验失败等未进入等待的分支兜底清挂起(正常路径 Await 已清)。
				hub.Finish(sessionID, tc.ID)
			}
			if outcome.abort != nil {
				if errors.Is(outcome.abort, ErrReceiptTimeout) {
					emit(promptgen.AgentEvent{Type: "error", Error: &promptgen.AgentEventError{
						Code:    "receipt_timeout",
						Message: "等待前端回执超时(5 分钟),本轮中断;请重试或重新发起",
					}})
					hub.Abort(sessionID)
					return outcome.abort
				}
				// 断线(ctx 取消):照公共循环纪律不再推事件。
				hub.Abort(sessionID)
				return outcome.abort
			}
			if ctx.Err() != nil {
				// 工具内部感知到断线:循环就此中断。
				hub.Abort(sessionID)
				return ctx.Err()
			}
			emit(promptgen.AgentEvent{Type: "tool_result", ID: tc.ID, Name: tc.Name, Summary: outcome.summary, Payload: outcome.payload})
			messages = append(messages, promptgen.ChatMessage{Role: "tool", ToolCallID: tc.ID, Content: outcome.feedback})
			toolLine = append(toolLine, toolLineItem(tc.Name, outcome.summary))
		}

		if round >= assistantMaxToolRounds {
			wrapup = true
			messages = append(messages, promptgen.ChatMessage{Role: "user", Content: assistantWrapupInstruction})
		}
	}
}

// loopOutcome is one invocation's result inside the loop.
type loopOutcome struct {
	summary  string
	payload  json.RawMessage
	feedback string
	abort    error
}

// runToolCall executes one tool call:草案工具走 executeDraft(回执三态),
// 其余走普通 Execute(同步完成)。未知工具与执行失败都不炸会话,错误以
// {"error":…} 喂回模型。
func runToolCall(ctx context.Context, registry *promptgen.AgentToolRegistry, hub *Hub, sessionID int64, tc promptgen.ToolCall) loopOutcome {
	if registry == nil {
		return loopOutcome{summary: unknownToolSummary(tc.Name), feedback: feedbackError("unknown tool: " + tc.Name)}
	}
	tool, ok := registry.Lookup(tc.Name)
	if !ok {
		return loopOutcome{summary: unknownToolSummary(tc.Name), feedback: feedbackError("unknown tool: " + tc.Name)}
	}
	args := json.RawMessage(tc.Arguments)
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	if draft, isDraft := tool.(draftTool); isDraft {
		call, _ := receiptCallFrom(WithReceiptCall(ctx, sessionID, tc.ID))
		out := draft.executeDraft(ctx, args, call)
		return loopOutcome{summary: out.summary, payload: out.payload, feedback: out.feedback, abort: out.abort}
	}
	summary, payload, err := tool.Execute(ctx, args)
	if err != nil {
		return loopOutcome{summary: "执行失败:" + err.Error(), feedback: feedbackError(err.Error())}
	}
	return loopOutcome{summary: summary, payload: payload, feedback: feedbackResult(summary, payload)}
}

func unknownToolSummary(name string) string {
	return "未知工具 " + name + ",已告知模型不可用"
}

func toolLineItem(name, summary string) string {
	if summary == "" {
		return name
	}
	return fmt.Sprintf("%s(%s)", name, summary)
}

func composeToolSummary(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return "本轮工具调用:" + strings.Join(items, ";")
}
