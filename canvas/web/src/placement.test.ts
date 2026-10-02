import { describe, expect, it } from 'vitest'

import { newNodeAnchor } from './placement'

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
