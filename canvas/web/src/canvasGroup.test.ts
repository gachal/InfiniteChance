import { describe, expect, it } from 'vitest'

import {
  changedOnly,
  clampUserGroupSize,
  defaultGroupData,
  groupColorOrDefault,
  groupForLandedNode,
  groupFrameSize,
  groupIdsToUnselect,
  GROUP_MIN_HEIGHT,
  GROUP_MIN_WIDTH,
  GROUP_PADDING,
  isCenterInBox,
  mergeGroupMembers,
  nodeCenter,
  planCreateGroupSelection,
  pruneGroupsAfterRemoval,
  removeGroupMembers,
  resolveDragMembership,
  unionMemberBounds,
  type GroupFrame,
  type GroupMembership,
  type NodeBox,
} from './canvasGroup'

/** 测试用组框:id + 矩形(x/y 左上角 + 宽高)。 */
function frame(id: string, x: number, y: number, width: number, height: number): GroupFrame {
  return { id, x, y, width, height }
}

/** 测试用成员盒子:x/y 左上角 + 宽高。 */
function box(x: number, y: number, width = 200, height = 100): NodeBox {
  return { x, y, width, height }
}

describe('defaultGroupData / 色板(72 号票 B3)', () => {
  it('标题缺省「分组」、颜色 = 色板第一色、成员列表为空', () => {
    const data = defaultGroupData()
    expect(data.title).toBe('分组')
    expect(data.color).toBe(groupColorOrDefault(undefined))
    expect(data.memberIds).toEqual([])
  })

  it('色板内 token 原样通过,色板外与缺省回落第一色', () => {
    expect(groupColorOrDefault('violet')).toBe('violet')
    expect(groupColorOrDefault('magenta-neon')).toBe(groupColorOrDefault(undefined))
    expect(groupColorOrDefault(undefined)).toBe('sky')
  })
})

describe('归属判定:节点包围盒中心(72 号票 C1)', () => {
  it('中心 = position + 尺寸一半', () => {
    expect(nodeCenter({ x: 100, y: 100 }, { width: 200, height: 100 })).toEqual({ x: 200, y: 150 })
  })

  it('尺寸未量出时按类型近似宽高回落,宽高可各自缺省', () => {
    const center = nodeCenter({ x: 0, y: 0 })
    expect(center.x).toBeGreaterThan(0)
    expect(center.y).toBeGreaterThan(0)
    // 只有宽度量出:高度走近似值。
    const half = nodeCenter({ x: 0, y: 0 }, { width: 400 } as { width?: number; height?: number })
    expect(half.x).toBe(200)
    expect(half.y).toBe(nodeCenter({ x: 0, y: 0 }).y)
  })

  it('中心入框/出框;边界(恰在框边/角)按闭区间算入框', () => {
    const f = frame('g1', 0, 0, 400, 300)
    expect(isCenterInBox({ x: 200, y: 150 }, f)).toBe(true)
    expect(isCenterInBox({ x: 500, y: 150 }, f)).toBe(false)
    expect(isCenterInBox({ x: -10, y: 150 }, f)).toBe(false)
    // 边界:恰在右边线与角点。
    expect(isCenterInBox({ x: 400, y: 150 }, f)).toBe(true)
    expect(isCenterInBox({ x: 400, y: 300 }, f)).toBe(true)
  })

  it('多节点批量判定互相独立:一进一出互不牵连', () => {
    const f = frame('g1', 0, 0, 400, 300)
    const centers = [
      { id: 'a', center: { x: 100, y: 100 } },
      { id: 'b', center: { x: 900, y: 900 } },
    ]
    const verdicts = centers.map((n) => isCenterInBox(n.center, f))
    expect(verdicts).toEqual([true, false])
  })
})

