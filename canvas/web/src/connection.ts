/**
 * 连线合法矩阵(30 号票):画布连线的前端轻校验 —— 只约束「新建立的连
 * 线」;历史画布已存边不清洗不校验(展示容忍),后端整图落库不校验(单
 * 管理员,矩阵错漏改代码即可,不上锁)。
 */

import type { CanvasNodeType } from './graph'

/**
 * 合法矩阵(源类型 → 可达目标类型),语义收敛为五种(CONTEXT.md「连线」):
 * - agent → image/video        提示词投递通道(29 号票推模式)
 * - video → agent              反推来源(13 号票)
 * - image/video → analysis     分析来源(17 号票)
 * - image/video → image/video  迭代来源(21 号票)
 * - previz → image             渲染帧落点(39 号票,落画布成图片节点)
 * analysis 不作为任何连线的源;previz 不作为任何连线的目标(输入是用户
 * 摆的场景,不接上游);其余组合(agent→agent、previz→video 等)拒连。
 */
export const EDGE_MATRIX: Record<CanvasNodeType, readonly CanvasNodeType[]> = {
  agent: ['image', 'video'],
  image: ['image', 'video', 'analysis'],
  video: ['agent', 'image', 'video', 'analysis'],
  analysis: [],
  previz: ['image'],
}

/** source 类型到 target 类型能否建立新连线;未知/缺失类型一律拒绝。 */
export function canConnect(source: string | undefined, target: string | undefined): boolean {
  if (source === undefined || target === undefined) {
    return false
  }
  return (EDGE_MATRIX[source as CanvasNodeType] ?? []).includes(target as CanvasNodeType)
}
