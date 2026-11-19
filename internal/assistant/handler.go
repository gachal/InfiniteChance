package assistant

// 画布助手端点(91 号票):挂 canvas JWT 面(/canvases 组,与画布 CRUD
// 同一令牌体系)。会话 CRUD + SSE 会话流 + 回执端点三族:
//
//	POST   /:id/assistant/sessions            — 新会话 {mode?}(缺省 ask)
//	GET    /:id/assistant/sessions            — 会话目录(新在前)
//	GET    /:id/assistant/sessions/:sid       — 会话详情(含 messages)
//	PATCH  /:id/assistant/sessions/:sid       — {title?/mode?}(面板切档)
//	DELETE /:id/assistant/sessions/:sid       — 删会话
//	POST   /:id/assistant/sessions/:sid/receipts — 前端回执 {call_id,status,message?,result?}
//	POST   /:id/assistant/stream              — SSE {session_id,message,model}
//
// SSE 帧契约与 Agent 循环五形状一致(delta/tool_call/tool_result/done/
// error,收尾 data: [DONE];错误帧后不再写 [DONE])。协议扩展:草案工具
// 的 tool_call 之后流会静默等待前端应用与回执(最长 5 分钟),回执到即
// tool_result 续跑。持久纪律:done 后 assistant 轮落「最终回答全文 +
// 工具调用摘要行」,循环中间帧不入库;流中断(错误/断线/超时)不持久化
// —— 断线可重试,重试整轮重放。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/apierr"
	"github.com/gachal/InfiniteChance/internal/canvas"
	"github.com/gachal/InfiniteChance/internal/pricing"
	"github.com/gachal/InfiniteChance/internal/promptgen"
)

// assistantHistoryMaxMessages 是发给 LLM 的上下文窗口上限(照
// promptgen.agentHistoryMaxMessages = 40 先例):截断只发生在拼请求时,
// 库里存全量。
const assistantHistoryMaxMessages = 40

// maxMessageRunes 是单条用户输入宽度(照画布任务 prompt 8000 同规);
// title ≤128(列宽)、model ≤200(照画布任务 model)。
const (
	maxMessageRunes = 8000
	maxTitleRunes   = 128
	maxModelRunes   = 200
)

// titleRunes 是自动生成会话标题的宽度(LLM 失败回落首条消息截断,同宽)。
const titleRunes = 24

// Handlers serves the canvas assistant endpoints. Gateway nil = 未配服务
// key,流以 gateway_unconfigured 拒绝(与画布任务/提示词生成同纪律)。
type Handlers struct {
	Sessions Store
	// Canvases 读画布行:存在性裁决 + 会话作用域注入(CanvasID/IPID)。
	Canvases promptgen.CanvasGetter
	// Models 供开流前的聊天轨配价预检(model_not_priced,硬性约束 3)。
	Models promptgen.ModelPricer
	Gateway promptgen.Gateway
	// Tools 是助手工具面(七新 + 三复用);nil = 不带 tools 字段,退化为
	// 普通流式会话(测试宽容路径)。
	Tools *promptgen.AgentToolRegistry
	// Hub 是回执中枢(SSE 流与回执端点共享同一实例;wiring 装配)。
	Hub *Hub
}

// RegisterRoutes mounts the assistant surface (relative to the /canvases
// group behind the JWT middleware).
func RegisterRoutes(group *gin.RouterGroup, h *Handlers) {
	g := group.Group("/:id/assistant")
	g.POST("/sessions", h.CreateSession)
	g.GET("/sessions", h.ListSessions)
	g.GET("/sessions/:sid", h.GetSession)
	g.PATCH("/sessions/:sid", h.UpdateSession)
	g.DELETE("/sessions/:sid", h.DeleteSession)
	g.POST("/sessions/:sid/receipts", h.Receipt)
	g.POST("/stream", h.Stream)
}

// —— 会话 CRUD ——

