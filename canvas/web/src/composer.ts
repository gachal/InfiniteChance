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

/** 尺寸预设:默认档 = 不传 size(维持各上游自身缺省),具体值由上游
 * 裁决,不认的 size 以 4xx 透出、节点上可见可重试(21 号票)。 */
export interface SizePreset {
  label: string
  value: string
}

export const SIZE_PRESETS: SizePreset[] = [
  { label: '默认尺寸', value: '' },
  { label: '1:1 · 1024×1024', value: '1024x1024' },
  { label: '16:9 · 1792×1024', value: '1792x1024' },
  { label: '9:16 · 1024×1792', value: '1024x1792' },
]
