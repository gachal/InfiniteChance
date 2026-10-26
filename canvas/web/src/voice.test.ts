import { describe, expect, it } from 'vitest'

import type { ModelPriceSummary } from '@infinitechance/api'
import {
  MAX_SAMPLE_BYTES,
  billableChars,
  estimateSpeechCNY,
  fmtCNY,
  validateSampleFile,
  validateSpeechForm,
  validateVoiceForm,
  voiceStatusBadge,
} from './voice'

const designForm = (over: Partial<Parameters<typeof validateVoiceForm>[0]> = {}) => ({
  mode: 'design' as const,
  name: '少年音',
  target_model: 'qwen-tts-plus',
  voice_prompt: '低沉磁性青年男声,语速平稳',
  preview_text: '这一段台词用来试听音色的整体效果如何。',
  ...over,
})

describe('validateVoiceForm', () => {
  it('accepts a complete design form', () => {
    expect(validateVoiceForm(designForm())).toBeNull()
  })

  it('rejects empty name / missing model', () => {
    expect(validateVoiceForm(designForm({ name: '  ' }))).toBe('音色名不能为空')
    expect(validateVoiceForm(designForm({ target_model: '' }))).toBe('请选择合成模型')
  })

  it('requires voice_prompt in design mode and caps it at 500', () => {
    expect(validateVoiceForm(designForm({ voice_prompt: undefined }))).toBe('设计模式需要填写音色描述')
    expect(validateVoiceForm(designForm({ voice_prompt: '声'.repeat(501) }))).toBe(
      '音色描述最多 500 个字符',
    )
    expect(validateVoiceForm(designForm({ voice_prompt: '声'.repeat(500) }))).toBeNull()
  })

  it('enforces the 15–200 preview window (按 rune 计数)', () => {
    expect(validateVoiceForm(designForm({ preview_text: '太短了' }))).toBe(
      '试听文本需在 15 到 200 个字符之间',
    )
    expect(validateVoiceForm(designForm({ preview_text: '试'.repeat(201) }))).toBe(
      '试听文本需在 15 到 200 个字符之间',
    )
    expect(validateVoiceForm(designForm({ preview_text: '试'.repeat(200) }))).toBeNull()
  })

  it('requires a sample asset in clone mode and rejects voice_prompt there', () => {
    expect(validateVoiceForm({ ...designForm({ mode: 'clone' }) })).toBe('克隆模式需要提供声音样本')
    expect(
      validateVoiceForm({ ...designForm({ mode: 'clone' }), sample_asset_id: 7, voice_prompt: undefined }),
    ).toBeNull()
  })
})

describe('validateSpeechForm', () => {
  it('accepts text within 600 and optional instruction', () => {
    expect(validateSpeechForm('你好,世界。')).toBeNull()
    expect(validateSpeechForm('你好', '用河南话,带笑意')).toBeNull()
  })

  it('rejects empty and over-600 text', () => {
    expect(validateSpeechForm('')).toBe('台词不能为空')
    expect(validateSpeechForm('词'.repeat(601))).toBe('台词最多 600 个字符')
    expect(validateSpeechForm('词'.repeat(600))).toBeNull()
  })

  it('caps instruction at 500', () => {
    expect(validateSpeechForm('你好', '笑'.repeat(501))).toBe('演绎指令最多 500 个字符')
  })
})

describe('validateSampleFile', () => {
  it('accepts audio files within 10MB and MIME-less files (服务端嗅探兜底)', () => {
    expect(validateSampleFile({ type: 'audio/wav', size: 1024 })).toBeNull()
    expect(validateSampleFile({ type: '', size: 1024 })).toBeNull()
    expect(validateSampleFile({ type: 'audio/mpeg', size: MAX_SAMPLE_BYTES })).toBeNull()
  })

  it('rejects non-audio mime and oversize files', () => {
    expect(validateSampleFile({ type: 'image/png', size: 10 })).toBe(
      '声音样本必须是音频文件(WAV/MP3/M4A)',
    )
    expect(validateSampleFile({ type: 'audio/wav', size: MAX_SAMPLE_BYTES + 1 })).toBe(
      '声音样本超过 10MB 上限',
    )
  })
})

describe('voiceStatusBadge', () => {
  it('maps the three upstream states', () => {
    expect(voiceStatusBadge('DEPLOYING')).toEqual({ label: '部署中', tone: 'deploying' })
    expect(voiceStatusBadge('OK')).toEqual({ label: '可用', tone: 'ok' })
    expect(voiceStatusBadge('UNDEPLOYED')).toEqual({ label: '审核未通过', tone: 'rejected' })
  })

  it('falls back to deploying for unknown/undefined states', () => {
    expect(voiceStatusBadge(undefined).label).toBe('部署中')
  })
})

describe('billableChars / estimateSpeechCNY', () => {
  it('weights CJK and fullwidth runes as 2, ASCII as 1 (与服务端同口径)', () => {
    expect(billableChars('ab')).toBe(2)
    expect(billableChars('你好')).toBe(4)
    expect(billableChars('a你')).toBe(3)
    expect(billableChars('!')).toBe(1)
  })

  it('estimates ⌈chars × per10k / 10000⌉, instruction included', () => {
    const price: ModelPriceSummary = { unit: 'character', cny_per_10k_chars: 1.4 }
    // 12 个汉字 = 24 计费字符:24 × 1.4 元/万 = 0.00336 元(微元 ceil 不变)。
    expect(estimateSpeechCNY('一二三四五六七八九十百一', undefined, price)).toBe(0.00336)
    // 11 个汉字 = 22 计费字符:22 × 140 微元 = 3080 微元。
    expect(estimateSpeechCNY('一二三四五六七八九十百', undefined, price)).toBe(0.00308)
    expect(estimateSpeechCNY('你好', '带笑意', price)).toBe(
      estimateSpeechCNY('你好带笑意', undefined, price),
    )
    expect(estimateSpeechCNY('', undefined, price)).toBe(0)
  })

  it('returns null without a character-track price', () => {
    expect(estimateSpeechCNY('你好', undefined, undefined)).toBeNull()
    expect(
      estimateSpeechCNY('你好', undefined, { unit: 'call', cny_per_call: 0.01 }),
    ).toBeNull()
  })
})

describe('fmtCNY', () => {
  it('matches the composer money style (去尾零,最多三位小数)', () => {
    expect(fmtCNY(1.4)).toBe('¥1.4')
    expect(fmtCNY(0.01)).toBe('¥0.01')
    expect(fmtCNY(0.004)).toBe('¥0.004')
    expect(fmtCNY(2)).toBe('¥2')
  })
})

describe('纯函数不改入参', () => {
  it('validateVoiceForm leaves the input untouched', () => {
    const form = designForm()
    const snapshot = JSON.stringify(form)
    validateVoiceForm(form)
    expect(JSON.stringify(form)).toBe(snapshot)
  })
})
