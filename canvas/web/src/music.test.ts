/**
 * music.ts 的纯逻辑单测(60 号票):表单校验与按次预估价。
 */
import { describe, expect, it } from 'vitest'

import type { ModelPriceSummary } from '@infinitechance/api'

import { estimateMusicCNY, fmtCNY, MAX_MUSIC_PROMPT_RUNES, validateMusicForm } from './music'

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
