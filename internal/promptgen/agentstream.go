package promptgen

// Agent 工具循环的 SSE 端点(63 号票):与 generate-prompt 同一套校验前置
// 与 SSE 写法(31 号票纪律:校验不过不给流),循环本体在 agentloop.go。

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

// AgentStream 承载带工具的 Agent 会话(63 号票,ADR 0006 的事件回传面):
// 请求体与 generate-prompt 相同(node_id/template_id/topic/model/media/
// history/character_ids/character_section),服务端按既有规则拼会话、把
// 工具定义一并上送,循环事件逐帧推给前端 ——
//
//	data: {"character_section":"…"} 流首,本轮注入的设定段(52 号票历史
//	                     固化,仅注入时出现,与 GenerateStream 同形同位)
//	data: {"type":"delta","text":"…"}          文本增量
//	data: {"type":"tool_call","id":…,"name":…,"args":…}   工具调用开始
//	data: {"type":"tool_result","id":…,"name":…,"summary":…,"payload":…}
//	                                           工具结果(写图类 payload 由
//	                                           前端应用,ADR 0006)
//	data: {"type":"done","text":"…","summary":"…"}        最终回答 + 工具
//	                                           调用摘要行(persist 纪律:
//	                                           assistant 轮 = 全文 + 摘要)
//	data: {"type":"error","error":{code,message}}         流中失败
//	data: [DONE]                               成功收尾
//
// 每轮 /v1 调用独立计费,来源标记 gen=agent;不支持 tools 的上游 4xx 原样
// 透出(error 帧文案即网关返回)。浏览器断线 = ctx 取消 = 循环中断,已流
// 出内容保留;重试从持久历史重建,不带工具帧。
func (h *Handlers) AgentStream(c *gin.Context) {
	prep, ok := h.prepareGenerate(c)
	if !ok {
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	writeEvent := func(payload []byte) {
		out := make([]byte, 0, len(sseDataFrame)+len(payload)+2)
		out = append(out, sseDataFrame...)
		out = append(out, payload...)
		out = append(out, '\n', '\n')
		_, _ = c.Writer.Write(out)
		c.Writer.Flush()
	}

	// 设定段随流带回(52 号票历史固化):编辑器把它持久进节点 data,
	// 后续轮次原样回传。与 GenerateStream 同位 —— 增量之前先发。
	if prep.characterSection != "" {
		if body, mErr := json.Marshal(gin.H{"character_section": prep.characterSection}); mErr == nil {
			writeEvent(body)
		}
	}

	// 「剧本导演」system 提示词(66 号票定案 3):带工具的会话以它开场,
	// 与技能渲染文本(首条 user 指令)正交叠加、不顶掉;仅当未选技能时
	// 首条指令才换 agent 专用缺省(导演人设已在 system,不再借用「提示词
	// 工程师」)。注册表为空时无工具可调:不注入、不换指令,请求与升级前
	// 的普通流式会话一致。工具经请求 context 拿到画布作用域(66 号票,
	// 库表类工具严格限本 IP)。
	instruction := prep.instructionText
	var tools []json.RawMessage
	if h.AgentTools != nil {
		tools = h.AgentTools.Definitions()
	}
	if len(tools) > 0 && prep.usedBuiltin {
		instruction = builtinAgentInstruction
	}
	messages := conversationMessages(instruction, prep.characterSection, prep.history, prep.topic, prep.currentMedia)
	ctx := c.Request.Context()
	if len(tools) > 0 {
		messages = append([]ChatMessage{{Role: "system", Content: agentDirectorSystemPrompt}}, messages...)
		ctx = WithAgentScope(ctx, AgentScope{CanvasID: prep.canvasID, IPID: prep.ipID})
	}

	err := runAgentLoop(ctx, h.Gateway, h.AgentTools, ChatRequest{
		Model:  prep.model,
		Source: canvasSource(prep.canvasID, prep.nodeID, "agent"),
	}, messages, func(ev AgentEvent) {
		if body, mErr := json.Marshal(ev); mErr == nil {
			writeEvent(body)
		}
	})
	if err != nil {
		// error 帧已由循环发出(已下发增量不回收);断线(ctx 取消)不打扰。
		if !errors.Is(err, context.Canceled) {
			log.Printf("promptgen: %s %s: agent loop: %v", c.Request.Method, c.Request.URL.Path, err)
		}
		return
	}
	writeEvent(sseDoneMarker)
}
