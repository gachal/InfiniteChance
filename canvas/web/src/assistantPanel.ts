/**
 * 画布助手面板的纯函数层(91 号票):草案参数的定形/校验、变更清单文案、
 * 会话消息的本地窗口。落图动作不在本模块 —— 视图经 AssistantHost 接口
 * 调 useVueFlow 的 addNodes/updateNodeData/removeNodes 走既有
 * markChanged → autosave 链(ADR 0006 前端唯一写者,应用器不绕过 autosave)。
 */

import { isCanvasNodeType, NODE_TYPE_LABEL, type CanvasNodeType } from './graph'

/** 授权档两档(91 号票定案;键名与后端 assistant.ModeAsk/ModeFull 同串)。 */
export type AssistantMode = 'ask' | 'full'

export const ASSISTANT_MODE_LABELS: Record<AssistantMode, string> = {
  ask: '先询问我',
  full: '完全授权',
}

/** 七个写图草案工具(read_canvas/list_assets/read_source/set_style 是读/
 * 库表类,服务端同步执行,前端不发回执、不出卡片)。 */
export const ASSISTANT_DRAFT_TOOLS = [
  'create_nodes',
  'update_nodes',
  'delete_nodes',
  'connect_nodes',
  'disconnect_nodes',
  'trigger_task',
] as const

export function isAssistantDraftTool(name: string): boolean {
  return (ASSISTANT_DRAFT_TOOLS as readonly string[]).includes(name)
}

// ---- 草案形状(与后端 tools.go 的 schema 同形)----

export interface AssistantNodeSpec {
  type: CanvasNodeType
  title?: string
  prompt?: string
  text?: string
  x?: number
  y?: number
}

/** from/to 是「本批下标(number)或既有节点 id(string)」的联合。 */
export interface AssistantEdgeRef {
  from: number | string
  to: number | string
}

export interface AssistantCreateNodesDraft {
  nodes: AssistantNodeSpec[]
  edges: AssistantEdgeRef[]
}

export interface AssistantUpdateNodeOp {
  id: string
  title?: string
  prompt?: string
  text?: string
}

export interface AssistantUpdateNodesDraft {
  nodes: AssistantUpdateNodeOp[]
}

export interface AssistantEdgePair {
  source: string
  target: string
}

/** 与 @infinitechance/api 的 CanvasVideoRef 同形(kind/role 收枚举,
 * 提交侧类型直接兼容)。 */
export interface AssistantVideoRef {
  url: string
  kind: 'image' | 'video' | 'audio'
  role: 'first_frame' | 'last_frame' | 'reference_image' | 'reference_video' | 'reference_audio'
}

export interface AssistantTriggerTaskDraft {
  node_id: string
  kind: 'image' | 'video'
  prompt: string
  model: string
  size?: string
  ratio?: string
  background?: string
  seconds?: number
  scene_type?: string
  image_urls?: string[]
  video_refs?: AssistantVideoRef[]
}

// ---- 取参小件(形状不对抛错 → 回执 failed,模型看到原因)----

function draftObject(args: unknown): Record<string, unknown> {
  if (typeof args !== 'object' || args === null || Array.isArray(args)) {
    throw new Error('调用参数不是合法对象')
  }
  return args as Record<string, unknown>
}

function optionalString(obj: Record<string, unknown>, key: string): string | undefined {
  const v = obj[key]
  if (v === undefined || v === null) {
    return undefined
  }
  if (typeof v !== 'string') {
    throw new Error(`${key} 必须是字符串`)
  }
  return v
}

function requiredString(obj: Record<string, unknown>, key: string): string {
  const v = optionalString(obj, key)
  if (v === undefined || v.trim() === '') {
    throw new Error(`${key} 不能为空`)
  }
  return v.trim()
}

function optionalNumber(obj: Record<string, unknown>, key: string): number | undefined {
  const v = obj[key]
  if (v === undefined || v === null) {
    return undefined
  }
  if (typeof v !== 'number' || !Number.isFinite(v)) {
    throw new Error(`${key} 必须是数字`)
  }
  return v
}

/** edgeRefOf 解析 from/to 联合:数字 = 本批下标,字符串 = 既有节点 id。 */
function edgeRefOf(obj: Record<string, unknown>, key: string): number | string {
  const v = obj[key]
  if (typeof v === 'number' && Number.isFinite(v)) {
    return v
  }
  if (typeof v === 'string' && v !== '') {
    return v
  }
  throw new Error(`${key} 必须是数字(本批下标)或字符串(节点 id)`)
}