describe('成组合并(72 号票 B1/E5)', () => {
  const groups: GroupMembership[] = [
    { id: 'g1', memberIds: ['a'] },
    { id: 'g2', memberIds: ['b'] },
    { id: 'g3', memberIds: ['x', 'y'] },
  ]

  it('跨组换组:所选成员从旧组 memberIds 移除,进入新组', () => {
    const plan = planCreateGroupSelection(['a', 'b', 'c'], groups)
    expect(plan.memberIds).toEqual(['a', 'b', 'c'])
    // 旧组只被摘除所选成员,且只回传有变化的组。
    expect(plan.updatedGroups).toEqual([
      { id: 'g1', memberIds: [] },
      { id: 'g2', memberIds: [] },
    ])
    // 无关组不动。
    expect(groups[2].memberIds).toEqual(['x', 'y'])
  })

  it('选区含组节点被排除:组不进新组 memberIds,旧组也不误删组 id', () => {
    const withGroup: GroupMembership[] = [{ id: 'g1', memberIds: [] }]
    const plan = planCreateGroupSelection(['a', 'g1', 'b'], withGroup)
    expect(plan.memberIds).toEqual(['a', 'b'])
    expect(plan.updatedGroups).toEqual([])
  })

  it('对已同组成员幂等:重复 id 去重,顺序保持', () => {
    expect(mergeGroupMembers(['a', 'b'], ['b', 'c'])).toEqual(['a', 'b', 'c'])
    expect(mergeGroupMembers(['a'], ['a', 'a'])).toEqual(['a'])
    // 同一选区跑两遍,memberIds 不变。
    const once = planCreateGroupSelection(['a', 'b'], groups)
    const twice = planCreateGroupSelection(once.memberIds, groups)
    expect(twice.memberIds).toEqual(once.memberIds)
  })

  it('removeGroupMembers 摘除后保序,未命中原样', () => {
    expect(removeGroupMembers(['a', 'b', 'c'], ['b'])).toEqual(['a', 'c'])
    expect(removeGroupMembers(['a'], ['zzz'])).toEqual(['a'])
  })

  it('changedOnly:三个计划函数共用的 diff 收口,逐位比较(换序也算变化)', () => {
    const origin: GroupMembership[] = [
      { id: 'g1', memberIds: ['a', 'b'] },
      { id: 'g2', memberIds: [] },
    ]
    // 无变化整组不回传;换序(长度同、位不同)算变化。
    expect(changedOnly(origin, [
      { id: 'g1', memberIds: ['b', 'a'] },
      { id: 'g2', memberIds: [] },
    ])).toEqual([{ id: 'g1', memberIds: ['b', 'a'] }])
    // 新增与摘除都命中。
    expect(changedOnly(origin, [
      { id: 'g1', memberIds: ['a', 'b', 'c'] },
      { id: 'g2', memberIds: [] },
    ])).toEqual([{ id: 'g1', memberIds: ['a', 'b', 'c'] }])
    // origin 中不存在的组视为有变化。
    expect(changedOnly(origin, [{ id: 'g9', memberIds: ['x'] }])).toEqual([{ id: 'g9', memberIds: ['x'] }])
    // 全无变化:空数组。
    expect(changedOnly(origin, origin.map((g) => ({ ...g, memberIds: [...g.memberIds] })))).toEqual([])
  })
})

describe('选区的组过滤口径(72 号票 E1)', () => {
  const selected = [
    { id: 'n1', isGroup: false },
    { id: 'g1', isGroup: true },
    { id: 'n2', isGroup: false },
  ]

  it('框选结果恒无组:只扫中单个组也要摘', () => {
    expect(groupIdsToUnselect(selected, 'boxSelect')).toEqual(['g1'])
    expect(groupIdsToUnselect([{ id: 'g1', isGroup: true }], 'boxSelect')).toEqual(['g1'])
    expect(groupIdsToUnselect([{ id: 'n1', isGroup: false }], 'boxSelect')).toEqual([])
  })

  it('修饰键点选(Shift 框选键 / ⌘·Ctrl 多选键)不含组', () => {
    expect(groupIdsToUnselect(selected, 'modifierClick')).toEqual(['g1'])
    expect(groupIdsToUnselect([{ id: 'g1', isGroup: true }], 'modifierClick')).toEqual(['g1'])
  })

  it('纯单击点选单选保留组(组只走点选)', () => {
    expect(groupIdsToUnselect(selected, 'plainClick')).toEqual([])
    expect(groupIdsToUnselect([{ id: 'g1', isGroup: true }], 'plainClick')).toEqual([])
  })
})

