import { describe, expect, it } from 'vitest'

import {
  AGENT_TOOL_LABELS,
  type AgentToolStep,
  agentToolLabel,
  applyAgentToolResult,
  scriptDraftFromPayload,
  setAgentToolApplier,
  storyboardDraftFromPayload,
  upsertAgentToolStep,
} from './agentTools'

describe('agentToolLabel', () => {
  it('maps known tools to Chinese labels and falls back to the raw name', () => {
    // 66 号票八工具的中文标签落位;未命中仍原样回退。
    expect(agentToolLabel('create_character')).toBe('创建角色')
    expect(agentToolLabel('create_scene')).toBe('创建场景')
    expect(agentToolLabel('create_prop')).toBe('创建道具')
    expect(agentToolLabel('list_assets')).toBe('读取资产库')
    expect(agentToolLabel('save_outline')).toBe('保存大纲')
    expect(agentToolLabel('save_script')).toBe('保存剧本')
    expect(agentToolLabel('save_storyboard')).toBe('保存分镜板')
    expect(agentToolLabel('read_source')).toBe('读取原文')
    expect(agentToolLabel('no_such_tool')).toBe('no_such_tool')
    expect(Object.keys(AGENT_TOOL_LABELS)).toHaveLength(8)
  })
})

describe('scriptDraftFromPayload(save_outline / save_script 的草案)', () => {
  it('parses title + text and trims both', () => {
    expect(scriptDraftFromPayload({ title: '  第3集  ', text: ' 正文 ' })).toEqual({
      title: '第3集',
      text: '正文',
    })
    expect(scriptDraftFromPayload({ text: '只有正文' })).toEqual({ title: '', text: '只有正文' })
  })

  it('rejects non-objects, empty text and overlength text explicitly', () => {
    expect(() => scriptDraftFromPayload('一段文字')).toThrow('不是合法对象')
    expect(() => scriptDraftFromPayload({ text: '   ' })).toThrow('正文为空')
    expect(() => scriptDraftFromPayload({ text: 42 })).toThrow('text 必须是字符串')
    expect(() => scriptDraftFromPayload({ text: '字'.repeat(300_001) })).toThrow('上限')
  })
})

describe('storyboardDraftFromPayload(save_storyboard 的草案)', () => {
  it('maps cuts to StoryboardCut with empty frames', () => {
    const draft = storyboardDraftFromPayload({
      title: 'EP01 分镜',
      cuts: [
        { shot: '远', lines: '城市空镜', camera_moves: ['推', '手持'], seconds: 8, character_ids: [1, 2] },
        { note: '转场空镜' },
      ],
    })
    expect(draft.title).toBe('EP01 分镜')
    // camera_moves 恒在(emptyStoryboardCut 基形);shot/note/character_ids
    // 只在非空时携带,画面 asset_id 恒 0。
    expect(draft.cuts).toEqual([
      { asset_id: 0, lines: '城市空镜', camera_moves: ['推', '手持'], seconds: 8, shot: '远', character_ids: [1, 2] },
      { asset_id: 0, lines: '', camera_moves: [], note: '转场空镜' },
    ])
    expect('character_ids' in draft.cuts[1]).toBe(false)
    expect('shot' in draft.cuts[1]).toBe(false)
    expect('seconds' in draft.cuts[1]).toBe(false)
  })

  it('rejects missing/empty cuts and malformed cut fields explicitly', () => {
    expect(() => storyboardDraftFromPayload({ title: 'x' })).toThrow('cuts')
    expect(() => storyboardDraftFromPayload({ cuts: [] })).toThrow('至少')
    expect(() => storyboardDraftFromPayload({ cuts: ['一镜'] })).toThrow('必须是对象')
    expect(() => storyboardDraftFromPayload({ cuts: [{ camera_moves: '推' }] })).toThrow('camera_moves')
    expect(() => storyboardDraftFromPayload({ cuts: [{ character_ids: [0] }] })).toThrow('character_ids')
    expect(() => storyboardDraftFromPayload({ cuts: [{ seconds: -1 }] })).toThrow('seconds')
    expect(() => storyboardDraftFromPayload({ cuts: [{ shot: 7 }] })).toThrow('shot')
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
