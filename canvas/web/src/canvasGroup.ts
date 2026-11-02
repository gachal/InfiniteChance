/**
 * 画布分组(72 号票)的纯函数模块:归属判定(节点包围盒中心入框/出框)、
 * 成组合并(跨组换组、组节点排除、幂等)、解组与悬空成员清理、框尺寸合并
 * (max / expand / 回落 / resize 夹紧)、落点入组判定与拖动归属结算 ——
 * 全部无 vue 运行时依赖,可 vitest 直测(票 F3 定案)。
 *
 * 领域口径(见 CONTEXT.md「画布分组」与 issues/72-canvas-group.md):
 * - 成员关系单源存组侧(data.memberIds),成员节点 data 零改动;
 * - 归属模型 = 显式成员 + 自动进出,判定锚点恒为节点包围盒中心
 *   (拖入加入 / 拖出脱离 / 跨组换组 / 落点入组四处同一锚点),
 *   框是视觉不是归属判据;
 * - 框实际尺寸 = 组原点锚定的 max(用户设定尺寸, 成员包围盒 + 内边距
 *   24px),用户尺寸存组 data(w/h,「装饰尺寸不入库」惯例的显式例外);
 * - 组不吸纳组、不嵌套;组与组可视觉重叠(后画的框在上)。
 */

/** 组节点 data 形状与 graph.ts 的 GroupNodeData 保持同构(类型从 graph
 * 单向 import,graph 侧再引本模块的缺省数据工厂 —— 类型引用编译期擦除,
 * 无运行时环)。 */
import type { GroupNodeData } from './graph'

/** 组框与成员的内边距(票 D1 定值)。 */
export const GROUP_PADDING = 24

/** 空组最小占位(票 B3:无拖画的编程创建落 260×160;用户尺寸与 resize
 * 同用此下限夹紧)。 */
export const GROUP_MIN_WIDTH = 260
export const GROUP_MIN_HEIGHT = 160

/** 标题缺省值(票 B3)。 */
export const GROUP_DEFAULT_TITLE = '分组'

/** 节点尺寸未量出(addNodes 后 dimensions 未初始化的窄窗)时的近似宽高,
 * 与 placement.ts 的落位经验值同源(半宽 140 / 半高 90)。 */
export const NOMINAL_NODE_WIDTH = 280
export const NOMINAL_NODE_HEIGHT = 180

/** 组色板(票 E3:6-8 预设半透明色)。存 style.css 的 rgb 三元组 token
 * 名而非 hex —— 主题换色跟随,渲染层按 `rgba(var(--<token>-rgb), α)` 取色
 * (CanvasToolbar 的 .add-* 同配方)。顺序即色板展示序,第一色为缺省。 */
export const GROUP_PALETTE = ['sky', 'teal', 'violet', 'pink', 'orange', 'gold', 'success', 'red-signal'] as const

export type GroupColorToken = (typeof GROUP_PALETTE)[number]

/** resize 手柄四角(票 D4):GroupNode 渲染手柄、编辑器结算尺寸,共用此
 * 形 —— 两处曾各写一份字面量,收口在此防漂移。 */
export type GroupResizeCorner = 'nw' | 'ne' | 'sw' | 'se'

/** 四角枚举(渲染 v-for 顺序)。 */
export const GROUP_RESIZE_CORNERS: GroupResizeCorner[] = ['nw', 'ne', 'sw', 'se']

export interface Point {
  x: number
  y: number
}

/** 矩形:x/y 左上角 + 宽高(组框与成员盒子通用)。 */
export interface GroupBox {
  x: number
  y: number
  width: number
  height: number
}

/** 带组节点 id 的框(落点判定/拖动结算的输入形状)。 */
export interface GroupFrame extends GroupBox {
  id: string
}

/** 组侧成员清单(id = 组节点 id)。 */
export interface GroupMembership {
  id: string
  memberIds: string[]
}

/** 成员节点盒子(x/y 左上 + 宽高)。 */
export type NodeBox = GroupBox

/** 用户设定尺寸(data.w/h 同层;可空 = 未设定)。 */
export interface GroupUserSize {
  w?: number
  h?: number
}

/** 缺省组数据(票 B3:标题「分组」、色板第一色、空成员;w/h 不落 ——
 * 编程创建的尺寸占位由编辑器按落点计算)。 */
export function defaultGroupData(): GroupNodeData {
  return { title: GROUP_DEFAULT_TITLE, color: GROUP_PALETTE[0], memberIds: [] }
}

/** 色板内 token 原样通过;色板外/缺省回落第一色(历史 data 与手改数据的
 * 唯一兜底,不猜近色)。 */
export function groupColorOrDefault(color: string | undefined): GroupColorToken {
  if (color && (GROUP_PALETTE as readonly string[]).includes(color)) {
    return color as GroupColorToken
  }
  return GROUP_PALETTE[0]
}

