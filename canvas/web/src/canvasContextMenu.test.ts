// 画布右键上下文菜单(76 号票)条目定义的测试:菜单 = 四段分组的条目
// 数组,内容即数据(条目集/文案/命令/配色都住 canvasContextMenu.ts),
// 组件只渲染 —— 本测试按票钉住条目集与分组结构,防止增删漂移。
import { describe, expect, it } from 'vitest'

import { NODE_TYPE_LABEL } from './graph'
import { clampMenuPosition, CONTEXT_MENU_SECTIONS } from './canvasContextMenu'

const flatItems = CONTEXT_MENU_SECTIONS.flatMap((s) => s.items)

describe('CONTEXT_MENU_SECTIONS 四段分组(76 号票钉住)', () => {
  it('恰好四段:创作 / 规划 / 组织 / 面板,段标题与 id 对应', () => {
    expect(CONTEXT_MENU_SECTIONS.map((s) => s.id)).toEqual(['create', 'plan', 'organize', 'panels'])
    expect(CONTEXT_MENU_SECTIONS.map((s) => s.label)).toEqual(['创作', '规划', '组织', '面板'])
  })

  it('段规模:创作 8 钮 / 规划 4 钮 / 组织 1 钮 / 面板 4 入口(共 17,与原工具栏等量)', () => {
    expect(CONTEXT_MENU_SECTIONS.map((s) => s.items.length)).toEqual([8, 4, 1, 4])
    expect(flatItems.length).toBe(17)
  })

  it('创作组条目集与序:Agent/图片/视频/预演台/角色/音色/配乐/音效(音效在配乐后)', () => {
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
    ])
  })

  it('规划组条目集与序:分镜/分镜稿/关卡/剧本,文案沿用 NODE_TYPE_LABEL', () => {
    const plan = CONTEXT_MENU_SECTIONS[1]!
    expect(plan.items.map((i) => i.id)).toEqual(['storyboard', 'name', 'level', 'script'])
    for (const item of plan.items) {
      expect(item.label).toBe(
        NODE_TYPE_LABEL[item.command.kind === 'add-node' ? item.command.nodeType : 'group'],
      )
    }
  })

  it('组织组唯一条目 = 分组,命令是进画框模式(group-draw)', () => {
    const organize = CONTEXT_MENU_SECTIONS[2]!
    expect(organize.items.map((i) => i.label)).toEqual(['分组'])
    expect(organize.items[0]!.command).toEqual({ kind: 'group-draw' })
  })

  it('面板组四入口:素材库/生成记录/上传/帮助,toggle 语义保持、无开启态字段', () => {
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

  it('全部落节点条目不带「+」前缀、文案非空且各带 tone 配色与 title 提示', () => {
    for (const item of flatItems) {
      expect(item.label.startsWith('+'), `「${item.label}」带了 + 前缀`).toBe(false)
      expect(item.label.trim().length).toBeGreaterThan(0)
      expect(item.tone.length).toBeGreaterThan(0)
      expect(item.title?.trim().length ?? 0).toBeGreaterThan(0)
    }
  })

  it('条目 id 全局唯一(组件的 :key 与 CSS tone 类都吃它)', () => {
    expect(new Set(flatItems.map((i) => i.id)).size).toBe(flatItems.length)
  })
})

describe('clampMenuPosition 贴边翻转钳制(76 号票定案 5)', () => {
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
