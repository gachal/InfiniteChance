/**
 * 两击连线状态机(30 号票):节点旁常驻 + 按钮 → 点击进入连接态 → 预
 * 连线跟随鼠标 → 点目标节点完成连线(方向按 + 在左/右自动定);Esc、
 * 点空白、点非法节点都取消。状态与校验在这里(纯状态,可脱离 DOM 单测),
 * 几何换算与事件接线在编辑器。
 */

import { computed, ref } from 'vue'

import { canConnect } from '../connection'

/** + 按钮所在侧:left = 接上游(本节点为 target),right = 连下游(本节点为 source)。 */
export type ConnectSide = 'left' | 'right'

/** 连接态里节点的展示态:origin = 连线起点,valid = 可点目标,dimmed = 非法置灰。 */
export type ConnectState = 'origin' | 'valid' | 'dimmed'

/** 连接态:nodeId 是 + 按钮所在节点,side 是按钮侧。 */
export interface PendingConnection {
  nodeId: string
  side: ConnectSide
}

/** 状态机只关心节点的 id 与类型;vue-flow 的 GraphNode 天然满足。 */
export interface ConnectableNode {
  id: string
  type?: string
}

export function useConnection(options: {
  /** 图内节点现取快照(vue-flow store 每次读取都是最新)。 */
  nodes: () => ConnectableNode[]
  /** 连线落地回调;同向重复边的去重由编辑器负责。 */
  onCommit: (edge: { source: string; target: string }) => void
}) {
  const pending = ref<PendingConnection | null>(null)
  /** 预连线终点(画布区屏幕坐标),由编辑器在 mousemove 里喂进来。 */
  const pointer = ref<{ x: number; y: number } | null>(null)

  const active = computed(() => pending.value !== null)

  /** 点击 + 进入/切换连接态;同节点同侧再点 = 取消(幂等退出)。 */
  function start(nodeId: string, side: ConnectSide): void {
    if (pending.value?.nodeId === nodeId && pending.value.side === side) {
      cancel()
      return
    }
    pending.value = { nodeId, side }
    pointer.value = null
  }

  function cancel(): void {
    pending.value = null
    pointer.value = null
  }

  /** 连接态里点击节点:合法目标按 + 的左右自动定向提交,完成即退出;
   * 点回原点与点非法节点都按「未表达连线意图」处理 —— 取消,不产生连线。 */
  function clickNode(nodeId: string): void {
    const p = pending.value
    if (!p) {
      return
    }
    const source = p.side === 'right' ? p.nodeId : nodeId
    const target = p.side === 'right' ? nodeId : p.nodeId
    if (p.nodeId !== nodeId && canConnect(typeOf(source), typeOf(target))) {
      options.onCommit({ source, target })
    }
    cancel()
  }

  /** 预连线跟随:只连接态记录,退出即清。 */
  function moveTo(point: { x: number; y: number }): void {
    if (pending.value) {
      pointer.value = point
    }
  }

  /** 节点在连接态的展示态;不在连接态恒 undefined(正常渲染)。 */
  function stateOf(nodeId: string): ConnectState | undefined {
    const p = pending.value
    if (!p) {
      return undefined
    }
    if (p.nodeId === nodeId) {
      return 'origin'
    }
    const ok =
      p.side === 'right'
        ? canConnect(typeOf(p.nodeId), typeOf(nodeId))
        : canConnect(typeOf(nodeId), typeOf(p.nodeId))
    return ok ? 'valid' : 'dimmed'
  }

  function typeOf(nodeId: string): string | undefined {
    return options.nodes().find((n) => n.id === nodeId)?.type
  }

  return { pending, pointer, active, start, cancel, clickNode, moveTo, stateOf }
}
