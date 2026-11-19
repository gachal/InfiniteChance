// 90 号票:参数面板化与 meta 行胶囊的纯逻辑(参数摘要文案、时长自动档
// 默认值、成本预估的短句拆分)。纯函数集中在 composerPanel.ts,composer.ts
// 档位表与纯函数零改动(票面明确)——这里是新收纳件的独立行为面。
import { describe, expect, it } from 'vitest'

import type { ModelPriceSummary } from '@infinitechance/api'

import {
  IMAGE_RATIO_PRESETS,
  RESOLUTION_PRESETS,
  RATIO_PRESETS,
  VIDEO_RESOLUTION_PRESETS,
} from './composer'
import { autoDurationDefault, estimateCost, fmtCNY, paramSummary } from './composerPanel'

const imageDraft = {
  ratio: '',
  resolution: '',
  duration: '',
  transparent: false,
  withStyle: false,
}

describe('paramSummary(90 号票:参数摘要按钮按当前值实时拼)', () => {
  it('图片双自动 = 只报「自适应」(空档显示自适应,不再是「自动比例」)', () => {
    expect(paramSummary('image', imageDraft, { ratio: IMAGE_RATIO_PRESETS, resolution: RESOLUTION_PRESETS })).toBe(
      '自适应',
    )
  })

  it('图片自适应 / 1K(票面例句)', () => {
    expect(
      paramSummary(
        'image',
        { ...imageDraft, resolution: '1k' },
        { ratio: IMAGE_RATIO_PRESETS, resolution: RESOLUTION_PRESETS },
      ),
    ).toBe('自适应 / 1K')
  })

  it('比例实档 + 分辨率 + 透明 + 风格按序缀尾', () => {
    expect(
      paramSummary(
        'image',
        { ratio: '16:9', resolution: '4k', duration: '', transparent: true, withStyle: true },
        { ratio: IMAGE_RATIO_PRESETS, resolution: RESOLUTION_PRESETS },
      ),
    ).toBe('16:9 / 4K / 透明 / 风格')
  })

  it('视频 5s / 16:9 / 1080p(票面例句:时长在最前)', () => {
    expect(
      paramSummary(
        'video',
        { ratio: '16:9', resolution: '1080p', duration: '5', transparent: false, withStyle: false },
        { ratio: RATIO_PRESETS, resolution: VIDEO_RESOLUTION_PRESETS },
      ),
    ).toBe('5s / 16:9 / 1080p')
  })

  it('视频时长自动不进摘要(空 = 不传 seconds,保 24 号票缺省语义);透明不进视频摘要', () => {
    expect(
      paramSummary(
        'video',
        { ratio: '', resolution: '', duration: '', transparent: true, withStyle: false },
        { ratio: RATIO_PRESETS, resolution: VIDEO_RESOLUTION_PRESETS },
      ),
    ).toBe('自适应')
    expect(
      paramSummary(
        'video',
        { ratio: '9:16', resolution: '', duration: '', transparent: true, withStyle: true },
        { ratio: RATIO_PRESETS, resolution: VIDEO_RESOLUTION_PRESETS },
      ),
    ).toBe('9:16 / 风格')
  })

  it('分辨率只在选定实档时出现(自动尺寸/自动分辨率不入摘要)', () => {
    expect(
      paramSummary(
        'image',
        { ratio: '1:1', resolution: '', duration: '', transparent: false, withStyle: false },
        { ratio: IMAGE_RATIO_PRESETS, resolution: RESOLUTION_PRESETS },
      ),
    ).toBe('1:1')
    expect(
      paramSummary(
        'video',
        { ratio: '16:9', resolution: '', duration: '8', transparent: false, withStyle: false },
        { ratio: RATIO_PRESETS, resolution: VIDEO_RESOLUTION_PRESETS },
      ),
    ).toBe('8s / 16:9')
  })
})

describe('autoDurationDefault(90 号票:取消自动 = 滑杆出现默认 5s)', () => {
  it('常规区间落 5(24 号票裁量的逃生口默认值)', () => {
    expect(autoDurationDefault({ min: 4, max: 15 })).toBe('5')
    expect(autoDurationDefault({ min: 4, max: 30 })).toBe('5')
    expect(autoDurationDefault({ min: 5, max: 15 })).toBe('5')
  })

  it('区间不含 5 时夹紧进区间(防未来模型档位漂移)', () => {
    expect(autoDurationDefault({ min: 6, max: 10 })).toBe('6')
    expect(autoDurationDefault({ min: 3, max: 4 })).toBe('4')
  })
})

describe('estimateCost(90 号票:预估行退役为 ¥ 胶囊,text = 原句进 title)', () => {
  it('无价格摘要 → null(整颗隐藏,不显示 ¥0)', () => {
    expect(estimateCost(undefined, { ratio: '1:1', resolution: '1k', duration: '' })).toBeNull()
  })

  it('call 轨:按尺寸系数取整,text/amount 同源拆分', () => {
    const p: ModelPriceSummary = { unit: 'call', cny_per_call: 0.3, size_factors: { '1024x1024': 1.5 } }
    expect(estimateCost(p, { ratio: '1:1', resolution: '1k', duration: '' })).toEqual({
      text: '本次预估 ¥0.45',
      amount: '¥0.45',
    })
    expect(estimateCost(p, { ratio: '', resolution: '', duration: '' })).toEqual({
      text: '本次预估 ¥0.3',
      amount: '¥0.3',
    })
  })

  it('second 轨:时长实档报总额,自动档报单价(胶囊显示每秒价)', () => {
    const p: ModelPriceSummary = { unit: 'second', cny_per_call: 0.4, size_factors: { '1080p': 2 } }
    expect(estimateCost(p, { ratio: '', resolution: '1080p', duration: '5' })).toEqual({
      text: '本次预估 ¥4(¥0.8/秒 × 5 秒)',
      amount: '¥4',
    })
    expect(estimateCost(p, { ratio: '', resolution: '1080p', duration: '' })).toEqual({
      text: '¥0.8/秒,时长自动,按实结算',
      amount: '¥0.8/秒',
    })
  })

  it('token 轨:秒折算率 × 秒数 × 输出单价 × 倍率;时长缺失或无折算率 → null', () => {
    const p: ModelPriceSummary = {
      unit: 'token',
      output_cny_per_mtokens: 10,
      size_tokens_per_second: { '1080p': 5000 },
      ratio: 1,
    }
    expect(estimateCost(p, { ratio: '', resolution: '1080p', duration: '4' })).toEqual({
      text: '本次预估 约 ¥0.2(20000 tok)',
      amount: '约 ¥0.2',
    })
    expect(estimateCost(p, { ratio: '', resolution: '1080p', duration: '' })).toBeNull()
    expect(estimateCost({ ...p, default_tokens_per_second: 0, size_tokens_per_second: undefined }, { ratio: '', resolution: '', duration: '4' })).toBeNull()
  })

  it('未知轨道(unit 不属 call/second/token)→ null', () => {
    expect(estimateCost({ unit: 'character', cny_per_10k_chars: 1 }, { ratio: '', resolution: '', duration: '' })).toBeNull()
  })
})

describe('fmtCNY(人民币紧凑展示,38 号票语义原样迁出)', () => {
  it('整数不带小数、最多三位小数、去尾零', () => {
    expect(fmtCNY(4)).toBe('¥4')
    expect(fmtCNY(0.36)).toBe('¥0.36')
    expect(fmtCNY(1.5)).toBe('¥1.5')
    expect(fmtCNY(12.345)).toBe('¥12.345')
  })
})
