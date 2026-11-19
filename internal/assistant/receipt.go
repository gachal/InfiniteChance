package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

// 回执中枢(91 号票协议扩展的核心):现有 Agent 工具循环是同步执行,
// 画布图工具的写操作由前端应用(ADR 0006 前端唯一写者),应用结果须经
// 「前端回执」喂回循环续跑。Hub 是会话作用域的在途调用登记表 —— SSE 流
// 在 emit tool_call 时 Begin,工具 Execute 里 Await 阻塞等待,回执端点
// 收到 POST 后按 session+call_id 匹配投递。
//
// 匹配纪律:一个会话同一时刻至多一个挂起调用(循环顺序执行工具),回执
// 按 (sessionID, callID) 双键匹配;错号与无挂起都显式报错,不静默丢弃。

// receiptTimeout 是回执等待上限:用户在读变更卡片/离开座位,流不能无限
// 挂着。超时按该工具失败收尾(整个循环以 receipt_timeout 错误中断,已流
// 出内容保留,可重试),并在 SSE error 帧里如实报告。
const receiptTimeout = 5 * time.Minute

// ErrReceiptTimeout reports a draft tool that waited past receiptTimeout.
var ErrReceiptTimeout = errors.New("assistant: 回执等待超时")

// ErrNoPendingCall reports a receipt for a session with nothing in flight.
var ErrNoPendingCall = errors.New("assistant: 该会话没有等待回执的工具调用")

// ErrCallMismatch reports a receipt whose call_id is not the pending one.
var ErrCallMismatch = errors.New("assistant: 回执 call_id 与在途调用不符")

// ReceiptStatus is the wire status of one frontend receipt.
type ReceiptStatus string

const (
	// ReceiptApplied = 前端已应用(写图已落图/任务已提交),Result 携带
	// 结构化结果(如新建节点的 id 映射、任务 id)。
	ReceiptApplied ReceiptStatus = "applied"
	// ReceiptFailed = 前端应用失败(EDGE_MATRIX 拒绝、任务提交 4xx 等),
	// Message 是人读原因,如实喂回模型。
	ReceiptFailed ReceiptStatus = "failed"
	// ReceiptRejected = 「先询问我」档下用户拒绝了这批变更(整批),Message
	// 可携补充说明;不应用,如实告知 LLM。
	ReceiptRejected ReceiptStatus = "rejected"
)

// Receipt is one frontend receipt delivered to the assistant loop.
type Receipt struct {
	CallID  string          `json:"call_id"`
	Status  ReceiptStatus   `json:"status"`
	Message string          `json:"message,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
}

type receiptCtxKey struct{}

// receiptCall is the (session, call) pair a blocking draft tool waits on,
// injected into the tool's context by the assistant loop (AgentTool.Execute
// 只收 args,调用 id 走 ctx;注册表工具是会话间共享单例,不走可变状态).
type receiptCall struct {
	SessionID int64
	CallID    string
}

// WithReceiptCall returns a context carrying the pending-call binding.
func WithReceiptCall(ctx context.Context, sessionID int64, callID string) context.Context {
	return context.WithValue(ctx, receiptCtxKey{}, receiptCall{SessionID: sessionID, CallID: callID})
}

// receiptCallFrom extracts the binding;ok = false means the caller is not
// the assistant loop (装配缺口/测试直调),草案工具应拒绝执行。
func receiptCallFrom(ctx context.Context) (receiptCall, bool) {
	call, ok := ctx.Value(receiptCtxKey{}).(receiptCall)
	return call, ok
}

// pendingCall is one in-flight draft invocation: the loop registered the
// call id, the tool goroutine blocks on ch, the receipts endpoint delivers.
type pendingCall struct {
	callID string
	ch     chan Receipt
}

// Hub tracks in-flight draft calls per session plus the session's streaming
// state (一会话一在途流,SSE 端点据此拒绝并发流)。
type Hub struct {
	mu      sync.Mutex
	pending map[int64]*pendingCall
	active  map[int64]bool
	// timeout 是回执等待上限;生产装配用 receiptTimeout,测试注入短值。
	timeout time.Duration
}

// NewHub wires an empty hub with the default receipt timeout.
func NewHub() *Hub {
	return &Hub{pending: map[int64]*pendingCall{}, active: map[int64]bool{}, timeout: receiptTimeout}
}

// Begin marks callID as the session's pending draft call, dropping any
// stale leftover (正常序列下 Await 收尾即清,这里只兜异常路径).
func (h *Hub) Begin(sessionID int64, callID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pending[sessionID] = &pendingCall{callID: callID, ch: make(chan Receipt, 1)}
}

// Await blocks until the frontend receipt, the context cancellation (浏览器
// 断线 = 循环中断纪律) or the receipt timeout. 成功回执即清理挂起。
func (h *Hub) Await(ctx context.Context, sessionID int64, callID string) (Receipt, error) {
	h.mu.Lock()
	p := h.pending[sessionID]
	if p == nil || p.callID != callID {
		h.mu.Unlock()
		return Receipt{}, ErrNoPendingCall
	}
	h.mu.Unlock()

	timer := time.NewTimer(h.timeout)
	defer timer.Stop()
	select {
	case r := <-p.ch:
		h.clear(sessionID, p)
		return r, nil
	case <-ctx.Done():
		h.clear(sessionID, p)
		return Receipt{}, ctx.Err()
	case <-timer.C:
		h.clear(sessionID, p)
		return Receipt{}, ErrReceiptTimeout
	}
}

// Finish clears the session's pending call when it still matches callID
// (循环在草案工具收尾时调用;正常路径 Await 已清,这里兜校验失败等
// 未进入等待的分支)。
func (h *Hub) Finish(sessionID int64, callID string) {
	h.mu.Lock()
	if p := h.pending[sessionID]; p != nil && p.callID == callID {
		delete(h.pending, sessionID)
	}
	h.mu.Unlock()
}

// Deliver routes one receipt to the session's pending call;错号/无挂起都
// 显式报错(ch 带缓冲 1,投递永不阻塞)。
func (h *Hub) Deliver(sessionID int64, r Receipt) error {
	h.mu.Lock()
	p := h.pending[sessionID]
	if p == nil {
		h.mu.Unlock()
		return ErrNoPendingCall
	}
	if p.callID != r.CallID {
		h.mu.Unlock()
		return ErrCallMismatch
	}
	h.mu.Unlock()
	p.ch <- r
	return nil
}

func (h *Hub) clear(sessionID int64, p *pendingCall) {
	h.mu.Lock()
	if h.pending[sessionID] == p {
		delete(h.pending, sessionID)
	}
	h.mu.Unlock()
}

// Abort drops any pending call of the session (流收尾清理).
func (h *Hub) Abort(sessionID int64) {
	h.mu.Lock()
	delete(h.pending, sessionID)
	h.mu.Unlock()
}

// TryClaim marks the session as streaming;false = 已有在途流(一会话一流,
// 并发流按 session_busy 拒绝 —— 两条流抢同一张变更卡片没有意义)。
func (h *Hub) TryClaim(sessionID int64) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.active[sessionID] {
		return false
	}
	h.active[sessionID] = true
	return true
}

// Release clears the session's streaming claim.
func (h *Hub) Release(sessionID int64) {
	h.mu.Lock()
	delete(h.active, sessionID)
	h.mu.Unlock()
}

// TimeoutForTest overrides the receipt wait bound (测试注入短值;生产装配
// 恒用 receiptTimeout)。
func (h *Hub) TimeoutForTest(d time.Duration) {
	h.mu.Lock()
	h.timeout = d
	h.mu.Unlock()
}
