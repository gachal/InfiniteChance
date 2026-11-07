// 画布右键上下文菜单(76 号票落、77 号票二级化)条目定义的测试:菜单 =
// 两级结构 —— 一级是分区入口(创作/规划/组织/面板),hover 展开二级飞出
// 条目;内容即数据(条目集/文案/命令/配色都住 canvasContextMenu.ts),
// 组件只渲染 —— 本测试按票钉住两层结构与条目集,防止增删漂移。
import { describe, expect, it } from 'vitest'

import { NODE_TYPE_LABEL } from './graph'
import { clampFlyoutPosition, clampMenuPosition, CONTEXT_MENU_SECTIONS } from './canvasContextMenu'

const flatItems = CONTEXT_MENU_SECTIONS.flatMap((s) => s.items)

describe('CONTEXT_MENU_SECTIONS 两级结构(77 号票:一级分区入口 + 飞出条目)', () => {
  it('一级恰好四个分区入口:创作 / 规划 / 组织 / 面板,id 与入口文案对应', () => {
    expect(CONTEXT_MENU_SECTIONS.map((s) => s.id)).toEqual(['create', 'plan', 'organize', 'panels'])
    expect(CONTEXT_MENU_SECTIONS.map((s) => s.label)).toEqual(['创作', '规划', '组织', '面板'])
  })

  it('飞出条目规模:创作 11 / 规划 4 / 组织 1 / 面板 4(共 20;76 号票 17 条原样随迁,78 号票创作段增翻唱,79 号票增同曲换词,81 号票增全景)', () => {
    expect(CONTEXT_MENU_SECTIONS.map((s) => s.items.length)).toEqual([11, 4, 1, 4])
    expect(flatItems.length).toBe(20)
  })

  it('每个分区入口的飞出条目非空(空飞出 = 死入口)', () => {
    for (const section of CONTEXT_MENU_SECTIONS) {
      expect(section.items.length, `分区「${section.label}」飞出为空`).toBeGreaterThan(0)
    }
  })

  it('创作飞出条目集与序:Agent/图片/视频/预演台/角色/音色/配乐/音效/翻唱/同曲换词/全景(音效在配乐后、翻唱随其后、同曲换词随后,全景第 11 条压轴,78/79/81 号票)', () => {
    const create = CONTEXT_MENU_SECTIONS[0]!
    expect(create.items.map((i) => i.id)).toEqual([
      'agent',
      'image',
      'video',
      'previz',
      'character',
      'voice',
      'audio',
      'sfx',
      'cover',
      'relyric',
      'panorama',
    ])
    expect(create.items.map((i) => i.label)).toEqual([
      'Agent',
      '图片',
      '视频',
      '预演台',
      '角色',
      '音色',
      '配乐',
      '音效',
      '翻唱',
      '同曲换词',
      '全景',
    ])
  })

  it('规划飞出条目集与序:分镜/分镜稿/关卡/剧本,文案沿用 NODE_TYPE_LABEL', () => {
    const plan = CONTEXT_MENU_SECTIONS[1]!
    expect(plan.items.map((i) => i.id)).toEqual(['storyboard', 'name', 'level', 'script'])
    for (const item of plan.items) {
      expect(item.label).toBe(
        NODE_TYPE_LABEL[item.command.kind === 'add-node' ? item.command.nodeType : 'group'],
      )
    }
  })

  it('组织飞出唯一条目 = 分组,命令是进画框模式(group-draw)', () => {
    const organize = CONTEXT_MENU_SECTIONS[2]!
    expect(organize.items.map((i) => i.label)).toEqual(['分组'])
    expect(organize.items[0]!.command).toEqual({ kind: 'group-draw' })
  })

  it('面板飞出四入口:素材库/生成记录/上传/帮助,toggle 语义保持、无开启态字段', () => {
    const panels = CONTEXT_MENU_SECTIONS[3]!
    expect(panels.items.map((i) => i.id)).toEqual(['asset', 'records', 'upload', 'help'])
    expect(panels.items.map((i) => i.command.kind)).toEqual([
      'toggle-asset',
      'toggle-records',
      'upload',
      'help',
    ])
  })

  it('音效命令 = audio 节点带 source:sfx;配乐同落 audio 节点不带 dataPatch(61 号票口径)', () => {
    const create = CONTEXT_MENU_SECTIONS[0]!
    const audio = create.items.find((i) => i.id === 'audio')!
    const sfx = create.items.find((i) => i.id === 'sfx')!
    expect(audio.command).toEqual({ kind: 'add-node', nodeType: 'audio' })
    expect(sfx.command).toEqual({ kind: 'add-node', nodeType: 'audio', dataPatch: { source: 'sfx' } })
  })

  it('翻唱命令 = audio 节点带 source:cover(78 号票:第三音频入口,创作意图标记)', () => {
    const create = CONTEXT_MENU_SECTIONS[0]!
    const cover = create.items.find((i) => i.id === 'cover')!
    expect(cover.command).toEqual({ kind: 'add-node', nodeType: 'audio', dataPatch: { source: 'cover' } })
    expect(cover.title).toContain('翻唱')
  })

  it('同曲换词命令 = audio 节点带 source:relyric(79 号票:第四音频入口,创作意图标记)', () => {
    const create = CONTEXT_MENU_SECTIONS[0]!
    const relyric = create.items.find((i) => i.id === 'relyric')!
    expect(relyric.command).toEqual({ kind: 'add-node', nodeType: 'audio', dataPatch: { source: 'relyric' } })
    expect(relyric.title).toContain('同曲换词')
  })

  it('全景命令 = panorama 节点不带 dataPatch(81 号票:创作段第 11 条,创作态表单在节点上)', () => {
    const create = CONTEXT_MENU_SECTIONS[0]!
    const panorama = create.items.find((i) => i.id === 'panorama')!
    expect(panorama.command).toEqual({ kind: 'add-node', nodeType: 'panorama' })
    expect(panorama.label).toBe('全景')
    expect(panorama.title).toContain('全景')
  })

  it('全部飞出条目不带「+」前缀、文案非空且各带 tone 配色与 title 提示', () => {
    for (const item of flatItems) {
      expect(item.label.startsWith('+'), `「${item.label}」带了 + 前缀`).toBe(false)
      expect(item.label.trim().length).toBeGreaterThan(0)
      expect(item.tone.length).toBeGreaterThan(0)
      expect(item.title?.trim().length ?? 0).toBeGreaterThan(0)
    }
  })

  it('飞出条目 id 全局唯一(组件的 :key 与 CSS tone 类都吃它)', () => {
    expect(new Set(flatItems.map((i) => i.id)).size).toBe(flatItems.length)
  })
})

