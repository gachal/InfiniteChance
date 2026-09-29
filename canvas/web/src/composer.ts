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
