// Agent 工具循环的前端 seam(63 号票,ADR 0006 单一写者):后端工具永远
// 只产出「图改动草案」(tool_result.payload),应用图由前端独占 —— 走既有
// scene-change → updateNodeData → autosave 链。66 号票落八领域工具:
// save_outline/save_script/save_storyboard 在此定义草案形状与校验(纯函数,
// 落图动作由编辑器装配时注册),create_*/list_assets/read_source 是库表/
// 只读类(无可落图之物,编辑器注册空应用器);未知写图类结果到达时显式
// 报错,不静默。

import type { StoryboardCut } from './graph'
import { SECONDS_MAX } from './storyboard'
import { MAX_SCRIPT_CHARS } from './script'

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
 * 与后端注册表八工具一一对应;未映射的原样显示工具名。 */
export const AGENT_TOOL_LABELS: Record<string, string> = {
  create_character: '创建角色',
  create_scene: '创建场景',
  create_prop: '创建道具',
  list_assets: '读取资产库',
  save_outline: '保存大纲',
  save_script: '保存剧本',
  save_storyboard: '保存分镜板',
  read_source: '读取原文',
}

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

// ---- 写图类草案的定形与校验(66 号票,纯函数) ----
// 后端 payload 与前端 StoryboardCut 的契约在此收口:类型不对/越界一律抛错
// (ADR 0006:应用失败必须显式报错),成功返回可直接落进节点 data 的形状。

/** payloadObject 断言 payload 是对象,否则报错(非对象无法应用)。 */
function payloadObject(payload: unknown): Record<string, unknown> {
  if (typeof payload !== 'object' || payload === null || Array.isArray(payload)) {
    throw new Error('工具结果不是合法对象,无法应用到画布')
  }
  return payload as Record<string, unknown>
}

/** optionalString 取可选字符串字段:缺省/ null 返回空串,类型不对抛错。 */
function optionalString(obj: Record<string, unknown>, key: string): string {
  const v = obj[key]
  if (v === undefined || v === null) {
    return ''
  }
  if (typeof v !== 'string') {
    throw new Error(`${key} 必须是字符串`)
  }
  return v
}

/** optionalStringArray 取可选字符串数组字段。 */
function optionalStringArray(obj: Record<string, unknown>, key: string): string[] {
  const v = obj[key]
  if (v === undefined || v === null) {
    return []
  }
  if (!Array.isArray(v) || v.some((item) => typeof item !== 'string')) {
    throw new Error(`${key} 必须是字符串数组`)
  }
  return v
}

/** optionalPositiveIntArray 取可选正整数数组字段(cut 的出场角色引用)。 */
function optionalPositiveIntArray(obj: Record<string, unknown>, key: string): number[] {
  const v = obj[key]
  if (v === undefined || v === null) {
    return []
  }
  if (!Array.isArray(v) || v.some((item) => typeof item !== 'number' || !Number.isInteger(item) || item <= 0)) {
    throw new Error(`${key} 必须是正整数数组`)
  }
  return v
}

/** save_outline / save_script 的草案(title + text,script 节点 data)。 */
export interface AgentScriptDraft {
  title: string
  text: string
}

/** 定形 script 节点草案:正文必填、trim,30 万字上限与 script.ts 同规
 * (超限让模型分批保存,不静默截断)。 */
export function scriptDraftFromPayload(payload: unknown): AgentScriptDraft {
  const obj = payloadObject(payload)
  const title = optionalString(obj, 'title').trim()
  const rawText = optionalString(obj, 'text')
  if (rawText.trim() === '') {
    throw new Error('剧本正文为空,无法落图')
  }
  const text = rawText.trim()
  if (text.length > MAX_SCRIPT_CHARS) {
    throw new Error(`剧本正文超出 ${MAX_SCRIPT_CHARS} 字上限,请让 Agent 分批保存`)
  }
  return { title, text }
}

/** save_storyboard 的草案(絵コンテ节点 data)。 */
export interface AgentStoryboardDraft {
  title: string
  cuts: StoryboardCut[]
}

/** 定形絵コンテ节点草案:画面恒空(asset_id = 0,「未配画面」占位),
 * 景别/运镜/出场角色按 67 号票扩字段透传;字段形状不对抛错。 */
export function storyboardDraftFromPayload(payload: unknown): AgentStoryboardDraft {
  const obj = payloadObject(payload)
  const title = optionalString(obj, 'title').trim()
  const rawCuts = obj['cuts']
  if (rawCuts === undefined || rawCuts === null) {
    throw new Error('分镜板草案缺少 cuts')
  }
  if (!Array.isArray(rawCuts)) {
    throw new Error('cuts 必须是数组')
  }
  if (rawCuts.length === 0) {
    throw new Error('分镜板草案至少要有一个分镜')
  }
  const cuts = rawCuts.map((raw): StoryboardCut => {
    if (typeof raw !== 'object' || raw === null || Array.isArray(raw)) {
      throw new Error('每个 cut 必须是对象')
    }
    const cut = raw as Record<string, unknown>
    const draft: StoryboardCut = {
      asset_id: 0,
      lines: optionalString(cut, 'lines'),
      camera_moves: optionalStringArray(cut, 'camera_moves'),
    }
    const shot = optionalString(cut, 'shot')
    if (shot !== '') {
      draft.shot = shot
    }
    const note = optionalString(cut, 'note')
    if (note !== '') {
      draft.note = note
    }
    const seconds = cut['seconds']
    if (seconds !== undefined && seconds !== null) {
      if (typeof seconds !== 'number' || !Number.isFinite(seconds) || seconds < 0) {
        throw new Error('seconds 必须是非负数字')
      }
      draft.seconds = Math.min(Math.round(seconds), SECONDS_MAX)
    }
    const characterIds = optionalPositiveIntArray(cut, 'character_ids')
    if (characterIds.length > 0) {
      draft.character_ids = characterIds
    }
    return draft
  })
  return { title, cuts }
}