describe('clampMenuPosition 贴边翻转钳制(76 号票定案 5,一级菜单照旧)', () => {
  const viewport = { width: 1440, height: 900 }
  const size = { width: 160, height: 420 }

  it('空间足够时原点原样返回(右键点即菜单左上角)', () => {
    expect(clampMenuPosition({ x: 200, y: 300 }, size, viewport)).toEqual({ x: 200, y: 300 })
  })

  it('贴底向上弹:y 翻转到视口下缘 - margin - 菜单高', () => {
    expect(clampMenuPosition({ x: 200, y: 800 }, size, viewport)).toEqual({
      x: 200,
      y: 900 - 8 - 420,
    })
  })

  it('贴右向左收:x 钳到视口右缘 - margin - 菜单宽', () => {
    expect(clampMenuPosition({ x: 1400, y: 300 }, size, viewport)).toEqual({
      x: 1440 - 8 - 160,
      y: 300,
    })
  })

  it('贴左上(负坐标)钳到 margin,不出视口', () => {
    expect(clampMenuPosition({ x: -40, y: -10 }, size, viewport)).toEqual({ x: 8, y: 8 })
  })

  it('菜单比视口还大时钳到 margin(不抛错、不出负、不溢出)', () => {
    const huge = { width: 2000, height: 1200 }
    expect(clampMenuPosition({ x: 700, y: 450 }, huge, viewport)).toEqual({ x: 8, y: 8 })
  })

  it('贴底同时贴右:两轴各自翻转钳制', () => {
    expect(clampMenuPosition({ x: 1430, y: 880 }, size, viewport)).toEqual({
      x: 1440 - 8 - 160,
      y: 900 - 8 - 420,
    })
  })
})

