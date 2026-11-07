/**
 * 全景域纯逻辑(81 号票,第十四种节点类型「创作」组):2:1 判定与偏离
 * 度、查看层截图的比例档与尺寸表、终态任务对全景节点自身的回填补丁。
 * 组件只做渲染与事件,这里集中可单测的判定(/music.ts、/sfx.ts 同款分工)。
 * 地址规则沿用 composer.ts 的 taskUrlFor(素材内容寻址优先)。
 */
import type { PresetOption } from './composer'
import { taskUrlFor, type TaskForNodeSync } from './composer'
import type { PanoramaNodeData } from './graph'

/** 全景公开名即缺省模型(81 号票定案 1/5):/image-models 全量下拉、
 * 不按模型特判 —— 选非全景名由上游 400 透出,前端不拦。 */
export const PANO_DEFAULT_MODEL = 'hunyuan-world-panorama'

/** 全景场景类型(81 号票网关契约):createInput 可选字段,当前仅收此值;
 * 缺省不传 = 普通生图,行为不变。 */
export const PANO_SCENE_TYPE = '3d_panorama'

// ---- 2:1 判定(直接环视入口与产物校验共用)----

/** 宽高比对 2:1 的偏离度(百分点):|w/h − 2| / 2 × 100。零/负尺寸 =
 * ∞(图片未加载/坏数据一律不算全景)。 */
export function panoRatioDeviation(width: number, height: number): number {
  if (!(width > 0) || !(height > 0)) {
    return Number.POSITIVE_INFINITY
  }
  const ratio = width / height
  return (Math.abs(ratio - 2) / 2) * 100
}

/** 宽高比 ≈2:1 判定(±2%,容差百分点可调):参考图/素材图出「直接环视」
 * 与产物校验提示(偏离超 2% 才提示)共用同一条线。边界值(恰 2.04:1)
 * 带 1e-9 数值容差 —— IEEE75 下 |2.04−2| 的尾差比百分点粒度小若干量级,
 * 精确压线不应被舍入噪声拒掉。 */
export function isPanoRatio(width: number, height: number, tolerancePercent = 2): boolean {
  return panoRatioDeviation(width, height) <= tolerancePercent + 1e-9
}

// ---- 查看层截图(票·定案 8)----

/** 截图取景比例五档(复用 RATIO_PRESETS 家族的 PresetOption 形状;
 * 「自动」档不存在 —— 查看层截图必须落在具体画布上)。 */
export const PANO_CAPTURE_RATIOS: PresetOption[] = [
  { label: '1:1', value: '1:1' },
  { label: '16:9', value: '16:9' },
  { label: '9:16', value: '9:16' },
  { label: '4:3', value: '4:3' },
  { label: '3:4', value: '3:4' },
]

/** 缺省取景比例(定案:16:9)。 */
export const PANO_CAPTURE_DEFAULT_RATIO = '16:9'

/** 截图长边固定 1920(定案),短边按比例取整。 */
export const PANO_CAPTURE_LONG_EDGE = 1920

const CAPTURE_SIZE_TABLE: Record<string, [number, number]> = {
  '1:1': [1920, 1920],
  '16:9': [1920, 1080],
  '9:16': [1080, 1920],
  '4:3': [1920, 1440],
  '3:4': [1440, 1920],
}

/** 比例 → 截图画布像素(长边 1920);未知档位回落缺省 16:9 —— 下拉只出
 * 五档,这里只是防御坏数据,不虚构第六种画幅。 */
export function captureSize(ratio: string): { width: number; height: number } {
  const [w, h] = CAPTURE_SIZE_TABLE[ratio] ?? CAPTURE_SIZE_TABLE[PANO_CAPTURE_DEFAULT_RATIO]!
  return { width: w, height: h }
}

// ---- 终态任务对全景节点自身的回填(票·定案 5)----

/** panoramaSyncPatch 收的任务形状:TaskForNodeSync 加 scene_type(81 号票
 * 任务行回显;收窄到相关子集是为了不依赖 api 包,测试可自造轻量对象)。 */
export type PanoramaTaskForSync = TaskForNodeSync & { scene_type?: string }

/** 全景节点补丁:产物三件套 url/asset_id/model。ref_asset_id 是创作事实
 * (参考场景图),永不出现在补丁里 —— 节点 data 持有,发送时上送。 */
export interface PanoramaNodeSyncPatch {
  url: string
  asset_id: number
  model?: string
}

/**
 * 终态全景任务(image + scene_type=3d_panorama)对节点自身的对账补丁。
 * 与 mediaSyncPatch 的「补空缺」不同:全景产物回填节点自身、**重新生成 =
 * 原地替换** —— url/asset_id 每次成功都覆盖(旧产物留素材库,内容寻址
 * 引用换新),model 跟随本次生成覆盖(产物-元数据配对随产物走);任务行
 * model 为空不物化(不虚构模型事实)。非成功态 / 无素材行返回 null(全景
 * 产物恒落素材库,厂商地址回落资格不适用);与已落节点完全一致时返回
 * null(轮询重复同步不落盘,与 composer 补丁族同一纪律)。
 */
export function panoramaSyncPatch(
  task: PanoramaTaskForSync,
  data: PanoramaNodeData,
): PanoramaNodeSyncPatch | null {
  if (task.status !== 'succeeded' || task.asset_id <= 0) {
    return null
  }
  const url = taskUrlFor(task)
  if (url === '') {
    return null
  }
  const modelChanges = task.model !== '' && (data.model ?? '') !== task.model
  if (data.url === url && data.asset_id === task.asset_id && !modelChanges) {
    return null
  }
  return modelChanges
    ? { url, asset_id: task.asset_id, model: task.model }
    : { url, asset_id: task.asset_id }
}