describe('解组与悬空清理(72 号票 A3/E2)', () => {
  it('成员删除时各组摘除悬空 id,只回传有变化的组', () => {
    const groups: GroupMembership[] = [
      { id: 'g1', memberIds: ['a', 'b'] },
      { id: 'g2', memberIds: ['c'] },
    ]
    expect(pruneGroupsAfterRemoval(groups, ['a'])).toEqual([{ id: 'g1', memberIds: ['b'] }])
    expect(pruneGroupsAfterRemoval(groups, ['c'])).toEqual([{ id: 'g2', memberIds: [] }])
  })

  it('组节点自己被删(Backspace 删组):被删组跳过,其余组不受牵连', () => {
    const groups: GroupMembership[] = [
      { id: 'g1', memberIds: ['a', 'b'] },
      { id: 'g2', memberIds: ['c'] },
    ]
    expect(pruneGroupsAfterRemoval(groups, ['g2'])).toEqual([])
    expect(pruneGroupsAfterRemoval(groups, ['g2', 'c'])).toEqual([])
  })
})

describe('框尺寸:组原点锚定的 max 合并 / expand / 回落 / resize(72 号票 D1-D2)', () => {
  it('实际尺寸 = max(用户尺寸, 成员右/下缘 + 24px 内边距 − 组原点)', () => {
    // 原点对齐盒:成员右/下缘 400x300,+24(右/下单侧内边距)→ 424x324
    // > 用户 300x200;左/上缘无内边距(框锚在组原点,见下条用例)。
    expect(groupFrameSize({ w: 300, h: 200 }, [box(0, 0, 400, 300)], { x: 0, y: 0 })).toEqual({
      width: 400 + GROUP_PADDING,
      height: 300 + GROUP_PADDING,
    })
    // 用户尺寸更大:原样保留。
    expect(groupFrameSize({ w: 800, h: 600 }, [box(0, 0, 200, 100)], { x: 0, y: 0 })).toEqual({ width: 800, height: 600 })
  })

  it('偏移组(评审反例):成员在组原点右下深处,框保证包住', () => {
    // 组(0,0) 用户 260x160,成员 280x180 @(100,50):右缘 380 + 24 = 404,
    // 下缘 230 + 24 = 254 —— 框必须 ≥ 404x254 才包得住,旧实现(并集宽高
    // + 双侧 padding = 328x228)会漏出成员右下角。
    expect(groupFrameSize({ w: 260, h: 160 }, [box(100, 50, 280, 180)], { x: 0, y: 0 })).toEqual({
      width: 404,
      height: 254,
    })
  })

  it('偏移组:原点非零,尺寸按「右缘 − 组x」锚定不按并集宽', () => {
    // 组原点 (500,400),成员 @ (560,470) 200x100:宽 = 760+24−500 = 284
    // > 用户 260,高 = 570+24−400 = 194 > 用户 160。
    expect(groupFrameSize({ w: 260, h: 160 }, [box(560, 470, 200, 100)], { x: 500, y: 400 })).toEqual({
      width: 284,
      height: 194,
    })
  })

  it('左/上缘越界不回推原点:框锚在组原点向右下生长(视觉口径兜底)', () => {
    // 成员整体在组原点左上方:右/下缘 +24 − 原点为负,用户尺寸(≥最小
    // 占位)兜住;左/上缘轻微越界由「框是视觉不是归属判据」兜底,不回推。
    expect(groupFrameSize({ w: 260, h: 160 }, [box(50, 20, 100, 60)], { x: 200, y: 100 })).toEqual({
      width: 260,
      height: 160,
    })
  })

  it('成员入组撑破自动扩张(expand),多成员取并集', () => {
    const size = groupFrameSize({ w: 260, h: 160 }, [box(0, 0), box(600, 400)], { x: 0, y: 0 })
    expect(size.width).toBe(600 + 200 + GROUP_PADDING)
    expect(size.height).toBe(400 + 100 + GROUP_PADDING)
  })

  it('回落同样偏移锚定:expand 后成员移出/删除,按剩余成员右/下缘回落', () => {
    const origin = { x: 100, y: 80 }
    // expand 态:远成员把框撑到 (500+24−100)x(350+24−80) = 424x294。
    const expanded = groupFrameSize({ w: 300, h: 200 }, [box(150, 120, 40, 30), box(400, 300, 100, 50)], origin)
    expect(expanded).toEqual({ width: 424, height: 294 })
    // 移出远成员后回落:剩余成员更小,右/下缘 +24 − 原点 < 用户尺寸 →
    // 回落用户 300x200(不是按剩余包围盒宽高另起)。
    const afterLeave = groupFrameSize({ w: 300, h: 200 }, [box(150, 120, 40, 30)], origin)
    expect(afterLeave).toEqual({ width: 300, height: 200 })
    // 剩余成员仍在右下撑破时,回落值也按原点锚定:右缘 560+24−100 = 484,
    // 下缘 470+24−80 = 414。
    const stillOverflow = groupFrameSize({ w: 300, h: 200 }, [box(400, 300, 160, 170)], origin)
    expect(stillOverflow).toEqual({ width: 484, height: 414 })
    // 成员清空:回落用户画的大小。
    expect(groupFrameSize({ w: 500, h: 400 }, [], origin)).toEqual({ width: 500, height: 400 })
    // 用户尺寸未设定(编程创建):落最小占位 260x160。
    expect(groupFrameSize({}, [], origin)).toEqual({ width: GROUP_MIN_WIDTH, height: GROUP_MIN_HEIGHT })
  })

  it('resize 只改用户尺寸:下限夹紧,不影响成员(成员盒子原样进出)', () => {
    expect(clampUserGroupSize(100, 100)).toEqual({ width: GROUP_MIN_WIDTH, height: GROUP_MIN_HEIGHT })
    expect(clampUserGroupSize(640, 420)).toEqual({ width: 640, height: 420 })
    // 用户尺寸即便被夹小(= 最小占位),成员盒子也只是被框罩住,位置与
    // 尺寸零改动。
    const members = [box(0, 0, 400, 300)]
    const size = groupFrameSize({ w: GROUP_MIN_WIDTH, h: GROUP_MIN_HEIGHT }, members, { x: 0, y: 0 })
    expect(members[0]).toEqual(box(0, 0, 400, 300))
    expect(size.width).toBe(400 + GROUP_PADDING)
  })

  it('unionMemberBounds 空列表为 null,多成员取并集', () => {
    expect(unionMemberBounds([])).toBeNull()
    expect(unionMemberBounds([box(10, 20, 30, 40), box(100, 0, 50, 60)])).toEqual({
      minX: 10,
      minY: 0,
      maxX: 150,
      maxY: 60,
    })
  })
})