describe('clampFlyoutPosition 飞出贴边钳制(77 号票定案 4)', () => {
  const viewport = { width: 1440, height: 900 }
  const size = { width: 160, height: 340 }

  it('右侧空间足够:飞出贴一级菜单右缘、顶部与入口对齐,原样返回', () => {
    expect(clampFlyoutPosition({ x: 200, y: 300, flipX: 40 }, size, viewport)).toEqual({
      x: 200,
      y: 300,
    })
  })

  it('贴视口右缘:飞出翻到一级菜单左侧,右缘贴 flipX(菜单左缘)', () => {
    // 期望落点 x=1300,飞出宽 160 → 1300+160 越过视口右缘 1440-8,翻左。
    expect(clampFlyoutPosition({ x: 1300, y: 300, flipX: 1140 }, size, viewport)).toEqual({
      x: 1140 - 160,
      y: 300,
    })
  })

  it('翻左后仍放不下(超窄视口退化):钳到 margin,不溢出不出负', () => {
    expect(
      clampFlyoutPosition({ x: 1300, y: 300, flipX: 100 }, size, { width: 200, height: 900 }),
    ).toEqual({ x: 8, y: 300 })
  })

  it('纵向贴底:顶部对齐入口会让飞出溢出下缘,钳到视口下缘 - margin - 飞出高', () => {
    expect(clampFlyoutPosition({ x: 200, y: 800, flipX: 40 }, size, viewport)).toEqual({
      x: 200,
      y: 900 - 8 - 340,
    })
  })

  it('纵向贴顶:入口顶高于 margin 时钳到 margin', () => {
    expect(clampFlyoutPosition({ x: 200, y: 3, flipX: 40 }, size, viewport)).toEqual({
      x: 200,
      y: 8,
    })
  })

  it('贴右缘同时贴底:横向翻左、纵向钳底,两轴独立', () => {
    expect(clampFlyoutPosition({ x: 1300, y: 800, flipX: 1140 }, size, viewport)).toEqual({
      x: 1140 - 160,
      y: 900 - 8 - 340,
    })
  })

  it('飞出比视口还高:纵向钳到 margin(不抛错、不出负、不溢出)', () => {
    expect(
      clampFlyoutPosition({ x: 200, y: 400, flipX: 40 }, { width: 160, height: 1200 }, viewport),
    ).toEqual({ x: 200, y: 8 })
  })
})