export function parseCreateNodesDraft(args: unknown): AssistantCreateNodesDraft {
  const obj = draftObject(args)
  const rawNodes = obj['nodes']
  if (!Array.isArray(rawNodes) || rawNodes.length === 0) {
    throw new Error('nodes 不能为空数组')
  }
  const nodes = rawNodes.map((raw, i): AssistantNodeSpec => {
    if (typeof raw !== 'object' || raw === null || Array.isArray(raw)) {
      throw new Error(`nodes[${i}] 必须是对象`)
    }
    const m = raw as Record<string, unknown>
    const type = requiredString(m, 'type')
    if (!isCanvasNodeType(type)) {
      throw new Error(`nodes[${i}].type ${type} 不是合法的节点类型`)
    }
    const spec: AssistantNodeSpec = { type }
    const title = optionalString(m, 'title')
    if (title !== undefined && title !== '') {
      spec.title = title
    }
    const prompt = optionalString(m, 'prompt')
    if (prompt !== undefined && prompt !== '') {
      spec.prompt = prompt
    }
    const text = optionalString(m, 'text')
    if (text !== undefined && text !== '') {
      spec.text = text
    }
    const x = optionalNumber(m, 'x')
    if (x !== undefined) {
      spec.x = x
    }
    const y = optionalNumber(m, 'y')
    if (y !== undefined) {
      spec.y = y
    }
    return spec
  })
  const rawEdges = obj['edges']
  const edges: AssistantEdgeRef[] = []
  if (rawEdges !== undefined && rawEdges !== null) {
    if (!Array.isArray(rawEdges)) {
      throw new Error('edges 必须是数组')
    }
    rawEdges.forEach((raw, i) => {
      if (typeof raw !== 'object' || raw === null || Array.isArray(raw)) {
        throw new Error(`edges[${i}] 必须是对象`)
      }
      const m = raw as Record<string, unknown>
      edges.push({ from: edgeRefOf(m, 'from'), to: edgeRefOf(m, 'to') })
    })
  }
  return { nodes, edges }
}

export function parseUpdateNodesDraft(args: unknown): AssistantUpdateNodesDraft {
  const obj = draftObject(args)
  const rawNodes = obj['nodes']
  if (!Array.isArray(rawNodes) || rawNodes.length === 0) {
    throw new Error('nodes 不能为空数组')
  }
  const nodes = rawNodes.map((raw, i): AssistantUpdateNodeOp => {
    if (typeof raw !== 'object' || raw === null || Array.isArray(raw)) {
      throw new Error(`nodes[${i}] 必须是对象`)
    }
    const m = raw as Record<string, unknown>
    const op: AssistantUpdateNodeOp = { id: requiredString(m, 'id') }
    for (const field of ['title', 'prompt', 'text'] as const) {
      const v = optionalString(m, field)
      if (v !== undefined && v !== '') {
        op[field] = v
      }
    }
    if (op.title === undefined && op.prompt === undefined && op.text === undefined) {
      throw new Error(`nodes[${i}] 没有给出任何要修改的字段`)
    }
    return op
  })
  return { nodes }
}

export function parseDeleteNodesDraft(args: unknown): string[] {
  const obj = draftObject(args)
  const rawIds = obj['ids']
  if (!Array.isArray(rawIds) || rawIds.length === 0) {
    throw new Error('ids 不能为空数组')
  }
  return rawIds.map((raw, i) => {
    if (typeof raw !== 'string' || raw.trim() === '') {
      throw new Error(`ids[${i}] 必须是非空字符串(节点 id)`)
    }
    return raw.trim()
  })
}

export function parseEdgePairsDraft(args: unknown): AssistantEdgePair[] {
  const obj = draftObject(args)
  const rawEdges = obj['edges']
  if (!Array.isArray(rawEdges) || rawEdges.length === 0) {
    throw new Error('edges 不能为空数组')
  }
  return rawEdges.map((raw, i) => {
    if (typeof raw !== 'object' || raw === null || Array.isArray(raw)) {
      throw new Error(`edges[${i}] 必须是对象`)
    }
    const m = raw as Record<string, unknown>
    return { source: requiredString(m, 'source'), target: requiredString(m, 'target') }
  })
}

