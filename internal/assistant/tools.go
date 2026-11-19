package assistant

// 画布图工具面(91 号票):read_canvas 同步读图(服务端只读,不改库内图
// JSON);六个写图草案工具 create/update/delete_nodes、connect/
// disconnect_nodes、trigger_task 一律「草案 + 回执」——Execute 校验参数
// 形状后挂起等待前端回执(应用/失败/拒绝),绝不落库。单一写者纪律
// (ADR 0006):本包没有任何写 canvases.graph 的路径。
//
// 参数校验只做形状与批量上限(轻量):语义校验(EDGE_MATRIX 连线合法性、
// 任务契约 seconds/refs/计价)归前端应用器与既有画布任务端点——助手端
// 不重写一份(笔记 01 坑 6),失败经回执如实喂回模型。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/gachal/InfiniteChance/internal/agenttools"
	"github.com/gachal/InfiniteChance/internal/promptgen"
)

// 批量上限(护栏钉测对象):一次草案的操作数上限,超出按参数错误拒绝
// (模型自行分批)。上限量级对单管理员自用已是失控防线,不是业务配额。
const (
	maxBatchNodes  = 20 // create_nodes / update_nodes 单批节点数
	maxBatchIDs    = 50 // delete_nodes 单批节点数
	maxBatchEdges  = 40 // connect/disconnect 及 create_nodes.edges 单批边数
	maxTaskRefs    = 9  // trigger_task 参考图/视频参考上限(与画布任务契约同值)
	maxArgStringRunes = 8000 // 单个字符串参数宽度(prompt/title 等)
)

// canvasNodeTypes 是 create_nodes 接受的节点类型(与前端 CanvasNodeType
// 14 种同单;未知类型前端应用器也会拒绝,这里先拦让模型直接看到原因)。
var canvasNodeTypes = map[string]bool{
	"agent": true, "image": true, "video": true, "analysis": true,
	"previz": true, "character": true, "storyboard": true, "name": true,
	"level": true, "voice": true, "audio": true, "script": true,
	"group": true, "panorama": true,
}

// Deps carries the stores the assistant tools read through.
type Deps struct {
	// Canvases 供 read_canvas 读图与 set_style 越权核对(只读,无写路径)。
	Canvases agenttools.CanvasSource
	// Characters/Scenes/Props/IPs 原样转给复用的三件 agenttools 工具。
	Characters agenttools.CharacterSource
	Scenes     agenttools.SceneSource
	Props      agenttools.PropSource
	IPs        agenttools.IPSource
	// Hub 是草案工具的回执中枢(SSE 域与回执端点共享同一实例)。
	Hub *Hub
}

// NewRegistry wires the assistant tool face (91 号票首批:七新 + 三复用):
// read_canvas → 六个草案工具 → 复用三件。注册序 = tools 数组暴露序。
func NewRegistry(deps Deps) *promptgen.AgentToolRegistry {
	reg := promptgen.NewAgentToolRegistry()
	reg.Register(newReadCanvas(deps))
	reg.Register(newCreateNodes(deps.Hub))
	reg.Register(newUpdateNodes(deps.Hub))
	reg.Register(newDeleteNodes(deps.Hub))
	reg.Register(newConnectNodes(deps.Hub))
	reg.Register(newDisconnectNodes(deps.Hub))
	reg.Register(newTriggerTask(deps.Hub))
	// 复用三件:同一实现直接注册(导出构造器,零复制),作用域绑定照旧
	// 经 AgentScope(ctx 注入)。
	agentDeps := agenttools.Deps{
		Characters: deps.Characters,
		Scenes:     deps.Scenes,
		Props:      deps.Props,
		Canvases:   deps.Canvases,
		IPs:        deps.IPs,
	}
	reg.Register(agenttools.NewListAssets(agentDeps))
	reg.Register(agenttools.NewReadSource(agentDeps))
	reg.Register(agenttools.NewSetStyle(agentDeps))
	return reg
}

// draftOutcome is one draft invocation's full result:summary/payload 进
// SSE tool_result 事件,feedback 是 role=tool 帧文本;abort 非 nil 时循环
// 整体中断(回执超时/客户端断线)。
type draftOutcome struct {
	summary  string
	payload  json.RawMessage
	feedback string
	abort    error
}

// draftTool is the extension the assistant loop looks for:写图草案工具的
// 执行要挂起等回执,循环按它分流(promptgen.AgentToolRegistry 原样复用,
// 不另造注册表)。
type draftTool interface {
	promptgen.AgentTool
	executeDraft(ctx context.Context, args json.RawMessage, call receiptCall) draftOutcome
}

// draftCore 是六个草案工具共用的壳:name/definition 静态,Execute 校验后
// 挂起等回执,回执三态映射为模型反馈。
type draftCore struct {
	name        string
	description string
	parameters  map[string]any
	// validate 对模型参数做形状校验;通过后返回卡片摘要(变更清单文案,
	// 经 SSE args 一并下发,前端直接展示),失败返回错误(不挂起,模型
	// 可自行纠正)。
	validate func(args map[string]any) (string, error)
	hub      *Hub
}

func (t draftCore) Name() string { return t.name }