type sessionJSON struct {
	ID        int64     `json:"id"`
	CanvasID  int64     `json:"canvas_id"`
	Title     string    `json:"title"`
	Mode      string    `json:"mode"`
	Messages  []Message `json:"messages"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toSessionJSON(s Session) sessionJSON {
	if s.Messages == nil {
		s.Messages = []Message{}
	}
	return sessionJSON{
		ID: s.ID, CanvasID: s.CanvasID, Title: s.Title, Mode: s.Mode,
		Messages: s.Messages, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
	}
}

func (h *Handlers) bindCanvas(c *gin.Context) (int64, bool) {
	canvasID, ok := bindID(c)
	if !ok {
		return 0, false
	}
	if _, err := h.Canvases.Get(c.Request.Context(), canvasID); err != nil {
		if errors.Is(err, canvas.ErrNotFound) {
			apierr.NotFound(c, "画布不存在")
			return 0, false
		}
		log.Printf("assistant: %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
		apierr.Internal(c, "服务内部错误,请稍后再试")
		return 0, false
	}
	return canvasID, true
}

func bindSessionID(c *gin.Context) (int64, bool) {
	sid, err := parseID(c.Param("sid"))
	if err != nil || sid < 1 {
		apierr.InvalidRequest(c, "会话 id 必须是正整数")
		return 0, false
	}
	return sid, true
}

func normalizeMode(mode string) (string, bool) {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		return DefaultMode, true
	}
	if mode != ModeAsk && mode != ModeFull {
		return "", false
	}
	return mode, true
}

func (h *Handlers) CreateSession(c *gin.Context) {
	canvasID, ok := h.bindCanvas(c)
	if !ok {
		return
	}
	var in struct {
		Mode string `json:"mode"`
	}
	if c.Request.Body != nil {
		if err := c.ShouldBindJSON(&in); err != nil {
			apierr.InvalidRequest(c, "请求体必须是 {mode?} JSON")
			return
		}
	}
	mode, ok := normalizeMode(in.Mode)
	if !ok {
		apierr.InvalidRequest(c, "mode 只支持 ask(先询问我)或 full(完全授权)")
		return
	}
	s, err := h.Sessions.Create(c.Request.Context(), canvasID, mode)
	if err != nil {
		log.Printf("assistant: create session: %v", err)
		apierr.Internal(c, "服务内部错误,请稍后再试")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"session": toSessionJSON(s)})
}

func (h *Handlers) ListSessions(c *gin.Context) {
	canvasID, ok := h.bindCanvas(c)
	if !ok {
		return
	}
	sessions, err := h.Sessions.List(c.Request.Context(), canvasID)
	if err != nil {
		log.Printf("assistant: list sessions: %v", err)
		apierr.Internal(c, "服务内部错误,请稍后再试")
		return
	}
	out := make([]sessionJSON, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, toSessionJSON(s))
	}
	c.JSON(http.StatusOK, gin.H{"sessions": out})
}

func (h *Handlers) sessionOf(c *gin.Context, canvasID int64) (Session, bool) {
	sid, ok := bindSessionID(c)
	if !ok {
		return Session{}, false
	}
	s, err := h.Sessions.Get(c.Request.Context(), canvasID, sid)
	if errors.Is(err, ErrNotFound) {
		apierr.NotFound(c, "会话不存在(会话绑死画布,跨画布访问按不存在处理)")
		return Session{}, false
	}
	if err != nil {
		log.Printf("assistant: get session: %v", err)
		apierr.Internal(c, "服务内部错误,请稍后再试")
		return Session{}, false
	}
	return s, true
}

func (h *Handlers) GetSession(c *gin.Context) {
	canvasID, ok := h.bindCanvas(c)
	if !ok {
		return
	}
	s, ok := h.sessionOf(c, canvasID)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{"session": toSessionJSON(s)})
}

func (h *Handlers) UpdateSession(c *gin.Context) {
	canvasID, ok := h.bindCanvas(c)
	if !ok {
		return
	}
	s, ok := h.sessionOf(c, canvasID)
	if !ok {
		return
	}
	var in struct {
		Title *string `json:"title"`
		Mode  *string `json:"mode"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		apierr.InvalidRequest(c, "请求体必须是 {title?/mode?} JSON")
		return
	}
	u := &UpdateInput{}
	if in.Title != nil {
		title := strings.TrimSpace(*in.Title)
		if utf8.RuneCountInString(title) > maxTitleRunes {
			apierr.InvalidRequest(c, "标题最多 128 个字符")
			return
		}
		u.Title = &title
	}
	if in.Mode != nil {
		mode, ok := normalizeMode(*in.Mode)
		if !ok {
			apierr.InvalidRequest(c, "mode 只支持 ask(先询问我)或 full(完全授权)")
			return
		}
		u.Mode = &mode
	}
	updated, err := h.Sessions.Update(c.Request.Context(), canvasID, s.ID, u)
	if err != nil {
		log.Printf("assistant: update session: %v", err)
		apierr.Internal(c, "服务内部错误,请稍后再试")
		return
	}
	c.JSON(http.StatusOK, gin.H{"session": toSessionJSON(updated)})
}

