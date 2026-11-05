/**
 * 配乐/歌曲/翻唱/同曲换词生成的纯逻辑(60 号票;62 号票追加歌曲开关与
 * 歌词;78 号票追加翻唱表单校验;79 号票追加同曲换词表单校验,词必填、
 * 风格选填):创作表单校验与按次预估价。组件只做渲染与交互,这里集中可
 * 单测的判定 —— 与 voice.ts 同族。上限与服务端同源(internal/relay/
 * music.go 与 canvastask/handler.go 的 2000 字 prompt / 3500 字歌词),
 * 改一边要同步另一边。80 号票追加同曲换词旋律保持强度(relyric 表单
 * strength 字段与 RELYRIC_STRENGTH_* 滑杆参数常量)。
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

/** 翻唱创作表单(78 号票,创作态 cover 形态照 57 号票克隆样本范式):
 * refAssetId 是参考音频素材行 id(必填恰好 1 条 —— 无参考不可发送,
 * 参考是翻唱的身份所在;来源 = 本机上传 + 素材库挑选双入口),prompt 是
 * 演绎/风格描述(必填 1–2000 runes 照 music),lyrics 是歌词(选填
 * ≤3500,空 = 上游自动写词),model 是 /music-models 目录的公开模型名
 * (全量不过滤,选非 cover 模型由网关 400 兜底)。 */
export interface CoverFormInput {
  refAssetId: number
  prompt: string
  model: string
  lyrics?: string
}

/** 首个校验错误的用户文案;null = 通过(文案与后端 400 同口径)。参考
 * 音频不做大小/时长守卫(78 号票定案 11:聚合侧无明文限制,硬守卫可能
 * 误拒;上游裁决 4xx 透出)。 */
export function validateCoverForm(form: CoverFormInput): string | null {
  if (!(form.refAssetId > 0)) {
    return '请先选择参考音频(必填,恰好 1 条)'
  }
  const prompt = form.prompt.trim()
  if (prompt === '') {
    return '风格描述不能为空'
  }
  const runes = [...prompt].length
  if (runes > MAX_MUSIC_PROMPT_RUNES) {
    return `风格描述最多 ${MAX_MUSIC_PROMPT_RUNES} 个字符`
  }
  if (form.model.trim() === '') {
    return '请选择翻唱模型'
  }
  const lyrics = form.lyrics ?? ''
  if ([...lyrics].length > MAX_LYRICS_CHARS) {
    return `歌词最多 ${MAX_LYRICS_CHARS} 个字符`
  }
  return null
}

/** 同曲换词创作表单(79 号票,cover 形态的姊妹分叉):refAssetId 是参考
 * 原曲素材行 id(必填恰好 1 条 —— 无参考无旋律锁定,参考是身份所在;
 * 来源 = 本机上传 + 素材库挑选双入口),lyrics 是**新歌词,必填**(trim
 * 后非空,≤3500;同曲换词按新词重唱,词是身份必需,无「自动写词」,
 * 与翻唱的选填歌词区分),prompt 是风格描述**选填**(非空时 1–2000
 * runes 照 music 上限;空 = 不送风格描述),model 是 /music-models 目录
 * 的公开模型名(全量不过滤,选非 mureka 名由网关 400 兜底),strength 是
 * 旋律保持强度(80 号票,0–1 数值恒带 —— 组件侧以缺省常量兜底,提交
 * payload 恒显式送 cover_strength)。 */
export interface RelyricFormInput {
  refAssetId: number
  prompt: string
  model: string
  lyrics?: string
  strength: number
}

/** 旋律保持强度滑杆参数(80 号票定案 5,组件与测试共用防手抄漂移):
 * 越高越贴原曲旋律(重组自由度越低);0.3–0.9 取文档推荐起点带内、两端
 * 防呆(过低 ≈ 没锁旋律、过高 ≈ 重组受限),步进 0.05,缺省 0.55。
 * 范围校验交给滑杆钳制,校验函数只防 NaN 与超 0–1(数值型,无空串歧义)。 */
export const RELYRIC_STRENGTH_MIN = 0.3
export const RELYRIC_STRENGTH_MAX = 0.9
export const RELYRIC_STRENGTH_STEP = 0.05
export const RELYRIC_STRENGTH_DEFAULT = 0.55

/** 首个校验错误的用户文案;null = 通过(文案与后端 400 同口径)。参考
 * 音频不做大小/时长守卫(79 号票定案 8 照 78 号票先例:硬守卫可能误拒,
 * 上游裁决 4xx 透出);strength 只防 NaN/非有限值与超 0–1(80 号票,
 * 越界网关 400 同口径)。 */
export function validateRelyricForm(form: RelyricFormInput): string | null {
  if (!(form.refAssetId > 0)) {
    return '请先选择参考音频(必填,恰好 1 条)'
  }
  if (!Number.isFinite(form.strength) || form.strength < 0 || form.strength > 1) {
    return '旋律保持强度不合法(应为 0–1 的数值)'
  }
  const prompt = form.prompt.trim()
  if (prompt !== '' && [...prompt].length > MAX_MUSIC_PROMPT_RUNES) {
    return `风格描述最多 ${MAX_MUSIC_PROMPT_RUNES} 个字符`
  }
  if (form.model.trim() === '') {
    return '请选择同曲换词模型'
  }
  const lyrics = (form.lyrics ?? '').trim()
  if (lyrics === '') {
    return '新歌词不能为空'
  }
  if ([...(form.lyrics ?? '')].length > MAX_LYRICS_CHARS) {
    return `歌词最多 ${MAX_LYRICS_CHARS} 个字符`
  }
  return null
}

export { fmtCNY }