func (t draftCore) Definition() json.RawMessage {
	return functionDefinition(t.name, t.description, t.parameters)
}

// Execute 实现 promptgen.AgentTool:非助手循环的直调(无回执绑定)按
// 错误拒绝 —— 草案工具只在助手会话里有意义。
func (t draftCore) Execute(ctx context.Context, args json.RawMessage) (string, json.RawMessage, error) {
	call, ok := receiptCallFrom(ctx)
	if !ok {
		return "", nil, fmt.Errorf("工具缺少会话回执上下文,请通过画布助手会话调用")
	}
	out := t.executeDraft(ctx, args, call)
	if out.abort != nil {
		return "", nil, out.abort
	}
	if out.feedback != "" && out.summary == "" {
		return "", nil, errors.New(out.feedback)
	}
	return out.summary, out.payload, nil
}

// executeDraft 是助手循环的执行入口:校验 → 挂起 → 回执三态映射。
// 校验失败不挂起(前端永远看不到坏卡),错误如实喂回模型。
func (t draftCore) executeDraft(ctx context.Context, args json.RawMessage, call receiptCall) draftOutcome {
	m, err := argsOf(args)
	if err != nil {
		return draftOutcome{summary: "调用参数有误:" + err.Error(), feedback: feedbackError(err.Error())}
	}
	if _, err = t.validate(m); err != nil {
		return draftOutcome{summary: "调用参数有误:" + err.Error(), feedback: feedbackError(err.Error())}
	}

	r, err := t.hub.Await(ctx, call.SessionID, call.CallID)
	switch {
	case err == nil:
	case errors.Is(err, ErrReceiptTimeout):
		// 超时按该工具失败收尾,整个循环以 receipt_timeout 报告(可重试)。
		return draftOutcome{abort: err}
	case ctx.Err() != nil:
		return draftOutcome{abort: ctx.Err()}
	default:
		return draftOutcome{summary: "执行失败:" + err.Error(), feedback: feedbackError(err.Error())}
	}

	switch r.Status {
	case ReceiptApplied:
		summary := r.Message
		if strings.TrimSpace(summary) == "" {
			summary = "已应用"
		}
		return draftOutcome{summary: summary, payload: r.Result, feedback: feedbackResult(summary, r.Result)}
	case ReceiptFailed:
		msg := r.Message
		if strings.TrimSpace(msg) == "" {
			msg = "前端应用失败"
		}
		return draftOutcome{summary: "应用失败:" + msg, feedback: feedbackError(msg)}
	case ReceiptRejected:
		msg := "用户拒绝了这批变更"
		if strings.TrimSpace(r.Message) != "" {
			msg += ":" + r.Message
		}
		return draftOutcome{summary: msg, feedback: feedbackError(msg)}
	default:
		return draftOutcome{summary: "回执状态未知:" + string(r.Status), feedback: feedbackError("回执状态未知: "+string(r.Status))}
	}
}

// —— 参数小件(argsOf 复制自 agenttools 的同族惯例,本包独立不引入对
// agenttools 的参数工具依赖;agenttools 只被复用三件单向引用)——

func functionDefinition(name, description string, parameters map[string]any) json.RawMessage {
	raw, err := json.Marshal(map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        name,
			"description": description,
			"parameters":  parameters,
		},
	})
	if err != nil {
		panic(fmt.Sprintf("assistant: definition %q not marshalable: %v", name, err))
	}
	return raw
}

func argsOf(args json.RawMessage) (map[string]any, error) {
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	var m map[string]any
	if err := json.Unmarshal(args, &m); err != nil {
		return nil, fmt.Errorf("调用参数不是合法 JSON 对象")
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

// stringField / arrayField / objectField 是形状取参小件:类型不对报错,
// 让模型看到原因。
func stringField(m map[string]any, key string) (string, error) {
	v, ok := m[key]
	if !ok || v == nil {
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("参数 %s 必须是字符串", key)
	}
	return s, nil
}

func requiredString(m map[string]any, key string) (string, error) {
	s, err := stringField(m, key)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(s) == "" {
		return "", fmt.Errorf("参数 %s 不能为空", key)
	}
	return strings.TrimSpace(s), nil
}

func arrayField(m map[string]any, key string) ([]any, error) {
	v, ok := m[key]
	if !ok || v == nil {
		return nil, nil
	}
	a, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("参数 %s 必须是数组", key)
	}
	return a, nil
}

func runeLen(s string) int { return len([]rune(s)) }

// limitArg 封顶字符串参数宽度(prompt 段照画布任务 8000 同规)。
func limitArg(field, value string) error {
	if runeLen(value) > maxArgStringRunes {
		return fmt.Errorf("%s 最多 %d 个字符", field, maxArgStringRunes)
	}
	return nil
}

// feedbackError / feedbackResult 组装 role=tool 帧文本(与 promptgen 的
// executeToolCall 同族形状:错误 {"error":…},成功 {"summary",payload})。
func feedbackError(message string) string {
	raw, _ := json.Marshal(map[string]string{"error": message})
	return string(raw)
}

func feedbackResult(summary string, payload json.RawMessage) string {
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
