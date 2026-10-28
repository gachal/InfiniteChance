/**
 * 音效生成的纯逻辑(61 号票):创作表单校验与按秒预估价。组件只做渲染与
 * 交互,这里集中可单测的判定 —— 与 music.ts 同族。上限与服务端同源
 * (internal/relay/sfx.go 与 canvastask/handler.go 的 2000 字描述 /
 * 1–30 秒),改一边要同步另一边。
 */
import type { ModelPriceSummary } from '@infinitechance/api'

import { fmtCNY } from './voice'

export const MAX_SFX_TEXT_RUNES = 2000
export const SFX_MIN_SECONDS = 1
export const SFX_MAX_SECONDS = 30

/** 音效创作表单(创作态音频节点 source='sfx' 的提交载荷):text 是音效
 * 描述,model 是 /sfx-models 目录的公开模型名,seconds 是期望时长
 * (1–30 整数;模型域如 Kling [3,10] 由上游裁)。 */
export interface SfxFormInput {
  text: string
  model: string
  seconds: number
}

/** 首个校验错误的用户文案;null = 通过(文案与后端 400 同口径)。 */
export function validateSfxForm(form: SfxFormInput): string | null {
  const text = form.text.trim()
  if (text === '') {
    return '音效描述不能为空'
  }
  if ([...text].length > MAX_SFX_TEXT_RUNES) {
    return `音效描述最多 ${MAX_SFX_TEXT_RUNES} 个字符`
  }
  if (form.model.trim() === '') {
    return '请选择音效模型'
  }
  if (!Number.isInteger(form.seconds) || form.seconds < SFX_MIN_SECONDS || form.seconds > SFX_MAX_SECONDS) {
    return `秒数需在 ${SFX_MIN_SECONDS} 到 ${SFX_MAX_SECONDS} 之间的整数`
  }
  return null
}

/** 音效的按秒预估价(元):单价(元/秒)× seconds —— 微元向上取整再折回
 * 元(与网关「扣费向上取整不低估」同形);price 缺失或非按秒轨返回 null
 * (按钮不标价)。 */
export function estimateSfxCNY(price: ModelPriceSummary | undefined, seconds: number): number | null {
  if (!price || price.unit !== 'second' || price.cny_per_call == null || seconds < 1) {
    return null
  }
  return Math.ceil(price.cny_per_call * seconds * 1e6) / 1e6
}

export { fmtCNY }
