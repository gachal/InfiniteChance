import { describe, expect, it } from 'vitest'

import {
  clampUserGroupSize,
  defaultGroupData,
  groupColorOrDefault,
  groupForLandedNode,
  groupFrameSize,
  GROUP_MIN_HEIGHT,
  GROUP_MIN_WIDTH,
  GROUP_PADDING,
  isCenterInBox,
  mergeGroupMembers,
  nodeCenter,
  planCreateGroupSelection,
  planUngroup,
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
})

describe('解组与悬空清理(72 号票 A3/E2)', () => {
  it('解组 = 删组节点 + 成员原地保留(成员 data 本就不带组侧字段)', () => {
    const plan = planUngroup({ id: 'g1', memberIds: ['a', 'b'] })
    expect(plan.removedGroupId).toBe('g1')
    // 成员清单只是被交还调用方(节点原样留在画布),没有任何成员侧改动。
    expect(plan.memberIds).toEqual(['a', 'b'])
  })

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

describe('框尺寸:max 合并 / expand / 回落 / resize(72 号票 D1-D4)', () => {
  it('实际尺寸 = max(用户尺寸, 成员包围盒 + 24px 内边距)', () => {
    // 包围盒 400x300,加双侧 padding 48 → 448x348 > 用户 300x200。
    expect(groupFrameSize({ w: 300, h: 200 }, [box(0, 0, 400, 300)])).toEqual({
      width: 400 + GROUP_PADDING * 2,
      height: 300 + GROUP_PADDING * 2,
    })
    // 用户尺寸更大:原样保留。
    expect(groupFrameSize({ w: 800, h: 600 }, [box(0, 0, 200, 100)])).toEqual({ width: 800, height: 600 })
  })

  it('成员入组撑破自动扩张(expand),多成员取并集', () => {
    const size = groupFrameSize({ w: 260, h: 160 }, [box(0, 0), box(600, 400)])
    expect(size.width).toBe(600 + 200 + GROUP_PADDING * 2)
    expect(size.height).toBe(400 + 100 + GROUP_PADDING * 2)
  })

  it('移出/删除成员后回落剩余包围盒;清空回落用户尺寸(空组保留)', () => {
    // 剩余成员更小:回落到用户尺寸(不低于最小占位)。
    const afterLeave = groupFrameSize({ w: 300, h: 200 }, [box(10, 10, 100, 50)])
    expect(afterLeave).toEqual({ width: 300, height: 200 })
    // 成员清空:回落用户画的大小。
    expect(groupFrameSize({ w: 500, h: 400 }, [])).toEqual({ width: 500, height: 400 })
    // 用户尺寸未设定(编程创建):落最小占位 260x160。
    expect(groupFrameSize({}, [])).toEqual({ width: GROUP_MIN_WIDTH, height: GROUP_MIN_HEIGHT })
  })

  it('resize 只改用户尺寸:下限夹紧,不影响成员(成员盒子原样进出)', () => {
    expect(clampUserGroupSize(100, 100)).toEqual({ width: GROUP_MIN_WIDTH, height: GROUP_MIN_HEIGHT })
    expect(clampUserGroupSize(640, 420)).toEqual({ width: 640, height: 420 })
    // 用户尺寸即便被夹小,成员盒子也只是被框罩住,位置与尺寸零改动。
    const members = [box(0, 0, 400, 300)]
    const size = groupFrameSize(clampUserGroupSize(100, 100), members)
    expect(members[0]).toEqual(box(0, 0, 400, 300))
    expect(size.width).toBe(400 + GROUP_PADDING * 2)
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
