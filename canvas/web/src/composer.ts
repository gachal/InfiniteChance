/**
 * 生成对话框的纯逻辑(21 号票):参考图列表的收编规则与尺寸预设表。
 * 组件只做渲染与事件,这里集中可单测的判定 —— 参考图上限、去重、
 * 可进网关契约的地址形状。
 */

/** 对话框参考条上的一条参考图:素材引用或厂商地址。 */
export interface ComposerRef {
  url: string
  asset_id?: number
}

/** 一次图生图提交的参考图上限(与 canvas 后端 maxImageRefs 同值)。 */
export const MAX_COMPOSER_REFS = 4

/** 参考图地址可进网关媒体契约的形状:厂商 http(s) 地址,或素材内容寻
 * 址路径(服务端解出厂商地址)。data: URI 进不了契约,不收。 */
export function isRelayableRef(url: string): boolean {
  return url.startsWith('http://') || url.startsWith('https://') || url.startsWith('/api/assets/')
}

/** 追加一条参考图:URL 去重,超上限整条拒收(返回原数组)。纯函数,
 * 调用方拿返回值替换状态。 */
export function appendComposerRef(list: ComposerRef[], ref: ComposerRef): ComposerRef[] {
  if (!isRelayableRef(ref.url)) {
    return list
  }
  if (list.length >= MAX_COMPOSER_REFS) {
    return list
  }
  if (list.some((r) => r.url === ref.url)) {
    return list
  }
  return [...list, ref]
}

/** 对话框发出的 image_urls:参考图地址原样上送,服务端解引用。 */
export function composerImageUrls(refs: ComposerRef[]): string[] {
  return refs.map((r) => r.url)
}

/** 比例与分辨率预设(21 号票修订:拆成两个下拉,新增 4:3/3:4 与
 * 1K/2K/4K)。两者组合成具体的 size 串走既有透传契约;「自动 × 自动」
 * = 不传 size(维持各上游自身缺省),不认的组合由上游 4xx 透出、节点上
 * 可见可重试。 */
export interface PresetOption {
  label: string
  value: string
}

export const RATIO_PRESETS: PresetOption[] = [
  { label: '自动比例', value: '' },
  { label: '1:1', value: '1:1' },
  { label: '16:9', value: '16:9' },
  { label: '9:16', value: '9:16' },
  { label: '4:3', value: '4:3' },
  { label: '3:4', value: '3:4' },
]

export const RESOLUTION_PRESETS: PresetOption[] = [
  { label: '自动尺寸', value: '' },
  { label: '1K', value: '1k' },
  { label: '2K', value: '2k' },
  { label: '4K', value: '4k' },
]

/** 组合表:比例 × 分辨率 → 具体像素串。分辨率档位 = 短边量级,横竖比
 * 都取精确整数比(16:9/4:3 等 VOD 的宽高比就近与 openai 类上游都吃得
 * 下);1K 档 16:9 用精确 1920x1080,不再沿用 1792x1024 的近比旧值。 */
const SIZE_TABLE: Record<string, Record<string, string>> = {
  '1:1': { '1k': '1024x1024', '2k': '2048x2048', '4k': '4096x4096' },
  '16:9': { '1k': '1920x1080', '2k': '2560x1440', '4k': '3840x2160' },
  '9:16': { '1k': '1080x1920', '2k': '1440x2560', '4k': '2160x3840' },
  '4:3': { '1k': '1440x1080', '2k': '2048x1536', '4k': '4096x3072' },
  '3:4': { '1k': '1080x1440', '2k': '1536x2048', '4k': '3072x4096' },
}

/** 兜底档:只选了一轴时另一轴落这里 —— 选了比例给 1K,选了分辨率给
 * 1:1(都选「自动」= 完全不传,维持上游缺省)。 */
const DEFAULT_RESOLUTION = '1k'
const DEFAULT_RATIO = '1:1'

/** 比例 × 分辨率 → size 串。双自动返回空串;单选一轴按兜底档补全;
 * 未知组合返回空串(视同自动,不虚构尺寸)。 */
export function composeSize(ratio: string, resolution: string): string {
  if (ratio === '' && resolution === '') {
    return ''
  }
  const r = ratio === '' ? DEFAULT_RATIO : ratio
  const res = resolution === '' ? DEFAULT_RESOLUTION : resolution
  return SIZE_TABLE[r]?.[res] ?? ''
}

// ---- 视频模式(24 号票):全模态参考条 + 时长/分辨率档位 ----

/** 视频参考的媒体种类;与素材域的 kind 同词表(image/video/audio)。 */
export type VideoRefKind = 'image' | 'video' | 'audio'

/** 视频参考的角色:首帧/尾帧/参考图承载图片,参考视频/参考音频各归其位
 * (对齐即梦「全能参考」,方案 A 单条参考条 + 角色标记,不上 tab)。 */
