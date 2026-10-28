import { describe, expect, it } from 'vitest'

import {
  estimateSfxCNY,
  MAX_SFX_TEXT_RUNES,
  SFX_MAX_SECONDS,
  SFX_MIN_SECONDS,
  validateSfxForm,
} from './sfx'
import type { ModelPriceSummary } from '@infinitechance/api'

describe('validateSfxForm(61 号票)', () => {
  const ok = { text: '雷声轰鸣,由远及近', model: 'vod-kling-sfx', seconds: 5 }

  it('通过合法表单', () => {
    expect(validateSfxForm(ok)).toBeNull()
  })

  it('空描述(纯空白)拒绝', () => {
    expect(validateSfxForm({ ...ok, text: '   ' })).toContain('描述不能为空')
  })

  it('超长描述拒绝(与后端 2000 同源)', () => {
    expect(validateSfxForm({ ...ok, text: '雷'.repeat(MAX_SFX_TEXT_RUNES + 1) })).toContain(
      `最多 ${MAX_SFX_TEXT_RUNES}`,
    )
  })

  it('缺模型拒绝', () => {
    expect(validateSfxForm({ ...ok, model: ' ' })).toContain('选择音效模型')
  })

  it('秒数域:0 / 31 / 小数 / 缺省值域外的整数全部拒绝', () => {
    expect(validateSfxForm({ ...ok, seconds: 0 })).toContain('秒数')
    expect(validateSfxForm({ ...ok, seconds: SFX_MAX_SECONDS + 1 })).toContain('秒数')
    expect(validateSfxForm({ ...ok, seconds: 2.5 })).toContain('整数')
    expect(validateSfxForm({ ...ok, seconds: SFX_MIN_SECONDS })).toBeNull()
    expect(validateSfxForm({ ...ok, seconds: SFX_MAX_SECONDS })).toBeNull()
  })
})

describe('estimateSfxCNY(61 号票按秒预估)', () => {
  const secondPrice = { unit: 'second', cny_per_call: 0.1 } as ModelPriceSummary

  it('单价 × 秒,微元向上取整', () => {
    expect(estimateSfxCNY(secondPrice, 5)).toBe(0.5)
    // 0.03 元/秒 × 3 秒 = 0.09,精确值;向上取整不改变。
    expect(estimateSfxCNY({ unit: 'second', cny_per_call: 0.03 } as ModelPriceSummary, 3)).toBe(0.09)
  })

  it('非按秒轨 / 缺价 / 非法秒数返回 null(按钮不标价)', () => {
    expect(estimateSfxCNY({ unit: 'call', cny_per_call: 0.2 } as ModelPriceSummary, 5)).toBeNull()
    expect(estimateSfxCNY(undefined, 5)).toBeNull()
    expect(estimateSfxCNY(secondPrice, 0)).toBeNull()
  })
})
