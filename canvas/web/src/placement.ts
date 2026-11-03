/**
 * 新建节点落点换算(33 号票):素材/生成记录插入是无锚点落位,新节点
 * 落当前视口中心;级联偏移叠在锚点上,连加多个不错叠。
 * 输入为画布容器的 client 矩形与现有节点数,输出为屏幕(client)坐标
 * 锚点 —— 由调用方经 vue-flow 的 screenToFlowCoordinate 换算成图坐标,
 * 换算本身依赖运行时 viewport,不在本模块。
 *
 * 76 号票追加了**有锚点**落位入口 cascadeAnchor:右键菜单的落点语义是
 * 「右键位置即新节点落点」,调用方先把右键 client 点经
 * screenToFlowCoordinate 换算成 flow 图坐标锚点,级联在 **flow 空间**
 * 从该锚点起算 —— 缩放≠1 时屏幕步长随缩放,图坐标步长恒为 48px;
 * 与视口中心入口共用同一级联节奏(本模块单源),两个入口不漂移。
 * 函数本身不感知坐标系(纯 48px 加法),坐标系约定由调用方持有。
 */

/** 相对中心回退半个节点身位让节点近似居中:节点宽 220–280、高不一,
 * 取近似半宽 140 / 半高 90,视觉居中足够,不必按类型精算。 */
const NOMINAL_HALF_WIDTH = 140
const NOMINAL_HALF_HEIGHT = 90

/** 级联步进沿用既有节奏:8 个一循环、每步 48px(对角错开)。 */
const CASCADE_CYCLE = 8
const CASCADE_STEP = 48

export function cascadeAnchor(
  anchor: { x: number; y: number },
  nodeCount: number,
): { x: number; y: number } {
  const cascade = (nodeCount % CASCADE_CYCLE) * CASCADE_STEP
  return {
    x: anchor.x + cascade,
    y: anchor.y + cascade,
  }
}

export function newNodeAnchor(
  rect: { left: number; top: number; width: number; height: number },
  nodeCount: number,
): { x: number; y: number } {
  return cascadeAnchor(
    {
      x: rect.left + rect.width / 2 - NOMINAL_HALF_WIDTH,
      y: rect.top + rect.height / 2 - NOMINAL_HALF_HEIGHT,
    },
    nodeCount,
  )
}