export type VideoRefRole =
  | 'first_frame'
  | 'last_frame'
  | 'reference_image'
  | 'reference_video'
  | 'reference_audio'

/** 对话框视频参考条上的一条参考。 */
export interface VideoComposerRef {
  url: string
  kind: VideoRefKind
  role: VideoRefRole
  asset_id?: number
}

/** 每个角色的数量上限(与 canvas 后端 maxVideoRefsByRole 同值;上游真实
 * 上限是验证点,自限保守值,超限 4xx 透出即可)。 */
export const VIDEO_ROLE_CAPS: Record<VideoRefRole, number> = {
  first_frame: 1,
  last_frame: 1,
  reference_image: 4,
  reference_video: 1,
  reference_audio: 1,
}

export const VIDEO_ROLE_LABEL: Record<VideoRefRole, string> = {
  first_frame: '首帧',
  last_frame: '尾帧',
  reference_image: '参考图',
  reference_video: '参考视频',
  reference_audio: '参考音频',
}

/** 某媒体种类可扮演的角色(角色在 chip 上切换,候选按种类收敛)。 */
export function rolesForKind(kind: VideoRefKind): VideoRefRole[] {
  switch (kind) {
    case 'image':
      return ['first_frame', 'last_frame', 'reference_image']
    case 'video':
      return ['reference_video']
    case 'audio':
      return ['reference_audio']
  }
}

/** 追加一条视频参考:地址形状不对、角色与种类不符、URL 重复、角色满员都
 * 整条拒收(返回原数组)—— 与 appendComposerRef 同款纪律。 */
export function appendVideoRef(list: VideoComposerRef[], ref: VideoComposerRef): VideoComposerRef[] {
  if (!isRelayableRef(ref.url)) {
    return list
  }
  if (!rolesForKind(ref.kind).includes(ref.role)) {
    return list
  }
  if (list.some((r) => r.url === ref.url)) {
    return list
  }
  const held = list.filter((r) => r.role === ref.role).length
  if (held >= VIDEO_ROLE_CAPS[ref.role]) {
    return list
  }
  return [...list, ref]
}

/** 切换一条参考的角色:目标角色不属于该媒体或已满员则原样返回。 */
export function withVideoRefRole(
  list: VideoComposerRef[],
  index: number,
  role: VideoRefRole,
): VideoComposerRef[] {
  const ref = list[index]
  if (!ref || ref.role === role || !rolesForKind(ref.kind).includes(role)) {
    return list
  }
  const held = list.filter((r, i) => i !== index && r.role === role).length
  if (held >= VIDEO_ROLE_CAPS[role]) {
    return list
  }
  return list.map((r, i) => (i === index ? { ...r, role } : r))
}

/** 对话框发出的 video_refs:{url, kind, role} 原样上送,服务端解引用。 */
export function composerVideoRefs(
  refs: VideoComposerRef[],
): { url: string; kind: VideoRefKind; role: VideoRefRole }[] {
  return refs.map((r) => ({ url: r.url, kind: r.kind, role: r.role }))
}

/** 提示词占位随参考组合变化:空 = 文生视频,有首帧 = 图生视频,
 * 首帧+尾帧 = 首尾帧,仅参考图 = 多图参考(参考组合自然成模式,无显式
 * 模式 tab)。 */
export function videoPlaceholder(refs: VideoComposerRef[]): string {
  const roles = new Set(refs.map((r) => r.role))
  if (roles.size === 0) {
    return '描述要生成的画面与镜头(文生视频)…'
  }
  if (roles.has('first_frame') && roles.has('last_frame')) {
    return '描述首尾帧之间的镜头如何运动(首尾帧)…'
  }
  if (roles.has('first_frame')) {
    return '描述画面如何运动、镜头怎么走(图生视频)…'
  }
  if (roles.has('reference_image')) {
    return '描述画面,参考图提供风格与内容(多图参考)…'
  }
  return '描述要生成的画面与镜头(参考视频/音频)…'
}

/** 视频时长预设(24 号票):「自动」= 不传 seconds 维持厂商缺省;不做自由
 * 数字输入 —— duration 是厂商枚举,自由值多半 4xx,档位实测校准。 */
export const DURATION_PRESETS: PresetOption[] = [
  { label: '自动时长', value: '' },
  { label: '5 秒', value: '5' },
  { label: '10 秒', value: '10' },
  { label: '15 秒', value: '15' },
  { label: '20 秒', value: '20' },
  { label: '30 秒', value: '30' },
]

/** 视频分辨率预设:选中档位串直接作为 size 上送(second 轨计价系数表与
 * token 轨估算表都以这些字符串为键,契约零新增)。 */
export const VIDEO_RESOLUTION_PRESETS: PresetOption[] = [
  { label: '自动分辨率', value: '' },
  { label: '480p', value: '480p' },
  { label: '720p', value: '720p' },
  { label: '1080p', value: '1080p' },
]