describe('落点入组判定(72 号票 C4)', () => {
  const frames = [frame('g1', 0, 0, 400, 300), frame('g2', 500, 0, 200, 200)]

  it('落点中心在框内即入组,框外不入', () => {
    expect(groupForLandedNode({ type: 'image', center: { x: 100, y: 100 } }, frames)).toBe('g1')
    expect(groupForLandedNode({ type: 'image', center: { x: 550, y: 100 } }, frames)).toBe('g2')
    expect(groupForLandedNode({ type: 'image', center: { x: 450, y: 100 } }, frames)).toBeNull()
  })

  it('组节点永不被吸纳(type=group 恒 null)', () => {
    expect(groupForLandedNode({ type: 'group', center: { x: 100, y: 100 } }, frames)).toBeNull()
  })

  it('空组接受落点', () => {
    const empty = [frame('g0', 0, 0, 400, 300)]
    expect(groupForLandedNode({ type: 'agent', center: { x: 50, y: 50 } }, empty)).toBe('g0')
  })

  it('组与组重叠时取后画的(列表靠后的)框', () => {
    const overlap = [frame('g-old', 0, 0, 400, 300), frame('g-new', 0, 0, 400, 300)]
    expect(groupForLandedNode({ type: 'image', center: { x: 10, y: 10 } }, overlap)).toBe('g-new')
  })
})

