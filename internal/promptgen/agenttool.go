package promptgen

// Agent 工具注册表(63 号票机制,66 号票填实体):Agent 工具循环把注册表
// 里的全部工具定义作为 OpenAI tools 数组暴露给模型,模型发起调用后按名字
// 找回工具执行。本票只建机制——注册表由装配侧创建、可以留空(空注册表
// 的请求不带 tools 字段,行为与普通流式会话一致)。

import (
	"context"
	"encoding/json"
	"fmt"
)

// AgentTool is one domain tool the Agent tool loop may call (63 号票机制,
// 66 号票填六个领域工具):Definition 是 {"type":"function","function":{…}}
// 形状的 OpenAI tools 定义原文;Execute 收模型给的调用参数 JSON,返回人读
// summary(进会话摘要行与前端过程展示)与结构化 payload(写图类 = 前端
// 应用的图改动草案,库表类 = 后端已直写的证据)。单一写者原则(ADR 0006):
// Execute 永远不改库内图 JSON,写图只产出草案交前端应用。
type AgentTool interface {
	Name() string
	Definition() json.RawMessage
	Execute(ctx context.Context, args json.RawMessage) (summary string, payload json.RawMessage, err error)
}

// AgentToolRegistry holds the tools one canvas/server serves to the Agent
// loop. Registration happens at wiring time only — serve-time access is
// read-only, so no locking beyond the wiring phase is needed.
type AgentToolRegistry struct {
	tools map[string]AgentTool
	order []string // 注册序 = tools 数组暴露序
}

// NewAgentToolRegistry wires an empty registry.
func NewAgentToolRegistry() *AgentToolRegistry {
	return &AgentToolRegistry{tools: map[string]AgentTool{}}
}

// Register adds a tool; a duplicate name is a wiring-time programming error.
func (r *AgentToolRegistry) Register(tool AgentTool) {
	name := tool.Name()
	if _, dup := r.tools[name]; dup {
		panic(fmt.Sprintf("promptgen: agent tool %q registered twice", name))
	}
	r.tools[name] = tool
	r.order = append(r.order, name)
}

// Lookup finds a tool by the name the model called.
func (r *AgentToolRegistry) Lookup(name string) (AgentTool, bool) {
	tool, ok := r.tools[name]
	return tool, ok
}

// Definitions returns the OpenAI tools array in registration order; an
// empty registry answers nil, which keeps the tools field off the wire.
func (r *AgentToolRegistry) Definitions() []json.RawMessage {
	if len(r.order) == 0 {
		return nil
	}
	defs := make([]json.RawMessage, 0, len(r.order))
	for _, name := range r.order {
		defs = append(defs, r.tools[name].Definition())
	}
	return defs
}
