import { describe, expect, it } from 'vitest'

import {
  AGENT_TOOL_LABELS,
  type AgentToolStep,
  agentToolLabel,
  applyAgentToolResult,
  setAgentToolApplier,
  upsertAgentToolStep,
} from './agentTools'

describe('agentToolLabel', () => {
  it('maps known tools to Chinese labels and falls back to the raw name', () => {
    // 注册表标签随 66 号票领域工具落位,此处钉住「映射命中优先、未命中
    // 原样回退」的语义。
    expect(agentToolLabel('no_such_tool')).toBe('no_such_tool')
    expect(typeof AGENT_TOOL_LABELS).toBe('object')
  })
})

describe('applyAgentToolResult(ADR 0006 前端落图 seam)', () => {
  it('returns null when the payload carries nothing to apply', () => {
    expect(applyAgentToolResult('any_tool', undefined)).toBeNull()
    expect(applyAgentToolResult('any_tool', null)).toBeNull()
  })

  it('applies via the registered applier and reports success', () => {
    const seen: unknown[] = []
    setAgentToolApplier('draft_tool', (payload) => {
      seen.push(payload)
    })
    expect(applyAgentToolResult('draft_tool', { draft: { x: 1 } })).toBeNull()
    expect(seen).toEqual([{ draft: { x: 1 } }])
  })

  it('errors explicitly on a write-graph payload with no registered applier', () => {
    const err = applyAgentToolResult('unknown_writer', { draft: 1 })
    expect(err).not.toBeNull()
    expect(err).toContain('unknown_writer')
  })

  it('surfaces applier failures instead of swallowing them', () => {
    setAgentToolApplier('boom_tool', () => {
      throw new Error('校验拒绝')
    })
    const err = applyAgentToolResult('boom_tool', { draft: 1 })
    expect(err).toBe('校验拒绝')
  })

  it('removes appliers when cleared', () => {
    setAgentToolApplier('temp_tool', () => {})
    setAgentToolApplier('temp_tool', null)
    expect(applyAgentToolResult('temp_tool', { draft: 1 })).not.toBeNull()
  })
})

describe('agent tool steps(过程展示的状态机)', () => {
  it('upsertAgentToolStep starts a step on tool_call and closes it on tool_result', () => {
    let steps: AgentToolStep[] = []
    steps = upsertAgentToolStep(steps, { type: 'tool_call', id: 'c1', name: 'create_character' })
    expect(steps).toEqual([{ id: 'c1', name: 'create_character', state: 'running' }])

    steps = upsertAgentToolStep(steps, {
      type: 'tool_result',
      id: 'c1',
      name: 'create_character',
      summary: '已创建 张三',
    })
    expect(steps).toEqual([{ id: 'c1', name: 'create_character', state: 'done', summary: '已创建 张三' }])

    // 重复 id 不重复入列;失败态由 error 布尔位标记。
    steps = upsertAgentToolStep(steps, { type: 'tool_call', id: 'c1', name: 'create_character' })
    expect(steps).toHaveLength(1)
    steps = upsertAgentToolStep(steps, {
      type: 'tool_result',
      id: 'c1',
      name: 'create_character',
      summary: '执行失败',
    }, true)
    expect(steps[0]).toEqual({ id: 'c1', name: 'create_character', state: 'failed', summary: '执行失败' })
  })
})