describe('飞出与悬停入口的邻接不变量(77 号票评审收口:纵钳后飞出仍覆盖入口行)', () => {
  // 评审疑虑:纵向钳位后飞出与悬停入口几何不相邻,指针从入口横移跨缝
  // 进飞出会先离开飞出范围被收。不变量:钳后飞出顶 ≤ 入口顶 且 飞出底
  // ≥ 入口底(飞出纵向跨度覆盖入口行)。成立依据:纵钳只上移(顶不会
  // 低于入口顶),而入口行住在一级菜单内、菜单被 clampMenuPosition 以
  // 同一 margin 钳制(入口顶 ≥ margin、入口底 ≤ 菜单底 ≤ 视口下缘 -
  // margin),贴底钳位时飞出下缘正好顶满视口下缘 - margin。
  const viewport = { width: 1440, height: 900 }
  const menuSize = { width: 160, height: 180 }
  const flyoutSize = { width: 160, height: 348 }
  const ENTRY_HEIGHT = 38

  /** 入口行顶:y 从菜单顶按行高铺到贴菜单底(末行与菜单底齐平)。 */
  function entryRows(menuTop: number): number[] {
    const tops: number[] = []
    for (let top = menuTop; top + ENTRY_HEIGHT <= menuTop + menuSize.height; top += ENTRY_HEIGHT) {
      tops.push(top)
    }
    const flush = menuTop + menuSize.height - ENTRY_HEIGHT
    if (tops[tops.length - 1] !== flush) {
      tops.push(flush)
    }
    return tops
  }

  /** 组件装配还原(CanvasContextMenu.vue openSection):一级菜单先钳制
   * 落位,飞出锚 = 菜单右缘 + 入口顶、flipX = 菜单左缘,再钳飞出。 */
  function clampedFlyoutForEntry(input: { x: number; y: number }, entryTop: number) {
    const menuPos = clampMenuPosition(input, menuSize, viewport)
    return clampFlyoutPosition(
      { x: menuPos.x + menuSize.width, y: entryTop, flipX: menuPos.x },
      flyoutSize,
      viewport,
    )
  }

  /** 不变量主体:该场景下每个入口行,钳后飞出纵向跨度都覆盖入口行。 */
  function expectFlyoutCoversEveryEntry(input: { x: number; y: number }): void {
    const menuPos = clampMenuPosition(input, menuSize, viewport)
    for (const entryTop of entryRows(menuPos.y)) {
      const pos = clampedFlyoutForEntry(input, entryTop)
      expect(pos.y, `入口行 y=${entryTop}:飞出顶低于入口顶`).toBeLessThanOrEqual(entryTop)
      expect(
        pos.y + flyoutSize.height,
        `入口行 y=${entryTop}:飞出底高于入口底`,
      ).toBeGreaterThanOrEqual(entryTop + ENTRY_HEIGHT)
    }
  }

  it('不钳位:入口在视口上部、飞出放得下,飞出顶与入口顶精确对齐,跨度天然覆盖入口行', () => {
    const input = { x: 200, y: 100 }
    const menuPos = clampMenuPosition(input, menuSize, viewport)
    expect(menuPos).toEqual({ x: 200, y: 100 })
    // 全部入口行都不触发纵向钳制(最大飞出底 242+348=590 < 892)。
    for (const entryTop of entryRows(menuPos.y)) {
      expect(clampedFlyoutForEntry(input, entryTop).y).toBe(entryTop)
    }
    expectFlyoutCoversEveryEntry(input)
  })

  it('纵向钳位(贴底):入口贴近视口底、飞出高,钳后飞出顶不高于入口顶、下缘顶满视口下缘可用空间', () => {
    const input = { x: 200, y: 800 }
    const menuPos = clampMenuPosition(input, menuSize, viewport)
    expect(menuPos.y).toBe(900 - 8 - 180) // 菜单自身贴底钳到 712
    for (const entryTop of entryRows(menuPos.y)) {
      const pos = clampedFlyoutForEntry(input, entryTop)
      // 每行入口顶都低于钳制上界(712 > 892-348):飞出整体钳到下缘。
      expect(pos.y).toBe(900 - 8 - 348)
      // 钳制只上移:飞出顶仍不高于入口顶,入口与飞出之间无缝。
      expect(pos.y).toBeLessThanOrEqual(entryTop)
    }
    // 贴底行的飞出下缘正好顶满「视口下缘 - margin」;一级菜单底被同一
    // margin 钳在同一处,故入口行必落在飞出纵向跨度内。
    const bottomRow = clampedFlyoutForEntry(input, menuPos.y + menuSize.height - ENTRY_HEIGHT)
    expect(bottomRow.y + flyoutSize.height).toBe(900 - 8)
    expectFlyoutCoversEveryEntry(input)
  })

  it('翻左形态:贴右缘翻到菜单左侧后右缘贴菜单左缘(横向相邻),纵向不变量照旧成立', () => {
    const input = { x: 1300, y: 800 }
    const menuPos = clampMenuPosition(input, menuSize, viewport)
    expect(menuPos).toEqual({ x: 1440 - 8 - 160, y: 900 - 8 - 180 })
    for (const entryTop of entryRows(menuPos.y)) {
      const pos = clampedFlyoutForEntry(input, entryTop)
      // 期望落点(菜单右缘 1272+160=1432)+飞出宽越过视口右缘:整体翻左。
      expect(pos.x).toBe(menuPos.x - flyoutSize.width)
      // 翻左后飞出右缘贴一级菜单左缘 —— 横向跨缝同样无缝。
      expect(pos.x + flyoutSize.width).toBe(menuPos.x)
      // 纵向与贴底场景同款钳制,邻接不变量照旧。
      expect(pos.y).toBe(900 - 8 - 348)
    }
    expectFlyoutCoversEveryEntry(input)
  })
})