/** 节点包围盒中心:position + 尺寸一半;尺寸未量出(或半缺)时逐维按
 * 近似值回落(票 C1:三处判定同一锚点)。 */
export function nodeCenter(
  position: Point,
  size?: { width?: number; height?: number },
): Point {
  const width = size?.width ?? NOMINAL_NODE_WIDTH
  const height = size?.height ?? NOMINAL_NODE_HEIGHT
  return { x: position.x + width / 2, y: position.y + height / 2 }
}

/** 中心点是否在框内:闭区间(恰在边线/角点算入框)—— 高亮反馈已消除
 * 歧义,不留滞回阈值(票 C1)。 */
export function isCenterInBox(center: Point, box: GroupBox): boolean {
  return (
    center.x >= box.x &&
    center.x <= box.x + box.width &&
    center.y >= box.y &&
    center.y <= box.y + box.height
  )
}

/** 成员清单合并:既有序在前、新增按给出序追加,跨与内去重(对已同组成员
 * 幂等,票验证点)。 */
export function mergeGroupMembers(existing: string[], additions: string[]): string[] {
  const seen = new Set(existing)
  const merged = [...existing]
  for (const id of additions) {
    if (!seen.has(id)) {
      seen.add(id)
      merged.push(id)
    }
  }
  return merged
}

/** 成员清单摘除(保序;未命中原样语义 = 返回等值新数组,由调用方 diff)。 */
export function removeGroupMembers(existing: string[], removed: Iterable<string>): string[] {
  const gone = new Set(removed)
  return existing.filter((id) => !gone.has(id))
}

/** 只回传 memberIds 有变化的组(成组 / 删除清理 / 拖动结算三个计划函数
 * 共用的 diff 收口):next 与 origin 同 id 逐位比较 —— 长度不同或任一位
 * 不同即有变化;origin 中不存在的组视为有变化。成员清单顺序有语义(既有
 * 序在前、新增追加),逐位比较不会漏掉纯换序。 */
export function changedOnly(origin: GroupMembership[], next: GroupMembership[]): GroupMembership[] {
  const before = new Map(origin.map((g) => [g.id, g.memberIds]))
  return next.filter((g) => {
    const prev = before.get(g.id)
    if (prev === undefined) {
      return true
    }
    return prev.length !== g.memberIds.length || prev.some((id, i) => g.memberIds[i] !== id)
  })
}

/** 框选成组的成员计划(票 B1):所选节点进新组(memberIds,组节点 id
 * 一律排除 —— 组不进框选结果也不被误并入);各旧组摘除所选成员(跨组
 * 换组的旧组侧),只回传有变化的组。 */
export function planCreateGroupSelection(
  selectedIds: string[],
  groups: GroupMembership[],
): { memberIds: string[]; updatedGroups: GroupMembership[] } {
  const groupIds = new Set(groups.map((g) => g.id))
  const memberIds = mergeGroupMembers([], selectedIds.filter((id) => !groupIds.has(id)))
  const updatedGroups = changedOnly(
    groups,
    groups.map((g) => ({ id: g.id, memberIds: removeGroupMembers(g.memberIds, memberIds) })),
  )
  return { memberIds, updatedGroups }
}

/** 节点删除后的组侧悬空清理(票 A3):各组摘除被删 id;被删清单里的组
 * 节点自身跳过(组删只删组节点,成员原地保留);只回传 memberIds 有变化
 * 的组。 */
export function pruneGroupsAfterRemoval(
  groups: GroupMembership[],
  removedIds: Iterable<string>,
): GroupMembership[] {
  const removed = new Set(removedIds)
  return changedOnly(
    groups,
    groups
      .filter((g) => !removed.has(g.id))
      .map((g) => ({ id: g.id, memberIds: g.memberIds.filter((id) => !removed.has(id)) })),
  )
}

/** 用户设定尺寸夹紧:不低于空组最小占位(resize 手柄与编程创建同规)。 */
export function clampUserGroupSize(w: number, h: number): { width: number; height: number } {
  return { width: Math.max(GROUP_MIN_WIDTH, w), height: Math.max(GROUP_MIN_HEIGHT, h) }
}

/** 成员包围盒并集(空列表 null;成员盒子原样读,零改动)。 */
export function unionMemberBounds(members: NodeBox[]): {
  minX: number
  minY: number
  maxX: number
  maxY: number
} | null {
  if (members.length === 0) {
    return null
  }
  let minX = Number.POSITIVE_INFINITY
  let minY = Number.POSITIVE_INFINITY
  let maxX = Number.NEGATIVE_INFINITY
  let maxY = Number.NEGATIVE_INFINITY
  for (const m of members) {
    minX = Math.min(minX, m.x)
    minY = Math.min(minY, m.y)
    maxX = Math.max(maxX, m.x + m.width)
    maxY = Math.max(maxY, m.y + m.height)
  }
  return { minX, minY, maxX, maxY }
}

