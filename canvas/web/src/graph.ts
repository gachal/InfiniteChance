/**
 * 画布图文档的编辑器侧形状:vue-flow 的运行时对象与持久化 JSON 之间的
 * 边界。持久化只保留语义字段(id/type/position/data、连线的端点),
 * vue-flow 的内部装饰(尺寸、事件、选中态)不落库。
 */

/** 提示词节点数据:自由文本,后续 AI 动作(10/11 号票)以它为输入。 */
export interface PromptNodeData {
  text: string
}

/** 图片/视频节点数据:生成产物或占位;url 为空表示还没有产物。 */
export interface MediaNodeData {
  url?: string
  note?: string
}

export type CanvasNodeData = PromptNodeData | MediaNodeData

export type CanvasNodeType = 'prompt' | 'image' | 'video'

export function isCanvasNodeType(value: unknown): value is CanvasNodeType {
  return value === 'prompt' || value === 'image' || value === 'video'
}

/** 新节点的初始数据。 */
export function initialData(type: CanvasNodeType): CanvasNodeData {
  return type === 'prompt' ? { text: '' } : { url: '', note: '' }
}

export const NODE_TYPE_LABEL: Record<CanvasNodeType, string> = {
  prompt: '提示词',
  image: '图片',
  video: '视频',
}
