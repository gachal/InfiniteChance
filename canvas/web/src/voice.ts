/**
 * 音色闭环的纯逻辑(57 号票):创建表单/台词表单校验、克隆样本预检、
 * 状态徽章映射与字符预估价。组件只做渲染与交互,这里集中可单测的判定
 * —— 与 storyboard.ts 同族。上限与服务端同源(internal/relay/audio.go
 * 56 号票契约、canvastask/handler.go 57 号票),改一边要同步另一边。
 */
import type { ModelPriceSummary, VoiceStatus } from '@infinitechance/api'

/** 契约上限(与服务端常量一一同源):音色描述 ≤500 字(仅中英)、试听
 * 文本 15–200 字、台词 ≤600 字符、演绎指令 ≤500 字、音色名 ≤255 字。 */
export const MAX_VOICE_PROMPT_RUNES = 500
export const MIN_PREVIEW_TEXT_RUNES = 15
export const MAX_PREVIEW_TEXT_RUNES = 200
export const MAX_SPEECH_TEXT_RUNES = 600
export const MAX_INSTRUCTION_RUNES = 500
export const MAX_VOICE_NAME_RUNES = 255

/** 克隆样本的体积守卫(前端预检):≤10MB,时长质量交上游裁决
 * (样本 UI 提示「10–20 秒清晰人声,WAV/MP3/M4A」)。 */
export const MAX_SAMPLE_BYTES = 10 * 1024 * 1024

export type VoiceMode = 'design' | 'clone'

/** 音色创建表单(节点内联卡的提交载荷,与后端 voice 表单一一同形)。 */
export interface VoiceFormInput {
  mode: VoiceMode
  name: string
  target_model: string
  voice_prompt?: string
  preview_text: string
  sample_asset_id?: number
}

/** 首个校验错误的用户文案;null = 通过(文案与后端 400 同口径)。 */
export function validateVoiceForm(form: VoiceFormInput): string | null {
  if (form.mode !== 'design' && form.mode !== 'clone') {
    return '模式必须是设计或克隆'
  }
  if (form.name.trim() === '') {
    return '音色名不能为空'
  }
  if ([...form.name.trim()].length > MAX_VOICE_NAME_RUNES) {
    return `音色名最多 ${MAX_VOICE_NAME_RUNES} 个字符`
  }
  if (form.target_model.trim() === '') {
    return '请选择合成模型'
  }
  const prompt = (form.voice_prompt ?? '').trim()
  if (form.mode === 'design') {
    if (prompt === '') {
      return '设计模式需要填写音色描述'
    }
    if ([...prompt].length > MAX_VOICE_PROMPT_RUNES) {
      return `音色描述最多 ${MAX_VOICE_PROMPT_RUNES} 个字符`
    }
  }
  const preview = form.preview_text.trim()
  const previewRunes = [...preview].length
  if (previewRunes < MIN_PREVIEW_TEXT_RUNES || previewRunes > MAX_PREVIEW_TEXT_RUNES) {
    return `试听文本需在 ${MIN_PREVIEW_TEXT_RUNES} 到 ${MAX_PREVIEW_TEXT_RUNES} 个字符之间`
  }
  if (form.mode === 'clone' && !(form.sample_asset_id && form.sample_asset_id > 0)) {
    return '克隆模式需要提供声音样本'
  }
  return null
}

/** 台词表单校验:台词 1–600 字符(原文计费,不 trim 交服务端;这里只拦
 * 空与超长),演绎指令可选 ≤500 字。 */
export function validateSpeechForm(text: string, instruction?: string): string | null {
  const runes = [...text].length
  if (runes < 1) {
    return '台词不能为空'
  }
  if (runes > MAX_SPEECH_TEXT_RUNES) {
    return `台词最多 ${MAX_SPEECH_TEXT_RUNES} 个字符`
  }
  const inst = (instruction ?? '').trim()
  if ([...inst].length > MAX_INSTRUCTION_RUNES) {
    return `演绎指令最多 ${MAX_INSTRUCTION_RUNES} 个字符`
  }
  return null
}

/** 克隆样本预检:非音频 MIME 拒收(file.type 缺省时放行,服务端魔数嗅探
 * 兜底)、体积 ≤10MB。返回首个错误的用户文案,null = 通过。 */
export function validateSampleFile(file: { type: string; size: number }): string | null {
  if (file.type !== '' && !file.type.startsWith('audio/')) {
    return '声音样本必须是音频文件(WAV/MP3/M4A)'
  }
  if (file.size > MAX_SAMPLE_BYTES) {
    return `声音样本超过 ${MAX_SAMPLE_BYTES / 1024 / 1024}MB 上限`
  }
  return null
}

/** 状态徽章映射:DEPLOYING 部署中 / OK 可用 / UNDEPLOYED 审核未通过。
 * tone 供卡面配色(deploying 黄、ok 绿、rejected 红)。 */
export function voiceStatusBadge(status: VoiceStatus | string | undefined): {
  label: string
  tone: 'deploying' | 'ok' | 'rejected'
} {
  switch (status) {
    case 'OK':
      return { label: '可用', tone: 'ok' }
    case 'UNDEPLOYED':
      return { label: '审核未通过', tone: 'rejected' }
    default:
      return { label: '部署中', tone: 'deploying' }
  }
}

/** 计费字符权重(CJK/全角 2、其余 1 —— 与服务端 audiochars.go 同一口径,
 * 预估不低估)。 */
export function billableChars(text: string): number {
  let n = 0
  for (const ch of text) {
    n += isWideCodePoint(ch.codePointAt(0) ?? 0) ? 2 : 1
  }
  return n
}

function isWideCodePoint(code: number): boolean {
  return (
    (code >= 0x2e80 && code <= 0x9fff) ||
    (code >= 0xac00 && code <= 0xd7af) ||
    (code >= 0xf900 && code <= 0xfaff) ||
    (code >= 0xfe30 && code <= 0xfe4f) ||
    (code >= 0xff00 && code <= 0xff60) ||
    (code >= 0xffe0 && code <= 0xffe6) ||
    (code >= 0x20000 && code <= 0x2fffd) ||
    (code >= 0x30000 && code <= 0x3fffd)
  )
}

/** 台词的字符预估价(元):(text+instruction 计费字符)× 单价,微元
 * 向上取整再折回元 —— 与网关「扣费向上取整不低估」同形(在微元粒度
 * ceil,不是元粒度);price 缺失或非字符轨返回 null(按钮只标字数不标
 * 价)。 */
export function estimateSpeechCNY(
  text: string,
  instruction: string | undefined,
  price: ModelPriceSummary | undefined,
): number | null {
  if (!price || price.unit !== 'character' || !price.cny_per_10k_chars) {
    return null
  }
  const chars = billableChars(text) + billableChars(instruction ?? '')
  if (chars < 1) {
    return 0
  }
  const micros = Math.ceil((chars * price.cny_per_10k_chars * 1e6) / 10000)
  return micros / 1e6
}

/** 金额展示(元):整数不带小数、最多三位小数、去尾零 —— 与
 * GenerationComposer 的 fmtCNY 同形(成本可见性同一语言)。 */
export function fmtCNY(amount: number): string {
  return `¥${amount.toFixed(3).replace(/\.?0+$/, '')}`
}