func (h *Handlers) DeleteSession(c *gin.Context) {
	canvasID, ok := h.bindCanvas(c)
	if !ok {
		return
	}
	s, ok := h.sessionOf(c, canvasID)
	if !ok {
		return
	}
	if err := h.Sessions.Delete(c.Request.Context(), canvasID, s.ID); err != nil {
		log.Printf("assistant: delete session: %v", err)
		apierr.Internal(c, "服务内部错误,请稍后再试")
		return
	}
	c.Status(http.StatusNoContent)
}

// —— 回执端点 ——

// Receipt 承接前端回执(91 号票协议扩展):call_id 匹配在途草案调用,
// status 三态(应用/失败/拒绝),applied 时 result 是结构化结果(新建
// 节点 id 映射、任务提交证据),失败/拒绝时 message 是人读原因。
func (h *Handlers) Receipt(c *gin.Context) {
	canvasID, ok := h.bindCanvas(c)
	if !ok {
		return
	}
	s, ok := h.sessionOf(c, canvasID)
	if !ok {
		return
	}
	var in Receipt
	if err := c.ShouldBindJSON(&in); err != nil {
		apierr.InvalidRequest(c, "请求体必须是 {call_id, status, message?, result?} JSON")
		return
	}
	if strings.TrimSpace(in.CallID) == "" {
		apierr.InvalidRequest(c, "call_id 不能为空")
		return
	}
	switch in.Status {
	case ReceiptApplied, ReceiptFailed, ReceiptRejected:
	default:
		apierr.InvalidRequest(c, "status 只支持 applied、failed 或 rejected")
		return
	}
	if err := h.Hub.Deliver(s.ID, in); err != nil {
		if errors.Is(err, ErrNoPendingCall) {
			apierr.Conflict(c, "no_pending_call", "该会话当前没有等待回执的工具调用(流可能已结束)")
			return
		}
		if errors.Is(err, ErrCallMismatch) {
			apierr.Conflict(c, "call_mismatch", "回执 call_id 与在途调用不符")
			return
		}
		log.Printf("assistant: deliver receipt: %v", err)
		apierr.Internal(c, "服务内部错误,请稍后再试")
		return
	}
	c.Status(http.StatusNoContent)
}

// —— SSE 会话流 ——

type streamInput struct {
	SessionID int64  `json:"session_id"`
	Message   string `json:"message"`
	Model     string `json:"model"`
}

