import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

import { GROUP_RESIZE_CORNERS } from '../../canvasGroup'

/**
 * 标题栏 nodrag 布局的回归钉(72 号票评审 C5):vue-flow 缺省
 * noDragClassName: "nodrag",d3-drag 从 event.target 向上找 .nodrag 命中
 * 即滤掉拖动 —— 标题栏一旦挂 nodrag,从标题栏发起的整组拖动整个失效。
 * 这里钉两件事:标题栏不挂 nodrag(拖它 = 拖组)、真正交互的子元素保留
 * nodrag(改名输入/色板/解组钮/resize 手柄)。双击改名与拖动的共存不靠
 * class,靠 vue-flow 缺省 nodeDragThreshold: 1(原位双击位移不足阈值,
 * dist 实证:startDrag 仅在位移 > 阈值后触发),无 DOM 断言可钉。
 */
const source = readFileSync(fileURLToPath(new URL('./GroupNode.vue', import.meta.url)), 'utf8')

describe('GroupNode 标题栏可拖动(C5 回归钉)', () => {
  it('标题栏不挂 nodrag:它能发起整组拖动', () => {
    expect(source.includes('class="group-titlebar"')).toBe(true)
    expect(source.includes('group-titlebar nodrag')).toBe(false)
  })

  it('真正交互的子元素保留 nodrag:改名输入 / 色板 / 解组钮 / resize 手柄', () => {
    expect(source.includes('class="group-rename nodrag"')).toBe(true)
    expect(source.includes('class="group-swatch nodrag"')).toBe(true)
    expect(source.includes('class="group-ungroup nodrag"')).toBe(true)
    expect(source.includes('class="group-resize nodrag"')).toBe(true)
  })

  it('resize 手柄 v-for 走共享四角常量(与编辑器同源,防手抄漂移)', () => {
    expect(GROUP_RESIZE_CORNERS).toEqual(['nw', 'ne', 'sw', 'se'])
    expect(source.includes('GROUP_RESIZE_CORNERS')).toBe(true)
  })
})
