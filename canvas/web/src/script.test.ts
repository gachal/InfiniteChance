import { describe, expect, it } from 'vitest'

import {
  defaultScript,
  isScriptTextWithinLimit,
  MAX_SCRIPT_CHARS,
  scriptCardTitle,
  scriptCharCount,
  scriptCharCountLabel,
  scriptExcessChars,
  scriptLimitNotice,
  SCRIPT_SUMMARY_CHARS,
  scriptSummaryLine,
} from './script'

describe('defaultScript(64 号票)', () => {
  it('seeds an empty script with blank title and text', () => {
    // 新剧本节点:title 空(展示层回落「剧本」,三种用途靠 title 区分
    // ——「分集大纲」「第3集剧本」「原文:书名」)+ 空正文。
    expect(defaultScript()).toEqual({ title: '', text: '' })
  })
})

describe('scriptCharCount / isScriptTextWithinLimit(30 万字上限)', () => {
  it('counts UTF-16 code units as-is', () => {
    // 上限口径 = UTF-16 code unit(JS string.length 原生值,票面定案);
    // BMP 内一个 CJK 字算 1,增补平面字符(如 emoji)算 2 —— 宽松方向
    // 可接受(把长度算大只会更早拦,不会漏拦)。
    expect(scriptCharCount('')).toBe(0)
    expect(scriptCharCount('abc')).toBe(3)
    expect(scriptCharCount('第一场')).toBe(3)
    expect(scriptCharCount('夜🐼')).toBe(3)
  })

  it('admits text at the cap and rejects beyond it', () => {
    expect(MAX_SCRIPT_CHARS).toBe(300_000)
    expect(isScriptTextWithinLimit('a'.repeat(MAX_SCRIPT_CHARS))).toBe(true)
    expect(isScriptTextWithinLimit('a'.repeat(MAX_SCRIPT_CHARS + 1))).toBe(false)
    expect(isScriptTextWithinLimit('')).toBe(true)
  })

  it('reports the excess and zero when within', () => {
    expect(scriptExcessChars('a'.repeat(MAX_SCRIPT_CHARS + 120))).toBe(120)
    expect(scriptExcessChars('第一场')).toBe(0)
  })

  it('phrases the paste-in-segments notice only when over the cap', () => {
    expect(scriptLimitNotice('第一场')).toBe('')
    const notice = scriptLimitNotice('a'.repeat(MAX_SCRIPT_CHARS + 7))
    expect(notice).toContain('300000')
    expect(notice).toContain('7')
    expect(notice).toContain('分段粘贴')
  })
})

describe('scriptCardTitle(用途区分靠 title)', () => {
  it('falls back to 剧本 when the title is blank', () => {
    expect(scriptCardTitle(undefined)).toBe('剧本')
    expect(scriptCardTitle('')).toBe('剧本')
    expect(scriptCardTitle('   ')).toBe('剧本')
  })

  it('returns the trimmed title otherwise', () => {
    expect(scriptCardTitle('第3集剧本')).toBe('第3集剧本')
    expect(scriptCardTitle(' 原文:三体 ')).toBe('原文:三体')
  })
})

describe('scriptSummaryLine(卡面折叠摘要行)', () => {
  it('takes the first non-blank line trimmed', () => {
    expect(scriptSummaryLine('')).toBe('')
    expect(scriptSummaryLine('\n \n【第1场|内|日|咖啡馆】\n正文…')).toBe(
      '【第1场|内|日|咖啡馆】',
    )
  })

  it('truncates long lines with an ellipsis', () => {
    const long = '字'.repeat(SCRIPT_SUMMARY_CHARS + 10)
    expect(scriptSummaryLine(long)).toBe('字'.repeat(SCRIPT_SUMMARY_CHARS) + '…')
  })

  it('keeps short lines intact', () => {
    expect(scriptSummaryLine('第一场:雨夜')).toBe('第一场:雨夜')
  })
})

describe('scriptCharCountLabel(字数展示)', () => {
  it('shows raw counts below a 万', () => {
    expect(scriptCharCountLabel('')).toBe('0 字')
    expect(scriptCharCountLabel('第一场')).toBe('3 字')
    expect(scriptCharCountLabel('a'.repeat(9999))).toBe('9999 字')
  })

  it('switches to 万 above it', () => {
    expect(scriptCharCountLabel('a'.repeat(10_000))).toBe('1.0 万字')
    expect(scriptCharCountLabel('a'.repeat(300_000))).toBe('30.0 万字')
  })
})
