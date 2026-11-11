/**
 * 变体成套生成的纯逻辑(83 号票 B):同变体逐类型各一笔的批量编排 ——
 * 默认勾选视图三类(三视图/多视图/色板,表情集/姿态默认不勾省钱,按需
 * 加勾)与一次确认的 N 笔总价预估(38 号票按次轨口径,无价不显示)。
 *
 * 纪律边界:类型清单只有这组常量与预估文案两件纯事;串行逐笔发起、参考
 * 图冻结与逐笔锚定都是组件装配(AssetDrawer),端点零改动 —— 逐笔
 * POST /api/ips/:id/tasks(CreateIPTaskInput.sheet_type 各一),不新建
 * 批量端点,「一次请求生成多图」仍不做(70 号票口径修订:同变体逐类型
 * 各一笔进本票,一次生成一张不变)。
 */
import type { ModelPriceSummary } from '@infinitechance/api'

import { fmtCNY } from './voice'

/** 成套生成对话框的默认勾选(83 号票 B1):视图三类是变体的主资产,
 * 建变体后先补齐;表情集/姿态默认不勾 —— 省钱,按需加勾。 */
export const BUNDLE_DEFAULT_TYPES = ['三视图', '多视图', '色板'] as const

/** 一次确认的 N 笔任务总价预估文案(38 号票按次轨口径):总价 = 单价 × N,
 * 文案带单笔口径;无价 / 非按次轨 / 零笔不显示(空串)。仅作发送前预估,
 * 不参与计费(计费永远读服务端价格行,逐笔独立计价)。 */
export function bundleEstimateText(
  prices: Record<string, ModelPriceSummary>,
  model: string,
  count: number,
): string {
  const p = prices[model]
  if (count <= 0 || p?.unit !== 'call' || p.cny_per_call == null) {
    return ''
  }
  return `预估 ${fmtCNY(p.cny_per_call * count)}(${count} 笔 × ${fmtCNY(p.cny_per_call)}/笔)`
}
