import { describe, expect, it } from 'vitest'

import {
  AGENT_MAX_HISTORY_MESSAGES,
  type AgentChatMessage,
  agentCharacterInjection,
  agentRoundCount,
  appendAgentTurn,
  initialData,
  isCanvasNodeType,
  normalizeNodeType,
} from './graph'

describe('normalizeNodeType', () => {
  it('maps the legacy prompt type to agent in place', () => {
    // 29 号票就地迁移:历史 prompt 节点的 text 直接成为 Agent 节点文本区
    // 内容,无迁移损失。
    expect(normalizeNodeType('prompt')).toBe('agent')
  })

  it('keeps the known types untouched', () => {
    expect(normalizeNodeType('agent')).toBe('agent')
    expect(normalizeNodeType('image')).toBe('image')
    expect(normalizeNodeType('video')).toBe('video')
    expect(normalizeNodeType('analysis')).toBe('analysis')
    expect(normalizeNodeType('previz')).toBe('previz')
    // 52 号票:角色引用节点是第六种类型,原样通过(不作旧值迁移)。
    expect(normalizeNodeType('character')).toBe('character')
  })

  it('falls back to agent for unknown types', () => {
    expect(normalizeNodeType('mystery')).toBe('agent')
    expect(normalizeNodeType(undefined)).toBe('agent')
  })
})

describe('isCanvasNodeType / initialData', () => {
  it('accepts agent and rejects prompt', () => {
    expect(isCanvasNodeType('agent')).toBe(true)
    expect(isCanvasNodeType('prompt')).toBe(false)
  })

  it('seeds agent nodes with an empty text draft', () => {
    expect(initialData('agent')).toEqual({ text: '' })
    expect(initialData('image')).toEqual({ url: '', note: '' })
  })

  it('seeds previz nodes with the default scene (39 号票)', () => {
    const data = initialData('previz')
    expect(isCanvasNodeType('previz')).toBe(true)
    // 一个人形 + 一个机位,活跃机位指向列表内的实体。
    const scene = data as { objects: unknown[]; cameras: { id: string }[]; active_camera_id?: string }
    expect(scene.objects).toHaveLength(1)
    expect(scene.cameras).toHaveLength(1)
    expect(scene.active_camera_id).toBe(scene.cameras[0].id)
  })

  it('seeds character nodes with an unbound id (52 号票)', () => {
    // 只存 id 不存快照:新节点 character_id = 0(未绑),绑定后编辑器按
    // 画布 ip_id 的角色目录实时解析 —— 角色改名/换封面节点即变。
    expect(isCanvasNodeType('character')).toBe(true)
    expect(initialData('character')).toEqual({ character_id: 0 })
  })
})

describe('appendAgentTurn', () => {
  it('appends the user input and the assistant answer as one round', () => {
    const next = appendAgentTurn([], '赛博朋克城市', 'a neon cyberpunk city')
    expect(next).toEqual([
      { role: 'user', content: '赛博朋克城市' },
      { role: 'assistant', content: 'a neon cyberpunk city' },
    ])
  })

  it('carries media refs on the user turn when attachments were sent (32 号票)', () => {
    const media = [{ ref: '/api/assets/6/content', kind: 'image' as const }]
    const next = appendAgentTurn([], '分析这张图', 'a neon cyberpunk city', media)
    expect(next[0]).toEqual({ role: 'user', content: '分析这张图', media })
    expect(next[1]).toEqual({ role: 'assistant', content: 'a neon cyberpunk city' })
    // 无媒体轮不带 media 键,历史形状向后兼容。
    const plain = appendAgentTurn(next, '把色调改暖', 'warmer tones')
    expect(plain[2]).toEqual({ role: 'user', content: '把色调改暖' })
  })

  it('keeps alternation across rounds without mutating the input', () => {
    const seed: AgentChatMessage[] = [{ role: 'user', content: '主题' }]
    const next = appendAgentTurn(seed, '把色调改暖', 'warmer tones')
    expect(seed).toEqual([{ role: 'user', content: '主题' }])
    // 悬空的 user 消息保留,新轮按 user+assistant 追加。
    expect(next.map((m) => m.role)).toEqual(['user', 'user', 'assistant'])
  })

  it('truncates the oldest messages beyond the 20-round cap', () => {
    let history: AgentChatMessage[] = []
    for (let i = 0; i < 25; i++) {
      history = appendAgentTurn(history, `主题 ${i}`, `提示词 ${i}`)
    }
    expect(history.length).toBe(AGENT_MAX_HISTORY_MESSAGES)
    // 最旧的 5 轮被截掉:剩余首轮是第 5 轮的 user 消息。
    expect(history[0]).toEqual({ role: 'user', content: '主题 5' })
    expect(history[history.length - 1]).toEqual({ role: 'assistant', content: '提示词 24' })
  })
})

describe('agentRoundCount', () => {
  it('counts user messages as rounds', () => {
    expect(agentRoundCount([])).toBe(0)
    expect(
      agentRoundCount([
        { role: 'user', content: 'a' },
        { role: 'assistant', content: 'b' },
        { role: 'user', content: 'c' },
        { role: 'assistant', content: 'd' },
      ]),
    ).toBe(2)
  })
})

describe('agentCharacterInjection(52 号票历史固化)', () => {
  it('replays the frozen section alone once materialized', () => {
    // 有固化段只回传固化段:character_ids 不上送,服务端逐字复用、不取
    // 角色目录 —— 会话中途角色后改不影响已发生对话。
    expect(agentCharacterInjection([11, 12], '【角色设定】\n名字:林小满')).toEqual({
      character_section: '【角色设定】\n名字:林小满',
    })
  })

  it('sends character_ids on the first round of a session', () => {
    // 无固化段且有选择:上送 ids,服务端取最新角色数据渲染(新会话首轮)。
    expect(agentCharacterInjection([11, 12], undefined)).toEqual({ character_ids: [11, 12] })
  })

  it('sends nothing without a selection', () => {
    expect(agentCharacterInjection([], undefined)).toEqual({})
  })
})
