/**
 * 配乐/歌曲生成的纯逻辑(60 号票;62 号票追加歌曲开关与歌词):创作表单
 * 校验与按次预估价。组件只做渲染与交互,这里集中可单测的判定 —— 与
 * voice.ts 同族。上限与服务端同源(internal/relay/music.go 与
 * canvastask/handler.go 的 2000 字 prompt / 3500 字歌词),改一边要同步
 * 另一边。
 */
import type { ModelPriceSummary } from '@infinitechance/api'

import { fmtCNY } from './voice'

export const MAX_MUSIC_PROMPT_RUNES = 2000
export const MAX_LYRICS_CHARS = 3500

/** 配乐/歌曲创作表单(创作态音频节点的提交载荷):prompt 是风格描述,
 * model 是 /music-models 目录的公开模型名;instrumental 缺省 true(纯音乐,
 * 60 号票行为不变),false = 人声歌曲(62 号票),lyrics 只属于歌曲表单
 * (选填,留空由模型自动写词)。 */
export interface MusicFormInput {
  prompt: string
  model: string
  instrumental?: boolean
  lyrics?: string
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
  const instrumental = form.instrumental ?? true
  const lyrics = form.lyrics ?? ''
  if (instrumental && lyrics.trim() !== '') {
    return '纯音乐不能携带歌词;请关闭「人声歌曲」开关或清空歌词'
  }
  if (!instrumental && [...lyrics].length > MAX_LYRICS_CHARS) {
    return `歌词最多 ${MAX_LYRICS_CHARS} 个字符`
  }
  return null
}

/** 配乐的按次预估价(元):一任务一首,单价即预估 —— 微元向上取整再折
 * 回元(与网关「扣费向上取整不低估」同形);price 缺失或非按次轨返回
 * null(按钮不标价)。歌曲与配乐同价(62 号票:计费面不变)。 */
export function estimateMusicCNY(price: ModelPriceSummary | undefined): number | null {
  if (!price || price.unit !== 'call' || price.cny_per_call == null) {
    return null
  }
  return Math.ceil(price.cny_per_call * 1e6) / 1e6
}

export { fmtCNY }
