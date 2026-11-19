/**
 * 生成对话框参数面板与 meta 行的纯逻辑(90 号票)。参数收纳成「参数摘要
 * 按钮 + 弹出面板」后,摘要文案拼装、时长自动档默认值、成本预估的三段
 * 拆分都集中在这里供单测;composer.ts 的档位表与纯函数零改动(票面明确),
 * 本模块只消费既有档位类型。38 号票的成本预估逻辑从组件原样迁出,数字
 * 与文案逐字不变 —— text 是「本次预估 …」原句(进胶囊 title),amount 是
 * 胶囊短句。
 */
import type { ModelPriceSummary } from '@infinitechance/api'

import { composeSize, type DurationRange, type PresetOption } from './composer'

/** 人民币金额的紧凑展示:整数不带小数、最多三位小数、去尾零(38 号票
 * 组件内同名 helper 原样迁出,模型下拉带价展示继续用)。 */
export function fmtCNY(n: number): string {
  const s = n.toFixed(3).replace(/\.?0+$/, '')
  return `¥${s}`
}

/** 参数摘要的草稿快照:比例/分辨率两轴 + 视频时长 + 图片透明 + 带 IP
 * 风格。时长仅视频模式消费;透明仅图片模式消费(视频透明显式 out of
 * scope,37 号票)。 */
export interface ParamSummaryDraft {
  ratio: string
  resolution: string
  duration: string
  transparent: boolean
  withStyle: boolean
}

/** 参数摘要按钮的文案(90 号票 Q5a):按当前值实时拼。比例恒在(空档
 * 显示「自适应」—— 面板比例网格与摘要共用这个词,composer.ts 档位表的
 * 「自动比例」label 不动);分辨率/时长只在选定实档时出现(自动 = 不传,
 * 不进摘要);透明与 IP 风格以「透明」「风格」缀尾。视频时长自动(空串)
 * 保 24 号票「留空 = 不传 = 厂商缺省」语义,摘要随之省略。 */
export function paramSummary(
  mode: 'image' | 'video',
  draft: ParamSummaryDraft,
  presets: { ratio: PresetOption[]; resolution: PresetOption[] },
): string {
  const ratioLabel =
    draft.ratio === ''
      ? '自适应'
      : (presets.ratio.find((p) => p.value === draft.ratio)?.label ?? draft.ratio)
  const parts: string[] = []
  if (mode === 'video' && draft.duration !== '') {
    parts.push(`${Number(draft.duration)}s`)
  }
  parts.push(ratioLabel)
  if (draft.resolution !== '') {
    const res = presets.resolution.find((p) => p.value === draft.resolution)
    parts.push(res?.label ?? draft.resolution)
  }
  if (mode === 'image' && draft.transparent) {
    parts.push('透明')
  }
  if (draft.withStyle) {
    parts.push('风格')
  }
  return parts.join(' / ')
}

/** 取消时长「自动」时滑杆的默认值(90 号票裁量:默认 5s)。滑杆天然有
 * 值,必须有自动逃生口保「留空 = 不传」;默认值按所选模型区间夹紧,防
 * 未来模型档位不含 5 时落在区间外。 */
export function autoDurationDefault(range: DurationRange): string {
  const clamped = Math.min(Math.max(5, range.min), range.max)
  return String(clamped)
}

/** 成本预估的结构化结果:text = 「本次预估 …」原句(胶囊 title 带原句,
 * 38 号票文案逐字保留),amount = 胶囊短句;null = 无法预估(整颗隐藏,
 * 不显示 ¥0)。轨道分支与数字口径与 38 号票组件内实现逐字一致:call 轨按
 * 尺寸系数(不乘参考图数,参考加价各厂商不同,不虚报),second 轨按
 * 分辨率系数 × 秒数(时长自动只报单价),token 轨按 秒折算率 × 秒数 ×
 * 输出单价 × 倍率;向上取整对齐服务端预扣口径。 */
export interface EstimateDraft {
  ratio: string
  resolution: string
  duration: string
}

export interface EstimateInfo {
  text: string
  amount: string
}

export function estimateCost(
  p: ModelPriceSummary | undefined,
  draft: EstimateDraft,
): EstimateInfo | null {
  if (!p) {
    return null
  }
  if (p.unit === 'call' && p.cny_per_call != null) {
    const size = composeSize(draft.ratio, draft.resolution)
    const factor = size ? (p.size_factors?.[size] ?? 1) : 1
    const amount = fmtCNY(Math.ceil(p.cny_per_call * factor * 100) / 100)
    return { text: `本次预估 ${amount}`, amount }
  }
  if (p.unit === 'second' && p.cny_per_call != null) {
    const factor = draft.resolution ? (p.size_factors?.[draft.resolution] ?? 1) : 1
    const per = p.cny_per_call * factor
    const secs = Number(draft.duration)
    if (draft.duration !== '' && Number.isFinite(secs) && secs > 0) {
      const amount = fmtCNY(Math.ceil(per * secs * 100) / 100)
      return { text: `本次预估 ${amount}(${fmtCNY(per)}/秒 × ${secs} 秒)`, amount }
    }
    const perLabel = `${fmtCNY(per)}/秒`
    return { text: `${perLabel},时长自动,按实结算`, amount: perLabel }
  }
  if (p.unit === 'token' && p.output_cny_per_mtokens != null) {
    const secs = Number(draft.duration)
    if (draft.duration === '' || !Number.isFinite(secs) || secs <= 0) {
      return null
    }
    const rate = draft.resolution
      ? (p.size_tokens_per_second?.[draft.resolution] ?? p.default_tokens_per_second ?? 0)
      : (p.default_tokens_per_second ?? 0)
    if (rate <= 0) {
      return null
    }
    const tokens = Math.ceil(rate * secs)
    const cost = (tokens / 1_000_000) * p.output_cny_per_mtokens * (p.ratio ?? 1)
    const amount = `约 ${fmtCNY(Math.ceil(cost * 100) / 100)}`
    return { text: `本次预估 ${amount}(${tokens} tok)`, amount }
  }
  return null
}
