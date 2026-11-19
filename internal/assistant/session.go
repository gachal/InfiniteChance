// Package assistant implements the canvas assistant (91 号票):画布级的
// 全局对话助手实体 —— 会话持久化(canvas_assistant_sessions 表,双方言)、
// 画布图工具面(七新 + 三复用,草案 + 前端回执)、SSE 会话流与回执端点。
// 代码键名一律 assistant,与 Agent 节点(agenttools/agent 节点)零共用存储
// (CONTEXT.md「画布助手」词条)。执行架构延续「前端唯一写者」(ADR 0006):
// 服务端永不改库内图 JSON,写图工具只产出草案、经 SSE 发前端,前端应用后
// 经回执端点把结果喂回循环。
package assistant

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound reports a session id that has no row (or lives on another
// canvas —— 会话绑死画布,跨画布访问按不存在处理,不泄露存在性).
var ErrNotFound = errors.New("assistant: session not found")

// 授权档两档(91 号票定案):「先询问我」= 读自由、写操作按轮汇总变更清单
// 卡片整批批准/拒绝;「完全授权」= 图修改直接应用、生成任务直接提交。
// 授权档存 session 上,各会话各记各的,面板可随时切(切档 = PATCH mode)。
const (
	ModeAsk  = "ask"
	ModeFull = "full"
)

// DefaultMode 是新会话的缺省授权档(91 号票:「先询问我」).
const DefaultMode = ModeAsk

// Message is one persisted conversation turn. 持久纪律照 Agent 节点:assistant
// 轮落最终回答全文(Content)+ 工具调用摘要行(Summary),循环中间帧
// (assistant tool_calls / role=tool)绝不入持久历史。发给 LLM 的窗口按
// assistantHistoryMaxMessages = 40 条截最旧(照 promptgen.agentHistoryMaxMessages
// 先例),截断只发生在拼请求时,库里存全量。
type Message struct {
	Role    string `json:"role"` // user | assistant
	Content string `json:"content"`
	// Summary 是 assistant 轮的工具调用摘要行(「本轮工具调用:…」),
	// user 轮恒空。
	Summary string `json:"summary,omitempty"`
}

// Session is one canvas-bound assistant conversation.
type Session struct {
	ID        int64
	CanvasID  int64
	Title     string
	Mode      string
	Messages  []Message
	CreatedAt time.Time
	UpdatedAt time.Time
}

// UpdateInput is the partial-update shape of Update:title / mode / messages
// 各自 nil = 不动;流式回合结束后服务端写 messages + title。
type UpdateInput struct {
	Title    *string
	Mode     *string
	Messages *[]Message
}

// Store persists canvas assistant sessions. 所有读写在 canvasID 上收窄:
// 会话绑死画布,不跨画布移动,DeleteByCanvas 供删画布级联(91 号票:
// 删画布级联删会话行,不留孤儿)。
type Store interface {
	// Create opens one session on the canvas (mode 缺省 ask,由 handler
	// 归一). Title 起始为空,首轮回答后由服务端生成。
	Create(ctx context.Context, canvasID int64, mode string) (Session, error)
	// List answers the canvas's sessions newest first (目录面:不含
	// messages 全文也行,但一列文档哲学下量级有限,直接带全量省一次读).
	List(ctx context.Context, canvasID int64) ([]Session, error)
	// Get answers one session;跨画布的 id 按 ErrNotFound 处理。
	Get(ctx context.Context, canvasID, id int64) (Session, error)
	// Update patches title / mode / messages (非 nil 字段才写).
	Update(ctx context.Context, canvasID, id int64, u *UpdateInput) (Session, error)
	// Delete removes one session.
	Delete(ctx context.Context, canvasID, id int64) error
	// DeleteByCanvas removes every session of one canvas (删画布级联).
	DeleteByCanvas(ctx context.Context, canvasID int64) error
}
