import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  appendAssistantTurn,
  assistantSessionKey,
  ASSISTANT_MAX_LOCAL_MESSAGES,
  ASSISTANT_SESSION_KEY_PREFIX,
  describeDraft,
  isAssistantDraftTool,
  loadSavedAssistantSessionId,
  parseCreateNodesDraft,
  parseDeleteNodesDraft,
  parseEdgePairsDraft,
  parseTriggerTaskDraft,
  parseUpdateNodesDraft,
  restoreAssistantSessionTarget,
  saveAssistantSessionId,
} from './assistantPanel'

describe('assistantPanel 草案定形(91 号票)', () => {
  it('parseCreateNodesDraft shapes nodes and edges, rejecting bad shapes', () => {
    const draft = parseCreateNodesDraft({
      nodes: [
        { type: 'image', prompt: '主角立绘' },
        { type: 'script', text: '第一集', title: '第 1 集', x: 10, y: 20 },
      ],
      edges: [{ from: 0, to: 1 }, { from: 1, to: 'image-9' }],
    })
    expect(draft.nodes).toHaveLength(2)
    expect(draft.nodes[0]).toEqual({ type: 'image', prompt: '主角立绘' })
    expect(draft.nodes[1]).toEqual({ type: 'script', text: '第一集', title: '第 1 集', x: 10, y: 20 })
    expect(draft.edges).toEqual([
      { from: 0, to: 1 },
      { from: 1, to: 'image-9' },
    ])

    expect(() => parseCreateNodesDraft({ nodes: [] })).toThrow('nodes 不能为空数组')
    expect(() => parseCreateNodesDraft({ nodes: [{ type: 'music' }] })).toThrow('不是合法的节点类型')
    expect(() => parseCreateNodesDraft({ nodes: [{ type: 'image' }], edges: [{ from: true, to: 1 }] })).toThrow(
      'from 必须是数字(本批下标)或字符串(节点 id)',
    )
  })

  it('parseUpdateNodesDraft requires id and at least one field', () => {
    const draft = parseUpdateNodesDraft({ nodes: [{ id: 'image-9', prompt: '新提示词' }] })
    expect(draft.nodes).toEqual([{ id: 'image-9', prompt: '新提示词' }])
    expect(() => parseUpdateNodesDraft({ nodes: [{ id: 'image-9' }] })).toThrow('没有给出任何要修改的字段')
    expect(() => parseUpdateNodesDraft({ nodes: [{ prompt: '缺 id' }] })).toThrow('id 不能为空')
  })

  it('parseDeleteNodesDraft and parseEdgePairsDraft pin their shapes', () => {
    expect(parseDeleteNodesDraft({ ids: ['a', ' b '] })).toEqual(['a', 'b'])
    expect(() => parseDeleteNodesDraft({ ids: [] })).toThrow('ids 不能为空数组')
    expect(() => parseDeleteNodesDraft({ ids: [42] })).toThrow('必须是非空字符串')

    expect(parseEdgePairsDraft({ edges: [{ source: 'a', target: 'b' }] })).toEqual([{ source: 'a', target: 'b' }])
    expect(() => parseEdgePairsDraft({ edges: [{ source: 'a' }] })).toThrow('target 不能为空')
  })

  it('parseTriggerTaskDraft aligns with the composer contract and rejects other kinds', () => {
    const draft = parseTriggerTaskDraft({
      node_id: 'image-1',
      kind: 'video',
      prompt: '一段视频',
      model: 'seedance',
      seconds: 5.2,
      video_refs: [{ url: 'https://x/a.png', kind: 'image', role: 'first_frame' }],
    })
    expect(draft).toEqual({
      node_id: 'image-1',
      kind: 'video',
      prompt: '一段视频',
      model: 'seedance',
      seconds: 5,
      video_refs: [{ url: 'https://x/a.png', kind: 'image', role: 'first_frame' }],
    })
    // 缺省 image。
    expect(parseTriggerTaskDraft({ node_id: 'n', model: 'm', prompt: 'p' }).kind).toBe('image')
    expect(() => parseTriggerTaskDraft({ node_id: 'n', model: 'm', prompt: 'p', kind: 'music' })).toThrow(
      'kind 只支持 image 或 video',
    )
  })

  it('describeDraft renders the change-list card per tool', () => {
    expect(
      describeDraft('create_nodes', { nodes: [{ type: 'image', prompt: '主角立绘' }], edges: [{ from: 0, to: 'script-1' }] }),
    ).toEqual({ headline: '新建 1 个节点', lines: ['新建图片节点「主角立绘」', '连线 1 条'] })
    expect(describeDraft('update_nodes', { nodes: [{ id: 'a', title: 'T', text: 'x' }] })).toEqual({
      headline: '修改 1 个节点',
      lines: ['「a」改 标题、正文'],
    })
    expect(describeDraft('delete_nodes', { ids: ['a', 'b'] })).toEqual({
      headline: '删除 2 个节点',
      lines: ['「a」', '「b」'],
    })
    expect(describeDraft('trigger_task', { node_id: 'image-1', model: 'img-m', prompt: 'p' })).toEqual({
      headline: '提交图片生成任务(真实消耗额度)',
      lines: ['节点「image-1」', '模型 img-m'],
    })
    expect(() => describeDraft('read_canvas', {})).toThrow('不是画布助手草案工具')
    expect(isAssistantDraftTool('trigger_task')).toBe(true)
    expect(isAssistantDraftTool('read_canvas')).toBe(false)
  })

  it('appendAssistantTurn appends a round and caps at the window size', () => {
    const seeded = Array.from({ length: ASSISTANT_MAX_LOCAL_MESSAGES - 1 }, (_, i) => ({
      role: 'assistant' as const,
      content: `m${i}`,
    }))
    const next = appendAssistantTurn(seeded, '帮我加节点', { content: '已新建', summary: '本轮工具调用:create_nodes(已应用)' })
    expect(next).toHaveLength(ASSISTANT_MAX_LOCAL_MESSAGES)
    expect(next[next.length - 2]).toEqual({ role: 'user', content: '帮我加节点' })
    expect(next[next.length - 1]).toEqual({
      role: 'assistant',
      content: '已新建',
      summary: '本轮工具调用:create_nodes(已应用)',
    })
    // 不改入参;再追加一轮截掉最旧两条,首位是原第 3 条。
    const again = appendAssistantTurn(next, '再改', { content: 'ok' })
    expect(again).toHaveLength(ASSISTANT_MAX_LOCAL_MESSAGES)
    expect(again[0]).toEqual({ role: 'assistant', content: 'm3' })
  })
})

