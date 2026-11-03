import { describe, expect, it } from 'vitest'

import { cascadeAnchor, newNodeAnchor } from './placement'

describe('newNodeAnchor(33 号票视口中心落点)', () => {
  const rect = { left: 100, top: 50, width: 1200, height: 800 }

  it('锚点 = 容器中心回退半个节点身位(140/90)', () => {
    expect(newNodeAnchor(rect, 0)).toEqual({ x: 100 + 600 - 140, y: 50 + 400 - 90 })
  })

  it('级联偏移按节点数步进 48,8 个一循环', () => {
    expect(newNodeAnchor(rect, 1)).toEqual({ x: 560 + 48, y: 360 + 48 })
    expect(newNodeAnchor(rect, 7)).toEqual({ x: 560 + 336, y: 360 + 336 })
    expect(newNodeAnchor(rect, 8)).toEqual({ x: 560, y: 360 })
  })

  it('只依赖容器 client 矩形,画布平移缩放不参与(换算是调用方的事)', () => {
    const moved = { left: -3000, top: 800, width: 2560, height: 1440 }
    expect(newNodeAnchor(moved, 3)).toEqual({ x: -3000 + 1280 - 140 + 144, y: 800 + 720 - 90 + 144 })
  })
})

describe('cascadeAnchor(76 号票右键落点:级联从给定锚点起算)', () => {
  it('第 0 个节点落在锚点原位,不偏移', () => {
    expect(cascadeAnchor({ x: 300, y: 220 }, 0)).toEqual({ x: 300, y: 220 })
  })

  it('级联节奏与 newNodeAnchor 同款:每步 48、8 个一循环', () => {
    expect(cascadeAnchor({ x: 300, y: 220 }, 1)).toEqual({ x: 348, y: 268 })
    expect(cascadeAnchor({ x: 300, y: 220 }, 8)).toEqual({ x: 300, y: 220 })
    expect(cascadeAnchor({ x: 300, y: 220 }, 9)).toEqual({ x: 348, y: 268 })
  })

  it('与 newNodeAnchor 的级联段一致(同一节奏两个入口不漂移)', () => {
    const rect = { left: 100, top: 50, width: 1200, height: 800 }
    for (const count of [0, 1, 5, 8, 13]) {
      const centered = newNodeAnchor(rect, count)
      const base = newNodeAnchor(rect, 0)
      expect(cascadeAnchor(base, count)).toEqual({ x: centered.x, y: centered.y })
    }
  })
})