/** 框实际尺寸(票 D1):组原点锚定的 max(用户设定尺寸, 成员包围盒 +
 * 内边距 24px)—— 宽 = max(用户w, 最右成员右缘 + 24 − 组x),高同构。
 * 成员清空回落用户尺寸(空组保留);用户尺寸未设定落最小占位;成员撑破
 * 自动扩张、移出/删除后按剩余成员回落 —— 同一函数三态(入参即现状)。
 * 口径:框锚在组原点向右下生长,成员越出原点左/上方时不回推原点 ——
 * 左/上缘轻微越界由「框是视觉不是归属判据」兜底(票 A4),归属判定只认
 * 节点包围盒中心,不读框。 */
export function groupFrameSize(
  user: GroupUserSize,
  members: NodeBox[],
  origin: Point,
): { width: number; height: number } {
  const userSize = clampUserGroupSize(user.w ?? GROUP_MIN_WIDTH, user.h ?? GROUP_MIN_HEIGHT)
  const bounds = unionMemberBounds(members)
  if (!bounds) {
    return userSize
  }
  return {
    width: Math.max(userSize.width, bounds.maxX + GROUP_PADDING - origin.x),
    height: Math.max(userSize.height, bounds.maxY + GROUP_PADDING - origin.y),
  }
}

/** 落点入组判定(票 C4):落点中心在组框内即入组;组节点永不被吸纳
 * (组不吸纳组);空组照收;组与组重叠取列表靠后的框(后画的框在上的
 * 视觉序,落进看得见的那个)。 */
export function groupForLandedNode(
  node: { type?: string | null; center: Point },
  groups: GroupFrame[],
): string | null {
  if (node.type === 'group') {
    return null
  }
  let hit: string | null = null
  for (const g of groups) {
    if (isCenterInBox(node.center, g)) {
      hit = g.id
    }
  }
  return hit
}

/** 拖动停止的归属结算计划(票 C1-C3):逐个被拖的非组节点按包围盒中心
 * 判定归属 —— 入框加入(从其他组摘除、并入目标组)、出框脱离、跨组换组
 * 一次结算;多选拖动逐节点独立(可部分入组、部分换组、部分脱组);组节点
 * 跳过;无变化的组不进 groupUpdates(编辑器只在有变化时落 data)。 */
export interface DragMembershipPlan {
  assignments: { nodeId: string; groupId: string | null }[]
  groupUpdates: GroupMembership[]
}

export function resolveDragMembership(
  dragged: { id: string; type?: string | null; center: Point }[],
  frames: GroupFrame[],
  memberships: GroupMembership[],
): DragMembershipPlan {
  const lists = new Map<string, string[]>()
  for (const g of memberships) {
    lists.set(g.id, [...g.memberIds])
  }
  const assignments: DragMembershipPlan['assignments'] = []
  for (const node of dragged) {
    const target = groupForLandedNode(node, frames)
    if (node.type === 'group') {
      continue
    }
    assignments.push({ nodeId: node.id, groupId: target })
    if (target) {
      for (const [gid, list] of lists) {
        if (gid !== target) {
          lists.set(gid, removeGroupMembers(list, [node.id]))
        }
      }
      lists.set(target, mergeGroupMembers(lists.get(target) ?? [], [node.id]))
    } else {
      for (const [gid, list] of lists) {
        lists.set(gid, removeGroupMembers(list, [node.id]))
      }
    }
  }
  const groupUpdates = changedOnly(
    memberships,
    memberships.map((g) => ({ id: g.id, memberIds: lists.get(g.id) ?? g.memberIds })),
  )
  return { assignments, groupUpdates }
}

/** 选中集合的组过滤来源(票 E1)。vue-flow 1.48.2 实证:框选键 =
 * selectionKeyCode(缺省 Shift,pane 拖动,终局发 selectionEnd);多选键
 * = multiSelectionKeyCode(mac ⌘ / 其余 Ctrl,点选走 handleNodeClick,且
 * nodeClick 事件在内部更新选中之后发出,可按修饰位摘除抵消)。两条多选
 * 路径的框选结果/多选集合都恒不含组,纯单击单选保留组(组只走点选)。 */
export type SelectionSource = 'boxSelect' | 'modifierClick' | 'plainClick'

/** 给定选中集合与来源,返回要摘除的组节点 id(票 E1):框选与修饰键点选
 * 恒无组 —— 无论选中个数(框选只扫中单个组时选中数 = 1,也要摘);纯
 * 单击单选放行。isGroup 由调用方注入(本模块不依赖类型归一运行时)。 */
export function groupIdsToUnselect(
  selected: { id: string; isGroup: boolean }[],
  source: SelectionSource,
): string[] {
  if (source === 'plainClick') {
    return []
  }
  return selected.filter((n) => n.isGroup).map((n) => n.id)
}

/** 组 data 形状再导出:编辑器/组件从此处或 graph.ts 引入均可(同构)。 */
export type { GroupNodeData }