describe('assistantPanel 会话保持(2026-11-19 增补)', () => {
  const mem = new Map<string, string>()

  afterEach(() => {
    mem.clear()
    vi.unstubAllGlobals()
  })

  function stubStorage(): void {
    vi.stubGlobal('localStorage', {
      getItem: (k: string) => mem.get(k) ?? null,
      setItem: (k: string, v: string) => void mem.set(k, v),
      removeItem: (k: string) => void mem.delete(k),
    })
  }

  it('session key carries the canvas id', () => {
    expect(assistantSessionKey(7)).toBe(`${ASSISTANT_SESSION_KEY_PREFIX}7`)
  })

  it('save/load round-trips per canvas; null clears the memory', () => {
    stubStorage()
    expect(loadSavedAssistantSessionId(7)).toBeNull()
    saveAssistantSessionId(7, 42)
    saveAssistantSessionId(9, 43)
    expect(loadSavedAssistantSessionId(7)).toBe(42)
    expect(loadSavedAssistantSessionId(9)).toBe(43) // 互不串台(键含画布 id)
    expect(mem.get(assistantSessionKey(7))).toBe('42')
    saveAssistantSessionId(7, null)
    expect(loadSavedAssistantSessionId(7)).toBeNull()
    expect(mem.has(assistantSessionKey(7))).toBe(false)
  })

  it('ignores corrupt saved values', () => {
    stubStorage()
    mem.set(assistantSessionKey(3), 'not-a-number')
    expect(loadSavedAssistantSessionId(3)).toBeNull()
    mem.set(assistantSessionKey(3), '0')
    expect(loadSavedAssistantSessionId(3)).toBeNull()
  })

  it('storage failures degrade silently (privacy mode)', () => {
    vi.stubGlobal('localStorage', {
      getItem: () => {
        throw new Error('denied')
      },
      setItem: () => {
        throw new Error('denied')
      },
      removeItem: () => {
        throw new Error('denied')
      },
    })
    expect(loadSavedAssistantSessionId(1)).toBeNull()
    expect(() => saveAssistantSessionId(1, 5)).not.toThrow()
    expect(() => saveAssistantSessionId(1, null)).not.toThrow()
  })

  it('restoreAssistantSessionTarget opens the saved session when still listed', () => {
    const sessions = [{ id: 42 }, { id: 17 }, { id: 3 }]
    expect(restoreAssistantSessionTarget(sessions, 17)).toEqual({ kind: 'open', id: 17 })
    expect(restoreAssistantSessionTarget(sessions, 42)).toEqual({ kind: 'open', id: 42 })
  })

  it('restoreAssistantSessionTarget falls back to create on missing memory or a stale id', () => {
    const sessions = [{ id: 42 }]
    expect(restoreAssistantSessionTarget(sessions, null)).toEqual({ kind: 'create' })
    expect(restoreAssistantSessionTarget(sessions, 999)).toEqual({ kind: 'create' }) // 失靶(他端删除等)
    expect(restoreAssistantSessionTarget([], 42)).toEqual({ kind: 'create' })
  })
})
