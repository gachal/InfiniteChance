import { describe, expect, it } from 'vitest'

import { STYLE_SEGMENT_MARK, prependStyleSegment, styleSegment, styledSendPrompt } from './ipStyle'

describe('styleSegment(69 号票)', () => {
  it('renders the fixed-marker segment from the trimmed IP style', () => {
    // 与服务端 canvas.StyleSegment 同形(两侧各有字面量测试钉住):70 号票
    // 角色设定图 prompt 消费同一形状。
    expect(styleSegment('赛博朋克霓虹,高对比冷暖撞色')).toBe('画面风格:赛博朋克霓虹,高对比冷暖撞色')
    expect(styleSegment('  水彩手绘  ')).toBe('画面风格:水彩手绘')
    expect(STYLE_SEGMENT_MARK).toBe('画面风格:')
  })

  it('answers an empty string when the IP style is unset', () => {
    // 未定 = 空串:调用方据此不拼不显示,存量行为逐字节不变。
    expect(styleSegment('')).toBe('')
    expect(styleSegment(' \n\t ')).toBe('')
    expect(styleSegment(undefined)).toBe('')
    expect(styleSegment(null)).toBe('')
  })
})

describe('prependStyleSegment(69 号票注入面统一出口)', () => {
  it('puts the segment at the front of the prompt on its own line', () => {
    expect(prependStyleSegment('画面描述:雨夜巷口', '水彩手绘')).toBe('画面风格:水彩手绘\n画面描述:雨夜巷口')
  })

  it('returns the prompt untouched when the IP has no style', () => {
    expect(prependStyleSegment('画面描述:雨夜巷口', '')).toBe('画面描述:雨夜巷口')
    expect(prependStyleSegment('', undefined)).toBe('')
  })

  it('answers the segment alone for a blank prompt', () => {
    // 素材全空的直通预填仍带风格:用户在风格段之后续写画面内容。
    expect(prependStyleSegment('', '水彩手绘')).toBe('画面风格:水彩手绘')
    expect(prependStyleSegment('  ', '水彩手绘')).toBe('画面风格:水彩手绘')
  })

  it('does not stack the same segment twice', () => {
    // 直通预填已拼过风格段、composer 开关又开着:发送时不重复拼。
    const prefilled = '画面风格:水彩手绘\n画面描述:雨夜巷口'
    expect(prependStyleSegment(prefilled, '水彩手绘')).toBe(prefilled)
    expect(prependStyleSegment(prefilled, '  水彩手绘 ')).toBe(prefilled)
  })
})

describe('styledSendPrompt(composer「带 IP 风格」开关,69 号票)', () => {
  it('sends the hand-written prompt untouched while the switch is off', () => {
    // 默认关:手写 prompt 不被偷改,即便 IP 已定风格。
    expect(styledSendPrompt('城市夜景', false, '水彩手绘')).toBe('城市夜景')
  })

  it('prepends the style segment when the switch is on', () => {
    // 任务行 prompt 带风格段(生成记录面板可查)。
    expect(styledSendPrompt('城市夜景', true, '水彩手绘')).toBe('画面风格:水彩手绘\n城市夜景')
  })

  it('is a no-op when the IP has no style or the prompt already carries it', () => {
    expect(styledSendPrompt('城市夜景', true, '')).toBe('城市夜景')
    const prefilled = '画面风格:水彩手绘\n城市夜景'
    expect(styledSendPrompt(prefilled, true, '水彩手绘')).toBe(prefilled)
  })
})
