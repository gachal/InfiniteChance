package promptgen

// Agent 工具循环的画布作用域(66 号票):SSE 端点挂在 /canvases/:id 下,
// 端点知道画布与归属 IP;工具执行器(独立包,如 internal/agenttools)的
// Execute 只收调用参数 —— 作用域经请求 context 传递,库表类工具按它绑定
// IP、读原文类工具按它定位画布。契约在本包(agenttools 单向依赖,无环)。

import "context"

// AgentScope is the per-request canvas context the Agent tools operate in:
// the canvas the conversation runs on and the IP it belongs to (库表类工具
// 严格限本 IP,角色库 51 号票同一哲学)。
type AgentScope struct {
	CanvasID int64
	IPID     int64
}

type agentScopeKey struct{}

// WithAgentScope returns a context carrying the scope; the Agent stream
// endpoint injects it before running the loop.
func WithAgentScope(ctx context.Context, scope AgentScope) context.Context {
	return context.WithValue(ctx, agentScopeKey{}, scope)
}

// AgentScopeFrom extracts the scope a tool executes under; ok = false means
// the endpoint did not inject one (装配缺口/测试直调),工具应拒绝执行。
func AgentScopeFrom(ctx context.Context) (AgentScope, bool) {
	scope, ok := ctx.Value(agentScopeKey{}).(AgentScope)
	return scope, ok
}
