/**
 * 变体成套生成的纯逻辑(83 号票 B):同变体逐类型各一笔的批量编排 ——
 * 默认勾选视图三类(三视图/多视图/色板,表情集/姿态默认不勾省钱,按需
 * 加勾)与一次确认的 N 笔总价预估(38 号票口径;83-B2 评审修复轮补全:
 * 按量轨与无价模型不再静默退空串,显式标注「未配价,发起时以实际计价
 * 为准」,全部可估维持纯预估句)。
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

/** 单笔任务的可估价(元;null = 此轨在此任务形态下估不出):按次轨
 * (call)单价即一笔。按量轨(token)照 GenerationComposer 预估先例的
 * 算法 = 折算率 × 时长 × 输出单价 × 倍率,而设定图任务是静态图、无时长
 * 输入 —— 照先例「token 轨无时长退空」的同语义估不出,归「未配价」笔
 * (生图模型在网关只收 call 轨,`/image-models` 目录也只回 call 轨行,
 * 该形态在真实发行面进不了目录;这里按轨判定而非按「非 call 即有价」
 * 收窄,是为了与 38 号票先例的按轨口径对齐)。其余轨(second/character
 * 等)同非生图轨,估不出。 */
function perCallCNY(p: ModelPriceSummary | undefined): number | null {
  if (p?.unit === 'call' && p.cny_per_call != null) {
    return p.cny_per_call
  }
  return null
}

/** 一次确认的 N 笔任务总价预估(83 号票 B2 票面「含 N 笔任务总价预估」;
 * 83-B2 评审修复轮补全):笔笔同模型,可估性笔笔一致 ——
 * - 全部可估(按次轨):总价 = 单价 × N,文案带单笔口径(纯预估句,
 *   38 号票先例同形);
 * - 估不出(模型无价格行 / 按量轨无时长等):不再静默退空串 —— 成本
 *   可见性要求「没有预估也要说明为什么」,显式标注「N 笔未配价,发起时
 *   以实际计价为准」(笔笔同模型故无「部分未配价」中间态;未来每类型
 *   各异模型时在此展开逐笔求和与「K/N 笔未配价」标注);
 * - 零笔:连提示也不出(空串,对话框无待发笔)。
 * 仅作发送前预估,不参与计费(计费永远读服务端价格行,逐笔独立计价)。 */
export function bundleEstimateText(
  prices: Record<string, ModelPriceSummary>,
  model: string,
  count: number,
): string {
  if (count <= 0) {
    return ''
  }
  const per = perCallCNY(prices[model])
  if (per == null) {
    return `${count} 笔未配价,发起时以实际计价为准`
  }
  return `预估 ${fmtCNY(per * count)}(${count} 笔 × ${fmtCNY(per)}/笔)`
}