// Stream runs one assistant turn:流前校验全部完成才开流(31 号票纪律
// 「校验不过不给流」),循环事件逐帧直写。
func (h *Handlers) Stream(c *gin.Context) {
	canvas, canvasID, ok := h.loadCanvas(c)
	if !ok {
		return
	}
	if h.Gateway == nil {
		apierr.Write(c, http.StatusServiceUnavailable, "gateway_unconfigured",
			"画布服务未配置网关服务 key(CANVAS_SERVICE_KEY),暂时不能对话")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	var in streamInput
	if err := c.ShouldBindJSON(&in); err != nil {
		apierr.InvalidRequest(c, "请求体必须是 {session_id, message, model} JSON")
		return
	}
	message := strings.TrimSpace(in.Message)
	if message == "" {
		apierr.InvalidRequest(c, "message 不能为空")
		return
	}
	if utf8.RuneCountInString(message) > maxMessageRunes {
		apierr.InvalidRequest(c, "message 最多 8000 个字符")
		return
	}
	model := strings.TrimSpace(in.Model)
	if model == "" {
		apierr.InvalidRequest(c, "model 不能为空")
		return
	}
	if utf8.RuneCountInString(model) > maxModelRunes {
		apierr.InvalidRequest(c, "model 最多 200 个字符")
		return
	}
	if in.SessionID < 1 {
		apierr.InvalidRequest(c, "session_id 必须是正整数")
		return
	}
	s, err := h.Sessions.Get(c.Request.Context(), canvasID, in.SessionID)
	if errors.Is(err, ErrNotFound) {
		apierr.NotFound(c, "会话不存在(会话绑死画布,跨画布访问按不存在处理)")
		return
	}
	if err != nil {
		log.Printf("assistant: %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
		apierr.Internal(c, "服务内部错误,请稍后再试")
		return
	}
	mode := s.Mode
	if _, ok := normalizeMode(mode); !ok {
		mode = DefaultMode
	}
	if !h.chatModelPriced(c, model) {
		return
	}
	// 一会话一在途流:第二条流抢同一张变更卡片没有意义。
	if !h.Hub.TryClaim(s.ID) {
		apierr.Conflict(c, "session_busy", "该会话已有进行中的对话流,请等它结束")
		return
	}
	defer h.Hub.Release(s.ID)

	// 开流(SSE 头 + flusher 就绪)。
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	writeEvent := func(payload []byte) {
		out := make([]byte, 0, len(payload)+4)
		out = append(out, "data: "...)
		out = append(out, payload...)
		out = append(out, '\n', '\n')
		_, _ = c.Writer.Write(out)
		c.Writer.Flush()
	}

	messages := make([]promptgen.ChatMessage, 0, len(s.Messages)+3)
	messages = append(messages, promptgen.ChatMessage{Role: "system", Content: SystemPrompt(mode)})
	for _, m := range windowMessages(s.Messages, assistantHistoryMaxMessages) {
		messages = append(messages, promptgen.ChatMessage{Role: m.Role, Content: m.Content})
	}
	messages = append(messages, promptgen.ChatMessage{Role: "user", Content: message})

	ctx := promptgen.WithAgentScope(c.Request.Context(), promptgen.AgentScope{CanvasID: canvasID, IPID: canvas.IPID})
	var done promptgen.AgentEvent
	err = runAssistantLoop(ctx, h.Gateway, h.Tools, h.Hub, s.ID, promptgen.ChatRequest{
		Model:  model,
		Source: fmt.Sprintf("canvas=%d gen=assistant", canvasID),
	}, messages, func(ev promptgen.AgentEvent) {
		if ev.Type == "done" {
			done = ev
		}
		if body, mErr := json.Marshal(ev); mErr == nil {
			writeEvent(body)
		}
	})
	if err != nil {
		// error 帧已由循环发出(已下发增量不回收);断线(ctx 取消)不打扰。
		if !errors.Is(err, context.Canceled) {
			log.Printf("assistant: %s %s: loop: %v", c.Request.Method, c.Request.URL.Path, err)
		}
		return
	}

	// 持久纪律:整轮成功才落库 —— user 全文 + assistant(最终回答全文 +
	// 工具调用摘要行),循环中间帧不入库;流中断不持久化,可重试重放。
	turns := append(append([]Message{}, s.Messages...),
		Message{Role: "user", Content: message},
		Message{Role: "assistant", Content: done.Text, Summary: done.Summary},
	)
	u := &UpdateInput{Messages: &turns}
	if s.Title == "" {
		title := h.sessionTitle(ctx, model, canvasID, message, done.Text)
		u.Title = &title
	}
	if _, err := h.Sessions.Update(context.Background(), canvasID, s.ID, u); err != nil {
		// 对话已完整送达前端,落库失败只影响重开页面的历史:记日志不炸流。
		log.Printf("assistant: persist session %d: %v", s.ID, err)
	}
	writeEvent([]byte("[DONE]"))
}

// loadCanvas 校验画布存在并带回行(IP 归属给作用域注入)。
func (h *Handlers) loadCanvas(c *gin.Context) (canvas.Canvas, int64, bool) {
	canvasID, ok := bindID(c)
	if !ok {
		return canvas.Canvas{}, 0, false
	}
	cv, err := h.Canvases.Get(c.Request.Context(), canvasID)
	if errors.Is(err, canvas.ErrNotFound) {
		apierr.NotFound(c, "画布不存在")
		return canvas.Canvas{}, 0, false
	}
	if err != nil {
		log.Printf("assistant: %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
		apierr.Internal(c, "服务内部错误,请稍后再试")
		return canvas.Canvas{}, 0, false
	}
	return cv, canvasID, true
}

// chatModelPriced 照 promptgen 同名纪律:聊天轨(token)未配价即拒绝
// (model_not_priced),不做静默兜底(硬性约束 3)。
func (h *Handlers) chatModelPriced(c *gin.Context, model string) bool {
	price, err := h.Models.ByModel(c.Request.Context(), model)
	if errors.Is(err, pricing.ErrNotFound) {
		apierr.Write(c, http.StatusBadRequest, "model_not_priced",
			"模型 "+model+" 未配置 token 计价,请先在管理端配置价格")
		return false
	}
	if err != nil {
		log.Printf("assistant: price lookup: %v", err)
		apierr.Internal(c, "服务内部错误,请稍后再试")
		return false
	}
	if price.Unit != pricing.UnitToken || price.Token == nil {
		apierr.Write(c, http.StatusBadRequest, "model_not_priced",
			"模型 "+model+" 不是 token 计价的聊天模型")
		return false
	}
	return true
}

// sessionTitle 生成会话标题(裁量定形:LLM 短标题,10 秒超时内拿不到就
// 回落首条用户消息截断 —— 票面「倾向 LLM 生成失败回落截断」)。标题生成
// 失败不影响本轮对话。
func (h *Handlers) sessionTitle(ctx context.Context, model string, canvasID int64, userMessage, answer string) string {
	fallback := truncateRunes(strings.Join(strings.Fields(userMessage), " "), titleRunes)
	if h.Gateway == nil {
		return fallback
	}
	tctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	prompt := "请为下面这轮画布助手对话起一个不超过 " + itoa(titleRunes) + " 个字符的中文标题,概括用户的意图。" +
		"只输出标题本身,不要引号、句号或任何解释。\n用户:" + truncateRunes(userMessage, 200) +
		"\n助手:" + truncateRunes(answer, 200)
	res, err := h.Gateway.GenerateChat(tctx, promptgen.ChatRequest{
		Model:  model,
		Source: fmt.Sprintf("canvas=%d gen=assistant", canvasID),
		Content: prompt,
	})
	if err != nil {
		return fallback
	}
	title := strings.Join(strings.Fields(strings.Trim(res.Content, "\"'「」『』")), " ")
	if title == "" || utf8.RuneCountInString(title) > titleRunes {
		return fallback
	}
	return title
}

// windowMessages 取窗口内最旧的截断(拼请求用,不改入参;照
// appendAgentTurn 纯函数先例)。
func windowMessages(msgs []Message, max int) []Message {
	if len(msgs) <= max {
		return msgs
	}
	return msgs[len(msgs)-max:]
}

func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}

// parseID 解析路径 id 段;非法回 error(调用方按各自文案答 400)。
func parseID(raw string) (int64, error) {
	return strconv.ParseInt(raw, 10, 64)
}

// bindID 照 canvas/promptgen 的同款:非法 id 答 400(不是 404)——
// 让坏客户端看到自己的 bug,而不是误导性的「不存在」。
func bindID(c *gin.Context) (int64, bool) {
	id, err := parseID(c.Param("id"))
	if err != nil || id < 1 {
		apierr.InvalidRequest(c, "画布 id 必须是正整数")
		return 0, false
	}
	return id, true
}
