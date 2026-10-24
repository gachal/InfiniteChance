/**
 * 画布类型注册表(50 号票,ADR 0004 定案 2):canvases.type 存字符串、
 * 后端不校验枚举 —— 加类型 = 这里加一项,后端零改动。注册表驱动新建画布
 * 对话框的类型卡、IP 详情页的类型分组与编辑器顶栏的类型徽章。
 */

/** 画布类型的 id 词表:与后端 canvases.type 列的字符串同形。 */
export type CanvasTypeId = 'animation' | 'game' | 'manga' | 'general'

/** 一个画布类型的展示事实:label 中文名、description 定位一句话、icon
 * 是类型卡/徽章的字符标(纯文本,不引图标库)。 */
export interface CanvasTypeMeta {
  id: CanvasTypeId
  label: string
  description: string
  icon: string
}

/** 四类先行(50 号票定案):存量画布收编为 animation(「当前的画布就是
 * 动画画布」),新建缺省 general。 */
export const CANVAS_TYPES: CanvasTypeMeta[] = [
  {
    id: 'animation',
    label: '动画',
    description: '分镜、设定与动态影像的动画企划画布',
    icon: '🎬',
  },
  {
    id: 'game',
    label: '游戏',
    description: '关卡、实体与美术资源的游戏企划画布',
    icon: '🎮',
  },
  {
    id: 'manga',
    label: '漫画',
    description: '分镜页与格间叙事的漫画企划画布',
    icon: '📖',
  },
  {
    id: 'general',
    label: '通用',
    description: '不预设媒介的自由创作画布',
    icon: '✳️',
  },
]

export function isCanvasTypeId(value: string): value is CanvasTypeId {
  return value === 'animation' || value === 'game' || value === 'manga' || value === 'general'
}

/** 按类型 id 取展示事实;未知串(后端不校验枚举,历史数据可能携带)回落
 * 原样 id 作名字,不抛错 —— 展示面永远可用。 */
export function canvasTypeMeta(id: string): CanvasTypeMeta {
  return (
    CANVAS_TYPES.find((t) => t.id === id) ?? {
      id: id as CanvasTypeId,
      label: id,
      description: '',
      icon: '✳️',
    }
  )
}

export function canvasTypeLabel(id: string): string {
  return canvasTypeMeta(id).label
}

/** 新建画布的缺省类型(与后端 DefaultCanvasType 同源口径)。 */
export const DEFAULT_CANVAS_TYPE: CanvasTypeId = 'general'