describe('拖动归属结算(72 号票 C1-C5)', () => {
  const frames = [frame('g1', 0, 0, 400, 300), frame('g2', 500, 0, 300, 300)]
  const memberships: GroupMembership[] = [{ id: 'g1', memberIds: ['m1'] }, { id: 'g2', memberIds: [] }]

  it('拖入加入:原无组节点中心入框 → 加入', () => {
    const plan = resolveDragMembership([{ id: 'n1', type: 'image', center: { x: 100, y: 100 } }], frames, memberships)
    expect(plan.assignments).toEqual([{ nodeId: 'n1', groupId: 'g1' }])
    expect(plan.groupUpdates).toEqual([{ id: 'g1', memberIds: ['m1', 'n1'] }])
  })

  it('拖出脱离:成员中心出框 → 从组移除、不并入他组', () => {
    const plan = resolveDragMembership([{ id: 'm1', type: 'image', center: { x: 900, y: 900 } }], frames, memberships)
    expect(plan.assignments).toEqual([{ nodeId: 'm1', groupId: null }])
    expect(plan.groupUpdates).toEqual([{ id: 'g1', memberIds: [] }])
  })

  it('跨组换组:旧组移除、新组加入,一次结算', () => {
    const plan = resolveDragMembership([{ id: 'm1', type: 'image', center: { x: 600, y: 100 } }], frames, memberships)
    expect(plan.assignments).toEqual([{ nodeId: 'm1', groupId: 'g2' }])
    expect(plan.groupUpdates).toEqual([
      { id: 'g1', memberIds: [] },
      { id: 'g2', memberIds: ['m1'] },
    ])
  })

  it('多选拖动逐节点独立判定:可部分入组、部分脱组', () => {
    const dragged = [
      { id: 'm1', type: 'image', center: { x: 900, y: 900 } },
      { id: 'n2', type: 'video', center: { x: 600, y: 100 } },
    ]
    const plan = resolveDragMembership(dragged, frames, memberships)
    expect(plan.assignments).toEqual([
      { nodeId: 'm1', groupId: null },
      { nodeId: 'n2', groupId: 'g2' },
    ])
    expect(plan.groupUpdates).toEqual([
      { id: 'g1', memberIds: [] },
      { id: 'g2', memberIds: ['n2'] },
    ])
  })

  it('组节点跳过结算(组不吸纳组、拖组不换组)', () => {
    const plan = resolveDragMembership([{ id: 'g2', type: 'group', center: { x: 100, y: 100 } }], frames, memberships)
    expect(plan.assignments).toEqual([])
    expect(plan.groupUpdates).toEqual([])
  })

  it('原地拖动无归属变化:不产出更新(幂等)', () => {
    const plan = resolveDragMembership([{ id: 'm1', type: 'image', center: { x: 50, y: 50 } }], frames, memberships)
    expect(plan.groupUpdates).toEqual([])
  })
})
