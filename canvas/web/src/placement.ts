/**
 * 新建节点落点换算(33 号票):工具栏添加与素材/生成记录插入都是无锚点
 * 落位,新节点落当前视口中心;级联偏移叠在中心上,连加多个不错叠。
 * 输入为画布容器的 client 矩形与现有节点数,输出为屏幕(client)坐标
 * 锚点 —— 由调用方经 vue-flow 的 screenToFlowCoordinate 换算成图坐标,
 * 换算本身依赖运行时 viewport,不在本模块。
 */

/** 相对中心回退半个节点身位让节点近似居中:节点宽 220–280、高不一,
 * 取近似半宽 140 / 半高 90,视觉居中足够,不必按类型精算。 */
const NOMINAL_HALF_WIDTH = 140
const NOMINAL_HALF_HEIGHT = 90

/** 级联步进沿用既有节奏:8 个一循环、每步 48px(对角错开)。 */
const CASCADE_CYCLE = 8
const CASCADE_STEP = 48

export function newNodeAnchor(
  rect: { left: number; top: number; width: number; height: number },
  nodeCount: number,
): { x: number; y: number } {
  const cascade = (nodeCount % CASCADE_CYCLE) * CASCADE_STEP
  return {
    x: rect.left + rect.width / 2 - NOMINAL_HALF_WIDTH + cascade,
    y: rect.top + rect.height / 2 - NOMINAL_HALF_HEIGHT + cascade,
  }
}