export function parseTriggerTaskDraft(args: unknown): AssistantTriggerTaskDraft {
  const obj = draftObject(args)
  const kindRaw = optionalString(obj, 'kind') ?? 'image'
  if (kindRaw !== 'image' && kindRaw !== 'video') {
    throw new Error('kind 只支持 image 或 video')
  }
  const draft: AssistantTriggerTaskDraft = {
    node_id: requiredString(obj, 'node_id'),
    kind: kindRaw,
    prompt: optionalString(obj, 'prompt') ?? '',
    model: requiredString(obj, 'model'),
  }
  for (const field of ['size', 'ratio', 'background', 'scene_type'] as const) {
    const v = optionalString(obj, field)
    if (v !== undefined && v !== '') {
      draft[field] = v
    }
  }
  const seconds = optionalNumber(obj, 'seconds')
  if (seconds !== undefined) {
    draft.seconds = Math.round(seconds)
  }
  const urls = obj['image_urls']
  if (urls !== undefined && urls !== null) {
    if (!Array.isArray(urls) || urls.some((u) => typeof u !== 'string')) {
      throw new Error('image_urls 必须是字符串数组')
    }
    if (urls.length > 0) {
      draft.image_urls = urls as string[]
    }
  }
  const refs = obj['video_refs']
  if (refs !== undefined && refs !== null) {
    if (!Array.isArray(refs)) {
      throw new Error('video_refs 必须是数组')
    }
    const parsed = refs.map((raw, i): AssistantVideoRef => {
      if (typeof raw !== 'object' || raw === null || Array.isArray(raw)) {
        throw new Error(`video_refs[${i}] 必须是对象`)
      }
      const m = raw as Record<string, unknown>
      const ref: AssistantVideoRef = {
        url: requiredString(m, 'url'),
        kind: requiredString(m, 'kind') as AssistantVideoRef['kind'],
        role: requiredString(m, 'role') as AssistantVideoRef['role'],
      }
      return ref
    })
    if (parsed.length > 0) {
      draft.video_refs = parsed
    }
  }
  return draft
}

// ---- 变更清单文案(「先询问我」卡片;每工具一条标题 + 可选明细行)----

export interface DraftCard {
  headline: string
  lines: string[]
}

/** 把一次工具草案渲染成人读的变更清单(不做逐项勾选 —— 整批批准/拒绝)。 */
export function describeDraft(name: string, args: unknown): DraftCard {
  switch (name) {
    case 'create_nodes': {
      const draft = parseCreateNodesDraft(args)
      const lines = draft.nodes.map((n) => {
        const label = NODE_TYPE_LABEL[n.type] ?? n.type
        const content = n.title ?? n.prompt ?? n.text ?? ''
        const clipped = content.length > 24 ? `${content.slice(0, 24)}…` : content
        return `新建${label}节点${clipped ? `「${clipped}」` : ''}`
      })
      if (draft.edges.length > 0) {
        lines.push(`连线 ${draft.edges.length} 条`)
      }
      return { headline: `新建 ${draft.nodes.length} 个节点`, lines }
    }
    case 'update_nodes': {
      const draft = parseUpdateNodesDraft(args)
      const lines = draft.nodes.map((op) => {
        const fields = [op.title !== undefined && '标题', op.prompt !== undefined && '提示词', op.text !== undefined && '正文'].filter(Boolean)
        return `「${op.id}」改 ${fields.join('、')}`
      })
      return { headline: `修改 ${draft.nodes.length} 个节点`, lines }
    }
    case 'delete_nodes': {
      const ids = parseDeleteNodesDraft(args)
      return { headline: `删除 ${ids.length} 个节点`, lines: ids.map((id) => `「${id}」`) }
    }
    case 'connect_nodes': {
      const pairs = parseEdgePairsDraft(args)
      return { headline: `连线 ${pairs.length} 条`, lines: pairs.map((p) => `${p.source} → ${p.target}`) }
    }
    case 'disconnect_nodes': {
      const pairs = parseEdgePairsDraft(args)
      return { headline: `断开 ${pairs.length} 条连线`, lines: pairs.map((p) => `${p.source} ✕ ${p.target}`) }
    }
    case 'trigger_task': {
      const draft = parseTriggerTaskDraft(args)
      const kindLabel = draft.kind === 'video' ? '视频' : '图片'
      return {
        headline: `提交${kindLabel}生成任务(真实消耗额度)`,
        lines: [`节点「${draft.node_id}」`, `模型 ${draft.model}`],
      }
    }
    default:
      throw new Error(`工具 ${name} 不是画布助手草案工具`)
  }
}

// ---- 会话消息的本地窗口(照 appendAgentTurn 纯函数先例:超限截最旧,
// 不改入参)----

