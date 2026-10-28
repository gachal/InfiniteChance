/**
 * music.ts 的纯逻辑单测(60 号票):表单校验与按次预估价;62 号票追加
 * 歌曲开关(纯音乐互斥)与歌词上限。
 */
import { describe, expect, it } from 'vitest'

import type { ModelPriceSummary } from '@infinitechance/api'

import { estimateMusicCNY, fmtCNY, MAX_LYRICS_CHARS, MAX_MUSIC_PROMPT_RUNES, validateMusicForm } from './music'

describe('validateMusicForm(60 号票配乐表单)', () => {
  it('rejects empty prompt/model and over-long prompt', () => {
    expect(validateMusicForm({ prompt: '   ', model: 'm' })).toBe('风格描述不能为空')
    expect(validateMusicForm({ prompt: '轻快的电子爵士', model: '' })).toBe('请选择配乐模型')
    expect(
      validateMusicForm({ prompt: '雨'.repeat(MAX_MUSIC_PROMPT_RUNES + 1), model: 'm' }),
    ).toBe(`风格描述最多 ${MAX_MUSIC_PROMPT_RUNES} 个字符`)
  })

  it('accepts a trimmed non-empty prompt with a model', () => {
    expect(validateMusicForm({ prompt: '  轻快的电子爵士  ', model: 'minimax-music' })).toBeNull()
  })
})

describe('validateMusicForm(62 号票歌曲开关与歌词)', () => {
  it('rejects lyrics carried on an instrumental (纯音乐) form', () => {
    expect(
      validateMusicForm({ prompt: 'p', model: 'm', instrumental: true, lyrics: '多余的歌' }),
    ).toBe('纯音乐不能携带歌词;请关闭「人声歌曲」开关或清空歌词')
  })

  it('rejects over-long lyrics on a song form', () => {
    expect(
      validateMusicForm({ prompt: 'p', model: 'm', instrumental: false, lyrics: '雨'.repeat(MAX_LYRICS_CHARS + 1) }),
    ).toBe(`歌词最多 ${MAX_LYRICS_CHARS} 个字符`)
  })

  it('accepts a song with or without lyrics (自动写词) and a plain 纯音乐 form', () => {
    expect(validateMusicForm({ prompt: 'p', model: 'm', instrumental: false })).toBeNull()
    expect(validateMusicForm({ prompt: 'p', model: 'm', instrumental: false, lyrics: '夜色慢慢降落' })).toBeNull()
    // 60 号票既有形状(两字段)不受扰:instrumental 缺省 = 纯音乐。
    expect(validateMusicForm({ prompt: 'p', model: 'm' })).toBeNull()
  })
})

describe('estimateMusicCNY(按次预估价)', () => {
  it('returns the per-call price rounded up at micro granularity', () => {
    const price = { unit: 'call', cny_per_call: 0.1234567 } as ModelPriceSummary
    expect(estimateMusicCNY(price)).toBe(0.123457)
  })

  it('returns null without a call-track price', () => {
    expect(estimateMusicCNY(undefined)).toBeNull()
    expect(estimateMusicCNY({ unit: 'character', cny_per_10k_chars: 20 } as ModelPriceSummary)).toBeNull()
    expect(estimateMusicCNY({ unit: 'call' } as ModelPriceSummary)).toBeNull()
  })
})

describe('fmtCNY(re-export 与 voice.ts 同源)', () => {
  it('formats amounts like the shared formatter', () => {
    expect(fmtCNY(2)).toBe('¥2')
    expect(fmtCNY(0.5)).toBe('¥0.5')
  })
})
