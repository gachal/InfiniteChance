// 成套生成的纯逻辑测试(83 号票 B):默认勾选视图三类(表情集/姿态默认
// 不勾,省钱按需加勾)、N 笔任务的总价预估文案(38 号票按次轨口径,无价
// 不显示)。装配纪律(串行逐笔/参考图统一冻结/逐笔锚定)在
// components/AssetDrawer.bundle.test.ts 按源码断言钉住。
import { describe, expect, it } from 'vitest'

import type { ModelPriceSummary } from '@infinitechance/api'

import { SHEET_TYPES } from './sheet'
import { BUNDLE_DEFAULT_TYPES, bundleEstimateText } from './sheetBundle'

describe('BUNDLE_DEFAULT_TYPES(83 号票 B1 默认勾选)', () => {
  it('默认勾三视图/多视图/色板,不勾表情集/姿态', () => {
    expect([...BUNDLE_DEFAULT_TYPES]).toEqual(['三视图', '多视图', '色板'])
    expect(BUNDLE_DEFAULT_TYPES.includes('表情集' as (typeof BUNDLE_DEFAULT_TYPES)[number])).toBe(
      false,
    )
    expect(BUNDLE_DEFAULT_TYPES.includes('姿态' as (typeof BUNDLE_DEFAULT_TYPES)[number])).toBe(
      false,
    )
  })

  it('成员全部落在五类型白名单内(勾选面来自 SHEET_TYPES,不发白名单外的类型)', () => {
    for (const t of BUNDLE_DEFAULT_TYPES) {
      expect((SHEET_TYPES as readonly string[]).includes(t)).toBe(true)
    }
  })
})

describe('bundleEstimateText(一次确认的 N 笔总价预估)', () => {
  const prices: Record<string, ModelPriceSummary> = {
    按次: { unit: 'call', cny_per_call: 0.5 },
    按量: { unit: 'token', input_cny_per_mtokens: 1, output_cny_per_mtokens: 2 },
  }

  it('按次轨:总价 = 单价 × N,文案带单笔口径', () => {
    expect(bundleEstimateText(prices, '按次', 3)).toBe('预估 ¥1.5(3 笔 × ¥0.5/笔)')
  })

  it('无价 / 非按次轨 / 零笔:不显示(空串)', () => {
    expect(bundleEstimateText({}, '按次', 3)).toBe('')
    expect(bundleEstimateText(prices, '按量', 3)).toBe('')
    expect(bundleEstimateText(prices, '不存在的模型', 3)).toBe('')
    expect(bundleEstimateText(prices, '按次', 0)).toBe('')
  })
})
