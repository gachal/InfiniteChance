// Agent 工具循环的前端 seam(63 号票,ADR 0006 单一写者):后端工具永远
// 只产出「图改动草案」(tool_result.payload),应用图由前端独占 —— 走既有
// scene-change → updateNodeData → autosave 链。本票只建机制:工具名 → 应用
// 函数的映射表留空,66 号票的六个领域工具(save_outline/save_script/
// save_storyboard/create_* 等)在此注册各自的应用函数;未知写图类结果到达
// 时显式报错,不静默。

/** 一次工具调用的过程展示条目(节点内时间线/chip 的数据形状,UI 态不入
 * 节点 data)。 */
export interface AgentToolStep {
  id: string
  name: string
  state: 'running' | 'done' | 'failed'
  /** tool_result 的人读摘要,done/failed 时填充。 */
  summary?: string
}

/** 工具名的中文展示标签(过程展示文案,如 create_character → 「创建角色」
 * —— 「正在创建角色…」式文案由 AgentNode 以 running 态 + 标签拼出)。
 * 66 号票落领域工具时补映射;未映射的原样显示工具名。 */
export const AGENT_TOOL_LABELS: Record<string, string> = {}

export function agentToolLabel(name: string): string {
  return AGENT_TOOL_LABELS[name] ?? name
}

/** 应用函数签名:收 tool_result.payload(结构化草案),把改动应用进本地图
 * 状态。抛错 = 应用失败(校验拒等),由调用方显式报错不静默。 */
export type AgentToolApplier = (payload: unknown) => void

const agentToolAppliers: Record<string, AgentToolApplier> = {}

/** 注册/注销一个工具的前端应用函数(wiring 语义:编辑器装配时注册 66 号
 * 票的落图应用器;传 null 注销)。 */
export function setAgentToolApplier(name: string, apply: AgentToolApplier | null): void {
  if (apply === null) {
    delete agentToolAppliers[name]
    return
  }
  agentToolAppliers[name] = apply
}

/** 应用一条 tool_result(ADR 0006 Consequences):有应用器的工具走注册
 * 函数;没有应用器却携带 payload(写图类结果)的,显式报错文案交会话
 * 展示,不允许静默丢弃。返回错误文案,null = 应用成功或无可应用物。 */
export function applyAgentToolResult(name: string, payload: unknown): string | null {
  if (payload === undefined || payload === null) {
    return null
  }
  const apply = agentToolAppliers[name]
  if (!apply) {
    return `工具 ${name} 的结果无法应用到画布:没有注册该工具的应用器(请更新客户端后重试)`
  }
  try {
    apply(payload)
    return null
  } catch (e) {
    return e instanceof Error ? e.message : '工具结果应用失败'
  }
}

/** 过程展示的状态机:tool_call 开条目(running),tool_result 收口(done,
 * 失败用 failed 标记)。重复 id 原地更新不重复入列,返回新数组不改入参。 */
export function upsertAgentToolStep(
  steps: AgentToolStep[],
  event: { type: string; id: string; name: string; summary?: string },
  failed = false,
): AgentToolStep[] {
  const state: AgentToolStep['state'] = event.type === 'tool_call' ? 'running' : failed ? 'failed' : 'done'
  const existing = steps.find((s) => s.id === event.id)
  if (existing) {
    return steps.map((s) =>
      s.id === event.id
        ? { ...s, state, ...(event.summary !== undefined ? { summary: event.summary } : {}) }
        : s,
    )
  }
  return [...steps, { id: event.id, name: event.name, state }]
}