/** 视图实现的图应用回调面(面板持协议,视图持 useVueFlow 落图动作;ADR
 * 0006 前端唯一写者:全部走 markChanged → autosave,任务提交走既有
 * createCanvasTask 同链路)。errors 非空 = 部分条目被拒(EDGE_MATRIX /
 * 节点不存在等),如实进回执;抛错 = 整批失败。 */
export interface AssistantApplyOutcome {
  errors: string[]
}

export interface AssistantHost {
  applyCreateNodes(draft: AssistantCreateNodesDraft): AssistantApplyOutcome & {
    nodes: { index: number; id: string }[]
  }
  applyUpdateNodes(draft: AssistantUpdateNodesDraft): AssistantApplyOutcome & { updated: number }
  applyDeleteNodes(ids: string[]): AssistantApplyOutcome & { deleted: number }
  applyConnect(pairs: AssistantEdgePair[]): AssistantApplyOutcome & { applied: number }
  applyDisconnect(pairs: AssistantEdgePair[]): AssistantApplyOutcome & { applied: number }
  /** 走既有 POST /canvases/:id/tasks(composer 提交链:markChanged →
   * flush → createCanvasTask → taskSync.track);失败抛错 → 回执 failed。 */
  submitTask(draft: AssistantTriggerTaskDraft): Promise<{ task_id: string }>
}

/** 本地窗口与服务端同宽(发给 LLM 的上下文 40 条截最旧;本地渲染全量靠
 * 会话详情,这里只约束乐观追加后的内存数组防失控)。 */
export const ASSISTANT_MAX_LOCAL_MESSAGES = 40

/** 追加一轮(user + assistant)到本地消息数组,超限截最旧,返回新数组。 */
export function appendAssistantTurn(
  messages: { role: 'user' | 'assistant'; content: string; summary?: string }[],
  user: string,
  assistant: { content: string; summary?: string },
): { role: 'user' | 'assistant'; content: string; summary?: string }[] {
  const next = [
    ...messages,
    { role: 'user' as const, content: user },
    { role: 'assistant' as const, content: assistant.content, ...(assistant.summary ? { summary: assistant.summary } : {}) },
  ]
  return next.length <= ASSISTANT_MAX_LOCAL_MESSAGES ? next : next.slice(next.length - ASSISTANT_MAX_LOCAL_MESSAGES)
}

// ---- 面板打开时的会话保持(2026-11-19 增补):「默认打开就是新会话,
// 除非手动新建,一直是该会话」——上次活跃会话按画布记 localStorage(键含
// 画布 id,不进库不同步多端),面板每次挂载回读;存取照 canvasPet.ts 同款
// try/catch 纪律(隐私模式静默退化,只是不记忆)。----

export const ASSISTANT_SESSION_KEY_PREFIX = 'infinitechance.canvas.assistant.session.'

export function assistantSessionKey(canvasId: number): string {
  return `${ASSISTANT_SESSION_KEY_PREFIX}${canvasId}`
}

export function loadSavedAssistantSessionId(canvasId: number): number | null {
  try {
    const raw = localStorage.getItem(assistantSessionKey(canvasId))
    if (raw === null) {
      return null
    }
    const parsed = Number(raw)
    return Number.isInteger(parsed) && parsed > 0 ? parsed : null
  } catch {
    return null
  }
}

/** id 为 null = 清记忆(删除当前会话/列表里失靶时)。 */
export function saveAssistantSessionId(canvasId: number, id: number | null): void {
  try {
    if (id === null) {
      localStorage.removeItem(assistantSessionKey(canvasId))
    } else {
      localStorage.setItem(assistantSessionKey(canvasId), String(id))
    }
  } catch {
    // 静默:记忆不可用不影响面板本体。
  }
}

export type AssistantSessionRestoreTarget =
  | { kind: 'open'; id: number }
  | { kind: 'create' }

/** 面板挂载时的恢复决策:记忆的会话还在列表 → 续用;没有记忆/已失靶
 * (他端删除等)→ 自动建新会话。sessions 只取 id(结构化最小面,列表
 * 来自 listAssistantSessions,newest first 与决策无关)。 */
export function restoreAssistantSessionTarget(
  sessions: { id: number }[],
  savedId: number | null,
): AssistantSessionRestoreTarget {
  if (savedId !== null && sessions.some((s) => s.id === savedId)) {
    return { kind: 'open', id: savedId }
  }
  return { kind: 'create' }
}
