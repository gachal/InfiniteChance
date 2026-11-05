/**
 * music.ts 的纯逻辑单测(60 号票):表单校验与按次预估价;62 号票追加
 * 歌曲开关(纯音乐互斥)与歌词上限;78 号票追加翻唱表单校验;79 号票
 * 追加同曲换词表单校验(词必填、风格选填)。
 */
import { describe, expect, it } from 'vitest'

import type { ModelPriceSummary } from '@infinitechance/api'

import { estimateMusicCNY, fmtCNY, MAX_LYRICS_CHARS, MAX_MUSIC_PROMPT_RUNES, RELYRIC_STRENGTH_DEFAULT, RELYRIC_STRENGTH_MAX, RELYRIC_STRENGTH_MIN, RELYRIC_STRENGTH_STEP, validateCoverForm, validateMusicForm, validateRelyricForm } from './music'

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

describe('validateCoverForm(78 号票翻唱表单)', () => {
  it('rejects a missing reference audio (参考是翻唱的身份所在,必填恰好 1 条)', () => {
    expect(validateCoverForm({ refAssetId: 0, prompt: 'p', model: 'm' })).toBe(
      '请先选择参考音频(必填,恰好 1 条)',
    )
  })

  it('rejects empty prompt/model and over-long prompt, mirroring the music form', () => {
    expect(validateCoverForm({ refAssetId: 8, prompt: '   ', model: 'm' })).toBe('风格描述不能为空')
    expect(validateCoverForm({ refAssetId: 8, prompt: 'city pop cover', model: '' })).toBe('请选择翻唱模型')
    expect(
      validateCoverForm({ refAssetId: 8, prompt: '雨'.repeat(MAX_MUSIC_PROMPT_RUNES + 1), model: 'm' }),
    ).toBe(`风格描述最多 ${MAX_MUSIC_PROMPT_RUNES} 个字符`)
  })

  it('rejects over-long lyrics', () => {
    expect(
      validateCoverForm({ refAssetId: 8, prompt: 'p', model: 'm', lyrics: '雨'.repeat(MAX_LYRICS_CHARS + 1) }),
    ).toBe(`歌词最多 ${MAX_LYRICS_CHARS} 个字符`)
  })

  it('accepts a full cover form with or without lyrics (空 = 上游自动写词)', () => {
    expect(validateCoverForm({ refAssetId: 8, prompt: 'city pop cover', model: 'vod-minimax-music-3.0-cover' })).toBeNull()
    expect(
      validateCoverForm({ refAssetId: 8, prompt: 'city pop cover', model: 'm', lyrics: '夜色慢慢降落' }),
    ).toBeNull()
  })
})

describe('validateRelyricForm(79 号票同曲换词表单)', () => {
  it('rejects a missing reference audio (参考曲是旋律锁定的身份所在,必填恰好 1 条)', () => {
    expect(validateRelyricForm({ refAssetId: 0, prompt: '', model: 'm', lyrics: '新词', strength: 0.55 })).toBe(
      '请先选择参考音频(必填,恰好 1 条)',
    )
  })

  it('rejects empty lyrics (词是身份必需,无「自动写词」;与翻唱的选填歌词区分)', () => {
    expect(validateRelyricForm({ refAssetId: 8, prompt: '', model: 'm', lyrics: '   ', strength: 0.55 })).toBe(
      '新歌词不能为空',
    )
    expect(validateRelyricForm({ refAssetId: 8, prompt: '', model: 'm', strength: 0.55 })).toBe('新歌词不能为空')
  })

  it('rejects over-long lyrics and over-long prompt', () => {
    expect(
      validateRelyricForm({ refAssetId: 8, prompt: '', model: 'm', lyrics: '雨'.repeat(MAX_LYRICS_CHARS + 1), strength: 0.55 }),
    ).toBe(`歌词最多 ${MAX_LYRICS_CHARS} 个字符`)
    expect(
      validateRelyricForm({ refAssetId: 8, prompt: '雨'.repeat(MAX_MUSIC_PROMPT_RUNES + 1), model: 'm', lyrics: '新词', strength: 0.55 }),
    ).toBe(`风格描述最多 ${MAX_MUSIC_PROMPT_RUNES} 个字符`)
  })

  it('rejects a missing model', () => {
    expect(validateRelyricForm({ refAssetId: 8, prompt: '', model: '', lyrics: '新词', strength: 0.55 })).toBe(
      '请选择同曲换词模型',
    )
  })

  it('accepts a full relyric form (prompt 选填,空串/空白即不送风格描述)', () => {
    expect(
      validateRelyricForm({ refAssetId: 8, prompt: '', model: 'mureka-song-edit', lyrics: '  新词第一句  ', strength: 0.55 }),
    ).toBeNull()
    expect(
      validateRelyricForm({ refAssetId: 8, prompt: '维持原曲编曲走向', model: 'mureka-song-edit', lyrics: '新词', strength: 0.55 }),
    ).toBeNull()
  })
})

describe('validateRelyricForm strength(80 号票旋律保持强度)', () => {
  it('rejects NaN / 非有限值 / 超出 0–1 的强度(防呆守卫,滑杆钳 0.3–0.9)', () => {
    expect(
      validateRelyricForm({ refAssetId: 8, prompt: '', model: 'm', lyrics: '新词', strength: Number.NaN }),
    ).toBe('旋律保持强度不合法(应为 0–1 的数值)')
    expect(
      validateRelyricForm({ refAssetId: 8, prompt: '', model: 'm', lyrics: '新词', strength: Number.POSITIVE_INFINITY }),
    ).toBe('旋律保持强度不合法(应为 0–1 的数值)')
    expect(
      validateRelyricForm({ refAssetId: 8, prompt: '', model: 'm', lyrics: '新词', strength: -0.1 }),
    ).toBe('旋律保持强度不合法(应为 0–1 的数值)')
    expect(
      validateRelyricForm({ refAssetId: 8, prompt: '', model: 'm', lyrics: '新词', strength: 1.1 }),
    ).toBe('旋律保持强度不合法(应为 0–1 的数值)')
  })

  it('accepts 0–1 带内强度(滑杆两端 0.3/0.9 与缺省 0.55 都过;0–1 内非滑杆档不拒)', () => {
    expect(validateRelyricForm({ refAssetId: 8, prompt: '', model: 'm', lyrics: '新词', strength: 0.3 })).toBeNull()
    expect(validateRelyricForm({ refAssetId: 8, prompt: '', model: 'm', lyrics: '新词', strength: 0.55 })).toBeNull()
    expect(validateRelyricForm({ refAssetId: 8, prompt: '', model: 'm', lyrics: '新词', strength: 0.9 })).toBeNull()
    expect(validateRelyricForm({ refAssetId: 8, prompt: '', model: 'm', lyrics: '新词', strength: 0.42 })).toBeNull()
  })

  it('滑杆参数常量与票面定案 5 同源(min 0.3 / max 0.9 / step 0.05 / default 0.55)', () => {
    expect(RELYRIC_STRENGTH_MIN).toBe(0.3)
    expect(RELYRIC_STRENGTH_MAX).toBe(0.9)
    expect(RELYRIC_STRENGTH_STEP).toBe(0.05)
    expect(RELYRIC_STRENGTH_DEFAULT).toBe(0.55)
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
