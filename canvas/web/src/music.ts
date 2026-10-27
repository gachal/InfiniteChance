/**
 * 配乐生成的纯逻辑(60 号票):创作表单校验与按次预估价。组件只做渲染
 * 与交互,这里集中可单测的判定 —— 与 voice.ts 同族。上限与服务端同源
 * (internal/relay/music.go 与 canvastask/handler.go 的 2000 字,MPS
 * Prompt 厂商上限),改一边要同步另一边。
 */
import type { ModelPriceSummary } from '@infinitechance/api'

import { fmtCNY } from './voice'

export const MAX_MUSIC_PROMPT_RUNES = 2000

/** 配乐创作表单(创作态音频节点的提交载荷):prompt 是风格描述,
 * model 是 /music-models 目录的公开模型名。 */
export interface MusicFormInput {
  prompt: string
  model: string
}

/** 首个校验错误的用户文案;null = 通过(文案与后端 400 同口径)。 */
export function validateMusicForm(form: MusicFormInput): string | null {
  const prompt = form.prompt.trim()
  if (prompt === '') {
    return '风格描述不能为空'
  }
  const runes = [...prompt].length
  if (runes > MAX_MUSIC_PROMPT_RUNES) {
    return `风格描述最多 ${MAX_MUSIC_PROMPT_RUNES} 个字符`
  }
  if (form.model.trim() === '') {
    return '请选择配乐模型'
  }
  return null
}

/** 配乐的按次预估价(元):一任务一首,单价即预估 —— 微元向上取整再折
 * 回元(与网关「扣费向上取整不低估」同形);price 缺失或非按次轨返回
 * null(按钮不标价)。 */
export function estimateMusicCNY(price: ModelPriceSummary | undefined): number | null {
  if (!price || price.unit !== 'call' || price.cny_per_call == null) {
    return null
  }
  return Math.ceil(price.cny_per_call * 1e6) / 1e6
}

export { fmtCNY }
